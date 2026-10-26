package acceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/control"
	"github.com/ronigooja/Cylo/internal/memory"
	"github.com/ronigooja/Cylo/internal/storage"
	"github.com/ronigooja/Cylo/internal/voice"
)

func reviewFeatureServer(t *testing.T) (*storage.Store, http.Handler, string, string, string) {
	t.Helper()
	code, token := reviewToken(t), reviewToken(t)
	store, err := storage.Open(context.Background(), storage.Config{Path: filepath.Join(t.TempDir(), "features.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err = storage.RegisterCoreMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err = control.RegisterInstanceMaintenanceMigration(store); err != nil {
		t.Fatal(err)
	}
	runnerToken := reviewToken(t)
	cfg := reviewRuntimeConfig(code, runnerToken)
	cfg.InstanceScopes = append(control.DefaultOwnerScopes(), "instance:admin")
	srv, err := control.NewServer(store, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = control.RegisterInstanceRoutes(srv); err != nil {
		t.Fatal(err)
	}
	if err = memory.Register(srv, store); err != nil {
		t.Fatal(err)
	}
	if err = voice.Register(srv, store, voice.Config{}); err != nil {
		t.Fatal(err)
	}
	if err = control.RegisterExportRoutes(srv); err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := srv.RegisterRunnerCredential(context.Background(), runnerToken, "review", []string{"default"}, cfg.RunnerScopes); err != nil {
		t.Fatal(err)
	}
	h := bindReviewHandler(srv.Handler(), store)
	registration := reviewBootstrap(t, h, code, token)
	return store, h, token, registration.Device.ID, runnerToken
}
func featureCommand(t *testing.T, h http.Handler, token, key, kind string, payload any) *contracts.CommandReceipt {
	t.Helper()
	raw, _ := json.Marshal(payload)
	w := reviewRequest(h, "POST", "/v1/commands", token, key, contracts.CommandRequest{Kind: kind, Payload: raw})
	if w.Code != 200 {
		t.Fatalf("%s=%d %s", kind, w.Code, w.Body.String())
	}
	var receipt contracts.CommandReceipt
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	return &receipt
}
func TestRestoreDeletionGateClosesBusinessRoutesButAllowsDiagnostics(t *testing.T) {
	store, h, owner, _, runnerToken := reviewFeatureServer(t)
	featureCommand(t, h, owner, "gate-memory", "memory.remember", map[string]any{"scope": map[string]any{"kind": "personal"}, "subject": "restore gate", "key": "private", "content": "snapshot-only private memory", "source_kind": "user_note"})
	before := reviewRequest(h, "GET", "/v1/memory?q=snapshot-only", owner, "", nil)
	if before.Code != http.StatusOK || !strings.Contains(before.Body.String(), "snapshot-only private memory") {
		t.Fatalf("fresh instance did not serve memory before restore gate: status=%d body=%s", before.Code, before.Body.String())
	}
	if _, err := store.DB().Exec(`UPDATE cylo_instance_state SET restore_deletion_reconciliation_required=1,dispatch_enabled=0,derived_retrieval_enabled=0 WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}

	requests := []struct {
		method, path, token string
		body                any
	}{
		{"GET", "/v1/memory?q=snapshot-only", owner, nil},
		{"GET", "/v1/memory/context?q=snapshot-only", owner, nil},
		{"GET", "/v1/export?scope=personal", owner, nil},
		{"GET", "/v1/snapshot", owner, nil},
		{"GET", "/v1/commands", owner, nil},
		{"POST", "/v1/commands", owner, map[string]any{"kind": "task.create"}},
		{"POST", "/v1/bootstrap", "", map[string]any{"request_id": "gate-bootstrap", "code": "unavailable", "device_name": "blocked", "device_token": "unavailable"}},
		{"POST", "/v1/pairings", owner, map[string]any{"name": "blocked"}},
		{"GET", "/v1/pairings/no-such-pairing", owner, nil},
		{"GET", "/runner/v1/instance", runnerToken, nil},
		{"POST", "/runner/v1/claims", runnerToken, map[string]any{"limit": 1}},
	}
	for _, request := range requests {
		w := reviewRequest(h, request.method, request.path, request.token, "restore-gate-attempt", request.body)
		if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "restore_deletion_reconciliation_required") {
			t.Errorf("restore gate did not close %s %s: status=%d body=%s", request.method, request.path, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "snapshot-only private memory") {
			t.Errorf("restore gate disclosed memory through %s %s", request.method, request.path)
		}
	}
	for _, path := range []string{"/v1/admin/instance", "/v1/admin/instance/migration-readiness"} {
		w := reviewRequest(h, "GET", path, owner, "", nil)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"restore_deletion_reconciliation_required":true`) {
			t.Errorf("restore diagnostic %s unavailable: status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
	if w := reviewRequest(h, "GET", "/healthz", "", "", nil); w.Code != http.StatusOK {
		t.Errorf("health route was blocked by restore gate: status=%d", w.Code)
	}
	for _, alias := range []string{"/v1//memory?q=snapshot-only", "/v1/memory/../memory?q=snapshot-only"} {
		w := reviewRequest(h, "GET", alias, owner, "", nil)
		if w.Code == http.StatusOK || strings.Contains(w.Body.String(), "snapshot-only private memory") {
			t.Errorf("route alias bypassed restore gate at %q: status=%d body=%s", alias, w.Code, w.Body.String())
		}
	}
}

func TestReviewVoiceAtomicTaskSourceOverHTTP(t *testing.T) {
	store, h, token, device, _ := reviewFeatureServer(t)
	var subject string
	if err := store.DB().QueryRow(`SELECT subject_id FROM devices WHERE id=?`, device).Scan(&subject); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.DB().Exec(`INSERT INTO voice_sessions(id,workspace_id,owner_id,device_id,request_id,request_hash,generation,state,created_at,updated_at) VALUES('review-atomic','default',?,?,'atomic-request',X'01',1,'active',?,?)`, subject, device, now, now); err != nil {
		t.Fatal(err)
	}
	create := func(revision int, key string) int {
		featureCommand(t, h, token, fmt.Sprintf("commit-%d", revision), "voice.turn.commit", map[string]any{"session_id": "review-atomic", "generation": 1, "turn_id": "stable", "revision": revision, "content": fmt.Sprintf("confirmed transcript %d", revision), "user_confirmed": true})
		raw, _ := json.Marshal(map[string]any{"title": "forged title", "goal": "forged goal", "initial_message": "forged message", "source": "voice", "source_reference": map[string]any{"kind": "voice_turn", "voice_session_id": "review-atomic", "generation": 1, "turn_id": "stable", "revision": revision}})
		return reviewRequest(h, "POST", "/v1/commands", token, key, contracts.CommandRequest{Kind: "task.create", Payload: raw}).Code
	}
	if status := create(1, "create-first"); status != 200 {
		t.Fatalf("first create=%d", status)
	}
	counts := func() string {
		t.Helper()
		var tasks, work, claims int
		for _, q := range []struct {
			sql  string
			dest *int
		}{{`SELECT count(*) FROM tasks`, &tasks}, {`SELECT count(*) FROM command_work WHERE kind='task.create'`, &work}, {`SELECT count(*) FROM runs`, &claims}} {
			if err := store.DB().QueryRow(q.sql).Scan(q.dest); err != nil {
				t.Fatal(err)
			}
		}
		return fmt.Sprintf("%d/%d/%d", tasks, work, claims)
	}
	before := counts()
	if status := create(2, "create-revised"); status != 409 {
		t.Fatalf("revised create=%d", status)
	}
	if after := counts(); after != before {
		t.Fatalf("duplicate changed task/work/run counts: %s -> %s", before, after)
	}
	var goal, entry string
	if err := store.DB().QueryRow(`SELECT goal FROM tasks`).Scan(&goal); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRow(`SELECT content FROM task_entries`).Scan(&entry); err != nil {
		t.Fatal(err)
	}
	if goal != "confirmed transcript 1" || entry != goal {
		t.Fatalf("canonical goal=%q entry=%q", goal, entry)
	}
	var raw []byte
	if err := store.DB().QueryRow(`SELECT payload FROM events WHERE event_type='task.created'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var event contracts.Event
	if err := json.Unmarshal(raw, &event); err != nil {
		t.Fatal(err)
	}
	var body struct {
		Task contracts.Task `json:"task"`
	}
	if err := json.Unmarshal(event.Payload, &body); err != nil {
		t.Fatal(err)
	}
	if body.Task.Goal != goal || body.Task.Title != goal {
		t.Fatalf("canonical event task=%+v", body.Task)
	}
}
