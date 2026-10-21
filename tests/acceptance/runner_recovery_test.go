package acceptance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/control"
)

func TestCurrentClaimRecoveryDoesNotDeadlockOrChangeSnapshot(t *testing.T) {
	bootstrapCode, deviceToken, runnerToken := reviewToken(t), reviewToken(t), reviewToken(t)
	profile := control.RuntimeProfile{ID: "worker", AOProjectID: "workers", RuntimeRouteID: "native", ModelRouteID: "model", Role: "worker", AOAgentKind: "worker", IsolationPolicy: "native-sandbox", CapabilityIDs: []string{"task.read"}, Scopes: []string{"task:read"}}
	profiles := map[string]control.RuntimeProfile{"worker": profile}
	store, handler := reviewServer(t, control.Config{BootstrapCode: bootstrapCode, RunnerToken: runnerToken, RunnerID: "runner", RunnerWorkspaceIDs: []string{"default"}, RunnerScopes: []string{"runner:claim", "runner:control:read"}, DefaultProfileID: "worker", Profiles: profiles}, nil)
	reviewBootstrap(t, handler, bootstrapCode, deviceToken)
	created := reviewRequest(handler, http.MethodPost, "/v1/commands", deviceToken, "create", contracts.CommandRequest{Kind: "task.create", Payload: json.RawMessage(`{"title":"original title","goal":"original goal","source":"user"}`)})
	if created.Code != http.StatusOK {
		t.Fatalf("task creation=%d %s", created.Code, created.Body.String())
	}
	live := httptest.NewServer(handler)
	defer live.Close()
	client := live.Client()
	request := func(method, path, bearer string) (*http.Response, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, method, live.URL+path, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+bearer)
		return client.Do(req)
	}
	claimed, err := request(http.MethodPost, "/runner/v1/claims", runnerToken)
	if err != nil {
		t.Fatal(err)
	}
	var claim contracts.JobClaim
	if err = json.NewDecoder(claimed.Body).Decode(&claim); err != nil {
		claimed.Body.Close()
		t.Fatal(err)
	}
	claimed.Body.Close()
	if claim.AOProjectID != "workers" || claim.TaskVersion != 2 {
		t.Fatalf("claim missing server profile or initial task snapshot: %+v", claim)
	}
	if _, err = store.DB().Exec(`UPDATE tasks SET title='changed',goal='changed',version=version+1 WHERE id=?`, claim.TaskID); err != nil {
		t.Fatal(err)
	}
	profiles["worker"] = control.RuntimeProfile{ID: "worker", AOProjectID: "changed-project", RuntimeRouteID: "changed-route", ModelRouteID: "changed-model", Role: "worker", AOAgentKind: "worker", IsolationPolicy: "changed-policy", CapabilityIDs: []string{"admin"}, Scopes: []string{"*"}}
	recovered, err := request(http.MethodGet, "/runner/v1/claims/current", runnerToken)
	if err != nil {
		t.Fatalf("current claim recovery blocked on its own query rows: %v", err)
	}
	defer recovered.Body.Close()
	if recovered.StatusCode != http.StatusOK {
		t.Fatalf("recovery status=%d", recovered.StatusCode)
	}
	var recovery contracts.ClaimRecovery
	if err = json.NewDecoder(recovered.Body).Decode(&recovery); err != nil {
		t.Fatal(err)
	}
	if len(recovery.Claims) != 1 || recovery.Claims[0].TaskVersion != claim.TaskVersion || recovery.Claims[0].AOProjectID != "workers" || recovery.Claims[0].Goal != "original goal" || len(recovery.Claims[0].Authorization.Scopes) != 1 || recovery.Claims[0].Authorization.Scopes[0] != "task:read" || recovery.Claims[0].RuntimeRouteID != "native" {
		t.Fatalf("recovery did not return immutable claim: %+v", recovery)
	}
}
