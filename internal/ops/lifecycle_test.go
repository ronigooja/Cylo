package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteEnvPreservesExistingValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zoe.env")
	if err := os.WriteFile(path, []byte("TOKEN=operator-value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeEnv(path, map[string]string{"TOKEN": "generated-value"}, 0600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "TOKEN=operator-value\n" {
		t.Fatalf("existing env was rewritten: %q", data)
	}
}

func TestStageReleaseKeepsDeploymentFilesUnderDeployLinux(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "release")
	if err := os.MkdirAll(filepath.Join(src, "deploy-linux"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "deploy-linux", "docker-compose.yml"), []byte("services: {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := copyDeployFiles(src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "deploy-linux", "docker-compose.yml")); err != nil {
		t.Fatalf("deployment file was not staged at the unit's relative path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "docker-compose.yml")); !os.IsNotExist(err) {
		t.Fatalf("deployment file was unexpectedly flattened into release root")
	}
}

func TestUpdateRollbackReportsFailingStepAndCommandOutput(t *testing.T) {
	root := t.TempDir()
	opt := filepath.Join(t.TempDir(), "opt")
	releases := filepath.Join(opt, "releases")
	if err := os.MkdirAll(releases, 0755); err != nil {
		t.Fatal(err)
	}
	previous := filepath.Join(releases, "previous")
	if err := os.MkdirAll(previous, 0755); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(releases, "current")
	if err := os.Symlink(previous, current); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"zoe-stack.env", "zoe-runner.env"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("VALUE=old\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var out strings.Builder
	composeRestarts := 0
	m := NewManager(func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "systemctl" && len(args) >= 2 && args[0] == "restart" && args[1] == "zoe-compose.service" {
			composeRestarts++
			if composeRestarts == 1 {
				return []byte("compose file missing\n"), errors.New("exit status 1")
			}
		}
		return nil, nil
	})
	// Exercise the same rollback path used by Update without needing a valid
	// release manifest or Docker pulls.
	stack, err := snapshot(filepath.Join(root, "zoe-stack.env"))
	if err != nil {
		t.Fatal(err)
	}
	runner, err := snapshot(filepath.Join(root, "zoe-runner.env"))
	if err != nil {
		t.Fatal(err)
	}
	_, cause := m.Run(context.Background(), "systemctl", "restart", "zoe-compose.service")
	if cause == nil {
		t.Fatal("expected compose restart to fail")
	}
	err = restoreUpdate(context.Background(), m, LifecycleOptions{Out: &out}, current, "previous", stack, runner, fmt.Errorf("start zoe-compose.service: %w", cause))
	if err == nil || !strings.Contains(err.Error(), "update rolled back") || !strings.Contains(err.Error(), "start zoe-compose.service") {
		t.Fatalf("rollback error = %v", err)
	}
	if !strings.Contains(out.String(), "compose file missing") {
		t.Fatalf("operator output = %q", out.String())
	}
}
