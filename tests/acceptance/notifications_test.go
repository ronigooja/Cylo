package acceptance

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/control"
)

type recordingPushSender struct {
	requests []control.PushRequest
	response control.PushResponse
}

func (s *recordingPushSender) Send(_ context.Context, req control.PushRequest) control.PushResponse {
	s.requests = append(s.requests, req)
	return s.response
}

func TestNotificationInboxDurableAndSubjectScoped(t *testing.T) {
	code, token := reviewToken(t), reviewToken(t)
	s, h := reviewServer(t, control.Config{BootstrapCode: code}, func(srv *control.Server) {
		if err := srv.RegisterCommand("review.notification", func(ctx context.Context, tx *sql.Tx, c control.CommandContext, _ contracts.CommandRequest) (contracts.CommandResult, []contracts.Event, error) {
			return contracts.CommandResult{}, []contracts.Event{{Type: "run.runtime_terminated", AggregateType: "run", AggregateID: "run1", AggregateVersion: 1, Payload: json.RawMessage(`{"task_id":"task1","task_status":"awaiting_user"}`)}}, nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	d := reviewBootstrap(t, h, code, token)
	if _, err := s.DB().Exec(`INSERT INTO tasks(id,workspace_id,title,goal,source,profile_id,status,version,created_at,updated_at,created_by_device_id,command_id) VALUES('task1','default','Inbox task','Goal','user','unused','awaiting_user',1,'2026-10-21T00:00:00Z','2026-10-21T00:00:00Z',?,'cmd1')`, d.Device.ID); err != nil {
		t.Fatal(err)
	}
	receipt := reviewRequest(h, http.MethodPost, "/v1/commands", token, "notify", contracts.CommandRequest{Kind: "review.notification", Payload: json.RawMessage(`{}`)})
	if receipt.Code != http.StatusOK {
		t.Fatalf("event commit=%d %s", receipt.Code, receipt.Body.String())
	}
	inbox := reviewRequest(h, http.MethodGet, "/v1/notifications?unread=true", token, "", nil)
	if inbox.Code != http.StatusOK {
		t.Fatalf("inbox=%d %s", inbox.Code, inbox.Body.String())
	}
	var response struct {
		Notifications []contracts.Notification `json:"notifications"`
	}
	if err := json.Unmarshal(inbox.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Notifications) != 1 || response.Notifications[0].Kind != "task.result_ready" || response.Notifications[0].SourceEventID == "" {
		t.Fatalf("unexpected inbox: %+v", response.Notifications)
	}
	read := reviewRequest(h, http.MethodPost, "/v1/commands", token, "read-notification", contracts.CommandRequest{Kind: "notification.mark_read", Payload: json.RawMessage(`{"notification_id":"` + response.Notifications[0].ID + `"}`)})
	if read.Code != http.StatusOK {
		t.Fatalf("mark read=%d %s", read.Code, read.Body.String())
	}
	unread := reviewRequest(h, http.MethodGet, "/v1/notifications?unread=true", token, "", nil)
	if err := json.Unmarshal(unread.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Notifications) != 0 {
		t.Fatalf("read notice remained unread: %+v", response.Notifications)
	}
}

func TestPushRegistrationCreatesSilentWakeupDelivery(t *testing.T) {
	code, token := reviewToken(t), reviewToken(t)
	sender := &recordingPushSender{response: control.PushResponse{StatusCode: http.StatusInternalServerError}}
	s, h := reviewServer(t, control.Config{BootstrapCode: code, PushSender: sender}, func(srv *control.Server) {
		if err := srv.RegisterCommand("review.notification", func(ctx context.Context, tx *sql.Tx, c control.CommandContext, _ contracts.CommandRequest) (contracts.CommandResult, []contracts.Event, error) {
			return contracts.CommandResult{}, []contracts.Event{{Type: "run.runtime_terminated", AggregateType: "run", AggregateID: "run-push", AggregateVersion: 1, Payload: json.RawMessage(`{"task_id":"task-push","task_status":"awaiting_user"}`)}}, nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	d := reviewBootstrap(t, h, code, token)
	if _, err := s.DB().Exec(`INSERT INTO tasks(id,workspace_id,title,goal,source,profile_id,status,version,created_at,updated_at,created_by_device_id,command_id) VALUES('task-push','default','Private title','Private goal','user','unused','awaiting_user',1,'2026-10-21T00:00:00Z','2026-10-21T00:00:00Z',?,'cmd-push')`, d.Device.ID); err != nil {
		t.Fatal(err)
	}
	pushToken := "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	registered := reviewRequest(h, http.MethodPost, "/v1/commands", token, "register-push", contracts.CommandRequest{Kind: "device.push_token.register", Payload: json.RawMessage(fmt.Sprintf(`{"platform":"ios","token":%q}`, pushToken))})
	if registered.Code != http.StatusOK {
		t.Fatalf("token registration=%d %s", registered.Code, registered.Body.String())
	}
	var leaked int
	if err := s.DB().QueryRow(`SELECT count(*) FROM events WHERE instr(CAST(payload AS TEXT),?)>0`, pushToken).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked != 0 {
		t.Fatalf("push token leaked into event payload (%d matches)", leaked)
	}
	committed := reviewRequest(h, http.MethodPost, "/v1/commands", token, "notify-push", contracts.CommandRequest{Kind: "review.notification", Payload: json.RawMessage(`{}`)})
	if committed.Code != http.StatusOK {
		t.Fatalf("notification commit=%d %s", committed.Code, committed.Body.String())
	}
	var pending int
	if err := s.DB().QueryRow(`SELECT count(*) FROM push_deliveries WHERE status='pending'`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 1 {
		t.Fatalf("pending pushes=%d, want 1", pending)
	}
	if err := control.DispatchPushDeliveries(context.Background(), s, sender, 10); err != nil {
		t.Fatal(err)
	}
	if len(sender.requests) != 1 || sender.requests[0].Token != pushToken || sender.requests[0].Cursor == "" {
		t.Fatalf("unexpected push request: %+v", sender.requests)
	}
	var status string
	if err := s.DB().QueryRow(`SELECT status FROM push_deliveries`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	var attempts int
	if err := s.DB().QueryRow(`SELECT attempts FROM push_deliveries`).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || attempts != 1 {
		t.Fatalf("temporary failure was not retained for retry: status=%q attempts=%d", status, attempts)
	}
	if _, err := s.DB().Exec(`UPDATE push_deliveries SET status='pending',next_attempt_at=?`, time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	sender.response = control.PushResponse{StatusCode: http.StatusOK, APNSID: "mock-apns-id"}
	if err := control.DispatchPushDeliveries(context.Background(), s, sender, 10); err != nil {
		t.Fatal(err)
	}
	if err := s.DB().QueryRow(`SELECT status FROM push_deliveries`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "delivered" {
		t.Fatalf("successful retry status=%q", status)
	}
	if _, err := s.DB().Exec(`UPDATE push_deliveries SET status='pending',next_attempt_at=?`, time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	sender.response = control.PushResponse{StatusCode: http.StatusGone, Reason: "Unregistered"}
	if err := control.DispatchPushDeliveries(context.Background(), s, sender, 10); err != nil {
		t.Fatal(err)
	}
	if err := s.DB().QueryRow(`SELECT status FROM device_push_tokens`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "disabled" {
		t.Fatalf("410 did not disable token: %q", status)
	}
}
