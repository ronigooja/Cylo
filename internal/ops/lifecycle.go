package ops

// This file contains the deliberately boring, host-side release lifecycle. It
// does not create application credentials: those belong to key/provider/memory
// commands and are kept outside the release selector.
import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

type ReleaseManifest struct {
	SourceRevision string                  `json:"source_revision"`
	Architecture   string                  `json:"architecture"`
	Artifacts      []ReleaseArtifact       `json:"artifacts"`
	Images         map[string]ReleaseImage `json:"images"`
}
type ReleaseArtifact struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Digest string `json:"digest"`
	SHA256 string `json:"sha256"`
}
type ReleaseImage struct {
	Ref    string `json:"ref"`
	Digest string `json:"digest"`
}
type LifecycleOptions struct {
	Root, OptRoot, DataRoot, Release, OpenCodeImage  string
	NonInteractive, Yes, PurgeData, PurgeCredentials bool
	HealthURL                                        string
	HealthTimeout                                    time.Duration
	Out                                              io.Writer
}

var revisionRE = regexp.MustCompile(`^[0-9a-f]{40}$`)
var imageRefRE = regexp.MustCompile(`^ghcr\.io/[a-z0-9][a-z0-9._-]*/[a-z0-9][a-z0-9._-]*:[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
var digestRE = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func ValidateRelease(dir string) (ReleaseManifest, error) {
	if !filepath.IsAbs(dir) {
		return ReleaseManifest{}, errors.New("release directory must be absolute")
	}
	manifestPath := filepath.Join(filepath.Clean(dir), "release-manifest.json")
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		return ReleaseManifest{}, fmt.Errorf("read release manifest: %w", err)
	}
	var m ReleaseManifest
	if json.Unmarshal(b, &m) != nil {
		return m, errors.New("invalid release manifest JSON")
	}
	if !revisionRE.MatchString(m.SourceRevision) {
		return m, errors.New("source_revision must be a 40-character lowercase git revision")
	}
	if m.Architecture != "amd64" && m.Architecture != "arm64" {
		return m, errors.New("architecture must be amd64 or arm64")
	}
	if len(m.Artifacts) == 0 {
		return m, errors.New("manifest artifacts must be a non-empty list")
	}
	seen := map[string]bool{}
	for _, a := range m.Artifacts {
		if a.Name == "" || seen[a.Name] || filepath.IsAbs(a.Path) || a.Path == "" || strings.Contains(filepath.ToSlash(a.Path), "../") {
			return m, fmt.Errorf("invalid artifact %q", a.Name)
		}
		seen[a.Name] = true
		if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(a.SHA256) {
			return m, fmt.Errorf("artifact %s has invalid sha256", a.Name)
		}
		p := filepath.Join(dir, filepath.FromSlash(a.Path))
		rel, e := filepath.Rel(dir, p)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return m, fmt.Errorf("artifact %s escapes release", a.Name)
		}
		data, e := os.ReadFile(p)
		if e != nil {
			return m, fmt.Errorf("artifact %s missing: %w", a.Name, e)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != a.SHA256 {
			return m, fmt.Errorf("artifact %s sha256 mismatch", a.Name)
		}
		if a.Digest != "" && !digestRE.MatchString(a.Digest) {
			return m, fmt.Errorf("artifact %s has invalid digest", a.Name)
		}
	}
	for name, image := range m.Images {
		if !regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`).MatchString(name) || !imageRefRE.MatchString(image.Ref) || !digestRE.MatchString(image.Digest) {
			return m, fmt.Errorf("image %s is invalid", name)
		}
	}
	return m, nil
}

func (m Manager) Install(ctx context.Context, o LifecycleOptions) (ReleaseManifest, error) {
	if o.Root == "" {
		o.Root = "/etc/zoe"
	}
	if o.OptRoot == "" {
		o.OptRoot = "/opt/zoe"
	}
	if o.DataRoot == "" {
		o.DataRoot = "/var/lib/zoe"
	}
	manifest, err := ValidateRelease(o.Release)
	if err != nil {
		return manifest, err
	}
	if err = validateArch(manifest); err != nil {
		return manifest, err
	}
	if err = validateOpenCode(o.OpenCodeImage); err != nil {
		return manifest, err
	}
	if err = ensureDirs(o); err != nil {
		return manifest, err
	}
	if err = m.stageRelease(o, manifest); err != nil {
		return manifest, err
	}
	if err = m.writeDeploymentFiles(o, manifest); err != nil {
		return manifest, err
	}
	if err = m.pull(ctx, manifest); err != nil {
		return manifest, err
	}
	if err = m.installUnits(o); err != nil {
		return manifest, err
	}
	if _, err := os.Stat(filepath.Join(o.Root, "secrets", "mem0-llm-api-key")); os.IsNotExist(err) {
		printNextSteps(o.Out)
		return manifest, nil
	}
	return manifest, m.start(ctx)
}

func (m Manager) Update(ctx context.Context, o LifecycleOptions) error {
	if o.Root == "" {
		o.Root = "/etc/zoe"
	}
	if o.OptRoot == "" {
		o.OptRoot = "/opt/zoe"
	}
	if o.HealthTimeout == 0 {
		o.HealthTimeout = 90 * time.Second
	}
	manifest, err := ValidateRelease(o.Release)
	if err != nil {
		return err
	}
	if err = validateArch(manifest); err != nil {
		return err
	}
	if err = validateOpenCode(o.OpenCodeImage); err != nil {
		return err
	}
	current := filepath.Join(o.OptRoot, "releases", "current")
	previous, _ := os.Readlink(current)
	oldStack, _ := os.ReadFile(filepath.Join(o.Root, "zoe-stack.env"))
	oldRunner, _ := os.ReadFile(filepath.Join(o.Root, "zoe-runner.env"))
	if err = m.stageRelease(o, manifest); err != nil {
		return err
	}
	if err = m.pull(ctx, manifest); err != nil {
		return err
	}
	if err = m.writeDeploymentFiles(o, manifest); err != nil {
		return err
	}
	if err = m.switchCurrent(o, manifest.SourceRevision); err != nil {
		return err
	}
	if err = m.start(ctx); err != nil {
		return m.rollback(ctx, o, current, previous, oldStack, oldRunner, err)
	}
	if err = waitHealth(ctx, o.HealthURL, o.HealthTimeout); err != nil {
		return m.rollback(ctx, o, current, previous, oldStack, oldRunner, err)
	}
	return pruneReleases(filepath.Join(o.OptRoot, "releases"), previous, manifest.SourceRevision)
}

func (m Manager) rollback(ctx context.Context, o LifecycleOptions, selector, previous string, stack, runner []byte, cause error) error {
	if previous != "" {
		_ = os.Remove(selector)
		_ = os.Symlink(previous, selector)
	}
	_ = os.WriteFile(filepath.Join(o.Root, "zoe-stack.env"), stack, 0600)
	_ = os.WriteFile(filepath.Join(o.Root, "zoe-runner.env"), runner, 0600)
	_ = m.start(ctx)
	return fmt.Errorf("update rolled back: %w", cause)
}

func validateArch(m ReleaseManifest) error {
	want := runtime.GOARCH
	if want == "amd64" || want == "arm64" {
		if m.Architecture != want {
			return fmt.Errorf("release architecture %s does not match host %s", m.Architecture, want)
		}
	}
	return nil
}
func validateOpenCode(v string) error {
	if !imageRefRE.MatchString(strings.Split(v, "@")[0]) || !strings.Contains(v, "@") || !digestRE.MatchString(strings.SplitN(v, "@", 2)[1]) {
		return errors.New("--opencode-image must be a pinned GHCR image reference")
	}
	return nil
}
func ensureDirs(o LifecycleOptions) error {
	for _, p := range []string{o.Root, filepath.Join(o.Root, "secrets"), filepath.Join(o.Root, "runner-secrets"), filepath.Join(o.OptRoot, "releases"), o.DataRoot} {
		if !filepath.IsAbs(p) {
			return errors.New("lifecycle paths must be absolute")
		}
		if err := os.MkdirAll(p, 0750); err != nil {
			return err
		}
	}
	for _, name := range []string{"runner-token", "memory-runner-token"} {
		p := filepath.Join(o.Root, "secrets", name)
		if _, e := os.Stat(p); os.IsNotExist(e) {
			b := make([]byte, 32)
			if _, e = rand.Read(b); e != nil {
				return e
			}
			if e = os.WriteFile(p, []byte(base64.RawURLEncoding.EncodeToString(b)), 0400); e != nil {
				return e
			}
		}
	}
	return nil
}
func (m Manager) stageRelease(o LifecycleOptions, manifest ReleaseManifest) error {
	target := filepath.Join(o.OptRoot, "releases", manifest.SourceRevision)
	if _, e := os.Stat(target); os.IsNotExist(e) {
		if e = os.MkdirAll(filepath.Join(target, "bin"), 0755); e != nil {
			return e
		}
		for _, a := range manifest.Artifacts {
			src := filepath.Join(o.Release, filepath.FromSlash(a.Path))
			name := a.Name
			if name == "zoe-server" || name == "zoe-runner" || name == "zoe" || name == "zoe-secret-entrypoint" {
				data, e := os.ReadFile(src)
				if e != nil {
					return e
				}
				if e = os.WriteFile(filepath.Join(target, "bin", name), data, 0755); e != nil {
					return e
				}
			}
		}
		if b, e := os.ReadFile(filepath.Join(o.Release, "SOURCE_REVISION")); e == nil {
			_ = os.WriteFile(filepath.Join(target, "SOURCE_REVISION"), b, 0644)
		}
		if e = copyDeployFiles(o.Release, target); e != nil {
			return e
		}
	}
	return m.switchCurrent(o, manifest.SourceRevision)
}
func copyDeployFiles(src, dst string) error {
	for _, name := range []string{"docker-compose.yml", "zoe-compose.service", "zoe-runner.service", "zoe-runner-tmpfiles.conf"} {
		p := filepath.Join(src, "deploy-linux", name)
		b, e := os.ReadFile(p)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(dst, name), b, 0644); e != nil {
			return e
		}
	}
	return nil
}
func (m Manager) switchCurrent(o LifecycleOptions, rev string) error {
	releases := filepath.Join(o.OptRoot, "releases")
	tmp := filepath.Join(releases, ".current.tmp")
	_ = os.Remove(tmp)
	if err := os.Symlink(rev, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(releases, "current"))
}
func (m Manager) writeDeploymentFiles(o LifecycleOptions, mfest ReleaseManifest) error {
	if err := os.MkdirAll(o.Root, 0750); err != nil {
		return err
	}
	stack := map[string]string{"ZOE_SERVER_IMAGE": image(mfest, "zoe-server"), "ZOE_MEMORY_WORKER_IMAGE": image(mfest, "zoe-memory-worker"), "ZOE_SERVER_DATA_ROOT": filepath.Join(o.DataRoot, "server"), "ZOE_MEMORY_DATA_ROOT": filepath.Join(o.DataRoot, "memory"), "ZOE_RUNNER_TOKEN_FILE": filepath.Join(o.Root, "secrets", "runner-token"), "ZOE_MEMORY_RUNNER_TOKEN_FILE": filepath.Join(o.Root, "secrets", "memory-runner-token"), "ZOE_MEM0_LLM_API_KEY_FILE": filepath.Join(o.Root, "secrets", "mem0-llm-api-key"), "ZOE_RUNTIME_PROFILES": "{}", "ZOE_DEFAULT_PROFILE_ID": "worker"}
	runner := map[string]string{"ZOE_OPENCODE_IMAGE": o.OpenCodeImage, "ZOE_CONTROL_URL": "http://127.0.0.1:8787", "ZOE_RUNNER_JOURNAL_PATH": filepath.Join(o.DataRoot, "runner", "journal.sqlite"), "ZOE_RUNNER_ROUTES": "{}"}
	if err := writeEnv(filepath.Join(o.Root, "zoe-stack.env"), stack, 0600); err != nil {
		return err
	}
	return writeEnv(filepath.Join(o.Root, "zoe-runner.env"), runner, 0600)
}
func image(m ReleaseManifest, name string) string { i := m.Images[name]; return i.Ref + "@" + i.Digest }
func writeEnv(path string, values map[string]string, mode os.FileMode) error {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, values[k])
	}
	return os.WriteFile(path, []byte(b.String()), mode)
}
func (m Manager) pull(ctx context.Context, manifest ReleaseManifest) error {
	for _, name := range []string{"zoe-server", "zoe-memory-worker", "zoe-runner"} {
		i := manifest.Images[name]
		if i.Digest == "" {
			continue
		}
		if _, e := m.Run(ctx, "docker", "pull", i.Ref+"@"+i.Digest); e != nil {
			return e
		}
	}
	return nil
}
func (m Manager) installUnits(o LifecycleOptions) error {
	dir := filepath.Join(o.Root, "systemd")
	if o.Root == "/etc/zoe" {
		dir = "/etc/systemd/system"
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	for _, n := range []string{"zoe-compose.service", "zoe-runner.service"} {
		b, e := os.ReadFile(filepath.Join(o.OptRoot, "releases", "current", n))
		if e != nil {
			continue
		}
		b = bytesReplacePaths(b, o)
		if e = os.WriteFile(filepath.Join(dir, n), b, 0644); e != nil {
			return e
		}
	}
	_, e := m.Run(context.Background(), "systemctl", "daemon-reload")
	return e
}
func bytesReplacePaths(b []byte, o LifecycleOptions) []byte {
	return []byte(strings.ReplaceAll(strings.ReplaceAll(string(b), "/etc/zoe", o.Root), "/opt/zoe", o.OptRoot))
}
func (m Manager) start(ctx context.Context) error {
	_, e := m.Run(ctx, "systemctl", "restart", "zoe-compose.service")
	if e != nil {
		return e
	}
	_, e = m.Run(ctx, "systemctl", "restart", "zoe-runner.service")
	return e
}
func waitHealth(ctx context.Context, url string, timeout time.Duration) error {
	if url == "" {
		url = "http://127.0.0.1:8787/healthz"
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		r, e := http.DefaultClient.Do(req)
		if e == nil {
			r.Body.Close()
			if r.StatusCode >= 200 && r.StatusCode < 300 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return errors.New("health check timed out")
}
func pruneReleases(dir, previous, current string) error {
	entries, e := os.ReadDir(dir)
	if e != nil {
		return e
	}
	for _, x := range entries {
		if x.IsDir() && x.Name() != "current" && x.Name() != filepath.Base(previous) && x.Name() != current {
			_ = os.RemoveAll(filepath.Join(dir, x.Name()))
		}
	}
	return nil
}
func printNextSteps(w io.Writer) {
	if w == nil {
		return
	}
	fmt.Fprintln(w, "Host installed but not started. Next steps:")
	fmt.Fprintln(w, "  zoe memory configure")
	fmt.Fprintln(w, "  zoe provider configure")
	fmt.Fprintln(w, "  zoe key init")
	fmt.Fprintln(w, "  systemctl start zoe-compose zoe-runner")
}

func (m Manager) Uninstall(ctx context.Context, o LifecycleOptions) error {
	if o.Root == "" {
		o.Root = "/etc/zoe"
	}
	if o.OptRoot == "" {
		o.OptRoot = "/opt/zoe"
	}
	if _, e := m.Run(ctx, "systemctl", "stop", "zoe-runner.service", "zoe-compose.service"); e != nil {
		return e
	}
	_, _ = m.Run(ctx, "systemctl", "disable", "zoe-runner.service", "zoe-compose.service")
	_, _ = m.Run(ctx, "docker", "rm", "-f", "zoe-server", "zoe-memory-worker", "qdrant")
	_, _ = m.Run(ctx, "docker", "image", "rm", "ghcr.io/ronigooja/zoe-server", "ghcr.io/ronigooja/zoe-memory-worker", "ghcr.io/ronigooja/zoe-runner")
	_, _ = m.Run(ctx, "docker", "network", "rm", "zoe")
	if e := os.RemoveAll(o.OptRoot); e != nil {
		return e
	}
	for _, p := range []string{
		filepath.Join(o.Root, "systemd", "zoe-compose.service"),
		filepath.Join(o.Root, "systemd", "zoe-runner.service"),
		filepath.Join(o.Root, "systemd", "zoe-runner-tmpfiles.conf"),
		"/etc/systemd/system/zoe-compose.service",
		"/etc/systemd/system/zoe-runner.service",
		"/etc/tmpfiles.d/zoe-runner.conf",
	} {
		_ = os.Remove(p)
	}
	_, _ = m.Run(ctx, "systemctl", "daemon-reload")
	if o.PurgeData {
		if e := os.RemoveAll(o.DataRoot); e != nil {
			return e
		}
		for _, n := range []string{"zoe-stack.env", "zoe-runner.env"} {
			_ = os.Remove(filepath.Join(o.Root, n))
		}
		if o.PurgeCredentials {
			for _, n := range []string{"cliproxyapi-api-key", "voice-gateway-api-key", "mem0-llm-api-key"} {
				_ = os.Remove(filepath.Join(o.Root, "secrets", n))
				_ = os.Remove(filepath.Join(o.Root, "runner-secrets", n))
			}
		}
	}
	return nil
}
