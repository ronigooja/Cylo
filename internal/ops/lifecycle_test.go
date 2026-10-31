package ops

import (
	"os"
	"path/filepath"
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
