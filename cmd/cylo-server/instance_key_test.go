package main

import (
	"context"
	"encoding/json"
	"github.com/ronigooja/Cylo/internal/control"
	"github.com/ronigooja/Cylo/internal/storage"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestKeyCommandsRequireExplicitSecureExportAndRotateExactlyOneCredential(t *testing.T) {
	root := t.TempDir()
	db := filepath.Join(root, "control.db")
	output := filepath.Join(root, "install.json")
	if err := manageInstanceKey("init-key", []string{"--database", db}); err == nil {
		t.Fatal("implicit secret output allowed")
	}
	if err := manageInstanceKey("init-key", []string{"--database", db, "--output", output, "--server", "https://cylo.example/base"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(output)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("export not private")
	}
	read := func(path string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var value struct {
			Key string `json:"key"`
			URL string `json:"installation_url"`
		}
		if err = json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		if value.URL != "" {
			link, err := url.Parse(value.URL)
			if err != nil {
				t.Fatal(err)
			}
			fragment, err := url.ParseQuery(link.Fragment)
			if err != nil || link.Query().Get("key") != "" || fragment.Get("key") != value.Key || link.Query().Get("server") != "https://cylo.example/base" {
				t.Fatal("invalid fragment installation link")
			}
		}
		return value.Key
	}
	old := read(output)
	if err = manageInstanceKey("rotate-key", []string{"--database", db, "--output", output}); err == nil {
		t.Fatal("existing export overwritten")
	}
	rotated := filepath.Join(root, "rotated.json")
	if err = manageInstanceKey("rotate-key", []string{"--database", db, "--output", rotated}); err != nil {
		t.Fatal(err)
	}
	next := read(rotated)
	if next == old {
		t.Fatal("key did not change")
	}
	store, err := storage.Open(context.Background(), storage.Config{Path: db})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, _, err = control.InitializeInstanceAuth(context.Background(), store, old); err == nil {
		t.Fatal("old deployment secret reset rotation")
	}
	if _, _, err = control.InitializeInstanceAuth(context.Background(), store, next); err != nil {
		t.Fatal(err)
	}
}
func TestFailedKeyInitializationRemovesUncommittedExport(t *testing.T) {
	root := t.TempDir()
	db := filepath.Join(root, "control.db")
	output := filepath.Join(root, "private.json")
	store, err := storage.Open(context.Background(), storage.Config{Path: db})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = manageInstanceKey("init-key", []string{"--database", db, "--output", output}); err == nil {
		t.Fatal("exclusive store lock ignored")
	}
	if _, err = os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("uncommitted export left behind")
	}
}

func TestInstallationLinksRejectServerAddressesAppleCannotImport(t *testing.T) {
	for _, server := range []string{"https://host:65536", "https://host:0", "https://host:", "https://host/a/../b", "https://host/%2E/child", "https://host/encoded%2fsegment", "https://host/back\\slash", "https://host?", "https://host#"} {
		if err := validateInstallationServer(server); err == nil {
			t.Errorf("unimportable server accepted: %s", server)
		}
	}
	for _, server := range []string{"https://host/base", "http://localhost:8787", "https://[::1]:443/base"} {
		if err := validateInstallationServer(server); err != nil {
			t.Errorf("valid server rejected: %s: %v", server, err)
		}
	}
}
