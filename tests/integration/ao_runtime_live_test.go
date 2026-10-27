package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Zoe/internal/agents/ao"
	"github.com/ronigooja/Zoe/internal/runner"
)

// TestAOLiveSupervisedCodexRuntime is an opt-in acceptance probe for a local AO
// daemon configured with the selected private provider reference. It exercises
// a real Responses request, one workspace write, the read-only container
// boundary, and the exact container/PID-namespace stop proof. It is skipped by
// ordinary test runs unless all ZOE_LIVE_AO_* values are supplied.
func TestAOLiveSupervisedCodexRuntime(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv("ZOE_LIVE_AO_URL"))
	projectID := strings.TrimSpace(os.Getenv("ZOE_LIVE_AO_PROJECT_ID"))
	workspaceRoot := strings.TrimSpace(os.Getenv("ZOE_LIVE_AO_WORKSPACE_ROOT"))
	aoDataDir := strings.TrimSpace(os.Getenv("ZOE_LIVE_AO_DATA_DIR"))
	evidenceRoot := strings.TrimSpace(os.Getenv("ZOE_LIVE_AO_EVIDENCE_ROOT"))
	if baseURL == "" || projectID == "" || workspaceRoot == "" || aoDataDir == "" || evidenceRoot == "" {
		t.Skip("set ZOE_LIVE_AO_URL, ZOE_LIVE_AO_PROJECT_ID, ZOE_LIVE_AO_WORKSPACE_ROOT, ZOE_LIVE_AO_DATA_DIR, and ZOE_LIVE_AO_EVIDENCE_ROOT for local AO acceptance")
	}
	client, err := ao.New(ao.Config{BaseURL: baseURL, Token: "isolated-local-probe"})
	if err != nil {
		t.Fatalf("configure local AO client failed (%T)", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	requestID := fmt.Sprintf("zoe-live-%d", time.Now().UTC().UnixNano())
	spec := ao.WorkerSpec{
		ClientRequestID: requestID,
		ProjectID:       projectID,
		Harness:         "codex",
		Model:           "gpt-6-luna",
		ApprovalMode:    "auto",
		DisplayName:     "Zoe supervised runtime acceptance",
		Prompt:          "Use your shell tool to create exactly one file named ZOE_RUNTIME_TOOL_PROBE.txt in the current workspace, containing exactly `ZOE_AO_TOOL_OK` followed by a newline. Read it back. Then make one attempt to create `/etc/ZOE_RUNTIME_OUTSIDE_PROBE.txt`; the isolated container filesystem should deny that write. Do not retry through another path. Reply exactly `workspace_write=ok outside_write=denied` if both outcomes match; otherwise report which failed.",
	}

	docker := runner.ExecDockerCommand{Binary: "docker"}
	supervisor := runner.DockerAOContainerSupervisor{Docker: docker}
	sessionID := ""
	containerID := ""
	containerWorkspace := ""
	stopVerified := false
	createAttempts := 0
	createOutcomeUnknown := false
	defer func() {
		if stopVerified {
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cleanupCancel()
		if sessionID == "" && createOutcomeUnknown {
			if session, reconcileErr := reconcileLiveWorker(cleanupCtx, client, spec, &createAttempts); reconcileErr == nil {
				sessionID = session.ID
			}
		}
		if sessionID == "" {
			if err := writeUnknownLiveCreateEvidence(evidenceRoot, "direct-runtime", requestID, projectID, createAttempts); err != nil {
				t.Errorf("persist unresolved AO create evidence failed (%T)", err)
			}
			return
		}
		if containerID == "" {
			if foundID, found, findErr := supervisor.Find(cleanupCtx, "codex", sessionID); findErr != nil {
				t.Errorf("inspect probe container during cleanup failed (%T)", findErr)
			} else if found {
				containerID = foundID
			}
		}
		if stopErr := stopExactLiveWorker(cleanupCtx, client, sessionID); stopErr != nil {
			t.Errorf("cleanup AO worker failed (%T)", stopErr)
		}
		if containerID != "" {
			proof, cleanupErr := supervisor.StopAndVerify(cleanupCtx, "codex", sessionID, containerID)
			if cleanupErr != nil || !proof.CleanupComplete || proof.EffectRef != "docker:"+containerID {
				t.Errorf("exact probe container cleanup could not be verified (error=%T)", cleanupErr)
			}
		}
	}()
	session, err := reconcileLiveWorker(ctx, client, spec, &createAttempts)
	if err != nil {
		if errors.Is(err, ao.ErrOutcomeUnknown) {
			createOutcomeUnknown = true
			t.Fatalf("AO worker create remains unknown after bounded replay with the original request identity (attempts=%d)", createAttempts)
		}
		t.Fatalf("AO rejected live worker creation (%T)", err)
	}
	sessionID = session.ID

	var final *ao.Conversation
	for ctx.Err() == nil {
		if containerID == "" {
			id, found, findErr := supervisor.Find(ctx, "codex", sessionID)
			if findErr != nil {
				t.Fatalf("inspect supervised AO container failed (%T)", findErr)
			}
			if found {
				containerID = id
				containerWorkspace = assertLiveContainerBoundary(t, ctx, docker, id, aoDataDir, workspaceRoot, sessionID)
			}
		}
		conversation, conversationErr := client.GetConversation(ctx, sessionID)
		if conversationErr == nil {
			for _, turn := range conversation.Turns {
				if turn.State == "completed" || turn.State == "failed" || turn.State == "interrupted" || turn.State == "cancelled" {
					copy := conversation
					final = &copy
					goto finished
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
finished:
	if final == nil {
		t.Fatalf("AO worker did not finish before deadline")
	}
	var completedTurn string
	for _, turn := range final.Turns {
		if turn.State == "completed" {
			completedTurn = turn.ID
			break
		}
	}
	if completedTurn == "" {
		states := make([]string, 0, len(final.Turns))
		for _, turn := range final.Turns {
			states = append(states, turn.State)
		}
		containerState, stateErr := docker.Run(ctx, "inspect", "--format", "{{.State.Status}}|{{.State.ExitCode}}", containerID)
		t.Fatalf("AO worker turn did not complete; states=%v container_state=%q inspect_err=%T", states, strings.TrimSpace(string(containerState)), stateErr)
	}
	var output string
	for _, message := range final.Messages {
		if message.Role == "assistant" && message.TurnID == completedTurn && !message.Streaming {
			output = strings.TrimSpace(message.Text)
		}
	}
	if !strings.Contains(output, "workspace_write=ok outside_write=denied") {
		t.Fatalf("AO completed without confirming the requested tool outcomes (assistant output length=%d)", len(output))
	}
	if containerID == "" {
		t.Fatalf("completed AO worker never exposed a supervised container identity")
	}
	if err := verifyWorkspaceArtifact(containerWorkspace); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("/etc/ZOE_RUNTIME_OUTSIDE_PROBE.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside-workspace probe unexpectedly exists on host (stat error type=%T)", err)
	}

	stopped, kill, err := client.StopWorker(ctx, sessionID)
	if err != nil {
		t.Fatalf("AO stop failed (%T)", err)
	}
	if stopped.Status != "terminated" || kill.CleanupPending || !validLiveStopProof(kill.TerminationProof, sessionID) {
		t.Fatalf("AO stop proof incomplete: status=%q cleanup_pending=%v proof_present=%v", stopped.Status, kill.CleanupPending, kill.TerminationProof != nil)
	}
	proof, err := supervisor.StopAndVerify(ctx, "codex", sessionID, containerID)
	if err != nil || !proof.CleanupComplete || proof.EffectRef != "docker:"+containerID {
		t.Fatalf("exact container/PID-namespace stop proof failed: cleanup_complete=%v effect_ref_matches=%v error=%T", proof.CleanupComplete, proof.EffectRef == "docker:"+containerID, err)
	}
	stopVerified = true
}

func assertLiveContainerBoundary(t *testing.T, ctx context.Context, docker runner.DockerCommand, id, aoDataDir, workspaceRoot, sessionID string) string {
	t.Helper()
	resolvedDataDir, err := filepath.EvalSymlinks(aoDataDir)
	if err != nil {
		t.Fatalf("resolve private AO data directory failed (%T)", err)
	}
	managedRoot, err := filepath.EvalSymlinks(filepath.Join(resolvedDataDir, "worktrees"))
	if err != nil {
		t.Fatalf("resolve actual AO managed worktree root failed (%T)", err)
	}
	resolvedWorkspaceRoot, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil || filepath.Clean(resolvedWorkspaceRoot) != filepath.Clean(managedRoot) {
		t.Fatalf("configured workspace root does not equal AO_DATA_DIR/worktrees (error=%T)", err)
	}
	workspaceRoot = managedRoot
	body, err := docker.Run(ctx, "inspect", "--format", "{{json .}}", id)
	if err != nil {
		t.Fatalf("inspect live AO container failed (%T)", err)
	}
	var inspection struct {
		ID     string `json:"Id"`
		Config struct {
			Env    []string          `json:"Env"`
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
		HostConfig struct {
			NetworkMode    string   `json:"NetworkMode"`
			ReadonlyRootfs bool     `json:"ReadonlyRootfs"`
			CapDrop        []string `json:"CapDrop"`
			SecurityOpt    []string `json:"SecurityOpt"`
			PidsLimit      int64    `json:"PidsLimit"`
		} `json:"HostConfig"`
		Mounts []struct {
			Type        string `json:"Type"`
			Source      string `json:"Source"`
			Destination string `json:"Destination"`
			RW          bool   `json:"RW"`
		} `json:"Mounts"`
	}
	if err := json.Unmarshal(body, &inspection); err != nil {
		t.Fatalf("decode live AO container inspect failed (%T)", err)
	}
	if inspection.ID != id || inspection.Config.Labels["io.zoe.managed"] != "true" || inspection.Config.Labels["io.zoe.runtime"] != "ao-codex-chat" || inspection.Config.Labels["io.zoe.ao.session"] != sessionID || inspection.HostConfig.NetworkMode != "none" || !inspection.HostConfig.ReadonlyRootfs || inspection.HostConfig.PidsLimit != 256 {
		t.Fatalf("AO container isolation settings are incomplete")
	}
	if !containsLiveString(inspection.HostConfig.CapDrop, "ALL") || !containsLiveString(inspection.HostConfig.SecurityOpt, "no-new-privileges") {
		t.Fatalf("AO container lacks dropped capabilities or no-new-privileges")
	}
	for _, value := range inspection.Config.Env {
		name, _, _ := strings.Cut(value, "=")
		if strings.HasPrefix(name, "ZOE_CODEX_") || name == "OPENAI_API_KEY" {
			t.Fatalf("provider credential or host route reached container environment as %q", name)
		}
	}
	if len(inspection.Mounts) != 3 {
		t.Fatalf("AO container has %d mounts; expected scoped workspace, private CODEX_HOME, and provider socket", len(inspection.Mounts))
	}
	workspaceMounted := false
	workspacePath := ""
	for _, mount := range inspection.Mounts {
		if mount.Type != "bind" || !mount.RW && mount.Destination == workspaceRoot {
			t.Fatalf("AO container mount has unexpected type or workspace mode")
		}
		if mount.RW && filepath.Clean(mount.Source) == filepath.Clean(mount.Destination) {
			resolvedSource, sourceErr := filepath.EvalSymlinks(mount.Source)
			rel, relErr := filepath.Rel(filepath.Clean(workspaceRoot), filepath.Clean(mount.Source))
			resolvedRel, resolvedRelErr := filepath.Rel(filepath.Clean(workspaceRoot), filepath.Clean(resolvedSource))
			if sourceErr == nil && relErr == nil && resolvedRelErr == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && resolvedRel != "." && resolvedRel != ".." && !strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)) {
				workspaceMounted = true
				workspacePath = filepath.Clean(mount.Source)
			}
		}
		if strings.Contains(mount.Source, "/docker.sock") {
			t.Fatalf("AO container has a Docker socket mount")
		}
	}
	if !workspaceMounted {
		t.Fatalf("AO project workspace is not the explicitly writable container mount")
	}
	return workspacePath
}

func verifyWorkspaceArtifact(workspaceRoot string) error {
	path := filepath.Join(workspaceRoot, "ZOE_RUNTIME_TOOL_PROBE.txt")
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("AO workspace artifact is missing at the registered project path (error_type=%T)", err)
	}
	if string(body) != "ZOE_AO_TOOL_OK\n" {
		return fmt.Errorf("AO workspace artifact content mismatch")
	}
	return nil
}

func containsLiveString(values []string, value string) bool {
	for _, candidate := range values {
		if strings.Contains(candidate, value) {
			return true
		}
	}
	return false
}

func validLiveStopProof(proof *ao.TerminationProof, sessionID string) bool {
	return proof != nil && proof.Source == "ao_chat_stop" && proof.Method == "native_process_exit_waited" && proof.SessionID == sessionID && proof.CleanupComplete && !proof.VerifiedAt.IsZero()
}

const maxLiveCreateReplays = 3

type liveWorkerCreator interface {
	CreateWorker(context.Context, ao.WorkerSpec) (ao.Session, error)
}

type liveWorkerStopper interface {
	StopWorker(context.Context, string) (ao.Session, ao.KillResponse, error)
}

func stopExactLiveWorker(ctx context.Context, client liveWorkerStopper, sessionID string) error {
	if sessionID == "" {
		return errors.New("exact AO session identity is required for cleanup")
	}
	stopped, kill, err := client.StopWorker(ctx, sessionID)
	if err != nil {
		return err
	}
	if stopped.ID != sessionID || stopped.Status != "terminated" || kill.SessionID != sessionID || kill.CleanupPending || !validLiveStopProof(kill.TerminationProof, sessionID) {
		return errors.New("AO session cleanup proof does not match the exact session identity")
	}
	return nil
}

func reconcileLiveWorker(ctx context.Context, client liveWorkerCreator, spec ao.WorkerSpec, attempts *int) (ao.Session, error) {
	var lastErr error
	for *attempts < maxLiveCreateReplays {
		*attempts++
		session, err := client.CreateWorker(ctx, spec)
		if err == nil {
			if session.ID == "" || session.ProjectID != spec.ProjectID {
				return ao.Session{}, errors.New("AO replay returned an unbound or mismatched session identity")
			}
			return session, nil
		}
		if !errors.Is(err, ao.ErrOutcomeUnknown) {
			return ao.Session{}, err
		}
		lastErr = err
		if *attempts < maxLiveCreateReplays {
			timer := time.NewTimer(250 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ao.Session{}, fmt.Errorf("%w: bounded reconciliation deadline", ao.ErrOutcomeUnknown)
			case <-timer.C:
			}
		}
	}
	if lastErr == nil {
		lastErr = ao.ErrOutcomeUnknown
	}
	return ao.Session{}, fmt.Errorf("%w: bounded reconciliation exhausted", ao.ErrOutcomeUnknown)
}

func writeUnknownLiveCreateEvidence(root, label, requestID, projectID string, attempts int) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return err
	}
	path := filepath.Join(root, "unknown-"+label+"-"+requestID+".json")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = fmt.Fprintf(file, "{\"status\":\"unknown\",\"client_request_id\":%q,\"project_id\":%q,\"replay_attempts\":%d}\n", requestID, projectID, attempts)
	return err
}

func writeExactLiveSessionEvidence(root, label, requestID, projectID, sessionID string) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return err
	}
	path := filepath.Join(root, "owned-session-"+label+"-"+requestID+".json")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = fmt.Fprintf(file, "{\"status\":\"session_bound_persist_failed\",\"client_request_id\":%q,\"project_id\":%q,\"session_id\":%q}\n", requestID, projectID, sessionID)
	return err
}

type fakeLiveWorkerCreator struct {
	responses []struct {
		session ao.Session
		err     error
	}
	seen       []ao.WorkerSpec
	stoppedIDs []string
}

func (f *fakeLiveWorkerCreator) CreateWorker(_ context.Context, spec ao.WorkerSpec) (ao.Session, error) {
	f.seen = append(f.seen, spec)
	if len(f.responses) == 0 {
		return ao.Session{}, ao.ErrOutcomeUnknown
	}
	response := f.responses[0]
	f.responses = f.responses[1:]
	return response.session, response.err
}

func (f *fakeLiveWorkerCreator) StopWorker(_ context.Context, id string) (ao.Session, ao.KillResponse, error) {
	f.stoppedIDs = append(f.stoppedIDs, id)
	proof := &ao.TerminationProof{Source: "ao_chat_stop", Method: "native_process_exit_waited", SessionID: id, VerifiedAt: time.Now().UTC(), CleanupComplete: true}
	return ao.Session{ID: id, Status: "terminated"}, ao.KillResponse{OK: true, SessionID: id, Freed: true, TerminationProof: proof}, nil
}

func TestReconcileLiveWorkerReplaysSameIdentityUntilLateSessionBinding(t *testing.T) {
	spec := ao.WorkerSpec{ClientRequestID: "run-original", ProjectID: "project-owned", Harness: "codex", DisplayName: "worker", Prompt: "fixture"}
	creator := &fakeLiveWorkerCreator{responses: []struct {
		session ao.Session
		err     error
	}{
		{err: ao.ErrOutcomeUnknown},
		{session: ao.Session{ID: "session-original", ProjectID: "project-owned"}},
	}}
	var attempts int
	session, err := reconcileLiveWorker(context.Background(), creator, spec, &attempts)
	if err != nil || session.ID != "session-original" || session.ProjectID != spec.ProjectID || attempts != 2 {
		t.Fatalf("late same-key reconciliation failed: session_bound=%t attempts=%d error_type=%T", session.ID != "", attempts, err)
	}
	if len(creator.seen) != 2 || creator.seen[0] != spec || creator.seen[1] != spec {
		t.Fatalf("reconciliation changed the original AO create spec: replay_count=%d", len(creator.seen))
	}
	if err := stopExactLiveWorker(context.Background(), creator, session.ID); err != nil || len(creator.stoppedIDs) != 1 || creator.stoppedIDs[0] != "session-original" {
		t.Fatalf("cleanup did not stop only the exact late-bound session: stop_count=%d error_type=%T", len(creator.stoppedIDs), err)
	}
}

func TestReconcileLiveWorkerLeavesRepeatedUnknownUnbound(t *testing.T) {
	spec := ao.WorkerSpec{ClientRequestID: "run-unknown", ProjectID: "project-owned", Harness: "codex", DisplayName: "worker", Prompt: "fixture"}
	creator := &fakeLiveWorkerCreator{}
	var attempts int
	session, err := reconcileLiveWorker(context.Background(), creator, spec, &attempts)
	if !errors.Is(err, ao.ErrOutcomeUnknown) || session.ID != "" || attempts != maxLiveCreateReplays || len(creator.seen) != maxLiveCreateReplays {
		t.Fatalf("unresolved outcome was not left fenced: session_bound=%t attempts=%d error_type=%T", session.ID != "", attempts, err)
	}
	for _, seen := range creator.seen {
		if seen != spec {
			t.Fatal("unknown outcome replay changed the original AO create spec")
		}
	}
}

func TestReconcileLiveWorkerDoesNotReplayDefiniteRejection(t *testing.T) {
	spec := ao.WorkerSpec{ClientRequestID: "run-rejected", ProjectID: "project-owned", Harness: "codex", DisplayName: "worker", Prompt: "fixture"}
	rejection := errors.New("AO rejected request")
	creator := &fakeLiveWorkerCreator{responses: []struct {
		session ao.Session
		err     error
	}{{err: rejection}}}
	var attempts int
	session, err := reconcileLiveWorker(context.Background(), creator, spec, &attempts)
	if !errors.Is(err, rejection) || session.ID != "" || attempts != 1 || len(creator.seen) != 1 {
		t.Fatalf("definite rejection triggered an extra create replay: calls=%d attempts=%d error_type=%T", len(creator.seen), attempts, err)
	}
}

func TestReconcileLiveWorkerRejectsMismatchedProjectBinding(t *testing.T) {
	spec := ao.WorkerSpec{ClientRequestID: "run-wrong-project", ProjectID: "project-owned", Harness: "codex", DisplayName: "worker", Prompt: "fixture"}
	creator := &fakeLiveWorkerCreator{responses: []struct {
		session ao.Session
		err     error
	}{{session: ao.Session{ID: "session-wrong-project", ProjectID: "other-project"}}}}
	var attempts int
	session, err := reconcileLiveWorker(context.Background(), creator, spec, &attempts)
	if err == nil || session.ID != "" || attempts != 1 {
		t.Fatalf("mismatched project binding was accepted: session_bound=%t attempts=%d", session.ID != "", attempts)
	}
}
