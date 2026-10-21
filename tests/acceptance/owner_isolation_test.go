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

func TestWorkspacePeersCannotReadOrMutateUnsharedProjectAndTasks(t *testing.T) {
	code, ownerToken := reviewToken(t), reviewToken(t)
	profile := control.RuntimeProfile{ID: "worker", AOProjectID: "workers", RuntimeRouteID: "native", ModelRouteID: "model", Role: "worker", AOAgentKind: "worker", IsolationPolicy: "native-sandbox"}
	store, h := reviewServer(t, control.Config{BootstrapCode: code, DefaultProfileID: "worker", Profiles: map[string]control.RuntimeProfile{"worker": profile}}, nil)
	reviewBootstrap(t, h, code, ownerToken)
	projectResponse := reviewRequest(h, http.MethodPost, "/v1/commands", ownerToken, "project", contracts.CommandRequest{Kind: "project.create", Payload: json.RawMessage(`{"name":"private"}`)})
	if projectResponse.Code != http.StatusOK {
		t.Fatalf("project create=%d", projectResponse.Code)
	}
	var project contracts.CommandReceipt
	if err := json.Unmarshal(projectResponse.Body.Bytes(), &project); err != nil {
		t.Fatal(err)
	}
	taskResponse := reviewRequest(h, http.MethodPost, "/v1/commands", ownerToken, "task", contracts.CommandRequest{Kind: "task.create", Payload: json.RawMessage(`{"project_id":"` + project.ResourceID + `","title":"private task","goal":"private goal","source":"user"}`)})
	if taskResponse.Code != http.StatusOK {
		t.Fatalf("task create=%d %s", taskResponse.Code, taskResponse.Body.String())
	}
	var task contracts.CommandReceipt
	if err := json.Unmarshal(taskResponse.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	peerToken, peerSubject := reviewToken(t), "peer-subject"
	decoded, err := base64.RawURLEncoding.DecodeString(peerToken)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(decoded)
	if _, err = store.DB().Exec(`INSERT INTO devices(id,workspace_id,subject_id,name,status,token_hash,created_at,approved_at) VALUES('peer-device','default',?,'peer','approved',?,?,?)`, peerSubject, hash[:], time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	snapshot := reviewRequest(h, http.MethodGet, "/v1/snapshot", peerToken, "", nil)
	if snapshot.Code != http.StatusOK {
		t.Fatalf("peer snapshot=%d", snapshot.Code)
	}
	var view contracts.Snapshot
	if err = json.Unmarshal(snapshot.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Projects) != 0 || len(view.Tasks) != 0 || len(view.Runs) != 0 || len(view.Approvals) != 0 {
		t.Fatalf("same-workspace peer saw unshared state: %+v", view)
	}
	appendResponse := reviewRequest(h, http.MethodPost, "/v1/commands", peerToken, "append", contracts.CommandRequest{Kind: "task.append", Payload: json.RawMessage(`{"task_id":"` + task.ResourceID + `","content":"unauthorized"}`)})
	if appendResponse.Code != http.StatusNotFound {
		t.Fatalf("peer mutated unshared task: status=%d", appendResponse.Code)
	}
	foreignTask := reviewRequest(h, http.MethodPost, "/v1/commands", peerToken, "foreign-project", contracts.CommandRequest{Kind: "task.create", Payload: json.RawMessage(`{"project_id":"` + project.ResourceID + `","title":"intrusion","goal":"intrusion","source":"user"}`)})
	if foreignTask.Code != http.StatusNotFound {
		t.Fatalf("peer attached task to unshared project: status=%d", foreignTask.Code)
	}
	var subject sql.NullString
	if err = store.DB().QueryRow(`SELECT owner_subject_id FROM projects WHERE id=?`, project.ResourceID).Scan(&subject); err != nil || !subject.Valid || subject.String == peerSubject {
		t.Fatalf("project lacks authoritative owner metadata: %v %v", subject, err)
	}
}
