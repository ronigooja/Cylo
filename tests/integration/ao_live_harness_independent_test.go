package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// Exercise the actual direct-harness fatal/deferred-cleanup path in a child
// test process against an entirely synthetic AO endpoint, without Docker/model.
func TestIndependentLiveDefiniteCreateFailureDoesNotReplayInCleanup(t *testing.T) {
	var posts atomic.Int32
	fakeAO := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"PROJECT_NOT_FOUND","message":"synthetic definite refusal"}}`))
	}))
	defer fakeAO.Close()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, binary, "-test.run=^TestAOLiveSupervisedCodexRuntime$", "-test.v")
	child.Env = append(os.Environ(),
		"ZOE_LIVE_AO_URL="+fakeAO.URL,
		"ZOE_LIVE_AO_PROJECT_ID=synthetic-owned-project",
		"ZOE_LIVE_AO_WORKSPACE_ROOT="+filepath.Join(root, "data", "worktrees"),
		"ZOE_LIVE_AO_DATA_DIR="+filepath.Join(root, "data"),
		"ZOE_LIVE_AO_EVIDENCE_ROOT="+filepath.Join(root, "evidence"))
	output, runErr := child.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatal("synthetic direct-harness child timed out")
	}
	if runErr == nil {
		t.Fatal("definite-rejected direct harness unexpectedly succeeded")
	}
	if posts.Load() != 1 {
		t.Fatalf("actual fatal/deferred cleanup path made %d create POSTs after definite refusal, want 1 (child output bytes=%d)", posts.Load(), len(output))
	}
}

type independentBoundaryDocker struct{ body []byte }

func (d independentBoundaryDocker) Run(context.Context, ...string) ([]byte, error) {
	return d.body, nil
}

func TestIndependentLiveBoundaryRejectsSymlinkEscape(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"inside", "symlink-escape"} {
		child := exec.Command(binary, "-test.run=^TestIndependentLiveBoundaryChild$")
		child.Env = append(os.Environ(), "ZOE_INDEPENDENT_BOUNDARY_MODE="+mode)
		output, runErr := child.CombinedOutput()
		if (mode == "inside") != (runErr == nil) {
			t.Fatalf("boundary mode=%s accepted=%t output_bytes=%d", mode, runErr == nil, len(output))
		}
	}
}

func TestIndependentLiveBoundaryChild(t *testing.T) {
	mode := os.Getenv("ZOE_INDEPENDENT_BOUNDARY_MODE")
	if mode == "" {
		t.Skip("synthetic child only")
	}
	root := t.TempDir()
	data := filepath.Join(root, "data")
	managed := filepath.Join(data, "worktrees")
	workspace := filepath.Join(managed, "owned")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if mode == "symlink-escape" {
		outside := filepath.Join(root, "outside")
		if err := os.Mkdir(outside, 0700); err != nil {
			t.Fatal(err)
		}
		workspace = filepath.Join(managed, "link")
		if err := os.Symlink(outside, workspace); err != nil {
			t.Fatal(err)
		}
	}
	body, err := json.Marshal(map[string]any{
		"Id":         "exact-container",
		"Config":     map[string]any{"Env": []string{}, "Labels": map[string]string{"io.zoe.managed": "true", "io.zoe.runtime": "ao-codex-chat", "io.zoe.ao.session": "owned-session"}},
		"HostConfig": map[string]any{"NetworkMode": "none", "ReadonlyRootfs": true, "CapDrop": []string{"ALL"}, "SecurityOpt": []string{"no-new-privileges"}, "PidsLimit": 256},
		"Mounts": []map[string]any{
			{"Type": "bind", "Source": workspace, "Destination": workspace, "RW": true},
			{"Type": "bind", "Source": filepath.Join(root, "private-home"), "Destination": "/codex-home", "RW": true},
			{"Type": "bind", "Source": filepath.Join(root, "provider.sock"), "Destination": "/run/zoe/provider.sock", "RW": false},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertLiveContainerBoundary(t, context.Background(), independentBoundaryDocker{body: body}, "exact-container", data, managed, "owned-session")
}
