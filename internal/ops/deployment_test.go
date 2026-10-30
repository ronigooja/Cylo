package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectNeverIncludesSecretContents(t *testing.T) {
	root := t.TempDir()
	secret := filepath.Join(root, "secret")
	if err := os.WriteFile(secret, []byte("super-secret-token"), 0600); err != nil {
		t.Fatal(err)
	}
	m := NewManager(func(context.Context, string, ...string) ([]byte, error) { return []byte("active\n"), nil })
	got := m.Inspect(context.Background(), InspectConfig{Mode: ModeSystemd, Services: []string{"zoe-runner.service"}, SecretPath: secret, ProviderBaseURL: "https://user:pass@gateway.example/v1?token=x"})
	if got.Secret.Mode != "0600" || !got.Secret.Exists {
		t.Fatalf("secret metadata = %#v", got.Secret)
	}
	if strings.Contains(got.Provider.BaseURL, "pass") || strings.Contains(got.Provider.BaseURL, "token") {
		t.Fatalf("provider URL not redacted: %q", got.Provider.BaseURL)
	}
}

func TestReloadReturnsActionableError(t *testing.T) {
	m := NewManager(func(context.Context, string, ...string) ([]byte, error) {
		return []byte("permission denied"), errors.New("exit status 1")
	})
	err := m.Reload(context.Background(), ModeSystemd, nil, "", "")
	if err == nil || !strings.Contains(err.Error(), "systemctl daemon-reload failed") {
		t.Fatalf("error = %v", err)
	}
}
