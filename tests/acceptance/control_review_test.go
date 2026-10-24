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
	w := reviewRequest(h, "POST", "/v1/bootstrap", "", "", contracts.BootstrapRequest{RequestID: "bootstrap", Code: code, DeviceName: "review", DeviceToken: token})
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
	retry := reviewRequest(h, "POST", "/v1/bootstrap", "", "", contracts.BootstrapRequest{RequestID: "bootstrap", Code: code, DeviceName: "review", DeviceToken: token})
	if retry.Code != 200 {
		t.Fatalf("lost ACK retry=%d", retry.Code)
	}
	var again contracts.DeviceRegistration
	json.Unmarshal(retry.Body.Bytes(), &again)
	if again.Device.ID != d.Device.ID {
		t.Error("bootstrap retry changed identity")
	}
	mismatch := reviewRequest(h, "POST", "/v1/bootstrap", "", "", contracts.BootstrapRequest{RequestID: "bootstrap", Code: code, DeviceName: "changed", DeviceToken: token})
	if mismatch.Code != 409 {
		t.Errorf("bootstrap mismatch=%d", mismatch.Code)
	}
	pendingToken := reviewToken(t)
	pair := reviewRequest(h, "POST", "/v1/pairings", "", "", contracts.PairingRequest{RequestID: "pairing", DeviceName: "pending", DeviceToken: pendingToken})
	if pair.Code != 201 {
		t.Fatalf("pair=%d", pair.Code)
	}
	var p contracts.PairingStatus
	json.Unmarshal(pair.Body.Bytes(), &p)
	for _, path := range []string{"/v1/snapshot", "/v1/commands?key=k", "/v1/pairings/other"} {
		w := reviewRequest(h, "GET", path, pendingToken, "", nil)
		if w.Code < 400 {
			t.Errorf("pending permitted %s", path)
		}
	}
	if w := reviewRequest(h, "GET", "/v1/pairings/"+p.PairingID, pendingToken, "", nil); w.Code != 200 {
		t.Errorf("own pairing status=%d", w.Code)
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
		if n != 1 {
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

func TestReviewPairingReusesExistingDeviceToken(t *testing.T) {
	code, token := reviewToken(t), reviewToken(t)
	s, h := reviewServer(t, control.Config{BootstrapCode: code}, nil)
	registered := reviewBootstrap(t, h, code, token)

	// The app can show the pairing screen after bootstrap. Reusing its device
	// token must return the approved identity rather than hit devices.token_hash
	// UNIQUE and surface a 500.
	retry := reviewRequest(h, "POST", "/v1/pairings", "", "", contracts.PairingRequest{
		RequestID: "pairing-after-bootstrap", DeviceName: "same-device", DeviceToken: token,
	})
	if retry.Code != http.StatusOK {
		t.Fatalf("existing approved pairing status=%d body=%s", retry.Code, retry.Body.String())
	}
	var approved contracts.PairingStatus
	if err := json.Unmarshal(retry.Body.Bytes(), &approved); err != nil {
		t.Fatal(err)
	}
	if approved.PairingID != registered.Device.ID || approved.Status != "approved" || approved.ApprovedAt == nil {
		t.Fatalf("existing approved pairing=%+v", approved)
	}
	var devices, authRequests int
	if err := s.DB().QueryRow("SELECT count(*) FROM devices").Scan(&devices); err != nil {
		t.Fatal(err)
	}
	if err := s.DB().QueryRow("SELECT count(*) FROM auth_requests").Scan(&authRequests); err != nil {
		t.Fatal(err)
	}
	if devices != 1 || authRequests != 1 {
		t.Fatalf("existing approved pairing created rows: devices=%d auth_requests=%d", devices, authRequests)
	}

	pendingToken := reviewToken(t)
	first := reviewRequest(h, "POST", "/v1/pairings", "", "", contracts.PairingRequest{
		RequestID: "pending-first", DeviceName: "pending", DeviceToken: pendingToken,
	})
	if first.Code != http.StatusCreated {
		t.Fatalf("new pairing status=%d body=%s", first.Code, first.Body.String())
	}
	var pending contracts.PairingStatus
	if err := json.Unmarshal(first.Body.Bytes(), &pending); err != nil {
		t.Fatal(err)
	}
	second := reviewRequest(h, "POST", "/v1/pairings", "", "", contracts.PairingRequest{
		RequestID: "pending-retry", DeviceName: "renamed-locally", DeviceToken: pendingToken,
	})
	if second.Code != http.StatusOK {
		t.Fatalf("existing pending pairing status=%d body=%s", second.Code, second.Body.String())
	}
	var pendingRetry contracts.PairingStatus
	if err := json.Unmarshal(second.Body.Bytes(), &pendingRetry); err != nil {
		t.Fatal(err)
	}
	if pendingRetry.PairingID != pending.PairingID || pendingRetry.Status != "pending" || !pendingRetry.ExpiresAt.Equal(pending.ExpiresAt) {
		t.Fatalf("existing pending pairing changed: first=%+v retry=%+v", pending, pendingRetry)
	}

	if _, err := s.DB().Exec("UPDATE devices SET pairing_expires_at=? WHERE id=?", time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), pending.PairingID); err != nil {
		t.Fatal(err)
	}
	expired := reviewRequest(h, "POST", "/v1/pairings", "", "", contracts.PairingRequest{
		RequestID: "pending-expired-retry", DeviceName: "pending", DeviceToken: pendingToken,
	})
	if expired.Code != http.StatusGone {
		t.Fatalf("expired pending retry status=%d body=%s", expired.Code, expired.Body.String())
	}
	expiredOriginal := reviewRequest(h, "POST", "/v1/pairings", "", "", contracts.PairingRequest{
		RequestID: "pending-first", DeviceName: "pending", DeviceToken: pendingToken,
	})
	if expiredOriginal.Code != http.StatusGone {
		t.Fatalf("expired original pairing retry status=%d body=%s", expiredOriginal.Code, expiredOriginal.Body.String())
	}

	if _, err := s.DB().Exec("UPDATE devices SET status='revoked',revoked_at=? WHERE id=?", time.Now().UTC().Format(time.RFC3339Nano), pending.PairingID); err != nil {
		t.Fatal(err)
	}
	revoked := reviewRequest(h, "POST", "/v1/pairings", "", "", contracts.PairingRequest{
		RequestID: "pending-revoked-retry", DeviceName: "pending", DeviceToken: pendingToken,
	})
	if revoked.Code != http.StatusConflict || !bytes.Contains(revoked.Body.Bytes(), []byte(`"code":"device_revoked"`)) {
		t.Fatalf("revoked pairing status=%d body=%s", revoked.Code, revoked.Body.String())
	}
	revokedOriginal := reviewRequest(h, "POST", "/v1/pairings", "", "", contracts.PairingRequest{
		RequestID: "pending-first", DeviceName: "pending", DeviceToken: pendingToken,
	})
	if revokedOriginal.Code != http.StatusConflict || !bytes.Contains(revokedOriginal.Body.Bytes(), []byte(`"code":"device_revoked"`)) {
		t.Fatalf("revoked original pairing retry status=%d body=%s", revokedOriginal.Code, revokedOriginal.Body.String())
	}

	// request_id is shared by bootstrap and pairing records; reject a cross-kind
	// reuse as a typed conflict instead of surfacing SQLite's PRIMARY KEY error.
	conflict := reviewRequest(h, "POST", "/v1/pairings", "", "", contracts.PairingRequest{
		RequestID: "bootstrap", DeviceName: "another", DeviceToken: reviewToken(t),
	})
	if conflict.Code != http.StatusConflict || !bytes.Contains(conflict.Body.Bytes(), []byte(`"code":"idempotency_conflict"`)) {
		t.Fatalf("cross-kind request reuse status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	conflictWithExistingToken := reviewRequest(h, "POST", "/v1/pairings", "", "", contracts.PairingRequest{
		RequestID: "bootstrap", DeviceName: "another", DeviceToken: token,
	})
	if conflictWithExistingToken.Code != http.StatusConflict || !bytes.Contains(conflictWithExistingToken.Body.Bytes(), []byte(`"code":"idempotency_conflict"`)) {
		t.Fatalf("cross-kind existing-token reuse status=%d body=%s", conflictWithExistingToken.Code, conflictWithExistingToken.Body.String())
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
		if n != 0 {
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
	if w := reviewRequest(h, "POST", "/v1/commands", token, "approve-device", contracts.CommandRequest{Kind: "device.approve", Payload: json.RawMessage(`{"device_id":"anything"}`)}); w.Code != http.StatusForbidden {
		t.Errorf("device management was implicitly granted: %d", w.Code)
	}
}

func TestReviewExpiredPairingCannotBeApproved(t *testing.T) {
	code, token := reviewToken(t), reviewToken(t)
	_, h := reviewServer(t, control.Config{BootstrapCode: code, PairingTTL: time.Nanosecond}, nil)
	reviewBootstrap(t, h, code, token)
	w := reviewRequest(h, "POST", "/v1/pairings", "", "", contracts.PairingRequest{RequestID: "expire", DeviceName: "expired", DeviceToken: reviewToken(t)})
	if w.Code != 201 {
		t.Fatalf("pair status=%d", w.Code)
	}
	var pair contracts.PairingStatus
	if e := json.Unmarshal(w.Body.Bytes(), &pair); e != nil {
		t.Fatal(e)
	}
	time.Sleep(time.Millisecond)
	payload, _ := json.Marshal(map[string]string{"device_id": pair.PairingID})
	w = reviewRequest(h, "POST", "/v1/commands", token, "approve-expired", contracts.CommandRequest{Kind: "device.approve", Payload: payload})
	if w.Code < 400 {
		t.Errorf("expired pairing approved: status=%d", w.Code)
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
	payload := `{"schema_version":1,"event_id":"after-revoke","cursor":"1","type":"review.after_revoke","occurred_at":"2026-10-21T00:00:00Z","workspace_id":"default","aggregate_type":"review","aggregate_id":"review","aggregate_version":1,"payload":{}}`
	if _, e = s.DB().Exec("INSERT INTO events(event_id,workspace_id,cursor,schema_version,event_type,occurred_at,aggregate_type,aggregate_id,aggregate_version,payload) VALUES('after-revoke','default',1,1,'review.after_revoke','now','review','review',1,?)", payload); e != nil {
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
		if n != 0 {
			t.Errorf("%s survived failed work insertion", table)
		}
	}
	var cursor int
	if e := s.DB().QueryRow("SELECT current_cursor FROM event_counters WHERE workspace_id='default'").Scan(&cursor); e != nil {
		t.Fatal(e)
	}
	if cursor != 0 {
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
