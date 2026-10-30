package acceptance

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ronigooja/Zoe/contracts"
	"github.com/ronigooja/Zoe/internal/control"
	"github.com/ronigooja/Zoe/internal/storage"
	"github.com/ronigooja/Zoe/internal/voice"
)

func newHangupHarness(t *testing.T, gatewayHandler http.Handler) (*storage.Store, http.Handler, string) {
	return newHangupHarnessWithMedia(t, gatewayHandler, nil)
}

func newHangupHarnessWithMedia(t *testing.T, gatewayHandler http.Handler, media voice.AccountMediaLifecycle) (*storage.Store, http.Handler, string) {
	t.Helper()
	ctx := context.Background()
	store, err := storage.Open(ctx, storage.Config{Path: filepath.Join(t.TempDir(), "voice-hangup.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := storage.RegisterCoreMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err := control.RegisterInstanceMaintenanceMigration(store); err != nil {
		t.Fatal(err)
	}
	gateway := httptest.NewTLSServer(gatewayHandler)
	t.Cleanup(gateway.Close)
	bootstrapCode, deviceToken := reviewToken(t), reviewToken(t)
	server, err := control.NewServer(store, control.Config{InstanceScopes: []string{"client:command", "voice:pool:reconcile"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := voice.Register(server, store, voice.Config{GatewayURL: gateway.URL, GatewayAPIKey: "test-key", HTTPClient: gateway.Client(), AccountMediaLifecycle: media}); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	registration := reviewInitializeInstance(t, bindReviewHandler(server.Handler(), store), bootstrapCode, deviceToken)
	var ownerID string
	if err := store.DB().QueryRowContext(ctx, `SELECT subject_id FROM actors WHERE id=?`, registration.Device.ID).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO voice_sessions(id,workspace_id,owner_id,device_id,request_id,request_hash,generation,state,gateway_session_id,gateway_media_generation,created_at,updated_at) VALUES('hangup-session','default',?,?,'hangup-request',X'01',1,'active','gateway-session',1,?,?)`, ownerID, registration.Device.ID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE voice_pool_state SET blocked=1,reason_code='active_session',source_session_id='hangup-session' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	return store, server.Handler(), deviceToken
}

type retryAccountMedia struct{ releases atomic.Int32 }

func (r *retryAccountMedia) Capabilities() voice.AccountMediaCapabilities {
	return voice.AccountMediaCapabilities{AccountLeaseIdentity: true, AccountScopedTermination: true, TrustedTerminationEvidence: true}
}

func (r *retryAccountMedia) ReleaseAccountLease(_ context.Context, lease voice.AccountLeaseReference, _ string) (voice.AccountTerminationObservation, error) {
	if lease.LeaseID != "test-lease" || lease.Generation != 8 || lease.SessionID != "gateway-session" || lease.SessionGeneration != 1 {
		return voice.AccountTerminationObservation{}, fmt.Errorf("unexpected account lease release reference")
	}
	if r.releases.Add(1) == 1 {
		return voice.AccountTerminationObservation{}, fmt.Errorf("temporary release uncertainty")
	}
	return voice.AccountTerminationObservation{AccountID: "test-account", LeaseID: lease.LeaseID, LeaseGeneration: lease.Generation, SessionID: lease.SessionID, SessionGeneration: lease.SessionGeneration, Terminated: true, EvidenceID: "retry-evidence", ObservedAt: time.Now().UTC()}, nil
}

func hangupRequest(handler http.Handler, token string) *httptest.ResponseRecorder {
	return reviewRequest(handler, http.MethodPost, "/v1/voice/sessions/hangup-session/hangup", token, "", map[string]any{"media_closed": true, "generation": 1, "account_lease_generation": 0})
}

type gatedAccountMedia struct {
	entered chan struct{}
	resume  chan struct{}
}

func (g gatedAccountMedia) Capabilities() voice.AccountMediaCapabilities {
	return voice.AccountMediaCapabilities{AccountLeaseIdentity: true, AccountScopedTermination: true, TrustedTerminationEvidence: true}
}

func (g gatedAccountMedia) ReleaseAccountLease(ctx context.Context, lease voice.AccountLeaseReference, idempotencyKey string) (voice.AccountTerminationObservation, error) {
	if lease.LeaseID != "test-lease" || lease.Generation != 8 || lease.SessionID != "gateway-session" || lease.SessionGeneration != 3 || idempotencyKey == "" {
		return voice.AccountTerminationObservation{}, fmt.Errorf("unexpected account lease release reference")
	}
	close(g.entered)
	select {
	case <-g.resume:
	case <-ctx.Done():
		return voice.AccountTerminationObservation{}, ctx.Err()
	}
	return voice.AccountTerminationObservation{AccountID: "test-account", LeaseID: lease.LeaseID, LeaseGeneration: lease.Generation, SessionID: lease.SessionID, SessionGeneration: lease.SessionGeneration, Terminated: true, EvidenceID: "test-evidence", ObservedAt: time.Now().UTC()}, nil
}

func TestVoiceReconciliationCannotRaceInFlightTrustedHangup(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, storage.Config{Path: filepath.Join(t.TempDir(), "voice.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := storage.RegisterCoreMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err := control.RegisterInstanceMaintenanceMigration(store); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	gateway := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/v1/voice/sessions/gateway-session" {
			t.Errorf("unexpected gateway request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer gateway.Close()
	adapter := gatedAccountMedia{entered: make(chan struct{}), resume: make(chan struct{})}
	bootstrapCode, deviceToken := reviewToken(t), reviewToken(t)
	server, err := control.NewServer(store, control.Config{InstanceScopes: []string{"client:command", "voice:pool:reconcile"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := voice.Register(server, store, voice.Config{GatewayURL: gateway.URL, GatewayAPIKey: "test-key", HTTPClient: gateway.Client(), AccountMediaLifecycle: adapter}); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	handler := bindReviewHandler(server.Handler(), store)
	registration := reviewInitializeInstance(t, handler, bootstrapCode, deviceToken)
	var ownerID string
	if err := store.DB().QueryRowContext(ctx, `SELECT subject_id FROM actors WHERE id=?`, registration.Device.ID).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	accountHash := sha256.Sum256([]byte("test-account"))
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO voice_sessions(id,workspace_id,owner_id,device_id,request_id,request_hash,generation,state,gateway_session_id,account_lease_id,account_lease_generation,account_id_hash,gateway_media_generation,created_at,updated_at) VALUES('session-race','default',?,?,'request-race',X'01',3,'active','gateway-session','test-lease',8,?,?,?,?)`, ownerID, registration.Device.ID, accountHash[:], 3, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE voice_pool_state SET blocked=1,reason_code='active_session',source_session_id='session-race' WHERE id=1`); err != nil {
		t.Fatal(err)
	}

	hangupResponse := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		hangupResponse <- reviewRequest(handler, http.MethodPost, "/v1/voice/sessions/session-race/hangup", deviceToken, "", map[string]any{"media_closed": true, "generation": 3, "account_lease_id": "test-lease", "account_lease_generation": 8})
	}()
	select {
	case <-adapter.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("hangup did not reach trusted termination check")
	}

	reconcile := func(key string) *httptest.ResponseRecorder {
		payload, _ := json.Marshal(map[string]any{"session_id": "session-race", "generation": 3, "reason": "Verified termination outside gateway", "confirmed_out_of_band_termination": true})
		return reviewRequest(handler, http.MethodPost, "/v1/commands", deviceToken, key, contracts.CommandRequest{Kind: "voice.pool.reconcile", Payload: payload})
	}
	if response := reconcile("reconcile-during-hangup"); response.Code != http.StatusConflict {
		t.Fatalf("reconciliation during external check=%d %s, want conflict", response.Code, response.Body.String())
	}
	var blocked int
	if err := store.DB().QueryRowContext(ctx, `SELECT blocked FROM voice_pool_state WHERE id=1`).Scan(&blocked); err != nil || blocked != 1 {
		t.Fatalf("pool fence cleared while termination check was pending: blocked=%d err=%v", blocked, err)
	}

	close(adapter.resume)
	select {
	case response := <-hangupResponse:
		if response.Code != http.StatusOK {
			t.Fatalf("trusted hangup=%d %s", response.Code, response.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("hangup did not complete after trusted termination result")
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT state FROM voice_sessions WHERE id='session-race'`).Scan(&ownerID); err != nil || ownerID != "ended" {
		t.Fatalf("session did not finalize after trusted check: state=%q err=%v", ownerID, err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT blocked FROM voice_pool_state WHERE id=1`).Scan(&blocked); err != nil || blocked != 0 {
		t.Fatalf("verified termination did not release pool fence: blocked=%d err=%v", blocked, err)
	}
	var evidenceRows int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM voice_account_terminations WHERE session_id='session-race' AND generation=3 AND evidence_id='test-evidence'`).Scan(&evidenceRows); err != nil || evidenceRows != 1 {
		t.Fatalf("termination evidence rows=%d err=%v", evidenceRows, err)
	}
}

func TestConcurrentHangupClaimsSendOneGatewayDelete(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var deletes atomic.Int32
	store, handler, token := newHangupHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/v1/voice/sessions/gateway-session" {
			t.Errorf("unexpected gateway request: %s %s", r.Method, r.URL.Path)
		}
		deletes.Add(1)
		close(entered)
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))

	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { firstDone <- hangupRequest(handler, token) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first hangup did not reach gateway")
	}
	second := hangupRequest(handler, token)
	if second.Code != http.StatusConflict {
		t.Fatalf("concurrent hangup=%d %s, want conflict while first request owns ending state", second.Code, second.Body.String())
	}
	close(release)
	select {
	case first := <-firstDone:
		if first.Code != http.StatusAccepted {
			t.Fatalf("first unverified hangup=%d %s, want accepted with pool quarantine", first.Code, first.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first hangup did not finish after gateway response")
	}
	if got := deletes.Load(); got != 1 {
		t.Fatalf("gateway DELETE count=%d, want 1", got)
	}
	var state string
	var blocked int
	if err := store.DB().QueryRow(`SELECT state FROM voice_sessions WHERE id='hangup-session'`).Scan(&state); err != nil || state != "quarantined" {
		t.Fatalf("session final state=%q err=%v", state, err)
	}
	if err := store.DB().QueryRow(`SELECT blocked FROM voice_pool_state WHERE id=1`).Scan(&blocked); err != nil || blocked != 1 {
		t.Fatalf("unverified termination did not preserve pool fence: blocked=%d err=%v", blocked, err)
	}
}

func TestHangupRejectsStaleMediaGenerationBeforeGatewayRelease(t *testing.T) {
	var deletes atomic.Int32
	store, handler, token := newHangupHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deletes.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer store.Close()
	for _, body := range []map[string]any{
		{"media_closed": true, "generation": 2, "account_lease_generation": 0},
		{"media_closed": true, "generation": 1, "account_lease_id": "wrong-lease", "account_lease_generation": 9},
	} {
		response := reviewRequest(handler, http.MethodPost, "/v1/voice/sessions/hangup-session/hangup", token, "", body)
		if response.Code != http.StatusConflict {
			t.Fatalf("stale hangup=%d %s, want generation conflict", response.Code, response.Body.String())
		}
	}
	if deletes.Load() != 0 {
		t.Fatalf("stale hangup sent %d external release requests", deletes.Load())
	}
}

func TestHangupDatabaseTransitionFailurePreventsGatewayDelete(t *testing.T) {
	var deletes atomic.Int32
	store, handler, token := newHangupHarness(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		deletes.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	if _, err := store.DB().Exec(`CREATE TRIGGER reject_voice_ending BEFORE UPDATE OF state ON voice_sessions WHEN NEW.state='ending' BEGIN SELECT RAISE(ABORT,'forced transition failure'); END`); err != nil {
		t.Fatal(err)
	}
	response := hangupRequest(handler, token)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("hangup with failed durable transition=%d %s, want internal error", response.Code, response.Body.String())
	}
	if got := deletes.Load(); got != 0 {
		t.Fatalf("gateway DELETE count=%d after local transition failure, want 0", got)
	}
	var state string
	var blocked int
	if err := store.DB().QueryRow(`SELECT state FROM voice_sessions WHERE id='hangup-session'`).Scan(&state); err != nil || state != "active" {
		t.Fatalf("failed transition changed session state=%q err=%v", state, err)
	}
	if err := store.DB().QueryRow(`SELECT blocked FROM voice_pool_state WHERE id=1`).Scan(&blocked); err != nil || blocked != 1 {
		t.Fatalf("pool fence changed after local transition failure: blocked=%d err=%v", blocked, err)
	}
}

func TestHangupRecoveryDoesNotRepeatDeleteFromEndingState(t *testing.T) {
	var deletes atomic.Int32
	store, handler, token := newHangupHarness(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		deletes.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	if _, err := store.DB().Exec(`UPDATE voice_sessions SET state='ending' WHERE id='hangup-session'`); err != nil {
		t.Fatal(err)
	}
	response := hangupRequest(handler, token)
	if response.Code != http.StatusConflict {
		t.Fatalf("recovered ending hangup=%d %s, want unresolved conflict", response.Code, response.Body.String())
	}
	if got := deletes.Load(); got != 0 {
		t.Fatalf("gateway DELETE count=%d for unresolved ending session, want 0", got)
	}
	var state string
	var blocked int
	if err := store.DB().QueryRow(`SELECT state FROM voice_sessions WHERE id='hangup-session'`).Scan(&state); err != nil || state != "ending" {
		t.Fatalf("unresolved session state=%q err=%v", state, err)
	}
	if err := store.DB().QueryRow(`SELECT blocked FROM voice_pool_state WHERE id=1`).Scan(&blocked); err != nil || blocked != 1 {
		t.Fatalf("unresolved ending session lost pool fence: blocked=%d err=%v", blocked, err)
	}
}

func TestHangupUnknownGatewayDeleteOutcomeIsNotAutomaticallyRetried(t *testing.T) {
	var deletes atomic.Int32
	store, handler, token := newHangupHarness(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		deletes.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	first := hangupRequest(handler, token)
	if first.Code != http.StatusServiceUnavailable {
		t.Fatalf("first uncertain gateway release=%d %s, want unavailable", first.Code, first.Body.String())
	}
	var state, reason string
	var unknown, blocked int
	if err := store.DB().QueryRow(`SELECT state,last_error_code,gateway_unbind_unknown FROM voice_sessions WHERE id='hangup-session'`).Scan(&state, &reason, &unknown); err != nil || state != "quarantined" || reason != "gateway_release_failed" || unknown != 1 {
		t.Fatalf("uncertain release state=%q reason=%q unknown=%d err=%v", state, reason, unknown, err)
	}
	second := hangupRequest(handler, token)
	if second.Code != http.StatusAccepted {
		t.Fatalf("quarantined retry=%d %s, want accepted but still isolated", second.Code, second.Body.String())
	}
	if got := deletes.Load(); got != 1 {
		t.Fatalf("gateway DELETE count after uncertain outcome=%d, want exactly one attempt", got)
	}
	if err := store.DB().QueryRow(`SELECT state,last_error_code FROM voice_sessions WHERE id='hangup-session'`).Scan(&state, &reason); err != nil || state != "quarantined" || reason != "gateway_unbind_unknown" {
		t.Fatalf("retry finalized or obscured unverified gateway outcome: state=%q reason=%q err=%v", state, reason, err)
	}
	if err := store.DB().QueryRow(`SELECT blocked,reason_code FROM voice_pool_state WHERE id=1`).Scan(&blocked, &reason); err != nil || blocked != 1 || reason != "gateway_unbind_unknown" {
		t.Fatalf("unknown gateway outcome lost its pool quarantine reason: blocked=%d reason=%q err=%v", blocked, reason, err)
	}
}

func TestHangupRetryAfterGatewayUnbindRetriesOnlyAccountRelease(t *testing.T) {
	var deletes atomic.Int32
	media := &retryAccountMedia{}
	store, handler, token := newHangupHarnessWithMedia(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		deletes.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}), media)
	accountHash := sha256.Sum256([]byte("test-account"))
	if _, err := store.DB().Exec(`UPDATE voice_sessions SET account_lease_id='test-lease',account_lease_generation=8,account_id_hash=?,gateway_media_generation=1 WHERE id='hangup-session'`, accountHash[:]); err != nil {
		t.Fatal(err)
	}
	request := func() *httptest.ResponseRecorder {
		return reviewRequest(handler, http.MethodPost, "/v1/voice/sessions/hangup-session/hangup", token, "", map[string]any{"media_closed": true, "generation": 1, "account_lease_id": "test-lease", "account_lease_generation": 8})
	}
	first := request()
	if first.Code != http.StatusAccepted {
		t.Fatalf("first unverified account release=%d %s, want accepted quarantine", first.Code, first.Body.String())
	}
	var state, reason string
	var blocked int
	if err := store.DB().QueryRow(`SELECT state FROM voice_sessions WHERE id='hangup-session'`).Scan(&state); err != nil || state != "quarantined" {
		t.Fatalf("uncertain release state=%q err=%v", state, err)
	}
	if err := store.DB().QueryRow(`SELECT blocked,reason_code FROM voice_pool_state WHERE id=1`).Scan(&blocked, &reason); err != nil || blocked != 1 || reason != "upstream_termination_unverified" {
		t.Fatalf("unverified release pool state blocked=%d reason=%q err=%v", blocked, reason, err)
	}
	second := request()
	if second.Code != http.StatusOK {
		t.Fatalf("explicit quarantined retry=%d %s, want trusted release", second.Code, second.Body.String())
	}
	if got := deletes.Load(); got != 1 {
		t.Fatalf("gateway DELETE count=%d after account release retry, want exactly 1", got)
	}
	if got := media.releases.Load(); got != 2 {
		t.Fatalf("account release calls=%d after retry, want 2", got)
	}
	if err := store.DB().QueryRow(`SELECT state FROM voice_sessions WHERE id='hangup-session'`).Scan(&state); err != nil || state != "ended" {
		t.Fatalf("retry final session state=%q err=%v", state, err)
	}
	if err := store.DB().QueryRow(`SELECT blocked FROM voice_pool_state WHERE id=1`).Scan(&blocked); err != nil || blocked != 0 {
		t.Fatalf("trusted account release did not clear pool fence: blocked=%d err=%v", blocked, err)
	}
}

func TestHangupPersistsUnknownGatewayOutcomeBeforeReportingQuarantine(t *testing.T) {
	var deletes atomic.Int32
	store, handler, token := newHangupHarness(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		deletes.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	if _, err := store.DB().Exec(`CREATE TRIGGER reject_voice_quarantine BEFORE UPDATE OF state ON voice_sessions WHEN NEW.state='quarantined' BEGIN SELECT RAISE(ABORT,'forced quarantine persistence failure'); END`); err != nil {
		t.Fatal(err)
	}
	response := hangupRequest(handler, token)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("hangup with failed outcome persistence=%d %s, want internal error", response.Code, response.Body.String())
	}
	if got := deletes.Load(); got != 1 {
		t.Fatalf("gateway DELETE count=%d, want one attempted release", got)
	}
	var state string
	var blocked int
	if err := store.DB().QueryRow(`SELECT state FROM voice_sessions WHERE id='hangup-session'`).Scan(&state); err != nil || state != "ending" {
		t.Fatalf("unpersisted unknown result state=%q, want unresolved ending, err=%v", state, err)
	}
	if err := store.DB().QueryRow(`SELECT blocked FROM voice_pool_state WHERE id=1`).Scan(&blocked); err != nil || blocked != 1 {
		t.Fatalf("failed outcome persistence cleared pool fence: blocked=%d err=%v", blocked, err)
	}
}
