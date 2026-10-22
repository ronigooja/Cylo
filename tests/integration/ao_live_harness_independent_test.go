package integration

import (
	"context"
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
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"FORBIDDEN","message":"synthetic definite refusal"}}`))
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
		"CYLO_LIVE_AO_URL="+fakeAO.URL,
		"CYLO_LIVE_AO_PROJECT_ID=synthetic-owned-project",
		"CYLO_LIVE_AO_WORKSPACE_ROOT="+filepath.Join(root, "data", "worktrees"),
		"CYLO_LIVE_AO_DATA_DIR="+filepath.Join(root, "data"),
		"CYLO_LIVE_AO_EVIDENCE_ROOT="+filepath.Join(root, "evidence"))
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
