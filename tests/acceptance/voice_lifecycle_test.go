package acceptance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/control"
	"github.com/ronigooja/Cylo/internal/storage"
	"github.com/ronigooja/Cylo/internal/voice"
)

type gatedAccountMedia struct {
	entered chan struct{}
	resume  chan struct{}
}

func (g gatedAccountMedia) Capabilities() voice.AccountMediaCapabilities {
	return voice.AccountMediaCapabilities{AccountLeaseIdentity: true, AccountScopedTermination: true, TrustedTerminationEvidence: true}
}

func (g gatedAccountMedia) ObserveTermination(ctx context.Context, sessionID string) (voice.AccountTerminationObservation, error) {
	close(g.entered)
	select {
	case <-g.resume:
	case <-ctx.Done():
		return voice.AccountTerminationObservation{}, ctx.Err()
	}
	return voice.AccountTerminationObservation{AccountID: "test-account", LeaseID: "test-lease", SessionID: sessionID, Terminated: true, EvidenceID: "test-evidence", ObservedAt: time.Now().UTC()}, nil
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
	server, err := control.NewServer(store, control.Config{BootstrapCode: bootstrapCode, DeviceScopes: []string{"client:command", "voice:pool:reconcile"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := voice.Register(server, store, voice.Config{GatewayURL: gateway.URL, GatewayAPIKey: "test-key", HTTPClient: gateway.Client(), AccountMediaLifecycle: adapter}); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	registration := reviewBootstrap(t, handler, bootstrapCode, deviceToken)
	var ownerID string
	if err := store.DB().QueryRowContext(ctx, `SELECT subject_id FROM devices WHERE id=?`, registration.Device.ID).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO voice_sessions(id,workspace_id,owner_id,device_id,request_id,request_hash,generation,state,gateway_session_id,created_at,updated_at) VALUES('session-race','default',?,?,'request-race',X'01',3,'active','gateway-session',?,?)`, ownerID, registration.Device.ID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE voice_pool_state SET blocked=1,reason_code='active_session',source_session_id='session-race' WHERE id=1`); err != nil {
		t.Fatal(err)
	}

	hangupResponse := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		hangupResponse <- reviewRequest(handler, http.MethodPost, "/v1/voice/sessions/session-race/hangup", deviceToken, "", map[string]bool{"media_closed": true})
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
