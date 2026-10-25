package acceptance

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/control"
	"github.com/ronigooja/Cylo/internal/storage"
)

func reviewToken(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		t.Fatal(e)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func reviewServer(t *testing.T, cfg control.Config, register func(*control.Server)) (*storage.Store, http.Handler) {
	t.Helper()
	s := openStore(t, filepath.Join(t.TempDir(), "control.db"))
	if e := storage.RegisterCoreMigrations(s); e != nil {
		t.Fatal(e)
	}
	if e := control.RegisterInstanceMaintenanceMigration(s); e != nil {
		t.Fatal(e)
	}
	if e := s.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	srv, e := control.NewServer(s, cfg)
	if e != nil {
		t.Fatal(e)
	}
	if register != nil {
		register(srv)
	}
	return s, srv.Handler()
}
func reviewRequest(h http.Handler, method, path, token, key string, body any) *httptest.ResponseRecorder {
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
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
	w := reviewRequest(h, "POST", "/v1/auth/setup", "", "", contracts.OwnerAuthRequest{Password: "owner-test-password", RequestID: "bootstrap", Code: code, DeviceName: "review", DeviceToken: token})
	if w.Code != 201 {
		t.Fatalf("bootstrap status=%d", w.Code)
	}
	var d contracts.DeviceRegistration
	if e := json.Unmarshal(w.Body.Bytes(), &d); e != nil {
		t.Fatal(e)
	}
	return d
}
func TestReviewHTTPIdentityAndReceipts(t *testing.T) {
	code := reviewToken(t)
	token := reviewToken(t)
	s, h := reviewServer(t, control.Config{BootstrapCode: code}, nil)
	d := reviewBootstrap(t, h, code, token)
	retry := reviewRequest(h, "POST", "/v1/auth/setup", "", "", contracts.OwnerAuthRequest{Password: "owner-test-password", RequestID: "bootstrap", Code: code, DeviceName: "review", DeviceToken: token})
	if retry.Code != 200 {
		t.Fatalf("lost ACK retry=%d", retry.Code)
	}
	var again contracts.DeviceRegistration
	json.Unmarshal(retry.Body.Bytes(), &again)
	if again.Device.ID != d.Device.ID {
		t.Error("bootstrap retry changed identity")
	}
	mismatch := reviewRequest(h, "POST", "/v1/auth/setup", "", "", contracts.OwnerAuthRequest{Password: "owner-test-password", RequestID: "bootstrap", Code: code, DeviceName: "changed", DeviceToken: token})
	if mismatch.Code != 409 {
		t.Errorf("bootstrap mismatch=%d", mismatch.Code)
	}
	secondToken := reviewToken(t)
	login := reviewRequest(h, "POST", "/v1/auth/login", "", "", contracts.OwnerAuthRequest{RequestID: "second-login", Password: "owner-test-password", DeviceName: "second", DeviceToken: secondToken})
	if login.Code != 201 {
		t.Fatalf("second login=%d %s", login.Code, login.Body.String())
	}
	req := contracts.CommandRequest{Kind: "project.create", Payload: json.RawMessage(`{"name":"review"}`)}
	first := reviewRequest(h, "POST", "/v1/commands", token, "project", req)
	if first.Code != 200 {
		t.Fatalf("command=%d %s", first.Code, first.Body.String())
	}
	var receipt contracts.CommandReceipt
	json.Unmarshal(first.Body.Bytes(), &receipt)
	second := reviewRequest(h, "POST", "/v1/commands", token, "project", req)
	if !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
		t.Error("duplicate changed receipt")
	}
	req.Payload = json.RawMessage(`{"name":"different"}`)
	if w := reviewRequest(h, "POST", "/v1/commands", token, "project", req); w.Code != 409 {
		t.Errorf("mismatch=%d", w.Code)
	}
	got := reviewRequest(h, "GET", "/v1/commands?key=project", token, "", nil)
	if !bytes.Equal(got.Body.Bytes(), first.Body.Bytes()) {
		t.Error("lost ACK lookup changed receipt")
	}
	for _, table := range []string{"projects", "command_receipts", "command_work", "events"} {
		var n int
		if e := s.DB().QueryRow("SELECT count(*) FROM " + table).Scan(&n); e != nil {
			t.Fatal(e)
		}
		want := 1
		if table == "events" {
			want = 4
		}
		if n != want {
			t.Errorf("%s rows=%d", table, n)
		}
	}
	var hashes int
	if e := s.DB().QueryRow("SELECT count(*) FROM devices WHERE length(token_hash)=32").Scan(&hashes); e != nil {
		t.Fatal(e)
	}
	if hashes != 2 {
		t.Error("hashed credential storage missing")
	}
}

func TestReviewOwnerLoginAndSessionRevocation(t *testing.T) {
	code, token := reviewToken(t), reviewToken(t)
	store, h := reviewServer(t, control.Config{BootstrapCode: code}, nil)
	reviewBootstrap(t, h, code, token)
	for _, path := range []string{"/v1/bootstrap", "/v1/pairings"} {
		if w := reviewRequest(h, "POST", path, "", "", map[string]string{}); w.Code != 410 {
			t.Fatalf("legacy %s=%d", path, w.Code)
		}
	}
	req := contracts.OwnerAuthRequest{RequestID: "login", Password: "owner-test-password", DeviceName: "phone", DeviceToken: reviewToken(t)}
	wrong := req
	wrong.Password = "incorrect-password"
	if w := reviewRequest(h, "POST", "/v1/auth/login", "", "", wrong); w.Code != 401 {
		t.Fatalf("wrong password=%d", w.Code)
	}
	first := reviewRequest(h, "POST", "/v1/auth/login", "", "", req)
	if first.Code != 201 {
		t.Fatalf("login=%d %s", first.Code, first.Body.String())
	}
	retry := reviewRequest(h, "POST", "/v1/auth/login", "", "", req)
	if retry.Code != 200 || !bytes.Equal(first.Body.Bytes(), retry.Body.Bytes()) {
		t.Fatal("login retry changed session")
	}
	if w := reviewRequest(h, "GET", "/v1/auth/session", req.DeviceToken, "", nil); w.Code != 200 {
		t.Fatalf("session=%d", w.Code)
	}
	if w := reviewRequest(h, "POST", "/v1/auth/logout", req.DeviceToken, "", nil); w.Code != 200 {
		t.Fatalf("logout=%d", w.Code)
	}
	if w := reviewRequest(h, "GET", "/v1/snapshot", req.DeviceToken, "", nil); w.Code < 400 {
		t.Fatal("logout session remains authorized")
	}
	if w := reviewRequest(h, "POST", "/v1/auth/login", "", "", req); w.Code != 409 {
		t.Fatalf("revoked retry=%d", w.Code)
	}
	if w := reviewRequest(h, "GET", "/v1/auth/session", token, "", nil); w.Code != 200 {
		t.Fatal("logout revoked another session")
	}
	var subjectCount int
	if err := store.DB().QueryRow("SELECT count(DISTINCT subject_id) FROM devices").Scan(&subjectCount); err != nil || subjectCount != 1 {
		t.Fatalf("sessions have different owners: %d %v", subjectCount, err)
	}
}

func TestReviewOwnerBruteForceLimitSurvivesServerRecreation(t *testing.T) {
	code, token := reviewToken(t), reviewToken(t)
	store, h := reviewServer(t, control.Config{BootstrapCode: code}, nil)
	reviewBootstrap(t, h, code, token)
	req := contracts.OwnerAuthRequest{RequestID: "guess", Password: "incorrect-password", DeviceName: "guess", DeviceToken: reviewToken(t)}
	for i := 0; i < 5; i++ {
		if w := reviewRequest(h, "POST", "/v1/auth/login", "", "", req); w.Code != 401 {
			t.Fatalf("guess %d=%d", i, w.Code)
		}
	}
	srv, err := control.NewServer(store, control.Config{})
	if err != nil {
		t.Fatal(err)
	}
	req.Password = "owner-test-password"
	if w := reviewRequest(srv.Handler(), "POST", "/v1/auth/login", "", "", req); w.Code != 429 {
		t.Fatalf("durable rate limit=%d", w.Code)
	}
}

func TestReviewExtensionRollback(t *testing.T) {
	code, token := reviewToken(t), reviewToken(t)
	s, h := reviewServer(t, control.Config{BootstrapCode: code}, func(srv *control.Server) {
		if e := srv.RegisterCommand("review.fail", func(ctx context.Context, tx *sql.Tx, c control.CommandContext, r contracts.CommandRequest) (contracts.CommandResult, []contracts.Event, error) {
			_, e := tx.ExecContext(ctx, "INSERT INTO projects VALUES('partial',?,'partial','','active',1,'now','now')", c.Principal.WorkspaceID)
			if e != nil {
				return contracts.CommandResult{}, nil, e
			}
			return contracts.CommandResult{}, nil, errors.New("injected extension failure")
		}); e != nil {
			t.Fatal(e)
		}
	})
	reviewBootstrap(t, h, code, token)
	w := reviewRequest(h, "POST", "/v1/commands", token, "rollback", contracts.CommandRequest{Kind: "review.fail", Payload: json.RawMessage(`{}`)})
	if w.Code != 500 {
		t.Fatalf("failure status=%d", w.Code)
	}
	for _, table := range []string{"projects", "command_receipts", "command_work", "events"} {
		var n int
		if e := s.DB().QueryRow("SELECT count(*) FROM " + table).Scan(&n); e != nil {
			t.Fatal(e)
		}
		want := 0
		if table == "events" {
			want = 2
		}
		if n != want {
			t.Errorf("partial %s persisted", table)
		}
	}
}
func TestReviewCanonicalHashPreservesLargeIntegers(t *testing.T) {
	code, token := reviewToken(t), reviewToken(t)
	_, h := reviewServer(t, control.Config{BootstrapCode: code}, func(s *control.Server) {
		s.RegisterCommand("review.integer", func(ctx context.Context, tx *sql.Tx, c control.CommandContext, r contracts.CommandRequest) (contracts.CommandResult, []contracts.Event, error) {
			return contracts.CommandResult{}, nil, nil
		})
	})
	reviewBootstrap(t, h, code, token)
	for i, payload := range []string{`{"revision":9007199254740992}`, `{"revision":9007199254740993}`} {
		w := reviewRequest(h, "POST", "/v1/commands", token, "large", contracts.CommandRequest{Kind: "review.integer", Payload: json.RawMessage(payload)})
		want := 200
		if i == 1 {
			want = 409
		}
		if w.Code != want {
			t.Errorf("request %d status=%d want=%d: distinct integers collide", i, w.Code, want)
		}
	}
}
func TestReviewConfiguredRunnerRevocation(t *testing.T) {
	token := reviewToken(t)
	s, h := reviewServer(t, control.Config{RunnerToken: token, RunnerID: "review", RunnerWorkspaceIDs: []string{"default"}}, func(srv *control.Server) {
		if e := srv.RegisterRunnerCredential(context.Background(), token, "review", []string{"default"}, []string{"runner:claim"}); e != nil {
			t.Fatal(e)
		}
		srv.RegisterRunnerRoutes(func(mux *http.ServeMux) {
			mux.HandleFunc("GET /runner/v1/review", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
		})
	})
	if _, e := s.DB().Exec("UPDATE runner_credentials SET revoked_at='now'"); e != nil {
		t.Fatal(e)
	}
	if w := reviewRequest(h, "GET", "/runner/v1/review", token, "", nil); w.Code < 400 {
		t.Errorf("configured bearer remains authorized after persistent revocation: status=%d", w.Code)
	}
}

func TestReviewExplicitDeviceScopesAreNotExpanded(t *testing.T) {
	code, token := reviewToken(t), reviewToken(t)
	_, h := reviewServer(t, control.Config{BootstrapCode: code, DeviceScopes: []string{"client:command"}}, nil)
	reviewBootstrap(t, h, code, token)
	if w := reviewRequest(h, "GET", "/v1/snapshot", token, "", nil); w.Code != http.StatusForbidden {
		t.Errorf("restricted device gained read scope: %d", w.Code)
	}
	if w := reviewRequest(h, "POST", "/v1/commands", token, "project", contracts.CommandRequest{Kind: "project.create", Payload: json.RawMessage(`{"name":"allowed"}`)}); w.Code != http.StatusOK {
		t.Errorf("configured command grant was lost: %d", w.Code)
	}
	if w := reviewRequest(h, "POST", "/v1/commands", token, "approve-device", contracts.CommandRequest{Kind: "device.approve", Payload: json.RawMessage(`{"device_id":"anything"}`)}); w.Code < 400 {
		t.Errorf("device management was implicitly granted: %d", w.Code)
	}
}

func TestReviewLegacyDeviceApprovalCannotCreateSession(t *testing.T) {
	code, token := reviewToken(t), reviewToken(t)
	_, h := reviewServer(t, control.Config{BootstrapCode: code}, nil)
	reviewBootstrap(t, h, code, token)
	w := reviewRequest(h, "POST", "/v1/commands", token, "approve", contracts.CommandRequest{Kind: "device.approve", Payload: json.RawMessage(`{"device_id":"legacy"}`)})
	if w.Code < 400 {
		t.Fatal("legacy device approval remains available")
	}
}
func TestReviewSSERevocationBlocksNewEvents(t *testing.T) {
	code, token := reviewToken(t), reviewToken(t)
	s, h := reviewServer(t, control.Config{BootstrapCode: code}, nil)
	d := reviewBootstrap(t, h, code, token)
	live := httptest.NewServer(h)
	defer live.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	r, e := http.NewRequestWithContext(ctx, "GET", live.URL+"/v1/events", nil)
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("Authorization", "Bearer "+token)
	response, e := live.Client().Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("SSE=%d", response.StatusCode)
	}
	// Simulate the authoritative revoke transaction before a later workspace event.
	if _, e = s.DB().Exec("UPDATE devices SET status='revoked',revoked_at='now' WHERE id=?", d.Device.ID); e != nil {
		t.Fatal(e)
	}
	payload := `{"schema_version":1,"event_id":"after-revoke","cursor":"3","type":"review.after_revoke","occurred_at":"2026-10-21T00:00:00Z","workspace_id":"default","aggregate_type":"review","aggregate_id":"review","aggregate_version":1,"payload":{}}`
	if _, e = s.DB().Exec("INSERT INTO events(event_id,workspace_id,cursor,schema_version,event_type,occurred_at,aggregate_type,aggregate_id,aggregate_version,payload) VALUES('after-revoke','default',3,1,'review.after_revoke','now','review','review',1,?)", payload); e != nil {
		t.Fatal(e)
	}
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "review.after_revoke") {
			t.Error("revoked SSE credential received an event committed after revocation")
			break
		}
	}
}

func TestReviewLateCommitFailureRollsBackReceiptWorkAndOutbox(t *testing.T) {
	code, token := reviewToken(t), reviewToken(t)
	s, h := reviewServer(t, control.Config{BootstrapCode: code}, nil)
	reviewBootstrap(t, h, code, token)
	if _, e := s.DB().Exec(`CREATE TRIGGER review_abort_work BEFORE INSERT ON command_work BEGIN SELECT RAISE(ABORT,'injected final work failure'); END`); e != nil {
		t.Fatal(e)
	}
	w := reviewRequest(h, "POST", "/v1/commands", token, "late-failure", contracts.CommandRequest{Kind: "project.create", Payload: json.RawMessage(`{"name":"late"}`)})
	if w.Code != 500 {
		t.Fatalf("status=%d", w.Code)
	}
	for _, table := range []string{"projects", "command_receipts", "command_work", "events"} {
		var n int
		if e := s.DB().QueryRow("SELECT count(*) FROM " + table).Scan(&n); e != nil {
			t.Fatal(e)
		}
		want := 0
		if table == "events" {
			want = 2
		}
		if n != want {
			t.Errorf("%s survived failed work insertion", table)
		}
	}
	var cursor int
	if e := s.DB().QueryRow("SELECT current_cursor FROM event_counters WHERE workspace_id='default'").Scan(&cursor); e != nil {
		t.Fatal(e)
	}
	if cursor != 2 {
		t.Error("event cursor increment survived rollback")
	}
}

func TestReviewProtectedRoutesAndReceiptIsolation(t *testing.T) {
	code, token, runnerToken := reviewToken(t), reviewToken(t), reviewToken(t)
	s, h := reviewServer(t, control.Config{BootstrapCode: code, RunnerToken: runnerToken}, func(srv *control.Server) {
		srv.RegisterAuthenticatedRoutes(func(mux *http.ServeMux) {
			mux.HandleFunc("GET /v1/review", func(w http.ResponseWriter, r *http.Request) {
				p, ok := control.PrincipalFromContext(r.Context())
				if !ok || p.DeviceID == "" {
					t.Error("missing trusted principal")
				}
				w.WriteHeader(204)
			})
		})
		srv.RegisterRunnerRoutes(func(mux *http.ServeMux) {
			mux.HandleFunc("GET /runner/v1/review", func(w http.ResponseWriter, r *http.Request) {
				if _, ok := control.RunnerPrincipalFromContext(r.Context()); !ok {
					t.Error("missing runner principal")
				}
				w.WriteHeader(204)
			})
		})
	})
	reviewBootstrap(t, h, code, token)
	for _, test := range []struct {
		path, token string
		status      int
	}{{"/v1/review", "", 401}, {"/v1/review", runnerToken, 401}, {"/v1/review", token, 204}, {"/runner/v1/review", token, 401}, {"/runner/v1/review", runnerToken, 204}, {"/review", token, 404}} {
		if w := reviewRequest(h, "GET", test.path, test.token, "", nil); w.Code != test.status {
			t.Errorf("%s got=%d want=%d", test.path, w.Code, test.status)
		}
	}
	req := contracts.CommandRequest{Kind: "project.create", Payload: json.RawMessage(`{"name":"private workspace"}`)}
	w := reviewRequest(h, "POST", "/v1/commands", token, "shared-key", req)
	if w.Code != 200 {
		t.Fatal("command failed")
	}
	var receipt contracts.CommandReceipt
	if e := json.Unmarshal(w.Body.Bytes(), &receipt); e != nil {
		t.Fatal(e)
	}
	otherToken := reviewToken(t)
	raw, e := base64.RawURLEncoding.DecodeString(otherToken)
	if e != nil {
		t.Fatal(e)
	}
	hash := sha256.Sum256(raw)
	if _, e = s.DB().Exec("INSERT INTO workspaces VALUES('other','Other','now'); INSERT INTO event_counters VALUES('other',0)"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB().Exec("INSERT INTO devices(id,workspace_id,subject_id,name,status,token_hash,version,created_at) VALUES('other-device','other','other-subject','other','approved',?,1,'2026-10-21T00:00:00Z')", hash[:]); e != nil {
		t.Fatal(e)
	}
	// Select the persisted owner fixture to exercise row-level isolation below
	// password admission. Production login cannot create a second subject.
	if _, err := s.DB().Exec(`UPDATE owner_auth SET subject_id=? WHERE id=1`, "other-subject"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`UPDATE devices SET owner_generation=1 WHERE id=?`, "other-device"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/v1/commands/" + receipt.CommandID, "/v1/commands?key=shared-key"} {
		if w := reviewRequest(h, "GET", path, otherToken, "", nil); w.Code != 404 {
			t.Errorf("cross-scope receipt status=%d", w.Code)
		}
	}
	w = reviewRequest(h, "GET", "/v1/snapshot", otherToken, "", nil)
	if w.Code != 200 {
		t.Fatalf("other snapshot=%d", w.Code)
	}
	var snapshot contracts.Snapshot
	if e := json.Unmarshal(w.Body.Bytes(), &snapshot); e != nil {
		t.Fatal(e)
	}
	if len(snapshot.Projects) != 0 {
		t.Error("cross-workspace project disclosed")
	}
	payload, _ := json.Marshal(map[string]string{"project_id": receipt.ResourceID, "title": "cross", "goal": "cross", "source": "user"})
	if w := reviewRequest(h, "POST", "/v1/commands", otherToken, "cross-project", contracts.CommandRequest{Kind: "task.create", Payload: payload}); w.Code != 404 {
		t.Errorf("cross-workspace project reference=%d", w.Code)
	}
	if w := reviewRequest(h, "POST", "/v1/commands", otherToken, "shared-key", req); w.Code != 200 {
		t.Errorf("independent principal key rejected=%d", w.Code)
	}
}

func TestReviewApprovalCannotResurrectStoppedTask(t *testing.T) {
	code, token := reviewToken(t), reviewToken(t)
	cfg := control.Config{BootstrapCode: code, DefaultProfileID: "review", Profiles: map[string]control.RuntimeProfile{"review": {ID: "review", AOProjectID: "review-project", RuntimeRouteID: "review", ModelRouteID: "review", Role: "worker", AOAgentKind: "worker", IsolationPolicy: "review-policy"}}}
	s, h := reviewServer(t, cfg, nil)
	reviewBootstrap(t, h, code, token)
	w := reviewRequest(h, "POST", "/v1/commands", token, "create-task", contracts.CommandRequest{Kind: "task.create", Payload: json.RawMessage(`{"title":"review","goal":"review","source":"user"}`)})
	if w.Code != 200 {
		t.Fatalf("create=%d", w.Code)
	}
	var receipt contracts.CommandReceipt
	if e := json.Unmarshal(w.Body.Bytes(), &receipt); e != nil {
		t.Fatal(e)
	}
	if _, e := s.DB().Exec("INSERT INTO approvals(id,workspace_id,task_id,status,prompt,created_at,task_version) VALUES('pending','default',?,'pending','Review','2026-10-21T00:00:00Z',1)", receipt.ResourceID); e != nil {
		t.Fatal(e)
	}
	stop, _ := json.Marshal(map[string]string{"task_id": receipt.ResourceID})
	if w := reviewRequest(h, "POST", "/v1/commands", token, "stop-task", contracts.CommandRequest{Kind: "task.stop", Payload: stop}); w.Code != 200 {
		t.Fatalf("stop=%d", w.Code)
	}
	approve, _ := json.Marshal(map[string]string{"task_id": receipt.ResourceID, "approval_id": "pending", "decision": "approve"})
	w = reviewRequest(h, "POST", "/v1/commands", token, "approve-stopped", contracts.CommandRequest{Kind: "task.approve", Payload: approve})
	if w.Code < 400 {
		t.Errorf("pending approval revived canceled task: status=%d", w.Code)
	}
	var status string
	if e := s.DB().QueryRow("SELECT status FROM tasks WHERE id=?", receipt.ResourceID).Scan(&status); e != nil {
		t.Fatal(e)
	}
	if status != "canceled" {
		t.Errorf("stopped task became %s", status)
	}
}

func TestReviewConsumedLegacyBootstrapCannotSetOwnerPassword(t *testing.T) {
	code, token := reviewToken(t), reviewToken(t)
	store, h := reviewServer(t, control.Config{BootstrapCode: code}, nil)
	registered := reviewBootstrap(t, h, code, token)
	// Model the pre-password upgrade state: authoritative subject is retained,
	// but only a host recovery code may initialize the password now.
	if _, err := store.DB().Exec(`UPDATE owner_auth SET password_hash=NULL,password_salt=NULL,generation=generation+1; UPDATE devices SET status='revoked'`); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/v1/snapshot", "/v1/auth/session"} {
		if w := reviewRequest(h, "GET", path, token, "", nil); w.Code != 401 {
			t.Fatalf("legacy bearer allowed %s=%d", path, w.Code)
		}
	}
	req := contracts.OwnerAuthRequest{RequestID: "legacy-setup", Code: code, Password: "replacement-password", DeviceName: "legacy", DeviceToken: reviewToken(t)}
	if w := reviewRequest(h, "POST", "/v1/auth/setup", token, "", req); w.Code != 401 {
		t.Fatalf("consumed original code initialized password=%d", w.Code)
	}
	var subject, deviceSubject string
	var required bool
	if err := store.DB().QueryRow(`SELECT subject_id,password_hash IS NULL FROM owner_auth WHERE id=1`).Scan(&subject, &required); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRow(`SELECT subject_id FROM devices WHERE id=?`, registered.Device.ID).Scan(&deviceSubject); err != nil {
		t.Fatal(err)
	}
	if subject != deviceSubject || !required {
		t.Fatal("failed legacy setup changed owner authority")
	}
}

func TestReviewRevocationRecheckedInsideMutationTransactions(t *testing.T) {
	code, token := reviewToken(t), reviewToken(t)
	var store *storage.Store
	store, h := reviewServer(t, control.Config{BootstrapCode: code}, func(srv *control.Server) {
		srv.RegisterAuthenticatedRoutes(func(mux *http.ServeMux) {
			mux.HandleFunc("POST /v1/review-stale-session", func(w http.ResponseWriter, r *http.Request) {
				p, _ := control.PrincipalFromContext(r.Context())
				// Deterministically put a revoke commit between authentication middleware
				// and both mutation entry points, reproducing an in-flight logout race.
				if _, err := store.DB().ExecContext(r.Context(), `UPDATE devices SET status='revoked' WHERE id=?`, p.DeviceID); err != nil {
					t.Fatal(err)
				}
				called := false
				err := srv.DeviceTransaction(r.Context(), func(*sql.Tx, contracts.Principal) error { called = true; return nil })
				if api, ok := err.(*control.APIError); !ok || api.Status != 401 || called {
					t.Fatalf("stale DeviceTransaction=%v called=%v", err, called)
				}
				_, err = srv.ExecuteCommand(r.Context(), p, "stale-command", contracts.CommandRequest{Kind: "project.create", Payload: json.RawMessage(`{"name":"stale"}`)}, []byte("stale"))
				if api, ok := err.(*control.APIError); !ok || api.Status != 401 {
					t.Fatalf("stale ExecuteCommand=%v", err)
				}
				w.WriteHeader(200)
			})
		})
	})
	reviewBootstrap(t, h, code, token)
	if w := reviewRequest(h, "POST", "/v1/review-stale-session", token, "", nil); w.Code != 200 {
		t.Fatalf("stale request fixture=%d", w.Code)
	}
	var projects int
	if err := store.DB().QueryRow(`SELECT count(*) FROM projects`).Scan(&projects); err != nil || projects != 0 {
		t.Fatalf("stale mutation committed: %d %v", projects, err)
	}
}
