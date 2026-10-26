package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ronigooja/Cylo/internal/control"
	"github.com/ronigooja/Cylo/internal/ops"
	"github.com/ronigooja/Cylo/internal/storage"
)

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
func fakeServices(t *testing.T, failReload bool) *[]string {
	t.Helper()
	old := opsManager
	calls := []string{}
	opsManager = func() ops.Manager {
		return ops.NewManager(func(_ context.Context, name string, args ...string) ([]byte, error) {
			command := name + " " + strings.Join(args, " ")
			calls = append(calls, command)
			if strings.Contains(command, "systemctl show") {
				return []byte("LoadState=loaded\n"), nil
			}
			if strings.Contains(command, "is-active") {
				return []byte("active\n"), nil
			}
			if failReload && strings.Contains(command, "reload-or-restart") {
				return nil, errors.New("simulated service error")
			}
			return nil, nil
		})
	}
	t.Cleanup(func() { opsManager = old })
	return &calls
}
func TestProviderConfigureTestStatusUsePersistedDeployment(t *testing.T) {
	fakeServices(t, false)
	token := "private-provider-token"
	requests := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("provider request did not use supplied configuration")
		}
		io.WriteString(w, `{"data":[{"id":"chosen-model"}]}`)
	}))
	defer provider.Close()
	root := t.TempDir()
	secret := filepath.Join(root, "private-token")
	var out, errOut bytes.Buffer
	args := []string{"--root", root, "--url", provider.URL + "/v1", "--model", "chosen-model", "--secret-file", secret, "--token", token, "--non-interactive"}
	if err := providerConfigureOperation(args, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if err := providerTestOperation([]string{"--root", root}, nil, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if err := statusOperation([]string{"--root", root}, nil, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("provider requests = %d", requests)
	}
	if !strings.Contains(out.String(), "chosen-model") || !strings.Contains(out.String(), "last-check=") {
		t.Fatal("status omitted persisted provider configuration")
	}
	if strings.Contains(out.String()+errOut.String(), token) {
		t.Fatal("token leaked to command output")
	}
}
func TestKeyMenuInitializesRotatesAndRestoresService(t *testing.T) {
	calls := fakeServices(t, false)
	root := t.TempDir()
	database := filepath.Join(root, "control.sqlite")
	var terminal, out, errOut bytes.Buffer
	old := secretTerminal
	secretTerminal = func() (io.WriteCloser, error) { return nopWriteCloser{&terminal}, nil }
	t.Cleanup(func() { secretTerminal = old })
	firstPath := filepath.Join(root, "initial.json")
	nextPath := filepath.Join(root, "rotated.json")
	args := []string{"--root", root, "--database", database, "--server", "https://cylo.example", "--output", firstPath}
	if err := keyOperation(false, args, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatal(err)
	}
	var first, next ops.KeyResult
	read := func(path string, dst *ops.KeyResult) {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, dst); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("key export is not private")
		}
	}
	read(firstPath, &first)
	terminal.Reset()
	args[len(args)-1] = nextPath
	args = append(args, "--yes")
	if err := keyOperation(true, args, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatal(err)
	}
	read(nextPath, &next)
	if first.Key == next.Key || !strings.Contains(terminal.String(), next.InstallationURL) || !strings.Contains(terminal.String(), "█") {
		t.Fatal("rotation or terminal connection details missing")
	}
	if strings.Contains(out.String()+errOut.String(), next.Key) {
		t.Fatal("key leaked outside controlling terminal")
	}
	store, err := storage.Open(context.Background(), storage.Config{Path: database})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, _, err := control.InitializeInstanceAuth(context.Background(), store, first.Key); err == nil {
		t.Fatal("old credential remains valid")
	}
	if _, _, err := control.InitializeInstanceAuth(context.Background(), store, next.Key); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(*calls, "\n")
	if strings.Count(joined, "systemctl stop cylo-compose.service") != 2 || strings.Count(joined, "systemctl start cylo-compose.service") != 2 {
		t.Fatalf("service lifecycle incomplete: %s", joined)
	}
}
func TestKeyFailureRestoresServiceAndDoesNotDisplaySecret(t *testing.T) {
	calls := fakeServices(t, false)
	root := t.TempDir()
	db := filepath.Join(root, "control.sqlite")
	store, err := storage.Open(context.Background(), storage.Config{Path: db})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var out, errOut bytes.Buffer
	err = keyOperation(false, []string{"--root", root, "--database", db, "--server", "https://host"}, strings.NewReader(""), &out, &errOut)
	if err == nil {
		t.Fatal("key operation ignored active database lock")
	}
	if !strings.Contains(strings.Join(*calls, "\n"), "systemctl start cylo-compose.service") {
		t.Fatal("service not restored after key failure")
	}
	matches, _ := filepath.Glob(filepath.Join(root, "instance-key-*.json"))
	if len(matches) != 0 {
		t.Fatal("failed action left a private export")
	}
}
func TestKeyRefusesStaleDeploymentCredentialBeforeStoppingService(t *testing.T) {
	calls := fakeServices(t, false)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "cylo-stack.env"), []byte("CYLO_INSTANCE_KEY_FILE=/private/existing-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := keyOperation(true, []string{"--root", root, "--database", filepath.Join(root, "control.sqlite"), "--server", "https://host", "--yes"}, strings.NewReader(""), &out, &out)
	if err == nil {
		t.Fatal("rotation accepted stale startup source")
	}
	for _, command := range *calls {
		if strings.Contains(command, " stop ") {
			t.Fatal("stale source check stopped services")
		}
	}
}
func TestKeyWithoutTerminalSavesExportWithoutPrintingCredentials(t *testing.T) {
	root := t.TempDir()
	old := secretTerminal
	secretTerminal = func() (io.WriteCloser, error) { return nil, errors.New("no tty") }
	t.Cleanup(func() { secretTerminal = old })
	var out bytes.Buffer
	if err := keyOperation(false, []string{"--offline", "--root", root, "--database", filepath.Join(root, "db"), "--server", "https://host"}, strings.NewReader(""), &out, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "cylo://") || !strings.Contains(out.String(), "No controlling terminal") {
		t.Fatal("non-terminal output exposed connection details")
	}
}
