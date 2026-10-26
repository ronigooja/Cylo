package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Deployment records resolved host paths; discovery never changes services.
type Deployment struct {
	Mode                                                                                                              Mode
	Root, AOEnv, RunnerEnv, StackEnv, DatabasePath, ComposeFile, ComposeProject, AOService, RunnerService, DropInPath string
}

func DiscoverDeployment(ctx context.Context, m Manager, root string) (Deployment, error) {
	if root == "" {
		root = "/etc/cylo"
	}
	if !filepath.IsAbs(root) {
		return Deployment{}, errors.New("deployment root must be absolute")
	}
	d := Deployment{Root: root, Mode: ModeUnknown, AOEnv: filepath.Join(root, "ao.env"), RunnerEnv: filepath.Join(root, "cylo-runner.env"), StackEnv: filepath.Join(root, "cylo-stack.env"), RunnerService: "cylo-runner.service", ComposeProject: "cylo"}
	if v := os.Getenv("CYLO_DEPLOYMENT_MODE"); v != "" {
		d.Mode = Mode(v)
	}
	if m.Run == nil {
		m = NewManager(nil)
	}
	// Only fixed metadata properties are requested; Environment is deliberately excluded.
	out, err := m.Run(ctx, "systemctl", "show", "cylo-ao.service", "--property=LoadState,EnvironmentFiles,FragmentPath", "--no-pager")
	if err == nil && strings.Contains(string(out), "LoadState=loaded") {
		d.AOService = "cylo-ao.service"
		if d.Mode == ModeUnknown {
			d.Mode = ModeSystemd
		}
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "EnvironmentFiles=") {
				v := strings.TrimPrefix(line, "EnvironmentFiles=")
				if i := strings.Index(v, " ("); i >= 0 {
					v = v[:i]
				}
				if filepath.IsAbs(v) && !strings.ContainsAny(v, "\r\n\x00") {
					d.AOEnv = v
				}
			}
		}
	}
	if d.Mode == ModeUnknown {
		if _, err := os.Stat(d.RunnerEnv); err == nil {
			d.Mode = ModeSystemd
		}
	}
	stack, _ := readEnv(d.StackEnv)
	if p := stack["CYLO_COMPOSE_FILE"]; filepath.IsAbs(p) {
		d.ComposeFile = p
	}
	if p := os.Getenv("CYLO_COMPOSE_FILE"); filepath.IsAbs(p) {
		d.ComposeFile = p
	}
	if d.ComposeFile != "" && d.Mode == ModeUnknown {
		d.Mode = ModeCompose
	}
	if p := stack["COMPOSE_PROJECT_NAME"]; p != "" {
		d.ComposeProject = p
	}
	if p := stack["CYLO_DATABASE_PATH"]; filepath.IsAbs(p) {
		d.DatabasePath = p
	} else if p := stack["CYLO_SERVER_DATA_ROOT"]; filepath.IsAbs(p) {
		d.DatabasePath = filepath.Join(p, "data", "cylo.sqlite")
	}
	if d.DatabasePath == "" {
		d.DatabasePath = "/var/lib/cylo/server/data/cylo.sqlite"
	}
	d.DropInPath = "/etc/systemd/system/cylo-ao.service.d/90-cylo-provider.conf"
	if root != "/etc/cylo" {
		d.DropInPath = filepath.Join(root, "systemd", "cylo-ao.service.d", "90-cylo-provider.conf")
	}
	// Reload the non-secret managed deployment descriptor, if present.
	if b, e := os.ReadFile(filepath.Join(root, "provider-deployment.json")); e == nil && len(b) < 16384 {
		var persisted Deployment
		if json.Unmarshal(b, &persisted) == nil && persisted.Root == root {
			d = persisted
		}
	}
	return d, nil
}

// readEnv parses literal values without shell evaluation and returns no raw output.
func readEnv(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) > 1<<20 {
		return nil, errors.New("environment file too large")
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, "\"") {
			decoded, e := strconv.Unquote(value)
			if e != nil {
				continue
			}
			value = decoded
		} else if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
			value = strings.Trim(value, "'")
		}
		values[strings.TrimSpace(key)] = value
	}
	return values, nil
}
func (d Deployment) ProviderConfig() (ProviderConfig, error) {
	c := ProviderConfig{SecretPath: DefaultSecretPath(d.Root)}
	v, err := readEnv(d.AOEnv)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	c.BaseURL = v["AO_CODEX_EXTERNAL_BASE_URL"]
	c.Model = v["AO_CODEX_EXTERNAL_MODEL"]
	if v["AO_CODEX_EXTERNAL_API_KEY_FILE"] != "" {
		c.SecretPath = v["AO_CODEX_EXTERNAL_API_KEY_FILE"]
	}
	return c, nil
}
func (d Deployment) InspectConfig() InspectConfig {
	c, _ := d.ProviderConfig()
	services := []string{}
	if d.Mode == ModeCompose {
		services = []string{"cylo-server", "cylo-memory-worker", "qdrant"}
		if d.AOService != "" {
			services = append(services, d.AOService)
		}
	} else {
		services = []string{"cylo-compose.service", d.RunnerService}
		if d.AOService != "" {
			services = append(services, d.AOService)
		}
	}
	return InspectConfig{Mode: d.Mode, Services: services, ComposeProject: d.ComposeProject, ComposeFile: d.ComposeFile, ComposeEnv: d.StackEnv, ComposeOverride: d.composeOverride(), SecretPath: c.SecretPath, ProviderBaseURL: c.BaseURL, ProviderModel: c.Model, ProviderConfigSource: d.AOEnv, ProviderLastCheck: LastProviderCheck(d)}
}

func lockDeployment(ctx context.Context, d Deployment) (func(), error) {
	if err := os.MkdirAll(d.Root, 0750); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(d.Root, ".operations.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

type fileSnapshot struct {
	path   string
	data   []byte
	info   os.FileInfo
	exists bool
}

func snapshot(path string) (fileSnapshot, error) {
	s := fileSnapshot{path: path}
	var err error
	s.info, err = os.Lstat(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if !s.info.Mode().IsRegular() {
		return s, errors.New("configuration must be a regular file")
	}
	s.data, err = os.ReadFile(path)
	s.exists = true
	return s, err
}
func (s fileSnapshot) restore() error {
	if !s.exists {
		err := os.Remove(s.path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		return syncDir(filepath.Dir(s.path))
	}
	if err := writeFileAtomic(s.path, s.data, s.info.Mode().Perm()); err != nil {
		return err
	}
	if stat, ok := s.info.Sys().(*syscall.Stat_t); ok {
		return os.Chown(s.path, int(stat.Uid), int(stat.Gid))
	}
	return nil
}

// ConfigureProvider validates a staged credential before modifying persistent state.
// On any post-write failure it restores all files and restarts the prior configuration.
func (m Manager) ConfigureProvider(ctx context.Context, d Deployment, c ProviderConfig, client *http.Client) (ProbeResult, error) {
	if m.Run == nil {
		m = NewManager(nil)
	}
	c, err := c.Normalized()
	if err != nil {
		return ProbeResult{}, err
	}
	if !validToken(c.Secret) {
		return ProbeResult{}, errors.New("provider token is empty or contains whitespace/control characters")
	}
	unlock, err := lockDeployment(ctx, d)
	if err != nil {
		return ProbeResult{}, err
	}
	defer unlock()
	if !validService(d.AOService) {
		return ProbeResult{}, errors.New("AO service is not discovered; select its deployment service before applying")
	}
	if d.Mode != ModeSystemd && d.Mode != ModeCompose {
		return ProbeResult{}, errors.New("unknown deployment mode")
	}
	result := Probe(ctx, c, client)
	if !result.OK() {
		_ = saveProviderCheck(d, result)
		return result, fmt.Errorf("provider validation failed: %s", result.ErrorCategory)
	}
	if d.Mode == ModeSystemd && strings.HasSuffix(d.DropInPath, "/cylo-ao.service.d/90-cylo-provider.conf") && d.AOService != "cylo-ao.service" {
		d.DropInPath = filepath.Join(filepath.Dir(filepath.Dir(d.DropInPath)), d.AOService+".d", "90-cylo-provider.conf")
	}
	composeUID, composeGID := -1, -1
	if d.Mode == ModeCompose {
		composeUID, composeGID, err = m.composeCredentialOwner(ctx, d)
		if err != nil {
			return result, err
		}
	}
	paths := []string{d.AOEnv, c.SecretPath, filepath.Join(d.Root, "provider-deployment.json")}
	if d.Mode == ModeSystemd {
		if d.DropInPath == "" {
			return result, errors.New("AO systemd drop-in path is required")
		}
		paths = append(paths, d.DropInPath)
	} else {
		if !filepath.IsAbs(d.ComposeFile) || !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`).MatchString(d.AOService) {
			return result, errors.New("Compose AO service and absolute base file are required")
		}
		paths = append(paths, d.composeOverride())
	}
	seen := map[string]bool{}
	saved := []fileSnapshot{}
	for _, path := range paths {
		if seen[path] {
			return result, errors.New("provider configuration paths must be distinct")
		}
		seen[path] = true
		s, e := snapshot(path)
		if e != nil {
			return result, e
		}
		saved = append(saved, s)
	}
	parent := filepath.Dir(c.SecretPath)
	parentInfo, parentErr := os.Stat(parent)
	rollback := func(cause error) error {
		errs := []error{cause}
		for i := len(saved) - 1; i >= 0; i-- {
			if e := saved[i].restore(); e != nil {
				errs = append(errs, fmt.Errorf("restore configuration: %w", e))
			}
		}
		if d.Mode == ModeSystemd {
			if parentErr == nil {
				if st, ok := parentInfo.Sys().(*syscall.Stat_t); ok {
					if e := os.Chown(parent, int(st.Uid), int(st.Gid)); e != nil {
						errs = append(errs, e)
					}
				}
				if e := os.Chmod(parent, parentInfo.Mode().Perm()); e != nil {
					errs = append(errs, e)
				}
			} else if os.IsNotExist(parentErr) {
				if e := os.Remove(parent); e != nil && !os.IsNotExist(e) {
					errs = append(errs, e)
				}
			}
		}
		fresh, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if e := m.RefreshProvider(fresh, d); e != nil {
			errs = append(errs, fmt.Errorf("reload restored configuration: %w", e))
		}
		return errors.Join(errs...)
	}
	if d.Mode == ModeSystemd {
		canonicalSecretDir := filepath.Clean(c.SecretPath) == filepath.Join("/etc/cylo", "runner-secrets", filepath.Base(c.SecretPath)) && filepath.Dir(filepath.Clean(c.SecretPath)) == "/etc/cylo/runner-secrets"
		if d.Root == "/etc/cylo" && !canonicalSecretDir {
			return result, errors.New("systemd credential must be inside /etc/cylo/runner-secrets")
		}
		uid, gid := -1, -1
		if canonicalSecretDir {
			u, e := user.Lookup("cylo-runner")
			if e != nil {
				return result, errors.New("cylo-runner account unavailable")
			}
			uid, e = strconv.Atoi(u.Uid)
			if e != nil {
				return result, e
			}
			gid, e = strconv.Atoi(u.Gid)
			if e != nil {
				return result, e
			}
			if err = os.MkdirAll(filepath.Dir(c.SecretPath), 0710); err != nil {
				return result, err
			}
			if err = os.Chown(filepath.Dir(c.SecretPath), 0, gid); err != nil {
				return result, rollback(err)
			}
			if err = os.Chmod(filepath.Dir(c.SecretPath), 0710); err != nil {
				return result, rollback(err)
			}
		}
		if err = writeFileAtomicOwned(c.SecretPath, []byte(c.Secret), 0600, uid, gid); err != nil {
			return result, rollback(err)
		}
	} else {
		if err = writeFileAtomicOwned(c.SecretPath, []byte(c.Secret), 0600, composeUID, composeGID); err != nil {
			return result, rollback(err)
		}
	}
	if err = WriteAOEnvAtomic(d.AOEnv, c); err != nil {
		return result, rollback(err)
	}
	if d.Mode == ModeSystemd {
		if strings.ContainsAny(d.AOEnv, "\r\n\x00%\"\\") {
			return result, rollback(errors.New("unsupported systemd environment path"))
		}
		if err = writeFileAtomic(d.DropInPath, []byte("[Service]\nEnvironmentFile=\""+d.AOEnv+"\"\n"), 0644); err != nil {
			return result, rollback(err)
		}
	}
	if d.Mode == ModeCompose {
		config := map[string]any{"services": map[string]any{d.AOService: map[string]any{"environment": map[string]string{"AO_CODEX_EXTERNAL_BASE_URL": c.BaseURL, "AO_CODEX_EXTERNAL_MODEL": c.Model, "AO_CODEX_EXTERNAL_API_KEY_FILE": c.SecretPath}, "volumes": []any{map[string]any{"type": "bind", "source": c.SecretPath, "target": c.SecretPath, "read_only": true, "bind": map[string]bool{"create_host_path": false}}}}}}
		data, _ := json.MarshalIndent(config, "", "  ")
		if err = writeFileAtomic(d.composeOverride(), data, 0600); err != nil {
			return result, rollback(err)
		}
	}
	data, _ := json.Marshal(d)
	if err = writeFileAtomic(filepath.Join(d.Root, "provider-deployment.json"), data, 0600); err != nil {
		return result, rollback(err)
	}
	fresh, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err = m.RefreshProvider(fresh, d); err != nil {
		return result, rollback(err)
	}
	if err = saveProviderCheck(d, result); err != nil {
		return result, rollback(fmt.Errorf("save provider check metadata: %w", err))
	}
	return result, nil
}
func (d Deployment) composeOverride() string {
	return filepath.Join(d.Root, "ao-provider.compose.json")
}
func (d Deployment) composeArgs() []string {
	args := composeArgs(d.ComposeProject, d.ComposeFile)
	if _, err := os.Stat(d.StackEnv); err == nil {
		args = append(args, "--env-file", d.StackEnv)
	}
	if _, err := os.Stat(d.composeOverride()); err == nil {
		args = append(args, "--file", d.composeOverride())
	}
	return args
}
func (m Manager) RefreshProvider(ctx context.Context, d Deployment) error {
	if m.Run == nil {
		m = NewManager(nil)
	}
	if !validService(d.AOService) {
		return errors.New("AO service is not discovered")
	}
	if d.Mode == ModeCompose {
		if _, err := m.Run(ctx, "docker", append(d.composeArgs(), "config", "--quiet")...); err != nil {
			return commandError("docker compose config", err)
		}
		if _, err := m.Run(ctx, "docker", append(d.composeArgs(), "up", "-d", "--no-build", "--no-deps", "--force-recreate", d.AOService)...); err != nil {
			return commandError("docker compose up", err)
		}
		return nil
	}
	return m.Reload(ctx, d.Mode, []string{d.AOService}, d.ComposeProject, d.ComposeFile)
}
func (m Manager) TestProvider(ctx context.Context, d Deployment, c ProviderConfig, client *http.Client) (ProbeResult, error) {
	unlock, err := lockDeployment(ctx, d)
	if err != nil {
		return ProbeResult{}, err
	}
	defer unlock()
	r := Probe(ctx, c, client)
	if err := saveProviderCheck(d, r); err != nil {
		return r, err
	}
	if !r.OK() {
		return r, fmt.Errorf("provider validation failed: %s", r.ErrorCategory)
	}
	return r, nil
}

type checkMetadata struct {
	CheckedAt string `json:"checked_at"`
	Outcome   string `json:"outcome"`
}

func saveProviderCheck(d Deployment, r ProbeResult) error {
	outcome := r.ErrorCategory
	if r.OK() {
		outcome = "ok"
	}
	data, _ := json.Marshal(checkMetadata{time.Now().UTC().Format(time.RFC3339), outcome})
	return writeFileAtomic(filepath.Join(d.Root, "provider-check.json"), data, 0600)
}
func LastProviderCheck(d Deployment) string {
	b, err := os.ReadFile(filepath.Join(d.Root, "provider-check.json"))
	if err != nil || len(b) > 1024 {
		return "not checked"
	}
	var c checkMetadata
	if json.Unmarshal(b, &c) != nil {
		return "unknown"
	}
	if _, err = time.Parse(time.RFC3339, c.CheckedAt); err != nil {
		return "unknown"
	}
	switch c.Outcome {
	case "ok", "invalid_configuration", "credential_unavailable", "request_error", "network_error", "timeout", "authentication_failed", "provider_http_error", "invalid_response", "model_unavailable":
		return c.CheckedAt + " " + c.Outcome
	}
	return "unknown"
}

func validService(name string) bool {
	return regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.@-]*$`).MatchString(name)
}

// LockDeployment serializes cooperating provider and instance-key operations.
func LockDeployment(ctx context.Context, d Deployment) (func(), error) { return lockDeployment(ctx, d) }

// composeCredentialOwner resolves only service identity metadata. Command output is
// never included in errors because rendered Compose configuration may contain secrets.
func (m Manager) composeCredentialOwner(ctx context.Context, d Deployment) (int, int, error) {
	out, err := m.Run(ctx, "docker", append(d.composeArgs(), "config", "--format", "json")...)
	if err != nil {
		return 0, 0, errors.New("cannot inspect Compose AO service identity")
	}
	var cfg struct {
		Services map[string]struct {
			User  string `json:"user"`
			Image string `json:"image"`
		} `json:"services"`
	}
	if json.Unmarshal(out, &cfg) != nil {
		return 0, 0, errors.New("invalid Compose identity metadata")
	}
	service, ok := cfg.Services[d.AOService]
	if !ok {
		return 0, 0, errors.New("AO service absent from Compose configuration")
	}
	identity := service.User
	if identity == "" {
		if service.Image == "" {
			return 0, 0, errors.New("AO image identity unavailable")
		}
		out, err = m.Run(ctx, "docker", "image", "inspect", "--format", "{{.Config.User}}", service.Image)
		if err != nil {
			return 0, 0, errors.New("cannot inspect AO image user")
		}
		identity = strings.TrimSpace(string(out))
	}
	if identity == "" || identity == "root" {
		return 0, 0, nil
	}
	parts := strings.Split(identity, ":")
	if len(parts) > 2 {
		return 0, 0, errors.New("unsupported AO container identity")
	}
	uid, err := strconv.Atoi(parts[0])
	if err != nil || uid < 0 {
		return 0, 0, errors.New("AO container user must have a numeric UID for credential ownership")
	}
	gid := uid
	if len(parts) == 2 {
		gid, err = strconv.Atoi(parts[1])
		if err != nil || gid < 0 {
			return 0, 0, errors.New("AO container group must have a numeric GID")
		}
	}
	return uid, gid, nil
}
