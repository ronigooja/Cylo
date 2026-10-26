package ops

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteSecretAtomicPrivateAndReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "token")
	if err := WriteSecretAtomic(path, []byte("old-token")); err != nil {
		t.Fatal(err)
	}
	if err := WriteSecretAtomic(path, []byte("new-token")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new-token" {
		t.Fatalf("secret = %q", got)
	}
}

func TestWriteAOEnvAtomicDoesNotIncludeSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ao.env")
	c := ProviderConfig{BaseURL: "http://127.0.0.1:18080/v1", Model: "gpt-test", SecretPath: "/tmp/secret"}
	if err := WriteAOEnvAtomic(path, c); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	s := string(got)
	for _, want := range []string{"AO_CODEX_EXTERNAL_BASE_URL=", "AO_CODEX_EXTERNAL_API_KEY_FILE=", "AO_CODEX_EXTERNAL_MODEL="} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s", want)
		}
	}
	if strings.Contains(s, "token") {
		t.Fatal("secret leaked into env")
	}
}

func TestProbeCategoriesAndModel(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		want      string
		available bool
	}{
		{"auth", http.StatusUnauthorized, `{}`, "authentication_failed", false},
		{"missing", http.StatusOK, `{"data":[{"id":"other"}]}`, "model_unavailable", false},
		{"ok", http.StatusOK, `{"data":[{"id":"gpt-test"}]}`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer secret" {
					t.Fatal("missing auth")
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			secret := filepath.Join(t.TempDir(), "token")
			if err := WriteSecretAtomic(secret, []byte("secret")); err != nil {
				t.Fatal(err)
			}
			result := Probe(context.Background(), ProviderConfig{BaseURL: server.URL, Model: "gpt-test", SecretPath: secret}, server.Client())
			if result.ErrorCategory != tt.want || result.ModelAvailable != tt.available {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}

func TestReadSecretDoesNotEchoValue(t *testing.T) {
	var prompt strings.Builder
	secret, err := ReadSecret(&prompt, func() ([]byte, error) { return []byte(" hidden-token \n"), nil })
	if err != nil || string(secret) != "hidden-token" {
		t.Fatalf("secret=%q err=%v", secret, err)
	}
	if strings.Contains(prompt.String(), "hidden-token") {
		t.Fatal("prompt leaked token")
	}
}
