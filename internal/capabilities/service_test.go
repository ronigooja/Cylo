package capabilities

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
	insertCapabilityTestDevice(t, store, "device", "owner")
	server, err := control.NewServer(store, control.Config{})
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{db: store.DB(), server: server, availableCapabilityIDs: map[string]struct{}{"mail.read": {}}}
	manifest := contracts.CapabilityManifest{SchemaVersion: 1, ArtifactRef: "oci://registry.example/cylo/mail@sha256:" + strings.Repeat("a", 64), ArtifactDigest: "sha256:" + strings.Repeat("a", 64), Entrypoint: "/app/run", Capabilities: []string{"mail.read"}, Budget: contracts.CapabilityResourceBudget{CPUMillis: 500, MemoryBytes: 1 << 20, DiskBytes: 1 << 20, TimeoutSeconds: 60, Concurrency: 1}, Network: contracts.CapabilityNetworkPolicy{Mode: "none"}, HealthCheck: &contracts.CapabilityHealthCheck{Kind: "http", Target: "/health", Port: 8080, IntervalSeconds: 10, TimeoutSeconds: 2, FailureThreshold: 2}}
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
	unregistered := manifest
	unregistered.Capabilities = []string{"mail.send"}
	tx = begin(t, store.DB())
	_, _, err = svc.submitRelease(ctx, tx, command, contracts.CommandRequest{Payload: marshal(submitReleasePayload{Manifest: unregistered})})
	_ = tx.Rollback()
	var registryErr *control.APIError
	if !asAPI(err, &registryErr) || registryErr.Code != "capability_not_registered" {
		t.Fatalf("unregistered capability ID was accepted: %v", err)
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
	command.Principal.DeviceID = ""
	tx = begin(t, store.DB())
	_, _, err = svc.authorizeDeployment(ctx, tx, command, contracts.CommandRequest{ExpectedVersion: &expected, Payload: authorize})
	_ = tx.Rollback()
	var deviceErr *control.APIError
	if !asAPI(err, &deviceErr) || deviceErr.Code != "device_required" {
		t.Fatalf("grant authorization without authenticated device was accepted: %v", err)
	}
	command.Principal.DeviceID = "device"
	tx = begin(t, store.DB())
	_, _, err = svc.authorizeDeployment(ctx, tx, command, contracts.CommandRequest{ExpectedVersion: &expected, Payload: authorize})
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var authorizedBy, authorizedByDevice string
	if err = store.DB().QueryRowContext(ctx, `SELECT authorized_by,authorized_by_device_id FROM capability_grants WHERE deployment_id=?`, deployment.ResourceID).Scan(&authorizedBy, &authorizedByDevice); err != nil {
		t.Fatal(err)
	}
	if authorizedBy != principal.SubjectID || authorizedByDevice != principal.DeviceID {
		t.Fatalf("grant authorization audit provenance=(%q,%q), want (%q,%q)", authorizedBy, authorizedByDevice, principal.SubjectID, principal.DeviceID)
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
	base := contracts.CapabilityManifest{SchemaVersion: 1, ArtifactRef: "oci://registry.example/app@sha256:" + strings.Repeat("b", 64), ArtifactDigest: "sha256:" + strings.Repeat("b", 64), Entrypoint: "/app/run", Capabilities: []string{"mail.read"}, HealthCheck: &contracts.CapabilityHealthCheck{Kind: "http", Target: "/health", Port: 8080, IntervalSeconds: 30, TimeoutSeconds: 5, FailureThreshold: 3}, Budget: contracts.CapabilityResourceBudget{CPUMillis: 1, MemoryBytes: 1, DiskBytes: 1, TimeoutSeconds: 1, Concurrency: 1}, Network: contracts.CapabilityNetworkPolicy{Mode: "none"}}
	if err := validateManifest(&base); err != nil {
		t.Fatal(err)
	}
	base.Triggers = []contracts.CapabilityTrigger{{Kind: "event", Source: "task.created", Mode: "worker"}}
	if err := validateManifest(&base); err != nil {
		t.Fatalf("worker trigger rejected: %v", err)
	}
	base.Triggers = []contracts.CapabilityTrigger{{Kind: "event", Source: "task.created", Mode: "super"}}
	if err := validateManifest(&base); err != nil {
		t.Fatalf("super trigger rejected: %v", err)
	}
	base.Triggers = nil
	base.HealthCheck = nil
	if err := validateManifest(&base); err == nil {
		t.Fatal("manifest without an executable health check was accepted")
	}
	base.HealthCheck = &contracts.CapabilityHealthCheck{Kind: "http", Target: "/health", Port: 8080, IntervalSeconds: 30, TimeoutSeconds: 5, FailureThreshold: 3}
	base.HealthCheck = &contracts.CapabilityHealthCheck{Kind: "exec", Target: "/bin/sh", Args: []string{"-c", "true"}, IntervalSeconds: 30, TimeoutSeconds: 5, FailureThreshold: 3}
	if err := validateManifest(&base); err == nil {
		t.Fatal("shell or host-path health check accepted")
	}
	base.HealthCheck = nil
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
	if err = control.RegisterInstanceMaintenanceMigration(store); err != nil {
		t.Fatal(err)
	}
	server, err := control.NewServer(store, control.Config{RunnerToken: runnerToken, RunnerID: "runner-test", RunnerWorkspaceIDs: []string{"default"}, RunnerScopes: []string{"capability:deploy"}, Profiles: map[string]control.RuntimeProfile{
		"worker-profile": {ID: "worker-profile", AOProjectID: "ao-project", RuntimeRouteID: "runtime", ModelRouteID: "model", Role: "worker", AOAgentKind: "worker", IsolationPolicy: "strict"},
		"super-profile":  {ID: "super-profile", AOProjectID: "ao-project", RuntimeRouteID: "runtime", ModelRouteID: "model", Role: "super", AOAgentKind: "worker", IsolationPolicy: "strict"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err = Register(server, store, Config{AvailableCapabilityIDs: []string{"mail.read"}, RuntimeBackend: runtimeBackendDocker}); err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	insertCapabilityTestDevice(t, store, "device", "owner")
	handler := server.Handler()
	svc := &Service{db: store.DB(), server: server, availableCapabilityIDs: map[string]struct{}{"mail.read": {}}, runtimeBackend: runtimeBackendDocker}
	manifest := contracts.CapabilityManifest{SchemaVersion: 1, ArtifactRef: "oci://registry.example/app@sha256:" + strings.Repeat("c", 64), ArtifactDigest: "sha256:" + strings.Repeat("c", 64), Entrypoint: "/app/run", Capabilities: []string{"mail.read"}, Triggers: []contracts.CapabilityTrigger{{Kind: "event", Source: "task.created", Mode: "program", Filters: []contracts.CapabilityTriggerFilter{{Field: "task_id", Operator: "eq", Value: "task-1"}}}, {Kind: "event", Source: "task.created", Mode: "worker", Scopes: []string{"mail.read"}}, {Kind: "event", Source: "task.created", Mode: "super", Scopes: []string{"mail.read"}}, {Kind: "schedule", Source: "schedule.interval", Mode: "program", IntervalSeconds: 60, MissedRunPolicy: "coalesce"}, {Kind: "schedule", Source: "schedule.interval", Mode: "super", IntervalSeconds: 60, MissedRunPolicy: "coalesce"}}, Budget: contracts.CapabilityResourceBudget{CPUMillis: 50, MemoryBytes: 100, DiskBytes: 100, TimeoutSeconds: 10, Concurrency: 1}, Network: contracts.CapabilityNetworkPolicy{Mode: "none"}, HealthCheck: &contracts.CapabilityHealthCheck{Kind: "http", Target: "/health", Port: 8080, IntervalSeconds: 10, TimeoutSeconds: 2, FailureThreshold: 2}}
	principal := contracts.Principal{WorkspaceID: "default", SubjectID: "owner", DeviceID: "device", Scopes: []string{"capability:release:submit", "capability:deployment:request", "capability:deployment:authorize"}}
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
	currentReq := httptest.NewRequest(http.MethodGet, "/runner/v1/deployments/claims/current", nil)
	currentReq.Header.Set("Authorization", "Bearer "+runnerToken)
	currentResp := httptest.NewRecorder()
	handler.ServeHTTP(currentResp, currentReq)
	if currentResp.Code != http.StatusOK {
		t.Fatalf("current claim status=%d body=%s", currentResp.Code, currentResp.Body.String())
	}
	var current contracts.DeploymentClaim
	if err = json.Unmarshal(currentResp.Body.Bytes(), &current); err != nil || current.AttemptID != claim.AttemptID || current.Fence != claim.Fence || current.ClaimState != "claimed" {
		t.Fatalf("current claim changed identity: current=%#v err=%v", current, err)
	}
	retryClaimReq := httptest.NewRequest(http.MethodPost, "/runner/v1/deployments/claims", nil)
	retryClaimReq.Header.Set("Authorization", "Bearer "+runnerToken)
	retryClaimResp := httptest.NewRecorder()
	handler.ServeHTTP(retryClaimResp, retryClaimReq)
	var retried contracts.DeploymentClaim
	if retryClaimResp.Code != http.StatusOK || json.Unmarshal(retryClaimResp.Body.Bytes(), &retried) != nil || retried.AttemptID != claim.AttemptID || retried.Fence != claim.Fence {
		t.Fatalf("claim retry created another attempt: status=%d body=%s", retryClaimResp.Code, retryClaimResp.Body.String())
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
	if _, err = store.DB().ExecContext(ctx, `UPDATE event_counters SET current_cursor=2 WHERE workspace_id='default'`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().ExecContext(ctx, `INSERT INTO events(event_id,workspace_id,cursor,schema_version,event_type,occurred_at,subject_id,aggregate_type,aggregate_id,aggregate_version,payload) VALUES('trigger-event-1','default',1,1,'task.created',?,'owner','task','task-1',1,'{"type":"task.created","payload":{"task":{"task_id":"task-1"}}}'),('trigger-event-2','default',2,1,'task.created',?,'owner','task','task-2',1,'{"type":"task.created","payload":{"task":{"task_id":"task-2"}}}')`, time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	var enabledTriggers int
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM capability_trigger_definitions WHERE enabled=1`).Scan(&enabledTriggers); err != nil || enabledTriggers != 5 {
		t.Fatalf("triggers were not enabled: count=%d err=%v", enabledTriggers, err)
	}
	triggerClaimRequest := httptest.NewRequest(http.MethodPost, "/runner/v1/capability-triggers/claims", nil)
	triggerClaimRequest.Header.Set("Authorization", "Bearer "+runnerToken)
	triggerClaimResponse := httptest.NewRecorder()
	handler.ServeHTTP(triggerClaimResponse, triggerClaimRequest)
	if triggerClaimResponse.Code != http.StatusOK {
		t.Fatalf("trigger claim status=%d body=%s", triggerClaimResponse.Code, triggerClaimResponse.Body.String())
	}
	var triggerClaim contracts.CapabilityTriggerClaim
	if err = json.Unmarshal(triggerClaimResponse.Body.Bytes(), &triggerClaim); err != nil || triggerClaim.EventID != "trigger-event-1" || triggerClaim.Mode != "program" || string(triggerClaim.EventPayload) != `{"task":{"task_id":"task-1"}}` {
		t.Fatalf("bad trigger claim: %#v err=%v", triggerClaim, err)
	}
	var admittedTasks int
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE source='service'`).Scan(&admittedTasks); err != nil || admittedTasks != 2 {
		t.Fatalf("worker/super event triggers admitted %d tasks, want 2 (err=%v)", admittedTasks, err)
	}
	var workerTasks, superTasks int
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE source='service' AND profile_id='worker-profile'`).Scan(&workerTasks); err != nil || workerTasks != 1 {
		t.Fatalf("worker trigger profile count=%d err=%v", workerTasks, err)
	}
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE source='service' AND profile_id='super-profile'`).Scan(&superTasks); err != nil || superTasks != 1 {
		t.Fatalf("super trigger profile count=%d err=%v", superTasks, err)
	}
	var admittedRuns, admissionEvents int
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM capability_trigger_runs WHERE mode IN ('worker','super') AND state='running' AND task_id<>''`).Scan(&admittedRuns); err != nil || admittedRuns != 2 {
		t.Fatalf("worker/super run-to-task links=%d err=%v", admittedRuns, err)
	}
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM events WHERE event_type='capability.trigger.task_admitted'`).Scan(&admissionEvents); err != nil || admissionEvents != 2 {
		t.Fatalf("trusted task admission events=%d err=%v", admissionEvents, err)
	}
	var queuedTaskRuns int
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM capability_trigger_runs WHERE mode IN ('worker','super') AND state='queued'`).Scan(&queuedTaskRuns); err != nil || queuedTaskRuns != 2 {
		t.Fatalf("concurrency limit did not leave one run queued per role: count=%d err=%v", queuedTaskRuns, err)
	}
	triggerCurrentRequest := httptest.NewRequest(http.MethodGet, "/runner/v1/capability-triggers/claims/current", nil)
	triggerCurrentRequest.Header.Set("Authorization", "Bearer "+runnerToken)
	triggerCurrentResponse := httptest.NewRecorder()
	handler.ServeHTTP(triggerCurrentResponse, triggerCurrentRequest)
	var triggerCurrent contracts.CapabilityTriggerClaim
	if triggerCurrentResponse.Code != http.StatusOK || json.Unmarshal(triggerCurrentResponse.Body.Bytes(), &triggerCurrent) != nil || triggerCurrent.AttemptID != triggerClaim.AttemptID || triggerCurrent.Fence != triggerClaim.Fence {
		t.Fatalf("trigger replay changed claim: status=%d body=%s", triggerCurrentResponse.Code, triggerCurrentResponse.Body.String())
	}
	triggerObservation := contracts.CapabilityTriggerObservation{TriggerRunID: triggerClaim.TriggerRunID, AttemptID: triggerClaim.AttemptID, Fence: triggerClaim.Fence, ObservationID: "trigger-completed", State: "completed", ObservedAt: time.Now().UTC()}
	triggerObservationRequest := httptest.NewRequest(http.MethodPost, "/runner/v1/capability-triggers/"+triggerClaim.TriggerRunID+"/observations", strings.NewReader(string(marshal(triggerObservation))))
	triggerObservationRequest.Header.Set("Authorization", "Bearer "+runnerToken)
	triggerObservationResponse := httptest.NewRecorder()
	handler.ServeHTTP(triggerObservationResponse, triggerObservationRequest)
	if triggerObservationResponse.Code != http.StatusAccepted {
		t.Fatalf("trigger observation status=%d body=%s", triggerObservationResponse.Code, triggerObservationResponse.Body.String())
	}
	triggerDuplicateRequest := httptest.NewRequest(http.MethodPost, "/runner/v1/capability-triggers/claims", nil)
	triggerDuplicateRequest.Header.Set("Authorization", "Bearer "+runnerToken)
	triggerDuplicateResponse := httptest.NewRecorder()
	handler.ServeHTTP(triggerDuplicateResponse, triggerDuplicateRequest)
	if triggerDuplicateResponse.Code != http.StatusNoContent {
		t.Fatalf("duplicate event produced another claim: status=%d body=%s", triggerDuplicateResponse.Code, triggerDuplicateResponse.Body.String())
	}
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE source='service'`).Scan(&admittedTasks); err != nil || admittedTasks != 2 {
		t.Fatalf("replaying trigger events duplicated admitted tasks: count=%d err=%v", admittedTasks, err)
	}
	if _, err = store.DB().ExecContext(ctx, `UPDATE capability_trigger_definitions SET next_run_at=? WHERE kind='schedule'`, time.Now().UTC().Add(-10*time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	scheduleRequest := httptest.NewRequest(http.MethodPost, "/runner/v1/capability-triggers/claims", nil)
	scheduleRequest.Header.Set("Authorization", "Bearer "+runnerToken)
	scheduleResponse := httptest.NewRecorder()
	handler.ServeHTTP(scheduleResponse, scheduleRequest)
	var scheduleClaim contracts.CapabilityTriggerClaim
	if scheduleResponse.Code != http.StatusOK || json.Unmarshal(scheduleResponse.Body.Bytes(), &scheduleClaim) != nil || scheduleClaim.ScheduledFor.IsZero() {
		t.Fatalf("coalesced schedule claim=%d body=%s", scheduleResponse.Code, scheduleResponse.Body.String())
	}
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE source='service'`).Scan(&admittedTasks); err != nil || admittedTasks != 3 {
		t.Fatalf("scheduled super trigger did not admit exactly one task: tasks=%d err=%v", admittedTasks, err)
	}
	scheduleObservation := contracts.CapabilityTriggerObservation{TriggerRunID: scheduleClaim.TriggerRunID, AttemptID: scheduleClaim.AttemptID, Fence: scheduleClaim.Fence, ObservationID: "schedule-completed", State: "completed", ObservedAt: time.Now().UTC()}
	scheduleObservationRequest := httptest.NewRequest(http.MethodPost, "/runner/v1/capability-triggers/"+scheduleClaim.TriggerRunID+"/observations", strings.NewReader(string(marshal(scheduleObservation))))
	scheduleObservationRequest.Header.Set("Authorization", "Bearer "+runnerToken)
	scheduleObservationResponse := httptest.NewRecorder()
	handler.ServeHTTP(scheduleObservationResponse, scheduleObservationRequest)
	if scheduleObservationResponse.Code != http.StatusAccepted {
		t.Fatalf("schedule observation=%d body=%s", scheduleObservationResponse.Code, scheduleObservationResponse.Body.String())
	}
	// Saturated old work must not consume the bounded candidate page and hide
	// an eligible trigger later in the workspace queue.
	var eventTrigger, activeGrant, activeDigest, activeConfigRef, activeConfigHash string
	var activeVersion int64
	if err = store.DB().QueryRowContext(ctx, `SELECT trigger_id FROM capability_trigger_definitions WHERE deployment_id=? AND kind='event'`, dep.ResourceID).Scan(&eventTrigger); err != nil {
		t.Fatal(err)
	}
	if err = store.DB().QueryRowContext(ctx, `SELECT grant_id,release_digest,config_ref,config_sha256,version FROM capability_deployments WHERE deployment_id=?`, dep.ResourceID).Scan(&activeGrant, &activeDigest, &activeConfigRef, &activeConfigHash, &activeVersion); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().ExecContext(ctx, `UPDATE capability_trigger_runs SET state='running',runner_id='',claim_json=NULL WHERE trigger_run_id=(SELECT trigger_run_id FROM capability_trigger_runs WHERE trigger_id=? ORDER BY trigger_run_id LIMIT 1)`, eventTrigger); err != nil {
		t.Fatal(err)
	}
	var eventKey string
	if err = store.DB().QueryRowContext(ctx, `SELECT trigger_key FROM capability_trigger_definitions WHERE trigger_id=?`, eventTrigger).Scan(&eventKey); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < triggerEventPageSize; i++ {
		id := fmt.Sprintf("blocked-run-%03d", i)
		if _, err = store.DB().ExecContext(ctx, `INSERT INTO capability_trigger_runs(trigger_run_id,trigger_id,workspace_id,deployment_id,trigger_key,dedupe_key,mode,state,created_at,updated_at) VALUES(?,?,?,?,?,?,'program','queued',?,?)`, id, eventTrigger, "default", dep.ResourceID, eventKey, "blocked-"+id, "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.DB().ExecContext(ctx, `INSERT INTO capability_deployments(deployment_id,workspace_id,service_id,release_digest,requested_operation,desired_state,observed_state,grant_id,config_ref,config_sha256,version,created_by,created_at,updated_at) VALUES('fair-deployment','default','fair-service',?,'install','running','running','fair-grant',?,?,?,'owner',?,?)`, activeDigest, activeConfigRef, activeConfigHash, activeVersion, time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().ExecContext(ctx, `INSERT INTO capability_grants(grant_id,workspace_id,deployment_id,release_digest,scope_kind,scope_id,principal_id,actions,budget,expires_at,authorized_by,created_at) SELECT 'fair-grant',workspace_id,'fair-deployment',release_digest,scope_kind,scope_id,principal_id,actions,budget,expires_at,authorized_by,created_at FROM capability_grants WHERE grant_id=?`, activeGrant); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().ExecContext(ctx, `INSERT INTO capability_trigger_definitions(trigger_id,workspace_id,deployment_id,release_digest,grant_id,trigger_key,kind,source,mode,scopes,filters,interval_seconds,missed_run_policy,enabled,expected_deployment_version,config_ref,config_sha256,created_at,updated_at) SELECT 'fair-trigger',workspace_id,'fair-deployment',release_digest,'fair-grant',trigger_key,kind,source,mode,scopes,filters,interval_seconds,missed_run_policy,1,expected_deployment_version,config_ref,config_sha256,?,? FROM capability_trigger_definitions WHERE trigger_id=?`, time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano), eventTrigger); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().ExecContext(ctx, `INSERT INTO capability_trigger_runs(trigger_run_id,trigger_id,workspace_id,deployment_id,trigger_key,dedupe_key,mode,state,created_at,updated_at) SELECT 'fair-eligible-run','fair-trigger',workspace_id,'fair-deployment',trigger_key,'fair-eligible','program','queued',?,? FROM capability_trigger_definitions WHERE trigger_id='fair-trigger'`, time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	fairClaimRequest := httptest.NewRequest(http.MethodPost, "/runner/v1/capability-triggers/claims", nil)
	fairClaimRequest.Header.Set("Authorization", "Bearer "+runnerToken)
	fairClaimResponse := httptest.NewRecorder()
	handler.ServeHTTP(fairClaimResponse, fairClaimRequest)
	var fairClaim contracts.CapabilityTriggerClaim
	if fairClaimResponse.Code != http.StatusOK || json.Unmarshal(fairClaimResponse.Body.Bytes(), &fairClaim) != nil || fairClaim.DeploymentID != "fair-deployment" {
		t.Fatalf("eligible trigger behind saturated queue not claimed: status=%d body=%s", fairClaimResponse.Code, fairClaimResponse.Body.String())
	}
	if _, err = store.DB().ExecContext(ctx, `UPDATE capability_grants SET expires_at=? WHERE grant_id=?`, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), activeGrant); err != nil {
		t.Fatal(err)
	}
	tx = begin(t, store.DB())
	if err = svc.admitQueuedTaskTriggers(ctx, tx, "default", time.Now().UTC()); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var expiredTaskRuns, serviceTasks int
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM capability_trigger_runs WHERE deployment_id=? AND mode IN ('worker','super') AND state='queued'`, dep.ResourceID).Scan(&expiredTaskRuns); err != nil || expiredTaskRuns != 0 {
		t.Fatalf("expired grant left queued task triggers: count=%d err=%v", expiredTaskRuns, err)
	}
	var canceledExpiredRuns int
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM capability_trigger_runs WHERE deployment_id=? AND mode IN ('worker','super') AND state='canceled' AND last_error_code='grant_expired'`, dep.ResourceID).Scan(&canceledExpiredRuns); err != nil || canceledExpiredRuns != 2 {
		t.Fatalf("expired worker/super runs were not durably canceled: count=%d err=%v", canceledExpiredRuns, err)
	}
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE source='service'`).Scan(&serviceTasks); err != nil || serviceTasks != 3 {
		t.Fatalf("expired grant admitted another task: count=%d err=%v", serviceTasks, err)
	}
	// A new grant for the same deployment must not reactivate definitions that
	// were created under its previous grant, even when the deployment is running.
	var currentGrant, currentDigest string
	if err = store.DB().QueryRowContext(ctx, `SELECT grant_id,release_digest FROM capability_deployments WHERE deployment_id=?`, dep.ResourceID).Scan(&currentGrant, &currentDigest); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().ExecContext(ctx, `INSERT INTO capability_grants(grant_id,workspace_id,deployment_id,release_digest,scope_kind,scope_id,principal_id,actions,budget,expires_at,authorized_by,created_at) SELECT 'grant-rollover','default',deployment_id,release_digest,scope_kind,scope_id,principal_id,actions,budget,expires_at,authorized_by,created_at FROM capability_grants WHERE grant_id=?`, currentGrant); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().ExecContext(ctx, `UPDATE capability_deployments SET grant_id='grant-rollover' WHERE deployment_id=?`, dep.ResourceID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().ExecContext(ctx, `INSERT INTO capability_trigger_runs(trigger_run_id,trigger_id,workspace_id,deployment_id,trigger_key,dedupe_key,mode,state,created_at,updated_at) SELECT 'obsolete-queued-run',trigger_id,workspace_id,deployment_id,trigger_key,'rollover-test',mode,'queued',?,? FROM capability_trigger_definitions WHERE deployment_id=? LIMIT 1`, time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano), dep.ResourceID); err != nil {
		t.Fatal(err)
	}
	tx = begin(t, store.DB())
	if err = svc.enableDeploymentTriggers(ctx, tx, dep.ResourceID, true, time.Now().UTC()); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM capability_trigger_definitions WHERE deployment_id=? AND enabled=1`, dep.ResourceID).Scan(&enabledTriggers); err != nil || enabledTriggers != 0 {
		t.Fatalf("old grant definitions were reactivated: count=%d err=%v", enabledTriggers, err)
	}
	var obsoleteState string
	if err = store.DB().QueryRowContext(ctx, `SELECT state FROM capability_trigger_runs WHERE trigger_run_id='obsolete-queued-run'`).Scan(&obsoleteState); err != nil || obsoleteState != "canceled" {
		t.Fatalf("queued work from old grant was not canceled: state=%q err=%v", obsoleteState, err)
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

func insertCapabilityTestDevice(t *testing.T, store *storage.Store, deviceID, subjectID string) {
	t.Helper()
	if _, err := store.DB().ExecContext(context.Background(), `INSERT INTO devices(id,workspace_id,subject_id,name,status,token_hash,created_at) VALUES(?, 'default', ?, 'capability test', 'approved', X'01', ?)`, deviceID, subjectID, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
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
