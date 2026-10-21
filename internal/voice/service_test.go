package voice

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
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
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO devices(id,workspace_id,subject_id,name,status,token_hash,version,created_at) VALUES('device-1','default','user-1','test','approved',X'01',1,?)`, now); err != nil {
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
