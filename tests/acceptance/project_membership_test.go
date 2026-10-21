package acceptance

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/control"
)

func TestProjectMembershipReadAndWriteBoundaries(t *testing.T) {
	code, ownerToken := reviewToken(t), reviewToken(t)
	profile := control.RuntimeProfile{ID: "worker", AOProjectID: "workers", RuntimeRouteID: "native", ModelRouteID: "model", Role: "worker", AOAgentKind: "worker", IsolationPolicy: "native-sandbox"}
	store, h := reviewServer(t, control.Config{BootstrapCode: code, DefaultProfileID: "worker", Profiles: map[string]control.RuntimeProfile{"worker": profile}}, nil)
	reviewBootstrap(t, h, code, ownerToken)
	projectResponse := reviewRequest(h, http.MethodPost, "/v1/commands", ownerToken, "project", contracts.CommandRequest{Kind: "project.create", Payload: json.RawMessage(`{"name":"shared"}`)})
	if projectResponse.Code != http.StatusOK {
		t.Fatalf("project create=%d", projectResponse.Code)
	}
	var project contracts.CommandReceipt
	if err := json.Unmarshal(projectResponse.Body.Bytes(), &project); err != nil {
		t.Fatal(err)
	}
	taskResponse := reviewRequest(h, http.MethodPost, "/v1/commands", ownerToken, "task", contracts.CommandRequest{Kind: "task.create", Payload: json.RawMessage(`{"project_id":"` + project.ResourceID + `","title":"shared task","goal":"shared goal","source":"user"}`)})
	if taskResponse.Code != http.StatusOK {
		t.Fatalf("task create=%d %s", taskResponse.Code, taskResponse.Body.String())
	}
	var task contracts.CommandReceipt
	if err := json.Unmarshal(taskResponse.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}

	peerToken, peerSubject := reviewToken(t), "member-subject"
	addMembershipDevice(t, store.DB(), peerToken, peerSubject, "member-device")
	if _, err := store.DB().Exec(`INSERT INTO project_memberships(workspace_id,project_id,subject_id,role,status,created_at) VALUES('default',?,?, 'viewer','active',?)`, project.ResourceID, peerSubject, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	peerSnapshot := reviewRequest(h, http.MethodGet, "/v1/snapshot", peerToken, "", nil)
	if peerSnapshot.Code != http.StatusOK {
		t.Fatalf("viewer snapshot=%d %s", peerSnapshot.Code, peerSnapshot.Body.String())
	}
	var snapshot contracts.Snapshot
	if err := json.Unmarshal(peerSnapshot.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Projects) != 1 || len(snapshot.Tasks) != 1 {
		t.Fatalf("active viewer membership did not grant read access: projects=%d tasks=%d", len(snapshot.Projects), len(snapshot.Tasks))
	}
	viewerAppend := reviewRequest(h, http.MethodPost, "/v1/commands", peerToken, "viewer-append", contracts.CommandRequest{Kind: "task.append", Payload: json.RawMessage(`{"task_id":"` + task.ResourceID + `","content":"viewer write"}`)})
	if viewerAppend.Code != http.StatusNotFound {
		t.Fatalf("viewer mutated shared task: status=%d body=%s", viewerAppend.Code, viewerAppend.Body.String())
	}
	viewerCreate := reviewRequest(h, http.MethodPost, "/v1/commands", peerToken, "viewer-create", contracts.CommandRequest{Kind: "task.create", Payload: json.RawMessage(`{"project_id":"` + project.ResourceID + `","title":"forbidden","goal":"forbidden","source":"user"}`)})
	if viewerCreate.Code != http.StatusNotFound {
		t.Fatalf("viewer created task in shared project: status=%d body=%s", viewerCreate.Code, viewerCreate.Body.String())
	}
	if _, err := store.DB().Exec(`UPDATE project_memberships SET role='member' WHERE workspace_id='default' AND project_id=? AND subject_id=?`, project.ResourceID, peerSubject); err != nil {
		t.Fatal(err)
	}
	memberAppend := reviewRequest(h, http.MethodPost, "/v1/commands", peerToken, "member-append", contracts.CommandRequest{Kind: "task.append", Payload: json.RawMessage(`{"task_id":"` + task.ResourceID + `","content":"member write"}`)})
	if memberAppend.Code != http.StatusOK {
		t.Fatalf("member could not append to shared task: status=%d body=%s", memberAppend.Code, memberAppend.Body.String())
	}
	memberCreate := reviewRequest(h, http.MethodPost, "/v1/commands", peerToken, "member-create", contracts.CommandRequest{Kind: "task.create", Payload: json.RawMessage(`{"project_id":"` + project.ResourceID + `","title":"member task","goal":"member goal","source":"user"}`)})
	if memberCreate.Code != http.StatusOK {
		t.Fatalf("member could not create task in shared project: status=%d body=%s", memberCreate.Code, memberCreate.Body.String())
	}
}

func addMembershipDevice(t *testing.T, db *sql.DB, token, subject, deviceID string) {
	t.Helper()
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(decoded)
	at := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = db.Exec(`INSERT INTO devices(id,workspace_id,subject_id,name,status,token_hash,created_at,approved_at) VALUES(?, 'default', ?, 'peer', 'approved', ?, ?, ?)`, deviceID, subject, hash[:], at, at); err != nil {
		t.Fatal(err)
	}
}
