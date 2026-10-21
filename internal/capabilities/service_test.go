package capabilities

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/control"
	"github.com/ronigooja/Cylo/internal/storage"
)

func TestCapabilityReleaseRequiresExplicitGrantAndAuthorizationBeforeJob(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, storage.Config{Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = storage.RegisterCoreMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err = RegisterMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	server, err := control.NewServer(store, control.Config{})
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{db: store.DB(), server: server}
	manifest := contracts.CapabilityManifest{SchemaVersion: 1, ArtifactRef: "oci://registry.example/cylo/mail@sha256:" + strings.Repeat("a", 64), ArtifactDigest: "sha256:" + strings.Repeat("a", 64), Entrypoint: "/app/run", Capabilities: []string{"mail.read"}, Budget: contracts.CapabilityResourceBudget{CPUMillis: 500, MemoryBytes: 1 << 20, DiskBytes: 1 << 20, TimeoutSeconds: 60, Concurrency: 1}, Network: contracts.CapabilityNetworkPolicy{Mode: "none"}}
	principal := contracts.Principal{WorkspaceID: "default", SubjectID: "owner", DeviceID: "device", Scopes: []string{"client:command"}}
	command := control.CommandContext{CommandID: "cmd", Principal: principal, AcceptedAt: time.Now().UTC()}
	payload, _ := json.Marshal(submitReleasePayload{Manifest: manifest})
	tx := begin(t, store.DB())
	_, _, err = svc.submitRelease(ctx, tx, command, contracts.CommandRequest{Payload: payload})
	_ = tx.Rollback()
	var apiErr *control.APIError
	if err == nil || !asAPI(err, &apiErr) || apiErr.Status != 403 {
		t.Fatalf("generic command grant submitted capability release: %v", err)
	}
	principal.Scopes = []string{"capability:release:submit", "capability:deployment:request", "capability:deployment:authorize"}
	command.Principal = principal
	tx = begin(t, store.DB())
	result, _, err := svc.submitRelease(ctx, tx, command, contracts.CommandRequest{Payload: payload})
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var digest string
	if err = store.DB().QueryRowContext(ctx, `SELECT digest FROM capability_releases WHERE release_id=?`, result.ResourceID).Scan(&digest); err != nil {
		t.Fatal(err)
	}
	request, _ := json.Marshal(deploymentRequestPayload{ServiceID: "mail-monitor", ReleaseDigest: digest, Operation: "install"})
	tx = begin(t, store.DB())
	deployment, _, err := svc.requestDeployment(ctx, tx, command, contracts.CommandRequest{Payload: request})
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var jobs int
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM capability_deployment_jobs`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("deployment scheduled before grant: jobs=%d err=%v", jobs, err)
	}
	authorize, _ := json.Marshal(authorizePayload{DeploymentID: deployment.ResourceID, ScopeKind: "workspace", Actions: []string{"install", "start", "stop"}, Budget: manifest.Budget, ExpiresAt: time.Now().UTC().Add(time.Hour)})
	expected := int64(1)
	tx = begin(t, store.DB())
	_, _, err = svc.authorizeDeployment(ctx, tx, command, contracts.CommandRequest{ExpectedVersion: &expected, Payload: authorize})
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM capability_deployment_jobs WHERE deployment_id=? AND status='pending'`, deployment.ResourceID).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatalf("authorized deployment job not durably queued: jobs=%d err=%v", jobs, err)
	}
	var desired string
	if err = store.DB().QueryRowContext(ctx, `SELECT desired_state FROM capability_deployments WHERE deployment_id=?`, deployment.ResourceID).Scan(&desired); err != nil || desired != "installing" {
		t.Fatalf("desired state not set: %q err=%v", desired, err)
	}
}

func TestManifestRejectsUnsafeAndOverbroadInputs(t *testing.T) {
	base := contracts.CapabilityManifest{SchemaVersion: 1, ArtifactRef: "oci://registry.example/app@sha256:" + strings.Repeat("b", 64), ArtifactDigest: "sha256:" + strings.Repeat("b", 64), Entrypoint: "/app/run", Capabilities: []string{"mail.read"}, Budget: contracts.CapabilityResourceBudget{CPUMillis: 1, MemoryBytes: 1, DiskBytes: 1, TimeoutSeconds: 1, Concurrency: 1}, Network: contracts.CapabilityNetworkPolicy{Mode: "none"}}
	if err := validateManifest(&base); err != nil {
		t.Fatal(err)
	}
	base.Mounts = []contracts.CapabilityMount{{VolumeID: "user-volume", Target: "/../../host", ReadOnly: false}}
	if err := validateManifest(&base); err == nil {
		t.Fatal("unsafe mount target accepted")
	}
}

func TestRunnerDeploymentClaimAndFencedObservation(t *testing.T) {
	ctx := context.Background()
	runnerToken := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	store, err := storage.Open(ctx, storage.Config{Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = storage.RegisterCoreMigrations(store); err != nil {
		t.Fatal(err)
	}
	server, err := control.NewServer(store, control.Config{RunnerToken: runnerToken, RunnerID: "runner-test", RunnerWorkspaceIDs: []string{"default"}, RunnerScopes: []string{"capability:deploy"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = Register(server, store); err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	svc := &Service{db: store.DB(), server: server}
	manifest := contracts.CapabilityManifest{SchemaVersion: 1, ArtifactRef: "oci://registry.example/app@sha256:" + strings.Repeat("c", 64), ArtifactDigest: "sha256:" + strings.Repeat("c", 64), Entrypoint: "/app/run", Capabilities: []string{"mail.read"}, Budget: contracts.CapabilityResourceBudget{CPUMillis: 50, MemoryBytes: 100, DiskBytes: 100, TimeoutSeconds: 10, Concurrency: 1}, Network: contracts.CapabilityNetworkPolicy{Mode: "none"}}
	principal := contracts.Principal{WorkspaceID: "default", SubjectID: "owner", Scopes: []string{"capability:release:submit", "capability:deployment:request", "capability:deployment:authorize"}}
	command := control.CommandContext{CommandID: "cmd", Principal: principal, AcceptedAt: time.Now().UTC()}
	tx := begin(t, store.DB())
	release, _, err := svc.submitRelease(ctx, tx, command, contracts.CommandRequest{Payload: marshal(submitReleasePayload{Manifest: manifest})})
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var digest string
	if err = store.DB().QueryRowContext(ctx, `SELECT digest FROM capability_releases WHERE release_id=?`, release.ResourceID).Scan(&digest); err != nil {
		t.Fatal(err)
	}
	tx = begin(t, store.DB())
	dep, _, err := svc.requestDeployment(ctx, tx, command, contracts.CommandRequest{Payload: marshal(deploymentRequestPayload{ServiceID: "service", ReleaseDigest: digest, Operation: "install"})})
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	tx = begin(t, store.DB())
	exp := int64(1)
	_, _, err = svc.authorizeDeployment(ctx, tx, command, contracts.CommandRequest{ExpectedVersion: &exp, Payload: marshal(authorizePayload{DeploymentID: dep.ResourceID, ScopeKind: "workspace", Actions: []string{"install"}, Budget: manifest.Budget, ExpiresAt: time.Now().UTC().Add(time.Hour)})})
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	claimReq := httptest.NewRequest(http.MethodPost, "/runner/v1/deployments/claims", nil)
	claimReq.Header.Set("Authorization", "Bearer "+runnerToken)
	claimResp := httptest.NewRecorder()
	handler.ServeHTTP(claimResp, claimReq)
	if claimResp.Code != http.StatusOK {
		t.Fatalf("claim status=%d body=%s", claimResp.Code, claimResp.Body.String())
	}
	var claim contracts.DeploymentClaim
	if err = json.Unmarshal(claimResp.Body.Bytes(), &claim); err != nil {
		t.Fatal(err)
	}
	if claim.DeploymentID != dep.ResourceID || claim.Fence != 1 || claim.DesiredOp != "install" {
		t.Fatalf("bad deployment claim: %#v", claim)
	}
	obs := contracts.DeploymentObservation{DeploymentID: claim.DeploymentID, AttemptID: claim.AttemptID, Fence: claim.Fence, ObservationID: "obs-one", Operation: "install", State: "running", ExternalEffectRef: []string{"container:test-id"}, Health: contracts.CapabilityHealth{State: "healthy", CheckedAt: time.Now().UTC(), EvidenceRef: "health:test"}, ObservedAt: time.Now().UTC()}
	body, _ := json.Marshal(obs)
	obsReq := httptest.NewRequest(http.MethodPost, "/runner/v1/deployments/"+claim.DeploymentID+"/observations", strings.NewReader(string(body)))
	obsReq.Header.Set("Authorization", "Bearer "+runnerToken)
	obsResp := httptest.NewRecorder()
	handler.ServeHTTP(obsResp, obsReq)
	if obsResp.Code != http.StatusAccepted {
		t.Fatalf("observation status=%d body=%s", obsResp.Code, obsResp.Body.String())
	}
	retryReq := httptest.NewRequest(http.MethodPost, "/runner/v1/deployments/"+claim.DeploymentID+"/observations", strings.NewReader(string(body)))
	retryReq.Header.Set("Authorization", "Bearer "+runnerToken)
	retryResp := httptest.NewRecorder()
	handler.ServeHTTP(retryResp, retryReq)
	if retryResp.Code != http.StatusAccepted {
		t.Fatalf("identical observation retry status=%d body=%s", retryResp.Code, retryResp.Body.String())
	}
	var observations int
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM capability_observations WHERE attempt_id=?`, claim.AttemptID).Scan(&observations); err != nil || observations != 1 {
		t.Fatalf("observation retry duplicated persistence: count=%d err=%v", observations, err)
	}
	var state string
	if err = store.DB().QueryRowContext(ctx, `SELECT observed_state FROM capability_deployments WHERE deployment_id=?`, claim.DeploymentID).Scan(&state); err != nil || state != "running" {
		t.Fatalf("observed state not persisted: %q err=%v", state, err)
	}
	obs.Fence++
	body, _ = json.Marshal(obs)
	badReq := httptest.NewRequest(http.MethodPost, "/runner/v1/deployments/"+claim.DeploymentID+"/observations", strings.NewReader(string(body)))
	badReq.Header.Set("Authorization", "Bearer "+runnerToken)
	badResp := httptest.NewRecorder()
	handler.ServeHTTP(badResp, badReq)
	if badResp.Code != http.StatusConflict {
		t.Fatalf("stale fence status=%d body=%s", badResp.Code, badResp.Body.String())
	}
}

func begin(t *testing.T, db *sql.DB) *sql.Tx {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return tx
}
func asAPI(err error, target **control.APIError) bool {
	if err == nil {
		return false
	}
	if v, ok := err.(*control.APIError); ok {
		*target = v
		return true
	}
	return false
}

func marshal(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
