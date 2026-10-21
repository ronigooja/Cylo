package acceptance

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/control"
)

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
