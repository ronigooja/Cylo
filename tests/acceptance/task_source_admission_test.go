package acceptance

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/control"
)

func TestTaskSourceAdmissionSharesCommandTransaction(t *testing.T) {
	code, token := reviewToken(t), reviewToken(t)
	profile := control.RuntimeProfile{ID: "worker", AOProjectID: "workers", RuntimeRouteID: "native", ModelRouteID: "model", Role: "worker", AOAgentKind: "worker", IsolationPolicy: "native-sandbox"}
	var server *control.Server
	var sawTrustedSource bool
	var sawGrantSnapshot string
	store, h := reviewServer(t, control.Config{BootstrapCode: code, DefaultProfileID: "worker", Profiles: map[string]control.RuntimeProfile{"worker": profile}}, func(s *control.Server) {
		server = s
		if err := s.RegisterTaskSourceHandler("voice", func(ctx context.Context, tx *sql.Tx, principal contracts.Principal, taskID string, task *control.TaskSourceRequest, ref json.RawMessage, _ time.Time) ([]contracts.Event, error) {
			if principal.DeviceID == "" || !json.Valid(ref) {
				return nil, control.NewAPIError(http.StatusForbidden, "source_denied", "invalid source", nil)
			}
			task.Title = "authoritative voice title"
			task.Goal = "authoritative voice transcript"
			task.InitialMessage = "authoritative voice transcript"
			if _, err := tx.ExecContext(ctx, `INSERT INTO source_bindings(task_id,reference) VALUES(?,?)`, taskID, ref); err != nil {
				return nil, control.NewAPIError(http.StatusConflict, "source_conflict", "source already bound", nil)
			}
			return []contracts.Event{{Type: "voice.task.created", AggregateType: "task", AggregateID: taskID, AggregateVersion: 1, Payload: json.RawMessage(`{"source":"voice"}`)}}, nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := s.RegisterTaskSourceHandler("service", func(ctx context.Context, tx *sql.Tx, _ contracts.Principal, taskID string, task *control.TaskSourceRequest, ref json.RawMessage, _ time.Time) ([]contracts.Event, error) {
			sawTrustedSource = task.TrustedSource
			sawGrantSnapshot = string(task.GrantSnapshot)
			task.Title = "authoritative service title"
			task.Goal = "authoritative service goal"
			task.InitialMessage = "authoritative service goal"
			if _, err := tx.ExecContext(ctx, `INSERT INTO source_bindings(task_id,reference) VALUES(?,?)`, taskID, ref); err != nil {
				return nil, err
			}
			return []contracts.Event{{Type: "service.task.created", AggregateType: "task", AggregateID: taskID, AggregateVersion: 1, Payload: json.RawMessage(`{"source":"service"}`)}}, nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := store.DB().Exec(`CREATE TABLE source_bindings(task_id TEXT PRIMARY KEY,reference TEXT NOT NULL UNIQUE)`); err != nil {
		t.Fatal(err)
	}
	device := reviewBootstrap(t, h, code, token)
	body := func(source, reference string) contracts.CommandRequest {
		return contracts.CommandRequest{Kind: "task.create", Payload: json.RawMessage(`{"title":"voice task","goal":"voice goal","source":"` + source + `","source_reference":` + reference + `}`)}
	}
	publicService := reviewRequest(h, http.MethodPost, "/v1/commands", token, "public-service-source", body("service", `{"kind":"service_trigger","id":"public"}`))
	if publicService.Code != http.StatusForbidden {
		t.Fatalf("public service-source task create=%d, want forbidden", publicService.Code)
	}
	created := reviewRequest(h, http.MethodPost, "/v1/commands", token, "voice-source", body("voice", `{"kind":"voice_turn","turn_id":"t1"}`))
	if created.Code != http.StatusOK {
		t.Fatalf("source-admitted task create=%d %s", created.Code, created.Body.String())
	}
	var receipt contracts.CommandReceipt
	if err := json.Unmarshal(created.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	var tasks, bindings, events int
	if err := store.DB().QueryRow(`SELECT count(*) FROM tasks WHERE id=?`, receipt.ResourceID).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRow(`SELECT count(*) FROM source_bindings WHERE task_id=?`, receipt.ResourceID).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRow(`SELECT count(*) FROM events WHERE event_type='voice.task.created' AND aggregate_id=?`, receipt.ResourceID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if tasks != 1 || bindings != 1 || events != 1 {
		t.Fatalf("source/task/event were not atomic: tasks=%d bindings=%d events=%d", tasks, bindings, events)
	}
	var goal, entry string
	if err := store.DB().QueryRow(`SELECT goal FROM tasks WHERE id=?`, receipt.ResourceID).Scan(&goal); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRow(`SELECT content FROM task_entries WHERE task_id=?`, receipt.ResourceID).Scan(&entry); err != nil {
		t.Fatal(err)
	}
	if goal != "authoritative voice transcript" || entry != goal {
		t.Fatalf("source handler content not applied atomically: goal=%q entry=%q", goal, entry)
	}
	var title string
	if err := store.DB().QueryRow(`SELECT title FROM tasks WHERE id=?`, receipt.ResourceID).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "authoritative voice title" {
		t.Fatalf("source handler title not applied atomically: title=%q", title)
	}
	var taskCreatedPayload, entryPayload []byte
	if err := store.DB().QueryRow(`SELECT payload FROM events WHERE event_type='task.created' AND aggregate_id=?`, receipt.ResourceID).Scan(&taskCreatedPayload); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRow(`SELECT payload FROM events WHERE event_type='task.entry_appended' AND aggregate_id=?`, receipt.ResourceID).Scan(&entryPayload); err != nil {
		t.Fatal(err)
	}
	var createdEnvelope, entryEnvelope contracts.Event
	if err := json.Unmarshal(taskCreatedPayload, &createdEnvelope); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(entryPayload, &entryEnvelope); err != nil {
		t.Fatal(err)
	}
	var createdEvent struct {
		Task contracts.Task `json:"task"`
	}
	if err := json.Unmarshal(createdEnvelope.Payload, &createdEvent); err != nil {
		t.Fatal(err)
	}
	var appendedEvent struct {
		Entry contracts.TaskEntry `json:"entry"`
	}
	if err := json.Unmarshal(entryEnvelope.Payload, &appendedEvent); err != nil {
		t.Fatal(err)
	}
	if createdEvent.Task.Title != title || createdEvent.Task.Goal != goal || appendedEvent.Entry.Content != entry {
		t.Fatalf("source events contain client content: created=%+v entry=%q", createdEvent.Task, appendedEvent.Entry.Content)
	}
	duplicate := reviewRequest(h, http.MethodPost, "/v1/commands", token, "voice-source-duplicate", body("voice", `{"kind":"voice_turn","turn_id":"t1"}`))
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate stable source admission=%d", duplicate.Code)
	}
	var after int
	if err := store.DB().QueryRow(`SELECT count(*) FROM tasks`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != 1 {
		t.Fatalf("rejected duplicate created a task: count=%d", after)
	}
	var subject string
	if err := store.DB().QueryRow(`SELECT subject_id FROM devices WHERE id=?`, device.Device.ID).Scan(&subject); err != nil {
		t.Fatal(err)
	}
	principal := contracts.Principal{WorkspaceID: device.Device.WorkspaceID, SubjectID: subject, DeviceID: device.Device.ID}
	grantSnapshot := json.RawMessage(`{"grant_id":"stored-grant"}`)
	ctx := context.Background()
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := server.AdmitTaskTx(ctx, tx, control.AdmissionContext{Principal: principal, Source: "service", TrustedSource: true, SourceRef: json.RawMessage(`{"kind":"service_trigger","id":"commit"}`), IdempotencyKey: "trigger-task-t2", GrantSnapshot: grantSnapshot, ProfileID: "worker", Title: "trigger task", Goal: "trigger goal", InitialMessage: "trigger initial"})
	if err != nil {
		tx.Rollback()
		t.Fatalf("trusted task admission=%v", err)
	}
	if len(admitted.Events) < 2 || admitted.Task.Source != "service" || admitted.Task.Title != "authoritative service title" {
		t.Fatalf("trusted admission did not reuse source hook: task=%+v events=%d", admitted.Task, len(admitted.Events))
	}
	if err = server.AppendEventsTx(ctx, tx, principal, admitted.Events); err != nil {
		tx.Rollback()
		t.Fatalf("append trusted task events=%v", err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var admittedTaskCount, grantVisible int
	if err = store.DB().QueryRow(`SELECT count(*) FROM tasks WHERE id=?`, admitted.Task.ID).Scan(&admittedTaskCount); err != nil {
		t.Fatal(err)
	}
	if err = store.DB().QueryRow(`SELECT count(*) FROM events WHERE aggregate_id=?`, admitted.Task.ID).Scan(&grantVisible); err != nil {
		t.Fatal(err)
	}
	if admittedTaskCount != 1 || grantVisible < 2 {
		t.Fatalf("trusted admission did not commit task and outbox atomically: tasks=%d events=%d", admittedTaskCount, grantVisible)
	}
	if !sawTrustedSource || sawGrantSnapshot != string(grantSnapshot) {
		t.Fatalf("trusted source callback did not receive trust and grant snapshot: trusted=%v snapshot=%s", sawTrustedSource, sawGrantSnapshot)
	}

	rollbackTx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	rolledBack, err := server.AdmitTaskTx(ctx, rollbackTx, control.AdmissionContext{Principal: principal, Source: "service", TrustedSource: true, SourceRef: json.RawMessage(`{"kind":"service_trigger","id":"rollback"}`), IdempotencyKey: "service-rollback", GrantSnapshot: grantSnapshot, ProfileID: "worker", Title: "service task", Goal: "service goal"})
	if err != nil {
		rollbackTx.Rollback()
		t.Fatalf("trusted service admission before rollback=%v", err)
	}
	if err = server.AppendEventsTx(ctx, rollbackTx, principal, rolledBack.Events); err != nil {
		rollbackTx.Rollback()
		t.Fatalf("append service task events before rollback=%v", err)
	}
	if err = rollbackTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var rolledBackTasks, rolledBackBindings, rolledBackEvents int
	if err = store.DB().QueryRow(`SELECT count(*) FROM tasks WHERE id=?`, rolledBack.Task.ID).Scan(&rolledBackTasks); err != nil {
		t.Fatal(err)
	}
	if err = store.DB().QueryRow(`SELECT count(*) FROM source_bindings WHERE task_id=?`, rolledBack.Task.ID).Scan(&rolledBackBindings); err != nil {
		t.Fatal(err)
	}
	if err = store.DB().QueryRow(`SELECT count(*) FROM events WHERE aggregate_id=?`, rolledBack.Task.ID).Scan(&rolledBackEvents); err != nil {
		t.Fatal(err)
	}
	if rolledBackTasks != 0 || rolledBackBindings != 0 || rolledBackEvents != 0 {
		t.Fatalf("caller rollback persisted service admission writes: tasks=%d bindings=%d events=%d", rolledBackTasks, rolledBackBindings, rolledBackEvents)
	}
}
