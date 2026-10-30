package ops

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func testDeployment(t *testing.T) Deployment {
	t.Helper()
	root := t.TempDir()
	return Deployment{Root: root, Mode: ModeSystemd, RunnerService: "zoe-runner.service", RunnerEnv: filepath.Join(root, "zoe-runner.env"), StackEnv: filepath.Join(root, "stack.env"), ComposeProject: "zoe", DropInPath: filepath.Join(root, "zoe-runner.service.d", "90-zoe-provider.conf")}
}
func modelServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer next" {
			t.Error("staged credential was not sent")
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"test-model"}]}`))
	}))
	t.Cleanup(s.Close)
	return s
}
func TestProviderTransactionPreservesConfigAndTargetsOnlyRunner(t *testing.T) {
	d := testDeployment(t)
	server := modelServer(t)
	secret := filepath.Join(d.Root, "token")
	_ = os.WriteFile(secret, []byte("old"), 0600)
	_ = os.WriteFile(d.RunnerEnv, []byte("KEEP=original\nZOE_OPENCODE_MODEL=old\n"), 0600)
	var commands [][]string
	m := NewManager(func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, append([]string{name}, args...))
		return nil, nil
	})
	result, err := m.ConfigureProvider(context.Background(), d, ProviderConfig{BaseURL: server.URL, Model: "test-model", SecretPath: secret, Secret: "next"}, server.Client())
	if err != nil || !result.OK() {
		t.Fatalf("apply: %v %+v", err, result)
	}
	contents, _ := os.ReadFile(d.RunnerEnv)
	if !strings.Contains(string(contents), "KEEP=original") || strings.Contains(string(contents), "=old") {
		t.Fatal("unrelated environment lost or stale provider survived")
	}
	got, _ := os.ReadFile(secret)
	if string(got) != "next" {
		t.Fatal("credential not replaced")
	}
	expected := []string{"systemctl", "reload-or-restart", "zoe-runner.service"}
	if !reflect.DeepEqual(commands[len(commands)-1], expected) {
		t.Fatalf("targeted reload args: %v", commands)
	}
	restored, err := DiscoverDeployment(context.Background(), m, d.Root)
	if err != nil || restored.RunnerService != "zoe-runner.service" {
		t.Fatal("managed deployment not rediscovered")
	}
	if LastProviderCheck(d) == "not checked" {
		t.Fatal("missing safe check metadata")
	}
}
func TestProviderTransactionRestoresAfterPartialReload(t *testing.T) {
	d := testDeployment(t)
	server := modelServer(t)
	secret := filepath.Join(d.Root, "token")
	_ = os.WriteFile(secret, []byte("old"), 0600)
	_ = os.WriteFile(d.RunnerEnv, []byte("KEEP=old\n"), 0640)
	calls := 0
	m := NewManager(func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "reload-or-restart") {
			calls++
			if calls == 1 {
				return nil, errors.New("partial failure")
			}
		}
		return nil, nil
	})
	_, err := m.ConfigureProvider(context.Background(), d, ProviderConfig{BaseURL: server.URL, Model: "test-model", SecretPath: secret, Secret: "next"}, server.Client())
	if err == nil || !strings.Contains(err.Error(), "partial failure") {
		t.Fatalf("error=%v", err)
	}
	data, _ := os.ReadFile(secret)
	if string(data) != "old" {
		t.Fatal("secret rollback failed")
	}
	data, _ = os.ReadFile(d.RunnerEnv)
	info, _ := os.Stat(d.RunnerEnv)
	if string(data) != "KEEP=old\n" || info.Mode().Perm() != 0640 {
		t.Fatal("environment rollback failed")
	}
	if calls != 2 {
		t.Fatal("old service configuration was not reloaded")
	}
}
func TestRejectedStagedCredentialDoesNotOverwrite(t *testing.T) {
	d := testDeployment(t)
	secret := filepath.Join(d.Root, "token")
	_ = os.WriteFile(secret, []byte("old"), 0600)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(401) }))
	defer s.Close()
	m := NewManager(func(context.Context, string, ...string) ([]byte, error) {
		t.Error("reload before validation")
		return nil, nil
	})
	_, err := m.ConfigureProvider(context.Background(), d, ProviderConfig{BaseURL: s.URL, Model: "m", SecretPath: secret, Secret: "bad"}, s.Client())
	if err == nil {
		t.Fatal("accepted invalid credential")
	}
	b, _ := os.ReadFile(secret)
	if string(b) != "old" {
		t.Fatal("overwrote credential before probe")
	}
}
func TestProbeNeverFollowsCredentialRedirect(t *testing.T) {
	followed := false
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { followed = true }))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 302) }))
	defer origin.Close()
	r := Probe(context.Background(), ProviderConfig{BaseURL: origin.URL, Model: "m", Secret: "next"}, origin.Client())
	if followed || r.ErrorCategory != "provider_http_error" {
		t.Fatalf("redirect followed: %+v", r)
	}
}
func TestProviderLockHonorsCancellation(t *testing.T) {
	d := testDeployment(t)
	unlock, err := lockDeployment(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = lockDeployment(ctx, d)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock error=%v", err)
	}
}
func TestRunnerStatusIsScopedAndAllowlisted(t *testing.T) {
	m := NewManager(func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[len(args)-1] != "zoe-runner.service" {
			t.Fatal("status missing service scope")
		}
		return []byte("active\n"), nil
	})
	s := m.Inspect(context.Background(), InspectConfig{Mode: ModeSystemd, Services: []string{"zoe-runner.service"}})
	if s.Services[0].State != "exited" {
		t.Fatal(s.Services)
	}
}
func TestProviderValidationRejectsUnsafeInputs(t *testing.T) {
	for _, raw := range []string{"http://remote.example/v1", "https://user:pass@example/v1", "https://example/v1?secret=x"} {
		if ValidateProviderBaseURL(raw) == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if validToken("has space") || validToken("line\nfeed") {
		t.Fatal("accepted whitespace token")
	}
}
func TestLastCheckRejectsUntrustedMetadata(t *testing.T) {
	d := testDeployment(t)
	_ = os.WriteFile(filepath.Join(d.Root, "provider-check.json"), []byte(`{"checked_at":"2026-10-25T00:00:00Z","outcome":"secret-token"}`), 0600)
	if strings.Contains(LastProviderCheck(d), "secret-token") {
		t.Fatal("untrusted metadata echoed")
	}
}
