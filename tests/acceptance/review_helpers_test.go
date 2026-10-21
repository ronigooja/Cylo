package acceptance

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/control"
	"github.com/ronigooja/Cylo/internal/storage"
)

func reviewToken(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func reviewServer(t *testing.T, cfg control.Config, register func(*control.Server)) (*storage.Store, http.Handler) {
	t.Helper()
	s := openStore(t, filepath.Join(t.TempDir(), "control.db"))
	if err := storage.RegisterCoreMigrations(s); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	srv, err := control.NewServer(s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if register != nil {
		register(srv)
	}
	return s, srv.Handler()
}

func openStore(t *testing.T, path string) *storage.Store {
	t.Helper()
	s, err := storage.Open(context.Background(), storage.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func reviewRequest(h http.Handler, method, path, token, key string, body any) *httptest.ResponseRecorder {
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func reviewBootstrap(t *testing.T, h http.Handler, code, token string) contracts.DeviceRegistration {
	t.Helper()
	w := reviewRequest(h, "POST", "/v1/bootstrap", "", "", contracts.BootstrapRequest{RequestID: "bootstrap", Code: code, DeviceName: "review", DeviceToken: token})
	if w.Code != http.StatusCreated {
		t.Fatalf("bootstrap status=%d", w.Code)
	}
	var d contracts.DeviceRegistration
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	return d
}
