package capabilities

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/control"
	"github.com/ronigooja/Cylo/internal/storage"
)

func capabilityRecoveryFixture(t *testing.T) (*storage.Store, http.Handler, string, contracts.DeploymentClaim) {
	t.Helper()
	ctx := context.Background()
	raw := make([]byte, 32)
	if _, e := rand.Read(raw); e != nil {
		t.Fatal(e)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	store, e := storage.Open(ctx, storage.Config{Path: filepath.Join(t.TempDir(), "authority.db")})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { store.Close() })
	if e = storage.RegisterCoreMigrations(store); e != nil {
		t.Fatal(e)
	}
	server, e := control.NewServer(store, control.Config{RunnerToken: token, RunnerID: "review", RunnerWorkspaceIDs: []string{"default"}, RunnerScopes: []string{"capability:deploy"}})
	if e != nil {
		t.Fatal(e)
	}
	if e = Register(server, store, Config{AvailableCapabilityIDs: []string{"mail.read"}, RuntimeBackend: "fixture"}); e != nil {
		t.Fatal(e)
	}
	if e = store.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	svc := &Service{db: store.DB(), server: server, availableCapabilityIDs: map[string]struct{}{"mail.read": {}}, runtimeBackend: "fixture"}
	manifest := contracts.CapabilityManifest{SchemaVersion: 1, ArtifactRef: "oci://registry.example/app@sha256:" + strings.Repeat("a", 64), ArtifactDigest: "sha256:" + strings.Repeat("a", 64), Entrypoint: "/app/run", Capabilities: []string{"mail.read"}, Budget: contracts.CapabilityResourceBudget{CPUMillis: 50, MemoryBytes: 100, DiskBytes: 100, TimeoutSeconds: 10, Concurrency: 1}, Network: contracts.CapabilityNetworkPolicy{Mode: "none"}, HealthCheck: &contracts.CapabilityHealthCheck{Kind: "http", Target: "/health", Port: 8080, IntervalSeconds: 10, TimeoutSeconds: 2, FailureThreshold: 2}}
	cmd := control.CommandContext{CommandID: "fixture", Principal: contracts.Principal{WorkspaceID: "default", SubjectID: "owner", Scopes: []string{"capability:release:submit", "capability:deployment:request", "capability:deployment:authorize"}}, AcceptedAt: time.Now().UTC()}
	tx := begin(t, store.DB())
	release, _, e := svc.submitRelease(ctx, tx, cmd, contracts.CommandRequest{Payload: marshal(submitReleasePayload{Manifest: manifest})})
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	var digest string
	if e = store.DB().QueryRow("SELECT digest FROM capability_releases WHERE release_id=?", release.ResourceID).Scan(&digest); e != nil {
		t.Fatal(e)
	}
	tx = begin(t, store.DB())
	dep, _, e := svc.requestDeployment(ctx, tx, cmd, contracts.CommandRequest{Payload: marshal(deploymentRequestPayload{ServiceID: "review", ReleaseDigest: digest, Operation: "install"})})
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	tx = begin(t, store.DB())
	version := int64(1)
	_, _, e = svc.authorizeDeployment(ctx, tx, cmd, contracts.CommandRequest{ExpectedVersion: &version, Payload: marshal(authorizePayload{DeploymentID: dep.ResourceID, ScopeKind: "workspace", Actions: []string{"install", "stop"}, Budget: manifest.Budget, ExpiresAt: time.Now().Add(time.Hour)})})
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	h := server.Handler()
	w := capabilityReviewHTTP(h, "POST", "/runner/v1/deployments/claims", token, "")
	if w.Code != 200 {
		t.Fatalf("claim status=%d body=%s", w.Code, w.Body.String())
	}
	var claim contracts.DeploymentClaim
	if e = json.Unmarshal(w.Body.Bytes(), &claim); e != nil {
		t.Fatal(e)
	}
	return store, h, token, claim
}
func capabilityReviewHTTP(h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestReviewCapabilityRecoveryPreservesImmutableClaim(t *testing.T) {
	store, h, token, claim := capabilityRecoveryFixture(t)
	// Recovery must use the admitted snapshot even if mutable source records change.
	if _, e := store.DB().Exec("UPDATE capability_grants SET actions='[\"stop\"]',expires_at='2000-01-01T00:00:00Z'; UPDATE capability_releases SET manifest='{}'"); e != nil {
		t.Fatal(e)
	}
	for _, method := range []string{"GET", "POST"} {
		path := "/runner/v1/deployments/claims"
		if method == "GET" {
			path += "/current"
		}
		w := capabilityReviewHTTP(h, method, path, token, "")
		if w.Code != 200 {
			t.Fatalf("recover %s=%d", method, w.Code)
		}
		var got contracts.DeploymentClaim
		if e := json.Unmarshal(w.Body.Bytes(), &got); e != nil {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(got, claim) {
			t.Errorf("%s changed immutable claim", method)
		}
	}
}
func TestReviewCapabilityRecoveredUncertainClaimAcceptsReconciliation(t *testing.T) {
	_, h, token, claim := capabilityRecoveryFixture(t)
	obs := contracts.DeploymentObservation{DeploymentID: claim.DeploymentID, AttemptID: claim.AttemptID, Fence: claim.Fence, ObservationID: "unknown", Operation: "install", State: "uncertain", ObservedAt: time.Now().UTC()}
	body, _ := json.Marshal(obs)
	path := "/runner/v1/deployments/" + claim.DeploymentID + "/observations"
	if w := capabilityReviewHTTP(h, "POST", path, token, string(body)); w.Code != 202 {
		t.Fatalf("unknown observation=%d", w.Code)
	}
	w := capabilityReviewHTTP(h, "GET", "/runner/v1/deployments/claims/current", token, "")
	if w.Code != 200 {
		t.Fatalf("recovery=%d", w.Code)
	}
	var current contracts.DeploymentClaim
	if e := json.Unmarshal(w.Body.Bytes(), &current); e != nil {
		t.Fatal(e)
	}
	if current.ClaimState != "uncertain" {
		t.Fatalf("recovered state=%s", current.ClaimState)
	}
	obs.ObservationID = "reconciled"
	obs.State = "running"
	obs.Health = contracts.CapabilityHealth{State: "healthy", CheckedAt: time.Now().UTC(), EvidenceRef: "fixture:probe"}
	body, _ = json.Marshal(obs)
	if w := capabilityReviewHTTP(h, "POST", path, token, string(body)); w.Code != 202 {
		t.Errorf("same owned attempt cannot reconcile unknown result: status=%d body=%s", w.Code, w.Body.String())
	}
}
