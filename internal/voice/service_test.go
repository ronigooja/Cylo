package voice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/control"
	"github.com/ronigooja/Cylo/internal/storage"
)

func newVoiceTestService(t *testing.T, cfg Config) (*storage.Store, *Service) {
	t.Helper()
	ctx := context.Background()
	store, err := storage.Open(ctx, storage.Config{Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := storage.RegisterCoreMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err := RegisterMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return store, newService(store.DB(), cfg)
}

func TestVoiceTurnCommitRequiresConfirmationAndDeduplicatesStableRevision(t *testing.T) {
	ctx := context.Background()
	store, svc := newVoiceTestService(t, Config{})
	_, err := store.DB().ExecContext(ctx, `INSERT INTO voice_sessions(id,workspace_id,owner_id,device_id,request_id,request_hash,generation,state,created_at,updated_at) VALUES('session-1','default','user-1','device-1','request-1',X'01',1,'active',?,?)`, time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	principal := contracts.Principal{WorkspaceID: "default", SubjectID: "user-1", DeviceID: "device-1", Scopes: []string{"voice:turn:commit"}}
	command := func(confirmed bool, text string) (contracts.CommandResult, []contracts.Event, error) {
		tx, err := store.DB().BeginTx(ctx, nil)
		if err != nil {
			return contracts.CommandResult{}, nil, err
		}
		payload, _ := json.Marshal(commitTurnPayload{SessionID: "session-1", Generation: 1, TurnID: "turn-a", Revision: 2, ProviderMessage: "provider-message-a", Content: text, UserConfirmed: confirmed})
		result, events, err := svc.commandCommitTurn(ctx, tx, control.CommandContext{CommandID: "command-a", Principal: principal, AcceptedAt: time.Now().UTC()}, contracts.CommandRequest{Payload: payload})
		if err != nil {
			_ = tx.Rollback()
			return result, events, err
		}
		if err := tx.Commit(); err != nil {
			return result, events, err
		}
		return result, events, nil
	}
	if _, _, err := command(false, "请创建一个任务"); err == nil {
		t.Fatal("unconfirmed voice text was accepted as a committed turn")
	}
	first, events, err := command(true, "请创建一个任务")
	if err != nil || first.ResourceID == "" || len(events) != 1 || events[0].Type != "voice.turn.committed" {
		t.Fatalf("confirmed turn not durably proposed: result=%#v events=%#v err=%v", first, events, err)
	}
	second, events, err := command(true, "请创建一个任务")
	if err != nil || second.ResourceID != first.ResourceID || len(events) != 0 {
		t.Fatalf("duplicate stable revision was not deduplicated: result=%#v events=%#v err=%v", second, events, err)
	}
	if _, _, err := command(true, "请创建另一个任务"); err == nil {
		t.Fatal("same turn revision with different content did not conflict")
	}
	var tasks int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks`).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if tasks != 0 {
		t.Fatalf("voice turn directly created %d task(s); task.create must remain a separate command", tasks)
	}
}

func TestVoiceTaskAdmissionBindsStableTurnAcrossRevisions(t *testing.T) {
	ctx := context.Background()
	store, svc := newVoiceTestService(t, Config{})
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO voice_sessions(id,workspace_id,owner_id,device_id,request_id,request_hash,generation,state,created_at,updated_at) VALUES('session-atomic','default','user-1','device-1','request-atomic',X'01',1,'active',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO voice_turns(id,workspace_id,session_id,generation,turn_id,revision,content,content_hash,confirmed_at,command_id) VALUES('turn-1','default','session-atomic',1,'stable-turn',1,'authoritative transcript',X'01',?,'turn-command'),('turn-2','default','session-atomic',1,'stable-turn',2,'edited transcript',X'02',?,'turn-command-2')`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO actors(id,workspace_id,subject_id,name,status,created_at) VALUES('device-1','default','user-1','test','approved',?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO tasks(id,workspace_id,title,goal,source,profile_id,status,version,created_at,updated_at,created_by_device_id,command_id,owner_subject_id) VALUES('task-1','default','client title','client goal','voice','default','queued',1,?,?,'device-1','create-1','user-1')`, now, now); err != nil {
		t.Fatal(err)
	}
	principal := contracts.Principal{WorkspaceID: "default", SubjectID: "user-1", DeviceID: "device-1"}
	ref, _ := json.Marshal(voiceTaskSourceReference{Kind: "voice_turn", SessionID: "session-atomic", Generation: 1, TurnID: "stable-turn", Revision: 1})
	request := &control.TaskSourceRequest{Title: "client title", Goal: "client goal", InitialMessage: "client message"}
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	events, err := svc.admitVoiceTask(ctx, tx, principal, "task-1", request, ref, time.Now().UTC())
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if request.Title != "authoritative transcript" || request.Goal != "authoritative transcript" || request.InitialMessage != "authoritative transcript" || len(events) != 1 || events[0].Type != "voice.task.created" {
		t.Fatalf("task not derived from confirmed transcript: request=%#v events=%#v", request, events)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	ref, _ = json.Marshal(voiceTaskSourceReference{Kind: "voice_turn", SessionID: "session-atomic", Generation: 1, TurnID: "stable-turn", Revision: 2})
	otherTask := &control.TaskSourceRequest{Title: "other", Goal: "other", InitialMessage: "other"}
	secondTx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.admitVoiceTask(ctx, secondTx, principal, "task-2", otherTask, ref, time.Now().UTC())
	_ = secondTx.Rollback()
	var apiErr *control.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "voice_turn_already_dispatched" {
		t.Fatalf("later revision created another task for stable turn: err=%v", err)
	}
}

func TestVoiceSessionRequiresConfiguredSpaceGateway(t *testing.T) {
	store, svc := newVoiceTestService(t, Config{})
	_, err := svc.create(context.Background(), contracts.Principal{WorkspaceID: "default", SubjectID: "user-1", DeviceID: "device-1"}, createRequest{RequestID: "request-1", OfferSDP: "v=0"})
	var api *control.APIError
	if !errors.As(err, &api) || api.Status != http.StatusServiceUnavailable || api.Code != "voice_unavailable" {
		t.Fatalf("unconfigured Space gateway did not fail closed: err=%v", err)
	}
	var sessions int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM voice_sessions`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatalf("unconfigured gateway created %d voice session records", sessions)
	}
}

func TestVoiceCreatePersistsOnlyAccountHashAndOpaqueLease(t *testing.T) {
	const accountID = "sensitive-account-correlation-id"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/voice/sessions" {
			t.Errorf("gateway request=%s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"answer_sdp": "v=0\\r\\n", "voice_session_id": "gateway-session",
			"account_lease_id": "opaque-lease", "account_lease_generation": 9,
			"account_id": accountID,
		})
	}))
	defer server.Close()
	store, svc := newVoiceTestService(t, Config{GatewayURL: server.URL, GatewayAPIKey: "test-key", HTTPClient: server.Client()})
	principal := contracts.Principal{WorkspaceID: "default", SubjectID: "user-1", DeviceID: "device-1"}
	created, err := svc.create(context.Background(), principal, createRequest{RequestID: "request-sensitive", OfferSDP: "v=0\\r\\n"})
	if err != nil {
		t.Fatal(err)
	}
	if created.AccountLeaseID != "opaque-lease" || created.AccountLeaseGeneration != 9 {
		t.Fatalf("lease identity was not returned: %#v", created)
	}
	var answer []byte
	var accountHash []byte
	if err := store.DB().QueryRow(`SELECT offer_answer,account_id_hash FROM voice_sessions WHERE id=?`, created.SessionID).Scan(&answer, &accountHash); err != nil {
		t.Fatal(err)
	}
	if string(answer) == "" || bytes.Contains(answer, []byte(accountID)) || bytes.Contains(answer, []byte("account_id")) {
		t.Fatalf("persisted answer contains account identity material: %s", answer)
	}
	wantHash := sha256.Sum256([]byte(accountID))
	if !bytes.Equal(accountHash, wantHash[:]) {
		t.Fatalf("persisted account hash=%x want=%x", accountHash, wantHash)
	}
}

func TestSpaceDownstreamSDPDTOUsesObservedFieldNames(t *testing.T) {
	body, err := json.Marshal(gatewaySessionRequest{OfferSDP: "v=0\r\n", Voice: "", VoiceMode: "", LanguageCode: ""})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["offer_sdp"] != "v=0\r\n" || len(fields) != 1 {
		t.Fatalf("new Space session request DTO=%s; expected only offer_sdp for a fresh session", body)
	}
	var answer gatewaySessionResponse
	if err := json.Unmarshal([]byte(`{"answer_sdp":"v=0\\r\\n","voice_session_id":"opaque-session-id"}`), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.AnswerSDP == "" || answer.VoiceSessionID != "opaque-session-id" {
		t.Fatalf("Space answer DTO did not decode: %#v", answer)
	}
}

func TestVoiceTaskLinkRequiresSameDeviceVoiceTaskAndExposesTaskEvents(t *testing.T) {
	ctx := context.Background()
	store, svc := newVoiceTestService(t, Config{})
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO actors(id,workspace_id,subject_id,name,status,version,created_at) VALUES('device-1','default','user-1','test','approved',1,?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO voice_sessions(id,workspace_id,owner_id,device_id,request_id,request_hash,generation,state,created_at,updated_at) VALUES('session-1','default','user-1','device-1','request-1',X'01',1,'ended',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO voice_turns(id,workspace_id,session_id,generation,turn_id,revision,content,content_hash,confirmed_at,command_id) VALUES('turn-row','default','session-1',1,'turn-a',1,'confirmed text',X'01',?,'voice-turn-cmd')`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO tasks(id,workspace_id,title,goal,source,profile_id,status,version,created_at,updated_at,accepted_at,created_by_device_id,command_id) VALUES('task-1','default','title','goal','voice','default','queued',1,?,?,?,'device-1','task-create-cmd')`, now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO events(event_id,workspace_id,cursor,schema_version,event_type,occurred_at,subject_id,aggregate_type,aggregate_id,aggregate_version,payload) VALUES('task-event','default',1,1,'task.created',?,'user-1','task','task-1',1,'{}')`, now); err != nil {
		t.Fatal(err)
	}
	principal := contracts.Principal{WorkspaceID: "default", SubjectID: "user-1", DeviceID: "device-1", Scopes: []string{"voice:turn:commit"}}
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	version := int64(1)
	payload, _ := json.Marshal(linkTaskPayload{SessionID: "session-1", Generation: 1, TurnID: "turn-a", TurnRevision: 1, TaskID: "task-1"})
	result, events, err := svc.commandLinkTask(ctx, tx, control.CommandContext{CommandID: "link-command", Principal: principal, AcceptedAt: time.Now().UTC()}, contracts.CommandRequest{ExpectedVersion: &version, Payload: payload})
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if result.ResourceID != "task-1" || len(events) != 1 || events[0].Type != "voice.task.linked" {
		t.Fatalf("voice task link result=%#v events=%#v", result, events)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var linkedEvents int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM voice_task_links l JOIN events e ON e.workspace_id=l.workspace_id AND e.aggregate_type='task' AND e.aggregate_id=l.task_id WHERE l.session_id='session-1' AND e.event_id='task-event'`).Scan(&linkedEvents); err != nil {
		t.Fatal(err)
	}
	if linkedEvents != 1 {
		t.Fatalf("linked authoritative task events=%d, want 1", linkedEvents)
	}
	other := principal
	other.DeviceID = "other-device"
	otherTx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer otherTx.Rollback()
	if _, _, err := svc.commandLinkTask(ctx, otherTx, control.CommandContext{CommandID: "bad-link", Principal: other, AcceptedAt: time.Now().UTC()}, contracts.CommandRequest{ExpectedVersion: &version, Payload: payload}); err == nil {
		t.Fatal("another device linked a voice turn to the task")
	}
}

func TestUnknownMediaOutcomeBlocksWholePoolUntilOperatorReconciliation(t *testing.T) {
	ctx := context.Background()
	store, svc := newVoiceTestService(t, Config{})
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO voice_sessions(id,workspace_id,owner_id,device_id,request_id,request_hash,generation,state,created_at,updated_at) VALUES('session-1','default','user-1','device-1','request-1',X'01',1,'allocating',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if err := svc.markSession(ctx, "session-1", "active", "", []byte(`{"answer_sdp":"transient"}`), "gateway-session-1"); err != nil {
		t.Fatal(err)
	}
	var blocked int
	var reason string
	if err := store.DB().QueryRowContext(ctx, `SELECT blocked,reason_code FROM voice_pool_state WHERE id=1`).Scan(&blocked, &reason); err != nil || blocked != 1 || reason != "active_session" {
		t.Fatalf("active voice generation did not reserve the hidden pool: blocked=%d reason=%q err=%v", blocked, reason, err)
	}
	if err := svc.markSession(ctx, "session-1", "quarantined", "gateway_outcome_unknown", nil, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT blocked FROM voice_pool_state WHERE id=1`).Scan(&blocked); err != nil || blocked != 1 {
		t.Fatalf("unknown media outcome did not block account pool: blocked=%d err=%v", blocked, err)
	}
	principal := contracts.Principal{WorkspaceID: "default", SubjectID: "operator", DeviceID: "operator-device", Scopes: []string{"voice:pool:reconcile"}}
	payload, _ := json.Marshal(reconcilePoolPayload{SessionID: "session-1", Generation: 1, Reason: "Manually verified no live media remains", ConfirmedOutOfBandTermination: true})
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, events, err := svc.commandReconcilePool(ctx, tx, control.CommandContext{CommandID: "reconcile-command", Principal: principal, AcceptedAt: time.Now().UTC()}, contracts.CommandRequest{Payload: payload})
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != "voice.pool.reconciled" {
		t.Fatalf("operator reconciliation event=%#v", events)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var reconciliations int
	if err := store.DB().QueryRowContext(ctx, `SELECT blocked FROM voice_pool_state WHERE id=1`).Scan(&blocked); err != nil || blocked != 0 {
		t.Fatalf("operator reconciliation did not clear pool block: blocked=%d err=%v", blocked, err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM voice_pool_reconciliations WHERE method='operator_attestation' AND source_session_id='session-1' AND generation=1`).Scan(&reconciliations); err != nil || reconciliations != 1 {
		t.Fatalf("operator reconciliation audit rows=%d err=%v", reconciliations, err)
	}
}

func TestEndingMediaCannotBeReconciledBeforeAtomicHangupFinalization(t *testing.T) {
	ctx := context.Background()
	store, svc := newVoiceTestService(t, Config{})
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO voice_sessions(id,workspace_id,owner_id,device_id,request_id,request_hash,generation,state,gateway_session_id,created_at,updated_at) VALUES('session-ending','default','user-1','device-1','request-ending',X'01',4,'ending','gateway-session',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE voice_pool_state SET blocked=1,reason_code='active_session',source_session_id='session-ending' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	principal := contracts.Principal{WorkspaceID: "default", SubjectID: "operator", DeviceID: "operator-device", Scopes: []string{"voice:pool:reconcile"}}
	payload, _ := json.Marshal(reconcilePoolPayload{SessionID: "session-ending", Generation: 4, Reason: "Verified gateway completion evidence", ConfirmedOutOfBandTermination: true})
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.commandReconcilePool(ctx, tx, control.CommandContext{CommandID: "reconcile-too-early", Principal: principal, AcceptedAt: time.Now().UTC()}, contracts.CommandRequest{Payload: payload})
	_ = tx.Rollback()
	var apiErr *control.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "voice_sessions_active" {
		t.Fatalf("in-flight ending session did not fence reconciliation: err=%v", err)
	}

	observation := AccountTerminationObservation{AccountID: "opaque-account", LeaseID: "opaque-lease", SessionID: "gateway-session", Terminated: true, EvidenceID: "evidence-1", ObservedAt: time.Now().UTC()}
	blocked, err := svc.completeHangup(ctx, contracts.Principal{WorkspaceID: "default", SubjectID: "user-1"}, "session-ending", 4, observation, true)
	if err != nil || blocked {
		t.Fatalf("trusted completion did not atomically release the matching pool fence: blocked=%t err=%v", blocked, err)
	}
	var state string
	if err := store.DB().QueryRowContext(ctx, `SELECT state FROM voice_sessions WHERE id='session-ending'`).Scan(&state); err != nil || state != "ended" {
		t.Fatalf("final state=%q err=%v", state, err)
	}
	var blockedDB, evidenceRows int
	if err := store.DB().QueryRowContext(ctx, `SELECT blocked FROM voice_pool_state WHERE id=1`).Scan(&blockedDB); err != nil || blockedDB != 0 {
		t.Fatalf("pool fence was not cleared with termination evidence: blocked=%d err=%v", blockedDB, err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM voice_account_terminations WHERE session_id='session-ending' AND generation=4 AND evidence_id='evidence-1'`).Scan(&evidenceRows); err != nil || evidenceRows != 1 {
		t.Fatalf("trusted termination evidence rows=%d err=%v", evidenceRows, err)
	}
}
