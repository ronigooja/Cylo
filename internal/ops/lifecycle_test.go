package ops

import (
	"context"
	"errors"
	"fmt"
	"io"
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

func TestWriteEnvMergesReleaseValuesAndPreservesOperatorLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zoe.env")
	original := "# operator comment\nZOE_SERVER_IMAGE=old\nZOE_VOICE_LANGUAGE=en\nUNKNOWN=value\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeEnv(path, map[string]string{"ZOE_SERVER_IMAGE": "new", "ZOE_COMPOSE_FILE": "/new/compose.yml"}, 0600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "# operator comment\nZOE_SERVER_IMAGE=new\nZOE_VOICE_LANGUAGE=en\nUNKNOWN=value\nZOE_COMPOSE_FILE=/new/compose.yml\n"
	if string(data) != want {
		t.Fatalf("merged env = %q, want %q", data, want)
	}
	if mode := dataMode(t, path); mode != 0600 {
		t.Fatalf("mode = %04o, want 0600", mode)
	}
}

func dataMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func TestEnsureNetworkCreatesOnlyWhenMissing(t *testing.T) {
	created := false
	m := NewManager(func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "docker" && args[0] == "network" && args[1] == "inspect" {
			return nil, errors.New("missing")
		}
		if name == "docker" && args[0] == "network" && args[1] == "create" {
			created = true
		}
		return nil, nil
	})
	if err := m.ensureNetwork(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("network was not created")
	}
}

func TestUninstallMidwayFailureLeavesReleaseRootForRetry(t *testing.T) {
	root, opt := t.TempDir(), filepath.Join(t.TempDir(), "opt")
	if err := os.MkdirAll(filepath.Join(opt, "releases", "current"), 0755); err != nil {
		t.Fatal(err)
	}
	var failNetworkRemoval = true
	m := NewManager(func(_ context.Context, name string, args ...string) ([]byte, error) {
		switch {
		case name == "docker" && len(args) >= 2 && args[0] == "ps":
			return nil, nil
		case name == "docker" && len(args) >= 3 && args[0] == "network" && args[1] == "rm" && failNetworkRemoval:
			failNetworkRemoval = false
			return []byte("temporary failure"), errors.New("failed")
		default:
			return nil, nil
		}
	})
	o := LifecycleOptions{Root: root, OptRoot: opt, Out: io.Discard}
	if err := m.Uninstall(context.Background(), o); err == nil {
		t.Fatal("expected first uninstall to fail")
	}
	if _, err := os.Stat(opt); err != nil {
		t.Fatalf("release root removed after failure: %v", err)
	}
	if err := m.Uninstall(context.Background(), o); err != nil {
		t.Fatalf("retry uninstall: %v", err)
	}
	if _, err := os.Stat(opt); !os.IsNotExist(err) {
		t.Fatalf("release root still exists: %v", err)
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
	for _, name := range []string{"zoe-stack.env", "zoe-runner.env"} {
		data, readErr := os.ReadFile(filepath.Join(root, name))
		if readErr != nil || string(data) != "VALUE=old\n" {
			t.Fatalf("rollback did not restore %s byte-for-byte: %q, %v", name, data, readErr)
		}
	}
}
