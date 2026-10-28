package ops

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverDeploymentTreatsSystemdSupervisedComposeAsCompose(t *testing.T) {
	root := t.TempDir()
	compose := filepath.Join(root, "active.compose.yaml")
	if err := os.WriteFile(compose, []byte("services: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "zoe-stack.env"), []byte("ZOE_COMPOSE_FILE="+compose+"\nCOMPOSE_PROJECT_NAME=zoe\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "provider-deployment.json"), []byte(`{"Mode":"systemd","Root":"`+root+`","ComposeFile":"`+compose+`","ComposeProject":"zoe"}`), 0600); err != nil {
		t.Fatal(err)
	}
	m := NewManager(func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("LoadState=loaded\nEnvironmentFiles=/etc/zoe/ao.env\n"), nil
	})
	d, err := DiscoverDeployment(context.Background(), m, root)
	if err != nil {
		t.Fatal(err)
	}
	if d.Mode != ModeCompose {
		t.Fatalf("mode=%q, want compose", d.Mode)
	}
}
