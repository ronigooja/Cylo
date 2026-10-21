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

func reviewFeatureServer(t *testing.T) (*storage.Store, http.Handler, string, string) {
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
	cfg := reviewRuntimeConfig(code, reviewToken(t))
	srv, err := control.NewServer(store, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = memory.Register(srv, store); err != nil {
		t.Fatal(err)
	}
	if err = voice.Register(srv, store, voice.Config{}); err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	registration := reviewBootstrap(t, h, code, token)
	return store, h, token, registration.Device.ID
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
func TestReviewMemoryACLTwoAuthenticatedSubjects(t *testing.T) {
	store, h, owner, _ := reviewFeatureServer(t)
	peer := reviewToken(t)
	addMembershipDevice(t, store.DB(), peer, "peer-subject", "peer-device")
	project := featureCommand(t, h, owner, "project", "project.create", map[string]any{"name": "private"})
	task := featureCommand(t, h, owner, "task", "task.create", map[string]any{"project_id": project.ResourceID, "title": "private", "goal": "private", "source": "user"})
	for _, scope := range []map[string]any{{"kind": "project", "id": project.ResourceID}, {"kind": "task", "id": task.ResourceID}} {
		featureCommand(t, h, owner, "remember-"+scope["kind"].(string), "memory.remember", map[string]any{"scope": scope, "subject": "database", "key": "decision", "content": "restricted SQLite evidence", "source_kind": "user_note"})
	}
	search := func(want bool) {
		t.Helper()
		w := reviewRequest(h, "GET", "/v1/memory?q=SQLite", peer, "", nil)
		if w.Code != 200 {
			t.Fatalf("search=%d", w.Code)
		}
		if strings.Contains(w.Body.String(), "restricted SQLite evidence") != want {
			t.Fatalf("memory access=%v wanted=%v", strings.Contains(w.Body.String(), "restricted SQLite evidence"), want)
		}
	}
	search(false)
	at := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.DB().Exec(`INSERT INTO project_memberships(workspace_id,project_id,subject_id,role,status,created_at) VALUES('default',?,'peer-subject','viewer','active',?)`, project.ResourceID, at); err != nil {
		t.Fatal(err)
	}
	search(true)
	raw, _ := json.Marshal(map[string]any{"scope": map[string]any{"kind": "project", "id": project.ResourceID}, "subject": "database", "key": "viewer-write", "content": "viewer mutation", "source_kind": "user_note"})
	w := reviewRequest(h, "POST", "/v1/commands", peer, "viewer-memory-write", contracts.CommandRequest{Kind: "memory.remember", Payload: raw})
	if w.Code < 400 {
		t.Errorf("project viewer changed memory with typed write grant: status=%d", w.Code)
	}
	if _, err := store.DB().Exec(`UPDATE project_memberships SET status='revoked' WHERE project_id=? AND subject_id='peer-subject'`, project.ResourceID); err != nil {
		t.Fatal(err)
	}
	search(false)
}
func TestReviewVoiceAtomicTaskSourceOverHTTP(t *testing.T) {
	store, h, token, device := reviewFeatureServer(t)
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
