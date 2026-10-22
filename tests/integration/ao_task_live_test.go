package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/agents/ao"
	"github.com/ronigooja/Cylo/internal/control"
	"github.com/ronigooja/Cylo/internal/runner"
	"github.com/ronigooja/Cylo/internal/storage"
)

// TestAOLiveCyloTaskReceiptRunnerCodexProof is an opt-in end-to-end acceptance
// probe for task admission, the independent runner Engine, AO Codex execution,
// a real tool artifact, and both AO process-wait and exact container stop proof.
func TestAOLiveCyloTaskReceiptRunnerCodexProof(t *testing.T) {
	aoURL := strings.TrimSpace(os.Getenv("CYLO_LIVE_AO_URL"))
	aoProject := strings.TrimSpace(os.Getenv("CYLO_LIVE_AO_PROJECT_ID"))
	workspaceRoot := strings.TrimSpace(os.Getenv("CYLO_LIVE_AO_WORKSPACE_ROOT"))
	aoDataDir := strings.TrimSpace(os.Getenv("CYLO_LIVE_AO_DATA_DIR"))
	evidenceRoot := strings.TrimSpace(os.Getenv("CYLO_LIVE_AO_EVIDENCE_ROOT"))
	if aoURL == "" || aoProject == "" || workspaceRoot == "" || aoDataDir == "" || evidenceRoot == "" {
		t.Skip("set CYLO_LIVE_AO_URL, CYLO_LIVE_AO_PROJECT_ID, CYLO_LIVE_AO_WORKSPACE_ROOT, CYLO_LIVE_AO_DATA_DIR, and CYLO_LIVE_AO_EVIDENCE_ROOT for local AO acceptance")
	}
	image := strings.TrimSpace(os.Getenv("CYLO_LIVE_AO_IMAGE"))
	if image == "" {
		image = "cylo-ao-codex:0.146.0"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	testEvidenceDir, err := createLiveEvidenceDir(evidenceRoot, "task")
	if err != nil {
		t.Fatalf("create private live evidence directory failed (%T)", err)
	}
	bootstrapCode, deviceToken, runnerToken := liveToken(t), liveToken(t), liveToken(t)
	profileID := "codex-live-worker"
	profile := control.RuntimeProfile{
		ID: profileID, AOProjectID: aoProject, RuntimeRouteID: "codex", ModelRouteID: "gpt-6-luna",
		Role: "worker", AOAgentKind: "worker", IsolationPolicy: runner.CodexWorkspaceWritePolicy,
		CapabilityIDs: []string{"workspace.read", "workspace.write"},
		Scopes:        []string{"workspace:read", "workspace:write"},
	}
	engineJoined := true
	dbPath := filepath.Join(testEvidenceDir, "control.db")
	store, err := storage.Open(ctx, storage.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open isolated control database failed (%T)", err)
	}
	defer func() {
		if engineJoined {
			_ = store.Close()
		}
	}()
	if err := storage.RegisterCoreMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err := control.RegisterInstanceMaintenanceMigration(store); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	server, err := control.NewServer(store, control.Config{
		BootstrapCode: bootstrapCode,
		RunnerToken:   runnerToken, RunnerID: "live-codex-runner", RunnerWorkspaceIDs: []string{"default"},
		RunnerScopes:     []string{"runner:claim", "runner:observe", "runner:control:read"},
		DefaultProfileID: profileID, Profiles: map[string]control.RuntimeProfile{profileID: profile},
	})
	if err != nil {
		t.Fatalf("create isolated control server failed (%T)", err)
	}
	if err := control.RegisterInstanceRoutes(server); err != nil {
		t.Fatalf("register runner instance routes failed (%T)", err)
	}
	controlHTTP := httptest.NewServer(server.Handler())
	defer func() {
		if engineJoined {
			controlHTTP.Close()
		}
	}()
	_, status, err := liveControlRequest(t, controlHTTP.Client(), controlHTTP.URL, http.MethodPost, "/v1/bootstrap", "", "", contracts.BootstrapRequest{RequestID: "live-bootstrap", Code: bootstrapCode, DeviceName: "Codex live acceptance", DeviceToken: deviceToken})
	if err != nil || status != http.StatusCreated {
		t.Fatalf("bootstrap device status=%d request_error=%T", status, err)
	}
	projectReceipt, status, _ := liveCommand(t, controlHTTP.Client(), controlHTTP.URL, deviceToken, "live-project", "project.create", map[string]any{"name": "AO Codex live acceptance"})
	if status != http.StatusOK {
		t.Fatalf("create Cylo project receipt status=%d", status)
	}
	taskReceipt, status, _ := liveCommand(t, controlHTTP.Client(), controlHTTP.URL, deviceToken, "live-task", "task.create", map[string]any{
		"project_id": projectReceipt.ResourceID,
		"title":      "Create a verified workspace artifact",
		"goal":       "Use your shell tool to create exactly one file named CYLO_RUNTIME_TOOL_PROBE.txt in the current workspace with exactly CYLO_AO_TOOL_OK followed by a newline. Read it back. Then attempt once to create /etc/CYLO_RUNTIME_OUTSIDE_PROBE.txt; do not retry by another path. Report workspace_write=ok outside_write=denied only if both outcomes match.",
		"source":     "user",
	})
	if status != http.StatusOK || taskReceipt.ResourceID == "" {
		t.Fatalf("create Cylo task receipt status=%d resource_present=%t", status, taskReceipt.ResourceID != "")
	}

	controlClient, err := runner.NewControlClient(controlHTTP.URL, runnerToken, controlHTTP.Client())
	if err != nil {
		t.Fatal(err)
	}
	aoClient, err := ao.New(ao.Config{BaseURL: aoURL, Token: "isolated-local-probe"})
	if err != nil {
		t.Fatalf("configure local AO client failed (%T)", err)
	}
	docker := runner.ExecDockerCommand{Binary: "docker"}
	supervisor := runner.DockerAOContainerSupervisor{Docker: docker}
	var claim *contracts.JobClaim
	var journal *runner.Journal
	var sessionID, containerID string
	stopVerified := false
	createReplayAttempts := 0
	cleanupAO := func() {
		if stopVerified {
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cleanupCancel()
		if sessionID == "" {
			if entry, found, entryErr := journal.Entry(cleanupCtx, claim.RunID); entryErr == nil && found && validLiveJournalIdentity(entry, *claim) {
				if entry.Session != "" {
					sessionID = entry.Session
					containerID = entry.Container
				} else if entry.Phase == "effect_unknown" {
					session, reconcileErr := reconcileUnknownJournalCreate(cleanupCtx, aoClient, entry, *claim, &createReplayAttempts)
					if reconcileErr == nil {
						sessionID = session.ID
						if setErr := journal.SetSession(cleanupCtx, claim.RunID, claim.AttemptID, claim.Fence, sessionID, "bound"); setErr != nil {
							sessionID = ""
						}
					}
				}
			}
		}
		if sessionID == "" {
			if err := writeUnknownLiveCreateEvidence(evidenceRoot, "task-runner", claim.RunID, aoProject, createReplayAttempts); err != nil {
				t.Errorf("persist unresolved task AO create evidence failed (%T)", err)
			}
			return
		}
		if containerID == "" {
			if foundID, found, findErr := supervisor.Find(cleanupCtx, "codex", sessionID); findErr != nil {
				t.Errorf("inspect exact AO container during cleanup failed (%T)", findErr)
			} else if found {
				containerID = foundID
			}
		}
		if stopErr := stopExactLiveWorker(cleanupCtx, aoClient, sessionID); stopErr != nil {
			t.Errorf("cleanup AO worker failed (%T)", stopErr)
		}
		if containerID != "" {
			proof, cleanupErr := supervisor.StopAndVerify(cleanupCtx, "codex", sessionID, containerID)
			if cleanupErr != nil || !proof.CleanupComplete || proof.EffectRef != "docker:"+containerID {
				t.Errorf("exact container cleanup could not be verified (error=%T)", cleanupErr)
			}
		}
	}
	claim, err = controlClient.Claim(ctx)
	if err != nil || claim == nil {
		t.Fatalf("claim admitted task failed: claim_present=%t error_type=%T", claim != nil, err)
	}
	if claim.TaskID != taskReceipt.ResourceID || claim.ProfileID != profileID || claim.AOProjectID != aoProject || claim.IsolationPolicy != runner.CodexWorkspaceWritePolicy {
		t.Fatalf("server claim identity mismatch: task_match=%t profile_match=%t project_match=%t isolation_match=%t", claim.TaskID == taskReceipt.ResourceID, claim.ProfileID == profileID, claim.AOProjectID == aoProject, claim.IsolationPolicy == runner.CodexWorkspaceWritePolicy)
	}
	journal, err = runner.OpenJournal(ctx, filepath.Join(testEvidenceDir, "runner.db"))
	if err != nil {
		t.Fatalf("open independent runner journal failed (%T)", err)
	}
	defer func() {
		if engineJoined {
			_ = journal.Close()
		}
	}()
	engine := &runner.Engine{
		Control: controlClient, Instance: controlClient, AO: aoClient, Journal: journal,
		Containers: supervisor,
		Isolation:  runner.SupervisedCodexIsolation{Docker: docker, Image: image},
		Routes: map[string]runner.Route{profileID: {
			ProfileID: profileID, RuntimeRouteID: profile.RuntimeRouteID, ModelRouteID: profile.ModelRouteID,
			IsolationPolicy: profile.IsolationPolicy, Harness: "codex", Model: "gpt-6-luna", ApprovalMode: "auto",
		}},
		PollInterval: 2 * time.Second,
	}
	finished, engineDone := startLiveEngine(ctx, engine, *claim)
	engineJoined = false
	// Stop and join the engine, then clean its exact session/container before
	// closing the journal or store, including on a fatal assertion.
	defer func() {
		cancel()
		select {
		case <-engineDone:
			engineJoined = true
		case <-time.After(30 * time.Second):
			t.Error("runner Engine did not stop after the live test context was cancelled")
			engineJoined = false
			return
		}
		if sessionID == "" {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			if entry, found, err := journal.Entry(cleanupCtx, claim.RunID); err != nil {
				t.Errorf("read exact runner session during cleanup failed (%T)", err)
			} else if found {
				sessionID = entry.Session
				containerID = entry.Container
			}
			cleanupCancel()
		}
		cleanupAO()
	}()

	containerID, containerWorkspace := "", ""
	for containerID == "" {
		select {
		case runErr := <-finished:
			<-engineDone
			entry, found, entryErr := journal.Entry(ctx, claim.RunID)
			if entryErr != nil {
				t.Fatalf("read runner journal after Engine exit failed (%T)", entryErr)
			}
			if entry.Session != "" {
				sessionID = entry.Session
				if id, found, findErr := supervisor.Find(ctx, "codex", entry.Session); findErr == nil && found {
					containerID = id
				}
			}
			terminalKind, noEffectOutcome := "", ""
			var terminalBody []byte
			if queryErr := store.DB().QueryRowContext(ctx, `SELECT body FROM runner_observations WHERE run_id=? AND kind='no_effect_terminal' ORDER BY occurred_at DESC LIMIT 1`, claim.RunID).Scan(&terminalBody); queryErr == nil {
				var terminal contracts.RunnerObservation
				if json.Unmarshal(terminalBody, &terminal) == nil {
					terminalKind = terminal.Kind
					if terminal.NoEffectProof != nil {
						noEffectOutcome = terminal.NoEffectProof.Outcome
					}
				}
			}
			if errors.Is(runErr, runner.ErrOutcomePending) && found && entry.Session == "" && entry.Phase == "effect_unknown" && validLiveJournalIdentity(entry, *claim) {
				session, reconcileErr := reconcileUnknownJournalCreate(ctx, aoClient, entry, *claim, &createReplayAttempts)
				if reconcileErr == nil {
					if entry.Session != "" && entry.Session != session.ID {
						t.Fatalf("same-key AO replay returned a conflicting session identity")
					}
					if err := journal.SetSession(ctx, claim.RunID, claim.AttemptID, claim.Fence, session.ID, "bound"); err != nil {
						t.Fatalf("bind reconciled original AO session to the exact runner claim failed (%T)", err)
					}
					sessionID = session.ID
					finished, engineDone = startLiveEngine(ctx, engine, *claim)
					continue
				}
				_ = writeUnknownLiveCreateEvidence(evidenceRoot, "task-runner", claim.RunID, aoProject, createReplayAttempts)
				t.Fatalf("task AO create remained unknown after bounded same-key reconciliation (attempts=%d)", createReplayAttempts)
			}
			t.Fatalf("runner Engine ended before supervised container observation: err_present=%t err_type=%T journal_found=%t phase=%q session_bound=%t terminal_kind=%q no_effect_outcome=%q claim_profile_match=%t claim_runtime_route_match=%t claim_model_route_match=%t claim_project_match=%t claim_isolation_match=%t",
				runErr != nil, runErr, found, entry.Phase, entry.Session != "", terminalKind, noEffectOutcome,
				claim.ProfileID == profile.ID, claim.RuntimeRouteID == profile.RuntimeRouteID,
				claim.ModelRouteID == profile.ModelRouteID, claim.AOProjectID == profile.AOProjectID,
				claim.IsolationPolicy == profile.IsolationPolicy)
		case <-ctx.Done():
			t.Fatal("timed out before AO created the supervised Codex container")
		case <-time.After(250 * time.Millisecond):
		}
		entry, ok, entryErr := journal.Entry(ctx, claim.RunID)
		if entryErr != nil {
			t.Fatal(entryErr)
		}
		if ok && entry.Session != "" {
			sessionID = entry.Session
			if id, found, findErr := supervisor.Find(ctx, "codex", entry.Session); findErr != nil {
				t.Fatalf("inspect exact AO session container failed (%T)", findErr)
			} else if found {
				containerID = id
				containerWorkspace = assertLiveContainerBoundary(t, ctx, docker, id, aoDataDir, workspaceRoot, entry.Session)
			}
		}
	}

	select {
	case err := <-finished:
		<-engineDone
		engineJoined = true
		if err != nil {
			t.Fatalf("Cylo runner Engine task chain failed (%T)", err)
		}
	case <-ctx.Done():
		t.Fatal("Cylo runner Engine task chain did not finish before its acceptance deadline")
	}
	entry, ok, err := journal.Entry(ctx, claim.RunID)
	if err != nil || !ok || entry.Phase != "terminated" || entry.Session == "" || entry.Container != containerID {
		t.Fatalf("runner journal did not persist exact AO/container termination: found=%t phase=%q session_bound=%t container_match=%t error_type=%T", ok, entry.Phase, entry.Session != "", entry.Container == containerID, err)
	}
	if err := verifyWorkspaceArtifact(containerWorkspace); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("/etc/CYLO_RUNTIME_OUTSIDE_PROBE.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside-workspace probe unexpectedly exists on host (stat error type=%T)", err)
	}
	var observationBody []byte
	if err := store.DB().QueryRowContext(ctx, `SELECT body FROM runner_observations WHERE run_id=? AND kind='runtime_terminated' ORDER BY occurred_at DESC LIMIT 1`, claim.RunID).Scan(&observationBody); err != nil {
		t.Fatalf("read persisted termination observation failed (%T)", err)
	}
	var observation contracts.RunnerObservation
	if err := json.Unmarshal(observationBody, &observation); err != nil {
		t.Fatalf("decode persisted termination observation failed (%T)", err)
	}
	if proof := observation.TerminationProof; proof == nil || proof.Source != "ao_chat_stop" || proof.Method != "native_process_exit_waited" || proof.SessionID != entry.Session || !proof.CleanupComplete || proof.VerifiedAt.IsZero() {
		t.Fatalf("persisted AO native-process proof is incomplete: present=%t source_match=%t method_match=%t session_match=%t cleanup_complete=%t verified_at_present=%t", proof != nil, proof != nil && proof.Source == "ao_chat_stop", proof != nil && proof.Method == "native_process_exit_waited", proof != nil && proof.SessionID == entry.Session, proof != nil && proof.CleanupComplete, proof != nil && !proof.VerifiedAt.IsZero())
	}
	var exactContainerProof bool
	for _, effect := range observation.ExternalEffects {
		if effect.Kind == "ao_container_pid_namespace" && effect.Status == "terminated" && effect.Reference == "docker:"+containerID {
			exactContainerProof = true
		}
	}
	if !exactContainerProof {
		t.Fatalf("persisted run observation lacks exact container/PID-namespace proof for %s", containerID)
	}
	var taskStatus string
	if err := store.DB().QueryRowContext(ctx, `SELECT status FROM tasks WHERE id=?`, taskReceipt.ResourceID).Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "active" {
		t.Fatalf("task status=%q after worker result; expected user review state active", taskStatus)
	}
	stopVerified = true
}

func liveToken(t *testing.T) string {
	t.Helper()
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(value)
}

func liveControlRequest(t *testing.T, client *http.Client, baseURL, method, endpoint, token, key string, payload any) ([]byte, int, error) {
	t.Helper()
	var body []byte
	if payload != nil {
		body, _ = json.Marshal(payload)
	}
	request, err := http.NewRequest(method, baseURL+endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	var result bytes.Buffer
	_, _ = result.ReadFrom(response.Body)
	return result.Bytes(), response.StatusCode, nil
}

func liveCommand(t *testing.T, client *http.Client, baseURL, token, key, kind string, payload any) (contracts.CommandReceipt, int, string) {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	body, status, err := liveControlRequest(t, client, baseURL, http.MethodPost, "/v1/commands", token, key, contracts.CommandRequest{Kind: kind, Payload: encoded})
	if err != nil {
		t.Fatal(err)
	}
	var receipt contracts.CommandReceipt
	if status >= 200 && status < 300 {
		if err := json.Unmarshal(body, &receipt); err != nil {
			t.Fatalf("decode %s command receipt failed (%T)", kind, err)
		}
	}
	return receipt, status, string(body)
}

func createLiveEvidenceDir(root, prefix string) (string, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(root, prefix+"-")
	if err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func validLiveJournalIdentity(entry runner.JournalEntry, claim contracts.JobClaim) bool {
	return entry.Claim.RunID == claim.RunID &&
		entry.Claim.TaskID == claim.TaskID &&
		entry.Claim.AttemptID == claim.AttemptID &&
		entry.Claim.Fence == claim.Fence &&
		entry.Claim.ProfileID == claim.ProfileID &&
		entry.Claim.Harness == claim.Harness &&
		entry.Claim.AOProjectID == claim.AOProjectID &&
		entry.Claim.RuntimeRouteID == claim.RuntimeRouteID &&
		entry.Claim.ModelRouteID == claim.ModelRouteID &&
		entry.Claim.IsolationPolicy == claim.IsolationPolicy &&
		entry.Spec.ClientRequestID == claim.RunID &&
		entry.Spec.ProjectID == claim.AOProjectID &&
		entry.Spec.Harness == "codex" &&
		entry.Spec.Model == "gpt-6-luna" &&
		entry.Spec.ApprovalMode == "auto" &&
		entry.Spec.DisplayName != "" &&
		strings.Contains(entry.Spec.Prompt, "CYLO_RUNTIME_TOOL_PROBE.txt")
}

func reconcileUnknownJournalCreate(ctx context.Context, client liveWorkerCreator, entry runner.JournalEntry, claim contracts.JobClaim, attempts *int) (ao.Session, error) {
	if !validLiveJournalIdentity(entry, claim) || entry.Phase != "effect_unknown" || entry.Session != "" {
		return ao.Session{}, errors.New("unknown AO create journal identity is not safe to replay")
	}
	session, err := reconcileLiveWorker(ctx, client, entry.Spec, attempts)
	if err != nil {
		return ao.Session{}, err
	}
	if session.ID == "" || session.ProjectID != entry.Spec.ProjectID {
		return ao.Session{}, errors.New("AO replay returned a mismatched project or session binding")
	}
	return session, nil
}

func startLiveEngine(ctx context.Context, engine *runner.Engine, claim contracts.JobClaim) (chan error, chan struct{}) {
	finished := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		finished <- engine.Process(ctx, claim)
	}()
	return finished, done
}

func TestValidLiveJournalIdentityRejectsChangedRunFenceAndRequest(t *testing.T) {
	claim := contracts.JobClaim{
		RunID: "run-1", TaskID: "task-1", AttemptID: "attempt-1", Fence: 7,
		ProfileID: "profile-1", AOProjectID: "project-1", RuntimeRouteID: "codex",
		ModelRouteID: "gpt-6-luna", IsolationPolicy: runner.CodexWorkspaceWritePolicy,
	}
	entry := runner.JournalEntry{
		Claim: claim,
		Spec: ao.WorkerSpec{
			ClientRequestID: claim.RunID, ProjectID: claim.AOProjectID, Harness: "codex",
			Model: "gpt-6-luna", ApprovalMode: "auto", DisplayName: "worker",
			Prompt: "Create CYLO_RUNTIME_TOOL_PROBE.txt",
		},
		Phase: "effect_unknown",
	}
	if !validLiveJournalIdentity(entry, claim) {
		t.Fatal("valid original journal identity was rejected")
	}
	wrongFence := entry
	wrongFence.Claim.Fence++
	wrongSession := entry
	wrongSession.Spec.ClientRequestID = "different-request"
	wrongProject := entry
	wrongProject.Spec.ProjectID = "other-project"
	wrongHarness := entry
	wrongHarness.Spec.Harness = "opencode"
	wrongModel := entry
	wrongModel.Spec.Model = "other-model"
	for name, candidate := range map[string]runner.JournalEntry{
		"fence": wrongFence, "request": wrongSession, "project": wrongProject,
		"harness": wrongHarness, "model": wrongModel,
	} {
		if validLiveJournalIdentity(candidate, claim) {
			t.Errorf("changed %s identity was accepted for reconciliation", name)
		}
	}
}

func TestReconcileUnknownJournalCreateRejectsChangedIdentityWithoutDispatch(t *testing.T) {
	claim := contracts.JobClaim{
		RunID: "run-1", TaskID: "task-1", AttemptID: "attempt-1", Fence: 7,
		ProfileID: "profile-1", AOProjectID: "project-1", RuntimeRouteID: "codex",
		ModelRouteID: "gpt-6-luna", IsolationPolicy: runner.CodexWorkspaceWritePolicy,
	}
	entry := runner.JournalEntry{
		Claim: claim,
		Spec: ao.WorkerSpec{
			ClientRequestID: claim.RunID, ProjectID: claim.AOProjectID, Harness: "codex",
			Model: "gpt-6-luna", ApprovalMode: "auto", DisplayName: "worker",
			Prompt: "Create CYLO_RUNTIME_TOOL_PROBE.txt",
		},
		Phase: "effect_unknown",
	}
	creator := &fakeLiveWorkerCreator{}
	for name, mutate := range map[string]func(*runner.JournalEntry){
		"fence":         func(e *runner.JournalEntry) { e.Claim.Fence++ },
		"request":       func(e *runner.JournalEntry) { e.Spec.ClientRequestID = "other-run" },
		"project":       func(e *runner.JournalEntry) { e.Spec.ProjectID = "other-project" },
		"harness":       func(e *runner.JournalEntry) { e.Spec.Harness = "opencode" },
		"model":         func(e *runner.JournalEntry) { e.Spec.Model = "other-model" },
		"bound_session": func(e *runner.JournalEntry) { e.Session = "already-bound-session" },
	} {
		candidate := entry
		mutate(&candidate)
		attempts := 0
		if session, err := reconcileUnknownJournalCreate(context.Background(), creator, candidate, claim, &attempts); err == nil || session.ID != "" || attempts != 0 {
			t.Errorf("changed %s identity triggered AO replay: session_bound=%t attempts=%d", name, session.ID != "", attempts)
		}
	}
	if len(creator.seen) != 0 {
		t.Fatalf("rejected journal identities still dispatched AO create %d times", len(creator.seen))
	}
}
