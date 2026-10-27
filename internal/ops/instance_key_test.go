package ops

import (
	"context"
	"encoding/base64"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronigooja/Zoe/internal/control"
	"github.com/ronigooja/Zoe/internal/storage"
)

func TestGeneratedKeyFormatAndRandomness(t *testing.T) {
	first, err := control.GenerateInstanceKey()
	if err != nil {
		t.Fatal(err)
	}
	second, err := control.GenerateInstanceKey()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{first, second} {
		decoded, err := base64.RawURLEncoding.DecodeString(key)
		if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != key {
			t.Fatalf("invalid key format %q", key)
		}
	}
	if first == second {
		t.Fatal("key generation repeated output")
	}
}

func TestInstallationURLKeepsKeyInFragment(t *testing.T) {
	key := "a_b-c"
	link := InstallationURL("https://host/base", key)
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("key") != "" || parsed.Query().Get("server") != "https://host/base" {
		t.Fatalf("credential leaked into query: %s", link)
	}
	if got := parsed.Fragment; got != "key="+key {
		t.Fatalf("fragment = %q", got)
	}
}

func TestDisplayIsExplicitAndDoesNotAddDiagnostics(t *testing.T) {
	var out strings.Builder
	result := KeyResult{Key: "secret", InstallationURL: "zoe://connect?server=x#key=secret"}
	if err := result.Display(&out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "secret\nzoe://connect?server=x#key=secret\n" {
		t.Fatalf("unexpected display output %q", out.String())
	}
}

func TestInitKeyFailsWhenDatabaseLockIsHeld(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(root, "control.db")
	store, err := storage.Open(context.Background(), storage.Config{Path: database})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	output := filepath.Join(root, "key.json")
	if _, err := InitKey(context.Background(), KeyOptions{DatabasePath: database, OutputPath: output}); err == nil {
		t.Fatal("operation ignored exclusive database lock")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("lock failure left key export behind: %v", err)
	}
}
