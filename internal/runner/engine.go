package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/agents/ao"
)

var ErrNoClaim = errors.New("no runner claim available")
var ErrOutcomePending = errors.New("external outcome remains unresolved and fenced")

type AO interface {
	CreateWorker(context.Context, ao.WorkerSpec) (ao.Session, error)
	GetSession(context.Context, string) (ao.Session, error)
	GetConversation(context.Context, string) (ao.Conversation, error)
	StopWorker(context.Context, string) (ao.Session, ao.KillResponse, error)
}

type WorkIsolation interface {
	Check(context.Context, contracts.JobClaim) error
}

type RunnerAPI interface {
	Claim(context.Context) (*contracts.JobClaim, error)
	Current(context.Context) ([]contracts.JobClaim, error)
	Control(context.Context, string) (contracts.RunControl, error)
	Observe(context.Context, string, contracts.RunnerObservation) error
}

type Route struct {
	ProfileID       string `json:"profile_id"`
	RuntimeRouteID  string `json:"runtime_route_id"`
	ModelRouteID    string `json:"model_route_id"`
	IsolationPolicy string `json:"isolation_policy"`
	Harness         string `json:"harness"`
	Model           string `json:"model"`
	Effort          string `json:"effort,omitempty"`
	ApprovalMode    string `json:"approval_mode"`
}

type Engine struct {
	Control        RunnerAPI
	Instance       InstanceAuthority
	AO             AO
	Containers     AOContainerSupervisor
	Journal        *Journal
	Routes         map[string]Route
	Isolation      WorkIsolation
	PollInterval   time.Duration
	ObserveTimeout time.Duration
}

func (e *Engine) RunOnce(ctx context.Context) error {
	if e.Control == nil || e.Journal == nil {
		return errors.New("runner engine requires control client and journal")
	}
	claim, err := e.Control.Claim(ctx)
	if err != nil {
		return err
	}
	if claim == nil {
		return ErrNoClaim
	}
	return e.Process(ctx, *claim)
}

// Process first journals the server claim and exact AO request. If the runner
// restarts, it can safely replay creation under AO's stable clientRequestId or
// continue controlling the already-bound session.
func (e *Engine) Process(ctx context.Context, claim contracts.JobClaim) error {
	if err := e.validateClaim(claim); err != nil {
		return err
	}
	if e.AO == nil {
		return errors.New("AO adapter is required")
	}
	persisted, hasPersisted, err := e.Journal.Entry(ctx, claim.RunID)
	if err != nil {
		return fmt.Errorf("read runner journal claim: %w", err)
	}
	var spec ao.WorkerSpec
	phase := "claimed"
	if hasPersisted {
		// Recovery must preserve the exact original AO request even if the
		// currently configured route has since changed or been removed.
		if !sameRecoveredClaim(persisted.Claim, claim, persisted.Session) || persisted.Spec.ClientRequestID != "" && persisted.Spec.Harness != effectiveHarness(claim.Harness) {
			return errors.New("runner journal claim and AO request harness do not match the server claim")
		}
		spec = persisted.Spec
		phase = ""
		if persisted.Phase == "claimed" && spec.ClientRequestID == "" && persisted.Session == "" && persisted.Container == "" {
			pending, pendingErr := e.Journal.PendingObservations(ctx, claim.RunID)
			if pendingErr != nil {
				return pendingErr
			}
			for _, queued := range pending {
				if queued.Body.Kind == "no_effect_terminal" {
					// Replay the exact queued receipt. A second terminal body would
					// conflict after the first one commits on the control plane.
					return e.flush(ctx, claim.RunID)
				}
			}
			// A prior preflight failure can persist its no-effect receipt before
			// reporting it. If the runner restarted between those writes, fail the
			// empty request closed instead of attempting AO creation.
			return e.reportNoEffectTerminal(ctx, claim, "failed", "claim_recovery_incomplete", "The runner recovered an incomplete claim before AO creation; no runtime was created.")
		}
	} else {
		route, ok := e.Routes[claim.ProfileID]
		if !ok || route.ProfileID != claim.ProfileID || route.RuntimeRouteID != claim.RuntimeRouteID || route.ModelRouteID != claim.ModelRouteID || !validAOApprovalMode(route.ApprovalMode) {
			return e.reportNoEffectTerminal(ctx, claim, "failed", "route_unavailable", "Server-selected runtime route is not available on this runner.")
		}
		if effectiveHarness(route.Harness) != effectiveHarness(claim.Harness) || route.IsolationPolicy != claim.IsolationPolicy {
			return e.reportNoEffectTerminal(ctx, claim, "failed", "route_unavailable", "Runner route harness and isolation do not match the server-selected run profile.")
		}
		if !supportedSupervisedRoute(route) {
			return e.reportNoEffectTerminal(ctx, claim, "failed", "runtime_unavailable", "This runner has no approved supervised runtime profile for the selected AO harness and isolation policy.")
		}
		if e.Isolation == nil {
			return e.reportNoEffectTerminal(ctx, claim, "failed", "isolation_unavailable", "Runtime dispatch is disabled because AO execution isolation has not been verified.")
		}
		var isolationErr error
		if routeIsolation, ok := e.Isolation.(interface {
			CheckRoute(context.Context, contracts.JobClaim, Route) error
		}); ok {
			isolationErr = routeIsolation.CheckRoute(ctx, claim, route)
		} else {
			isolationErr = e.Isolation.Check(ctx, claim)
		}
		if isolationErr != nil {
			return e.reportNoEffectTerminal(ctx, claim, "failed", "isolation_denied", "Runtime dispatch was denied by the configured isolation policy.")
		}
		if strings.TrimSpace(claim.Goal) == "" {
			return e.reportNoEffectTerminal(ctx, claim, "failed", "empty_goal", "The server supplied an empty task goal.")
		}
		spec = ao.WorkerSpec{
			ClientRequestID: claim.RunID,
			ProjectID:       claim.AOProjectID,
			Harness:         route.Harness,
			Model:           route.Model,
			Effort:          route.Effort,
			// The control plane grant plus the container boundary defines this
			// worker's authority. The selected harness auto-approves only inside
			// that configured scope.
			ApprovalMode: "auto",
			DisplayName:  workerName(claim),
			Prompt:       buildPrompt(claim),
		}
	}
	entry, err := e.Journal.SaveClaim(ctx, claim, spec, phase)
	if err != nil {
		return fmt.Errorf("journal runner claim: %w", err)
	}
	if err := e.flush(ctx, claim.RunID); err != nil {
		return err
	}
	if entry.Session == "" {
		control, err := e.Control.Control(ctx, claim.RunID)
		if err != nil {
			return err
		}
		if control.RunID != claim.RunID {
			return errors.New("runner control response run id mismatch")
		}
		if control.DesiredControl == "stop" {
			return e.reportStopWithoutSession(ctx, claim)
		}
		if control.DesiredControl != "run" {
			return fmt.Errorf("unsupported desired control %q", control.DesiredControl)
		}
		if entry.Phase == "claimed" || entry.Phase == "create_pending" || entry.Phase == "effect_unknown" {
			if err := e.checkInstance(ctx, claim, true); err != nil {
				return e.reportFailure(ctx, claim, "instance_fence_unavailable", "The active server instance changed or disabled dispatch; AO creation remains fenced.")
			}
			if err := e.Journal.SetPhase(ctx, claim.RunID, claim.AttemptID, claim.Fence, "create_pending"); err != nil {
				return err
			}
			if err := e.emit(ctx, claim, "dispatch_started", "ao_dispatch", "AO worker creation is beginning under the run idempotency key.", nil, nil, ""); err != nil {
				return err
			}
			if err := e.checkInstance(ctx, claim, true); err != nil {
				return e.reportFailure(ctx, claim, "instance_fence_changed", "The active server instance changed before AO creation; the run remains fenced.")
			}
			session, err := e.AO.CreateWorker(ctx, spec)
			if err != nil {
				if errors.Is(err, ao.ErrOutcomeUnknown) {
					_ = e.Journal.SetPhase(ctx, claim.RunID, claim.AttemptID, claim.Fence, "effect_unknown")
					return reportUnknown(e.emitWithEffects(ctx, claim, "effect_unknown", "ao_worker_create", "AO worker creation could not be reconciled after replaying the identical clientRequestId and request body.", nil, nil, "AO worker create remains unknown", []contracts.ExternalEffect{{ID: claim.RunID, Kind: "ao_worker_create", Status: "unknown"}}))
				}
				_ = e.Journal.SetPhase(ctx, claim.RunID, claim.AttemptID, claim.Fence, "failed")
				return e.reportFailure(ctx, claim, "ao_worker_create_failed", "AO rejected worker creation; no session was bound.")
			}
			if err := e.Journal.SetSession(ctx, claim.RunID, claim.AttemptID, claim.Fence, session.ID, "bound"); err != nil {
				return fmt.Errorf("persist AO session binding: %w", err)
			}
			entry.Session = session.ID
			entry.Phase = "bound"
			if err := e.emitWithEffects(ctx, claim, "dispatch_bound", "ao_session", "AO worker session id was returned and durably journaled.", nil, nil, "", []contracts.ExternalEffect{{ID: claim.RunID, Kind: "ao_worker_create", Status: "created", Reference: session.ID}}); err != nil {
				return err
			}
		}
	}
	return e.monitor(ctx, claim, entry.Session, effectiveHarness(spec.Harness))
}

func sameRecoveredClaim(persisted, current contracts.JobClaim, journalSession string) bool {
	if persisted.Harness == "" {
		// Older server claims did not serialize harness. The journaled AO request
		// separately binds the legacy claim to Codex, checked by the caller.
		if effectiveHarness(current.Harness) != "codex" {
			return false
		}
		persisted.Harness = "codex"
	}
	persisted.Harness = effectiveHarness(persisted.Harness)
	current.Harness = effectiveHarness(current.Harness)
	// A recovered current claim may include the AO session assigned after the
	// local claim was first journaled. Accept only the same session durably
	// recorded with this run; foreign or missing session bindings fail closed.
	if current.AOSessionID != journalSession || persisted.AOSessionID != "" && persisted.AOSessionID != current.AOSessionID {
		return false
	}
	persisted.AOSessionID = ""
	current.AOSessionID = ""
	return reflect.DeepEqual(persisted, current)
}

func supportedSupervisedRoute(route Route) bool {
	switch route.Harness {
	case "codex":
		return route.IsolationPolicy == CodexWorkspaceWritePolicy
	case "opencode":
		return route.IsolationPolicy == OpenCodeWorkspaceWritePolicy
	default:
		return false
	}
}

func (e *Engine) validateClaim(claim contracts.JobClaim) error {
	if claim.RunID == "" || claim.TaskID == "" || claim.AttemptID == "" || claim.WorkspaceID == "" || claim.Fence == 0 || claim.ProfileID == "" || claim.RuntimeRouteID == "" || claim.ModelRouteID == "" || claim.AOProjectID == "" || claim.IsolationPolicy == "" {
		return errors.New("runner claim is missing required server-selected identity or route fields")
	}
	if harness := effectiveHarness(claim.Harness); harness != "codex" && harness != "opencode" {
		return errors.New("runner claim selects an unsupported AO harness")
	}
	if claim.Role != "super" && claim.Role != "worker" && claim.Role != "curator" {
		return errors.New("runner claim has an unsupported role")
	}
	if len(claim.RunID) > 128 {
		return errors.New("run id is too long for AO clientRequestId")
	}
	return nil
}

func (e *Engine) monitor(ctx context.Context, claim contracts.JobClaim, sessionID, harness string) error {
	interval := e.PollInterval
	if interval <= 0 {
		interval = 3 * time.Second
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
		if err := e.flush(ctx, claim.RunID); err != nil {
			return err
		}
		if err := e.observeAOContainer(ctx, claim, harness, sessionID); err != nil {
			return reportUnknown(e.emitWithEffects(ctx, claim, "effect_unknown", "ao_container", "The supervised AO container identity or PID namespace could not be verified; the run remains fenced.", nil, nil, "AO container reconciliation is pending", []contracts.ExternalEffect{{ID: claim.RunID, Kind: "ao_container", Status: "unknown", Reference: sessionID}}))
		}
		entry, hasEntry, err := e.Journal.Entry(ctx, claim.RunID)
		if err != nil {
			return err
		}
		if hasEntry && entry.Phase == "terminated" {
			return nil
		}
		control, err := e.Control.Control(ctx, claim.RunID)
		if err != nil {
			continue
		}
		if control.RunID != claim.RunID {
			return errors.New("runner control response run id mismatch")
		}
		if control.DesiredControl == "stop" {
			return e.stopAndConfirm(ctx, claim, sessionID)
		}
		if control.DesiredControl != "run" {
			return fmt.Errorf("unsupported desired control %q", control.DesiredControl)
		}
		session, err := e.AO.GetSession(ctx, sessionID)
		if err != nil {
			continue
		}
		if hasEntry && (entry.Phase == "result_reported" || entry.Phase == "stop_pending" || entry.Phase == "stop_uncertain") {
			if entry.Phase == "result_reported" {
				return e.stopAndConfirm(ctx, claim, sessionID)
			}
			return reportUnknown(e.emitWithEffects(ctx, claim, "effect_unknown", "ao_worker_stop", "A previous AO stop request is unresolved; only read-only reconciliation is permitted.", nil, nil, "AO stop requires manual reconciliation", []contracts.ExternalEffect{{ID: claim.RunID, Kind: "ao_worker_stop", Status: "unknown", Reference: sessionID}}))
		}
		if session.Status == "terminated" {
			// AO status alone is not a process-exit proof. Ask the authorized kill
			// endpoint for its ownership-aware proof and stay fenced without it.
			return e.stopAndConfirm(ctx, claim, sessionID)
		}
		conversation, err := e.AO.GetConversation(ctx, sessionID)
		if err != nil {
			continue
		}
		if result, failure, ready := finalTurn(conversation); ready {
			if e.Containers != nil && entry.Container == "" {
				return reportUnknown(e.emitWithEffects(ctx, claim, "effect_unknown", "ao_container", "AO completed a worker turn without a journaled supervised container identity; the result remains fenced.", conversationEvidence(conversation, sessionID), nil, "AO container identity was not captured", []contracts.ExternalEffect{{ID: claim.RunID, Kind: "ao_container", Status: "unknown", Reference: sessionID}}))
			}
			if failure != "" {
				if err := e.emit(ctx, claim, "failure", "ao_conversation", "AO reported a failed worker turn.", conversationEvidence(conversation, sessionID), map[string]any{"turn_id": result.TurnID, "error": failure}, failure); err != nil {
					return err
				}
			} else {
				if err := e.emit(ctx, claim, "result", "ao_conversation", "AO reported a completed worker turn; the result remains unverified until deterministic or user review.", conversationEvidence(conversation, sessionID), map[string]any{"turn_id": result.TurnID, "assistant_output": result.Output}, ""); err != nil {
					return err
				}
				if err := e.Journal.SetPhase(ctx, claim.RunID, claim.AttemptID, claim.Fence, "result_reported"); err != nil {
					return err
				}
			}
			return e.stopAndConfirm(ctx, claim, sessionID)
		}
	}
}

func (e *Engine) stopAndConfirm(ctx context.Context, claim contracts.JobClaim, sessionID string) error {
	entry, ok, err := e.Journal.Entry(ctx, claim.RunID)
	if err != nil {
		return err
	}
	var session ao.Session
	var kill ao.KillResponse
	if ok && (entry.Phase == "stop_pending" || entry.Phase == "stop_uncertain") {
		// A previous process may have sent kill before it stopped. Never issue a
		// second non-idempotent kill. Re-read the session, but AO persists
		// "terminated" before workspace cleanup, so that status cannot resolve
		// whether cleanup finished after a lost kill reply.
		_, _ = e.AO.GetSession(ctx, sessionID)
		_ = e.Journal.SetPhase(ctx, claim.RunID, claim.AttemptID, claim.Fence, "stop_uncertain")
		return reportUnknown(e.emitWithEffects(ctx, claim, "effect_unknown", "ao_worker_stop", "A previous AO stop reply was lost; session status cannot verify whether AO workspace cleanup is complete, and the run remains fenced.", nil, nil, "AO stop requires manual reconciliation", []contracts.ExternalEffect{{ID: claim.RunID, Kind: "ao_worker_stop", Status: "unknown", Reference: sessionID}}))
	}
	if err := e.Journal.SetPhase(ctx, claim.RunID, claim.AttemptID, claim.Fence, "stop_pending"); err != nil {
		return err
	}
	if err := e.checkInstance(ctx, claim, false); err != nil {
		return reportUnknown(e.emitWithEffects(ctx, claim, "effect_unknown", "ao_worker_stop", "The active server instance changed before AO cleanup; stop remains fenced for the current owner.", nil, nil, "AO stop requires instance reconciliation", []contracts.ExternalEffect{{ID: claim.RunID, Kind: "ao_worker_stop", Status: "unknown", Reference: sessionID}}))
	}
	session, kill, err = e.AO.StopWorker(ctx, sessionID)
	var containerProof *contracts.CapabilityTerminationProof
	if e.Containers != nil {
		proof, proofErr := e.stopAndVerifyAOContainer(ctx, claim, entry.Spec.Harness, sessionID)
		if proofErr != nil {
			_ = e.Journal.SetPhase(ctx, claim.RunID, claim.AttemptID, claim.Fence, "stop_uncertain")
			return reportUnknown(e.emitWithEffects(ctx, claim, "effect_unknown", "ao_container_stop", "AO container PID namespace exit could not be confirmed; the run stays fenced.", nil, nil, "AO container termination is not confirmed", []contracts.ExternalEffect{{ID: claim.RunID, Kind: "ao_container", Status: "pending", Reference: sessionID}}))
		}
		containerProof = &proof
	}
	if err != nil {
		// Kill may have succeeded even if its response was lost. Reconcile only by
		// reading the same AO session; do not issue a second kill automatically.
		session, err = e.AO.GetSession(ctx, sessionID)
		if err != nil || session.Status != "terminated" {
			_ = e.Journal.SetPhase(ctx, claim.RunID, claim.AttemptID, claim.Fence, "stop_uncertain")
			return reportUnknown(e.emitWithEffects(ctx, claim, "effect_unknown", "ao_worker_stop", "AO stop outcome remains unknown; the run stays fenced.", nil, nil, "AO stop could not be verified", []contracts.ExternalEffect{{ID: claim.RunID, Kind: "ao_worker_stop", Status: "unknown", Reference: sessionID}}))
		}
		_ = e.Journal.SetPhase(ctx, claim.RunID, claim.AttemptID, claim.Fence, "stop_uncertain")
		return reportUnknown(e.emitWithEffects(ctx, claim, "effect_unknown", "ao_worker_stop", "AO reports terminal status after a lost kill reply, but workspace cleanup completion is unknown; the run stays fenced.", nil, nil, "AO cleanup could not be verified", []contracts.ExternalEffect{{ID: claim.RunID, Kind: "ao_worker_stop", Status: "unknown", Reference: sessionID}}))
	}
	if session.Status != "terminated" || kill.CleanupPending || !validTerminationProof(kill.TerminationProof, entry.Spec.Harness, sessionID) || (e.Containers != nil && (containerProof == nil || !containerProof.CleanupComplete)) {
		_ = e.Journal.SetPhase(ctx, claim.RunID, claim.AttemptID, claim.Fence, "stop_uncertain")
		return reportUnknown(e.emitWithEffects(ctx, claim, "effect_unknown", "ao_worker_stop", "AO has not provided trusted native process-exit and cleanup proof; the run stays fenced.", nil, nil, "AO termination is not confirmed", []contracts.ExternalEffect{{ID: claim.RunID, Kind: "ao_worker_stop", Status: "pending", Reference: sessionID}}))
	}
	entry, ok, err = e.Journal.Entry(ctx, claim.RunID)
	if err != nil {
		return err
	}
	kind := "runtime_terminated"
	if ok && entry.Phase != "result_reported" {
		control, controlErr := e.Control.Control(ctx, claim.RunID)
		if controlErr == nil && control.DesiredControl == "stop" {
			kind = "stop_confirmed"
		}
	}
	proof := toContractProof(kill.TerminationProof)
	effects := []contracts.ExternalEffect{{ID: claim.RunID, Kind: "ao_worker_runtime", Status: "terminated", Reference: sessionID}}
	if containerProof != nil {
		effects = append(effects, contracts.ExternalEffect{ID: claim.RunID, Kind: "ao_container_pid_namespace", Status: "terminated", Reference: containerProof.EffectRef})
	}
	if err := e.emitWithTerminationProof(ctx, claim, kind, "ao_session", "AO runtime host exit and its supervised container PID namespace were verified.", nil, nil, "", effects, proof); err != nil {
		return err
	}
	return e.Journal.SetPhase(ctx, claim.RunID, claim.AttemptID, claim.Fence, "terminated")
}

func validTerminationProof(proof *ao.TerminationProof, harness, sessionID string) bool {
	expectedMethod := ""
	switch effectiveHarness(harness) {
	case "codex":
		expectedMethod = "native_process_exit_waited"
	case "opencode":
		expectedMethod = "acp_process_exit_waited"
	}
	if expectedMethod == "" || proof == nil || proof.Source != "ao_chat_stop" || proof.Method != expectedMethod || proof.SessionID != sessionID || !proof.CleanupComplete || proof.VerifiedAt.IsZero() || proof.VerifiedAt.After(time.Now().UTC().Add(time.Minute)) {
		return false
	}
	_, offset := proof.VerifiedAt.Zone()
	return offset == 0
}

func effectiveHarness(harness string) string {
	if strings.TrimSpace(harness) == "" {
		return "codex"
	}
	return strings.TrimSpace(harness)
}

func toContractProof(proof *ao.TerminationProof) *contracts.TerminationProof {
	if proof == nil {
		return nil
	}
	return &contracts.TerminationProof{Source: proof.Source, Method: proof.Method, SessionID: proof.SessionID, VerifiedAt: proof.VerifiedAt, CleanupComplete: proof.CleanupComplete}
}

func (e *Engine) observeAOContainer(ctx context.Context, claim contracts.JobClaim, harness, sessionID string) error {
	if e.Containers == nil {
		return nil
	}
	containerID, found, err := e.Containers.Find(ctx, harness, sessionID)
	if err != nil || !found {
		return err
	}
	entry, ok, err := e.Journal.Entry(ctx, claim.RunID)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("AO container has no durable runner journal claim")
	}
	if entry.Container != "" && entry.Container != containerID {
		return errors.New("AO container identity changed after runner journal binding")
	}
	if entry.Container == "" {
		return e.Journal.SetContainer(ctx, claim.RunID, claim.AttemptID, claim.Fence, containerID)
	}
	return nil
}

func (e *Engine) stopAndVerifyAOContainer(ctx context.Context, claim contracts.JobClaim, harness, sessionID string) (contracts.CapabilityTerminationProof, error) {
	entry, ok, err := e.Journal.Entry(ctx, claim.RunID)
	if err != nil {
		return contracts.CapabilityTerminationProof{}, err
	}
	if !ok {
		return contracts.CapabilityTerminationProof{}, errors.New("AO container has no durable runner journal claim")
	}
	containerID := entry.Container
	if containerID == "" {
		foundID, found, findErr := e.Containers.Find(ctx, harness, sessionID)
		if findErr != nil {
			return contracts.CapabilityTerminationProof{}, findErr
		}
		if found {
			containerID = foundID
			if err := e.Journal.SetContainer(ctx, claim.RunID, claim.AttemptID, claim.Fence, containerID); err != nil {
				return contracts.CapabilityTerminationProof{}, err
			}
		}
	}
	if err := e.checkInstance(ctx, claim, false); err != nil {
		return contracts.CapabilityTerminationProof{}, err
	}
	return e.Containers.StopAndVerify(ctx, harness, sessionID, containerID)
}

func (e *Engine) reportStopWithoutSession(ctx context.Context, claim contracts.JobClaim) error {
	entry, ok, err := e.Journal.Entry(ctx, claim.RunID)
	if err != nil {
		return err
	}
	if ok && entry.Phase == "claimed" && entry.Session == "" && entry.Container == "" {
		return e.reportNoEffectTerminal(ctx, claim, "stopped", "stop_before_dispatch", "Stop was confirmed before any AO session was created.")
	}
	return reportUnknown(e.emitWithEffects(ctx, claim, "effect_unknown", "ao_worker_create", "Stop arrived while AO creation may have had an unknown outcome; the run remains fenced for reconciliation.", nil, nil, "", []contracts.ExternalEffect{{ID: claim.RunID, Kind: "ao_worker_create", Status: "unknown"}}))
}

func (e *Engine) reportNoEffectTerminal(ctx context.Context, claim contracts.JobClaim, outcome, reason, message string) error {
	entry, ok, err := e.Journal.Entry(ctx, claim.RunID)
	if err != nil {
		return err
	}
	if !ok {
		entry, err = e.Journal.SaveClaim(ctx, claim, ao.WorkerSpec{}, "claimed")
		if err != nil {
			return err
		}
	}
	if entry.Claim.AttemptID != claim.AttemptID || entry.Claim.Fence != claim.Fence || entry.Phase != "claimed" || entry.Session != "" || entry.Container != "" {
		return reportUnknown(e.emitWithEffects(ctx, claim, "effect_unknown", "ao_worker_create", "The runner journal cannot prove that AO creation never began; the run remains fenced.", nil, nil, "", []contracts.ExternalEffect{{ID: claim.RunID, Kind: "ao_worker_create", Status: "unknown"}}))
	}
	proof := &contracts.NoEffectProof{JournalPhase: "claimed", AOCreateStarted: false, Outcome: outcome}
	return e.emitNoEffectTerminal(ctx, claim, reason, message, proof)
}

func (e *Engine) emitNoEffectTerminal(ctx context.Context, claim contracts.JobClaim, reason, message string, proof *contracts.NoEffectProof) error {
	if e.Journal == nil || e.Control == nil {
		return errors.New("runner engine is not initialized")
	}
	encoded, err := json.Marshal(struct {
		Reason string
		Proof  *contracts.NoEffectProof
		Error  string
	}{reason, proof, message})
	if err != nil {
		return err
	}
	digest := sha256.Sum256(encoded)
	obsID := observationID(claim.RunID, claim.AttemptID, claim.Fence, "no_effect_terminal", reason+hex.EncodeToString(digest[:]))
	if exists, acknowledged, err := e.Journal.ObservationExists(ctx, obsID); err != nil {
		return err
	} else if exists {
		if acknowledged {
			return nil
		}
		return e.flush(ctx, claim.RunID)
	}
	observation := contracts.RunnerObservation{
		ObservationID: obsID,
		RunID:         claim.RunID,
		AttemptID:     claim.AttemptID,
		Fence:         claim.Fence,
		Kind:          "no_effect_terminal",
		OccurredAt:    time.Now().UTC(),
		Error:         message,
		NoEffectProof: proof,
		Evidence: []contracts.Evidence{{
			ID:        observationID(claim.RunID, claim.AttemptID, claim.Fence, "no_effect_terminal", reason),
			Kind:      "no_effect_terminal",
			Summary:   message,
			Source:    "runner_journal",
			CreatedAt: time.Now().UTC(),
			Verified:  false,
		}},
		ExternalEffects: []contracts.ExternalEffect{},
	}
	if err := e.Journal.QueueObservation(ctx, claim.RunID, observation); err != nil {
		return err
	}
	return e.flush(ctx, claim.RunID)
}

func reportUnknown(err error) error {
	if err != nil {
		return err
	}
	return ErrOutcomePending
}

func (e *Engine) reportFailure(ctx context.Context, claim contracts.JobClaim, code, message string) error {
	return e.emit(ctx, claim, "failure", code, message, nil, map[string]string{"code": code}, message)
}

func (e *Engine) emit(ctx context.Context, claim contracts.JobClaim, kind, source, summary string, evidence []contracts.Evidence, result any, message string) error {
	return e.emitWithEffects(ctx, claim, kind, source, summary, evidence, result, message, nil)
}

func (e *Engine) emitWithEffects(ctx context.Context, claim contracts.JobClaim, kind, source, summary string, evidence []contracts.Evidence, result any, message string, effects []contracts.ExternalEffect) error {
	return e.emitWithTerminationProof(ctx, claim, kind, source, summary, evidence, result, message, effects, nil)
}

func (e *Engine) emitWithTerminationProof(ctx context.Context, claim contracts.JobClaim, kind, source, summary string, evidence []contracts.Evidence, result any, message string, effects []contracts.ExternalEffect, proof *contracts.TerminationProof) error {
	if e.Journal == nil || e.Control == nil {
		return errors.New("runner engine is not initialized")
	}
	var raw json.RawMessage
	if result != nil {
		encoded, err := json.Marshal(result)
		if err != nil {
			return err
		}
		raw = encoded
	}
	digestPayload, err := json.Marshal(struct {
		Evidence []contracts.Evidence
		Result   json.RawMessage
		Error    string
		Effects  []contracts.ExternalEffect
	}{evidence, raw, message, effects})
	if err != nil {
		return err
	}
	digest := sha256.Sum256(digestPayload)
	obsID := observationID(claim.RunID, claim.AttemptID, claim.Fence, kind, summary+hex.EncodeToString(digest[:]))
	if exists, acknowledged, err := e.Journal.ObservationExists(ctx, obsID); err != nil {
		return err
	} else if exists {
		if acknowledged {
			return nil
		}
		return e.flush(ctx, claim.RunID)
	}
	observation := contracts.RunnerObservation{
		ObservationID:    obsID,
		RunID:            claim.RunID,
		AttemptID:        claim.AttemptID,
		Fence:            claim.Fence,
		Kind:             kind,
		OccurredAt:       time.Now().UTC(),
		AOProjectID:      claim.AOProjectID,
		AOSessionID:      claim.AOSessionID,
		Evidence:         evidence,
		Result:           raw,
		Error:            message,
		ExternalEffects:  effects,
		TerminationProof: proof,
	}
	if entry, ok, err := e.Journal.Entry(ctx, claim.RunID); err == nil && ok && entry.Session != "" {
		observation.AOSessionID = entry.Session
	}
	if len(evidence) == 0 && summary != "" {
		observation.Evidence = []contracts.Evidence{{ID: observationID(claim.RunID, claim.AttemptID, claim.Fence, kind, source), Kind: kind, Summary: summary, Source: source, CreatedAt: observation.OccurredAt, Verified: false}}
	}
	if err := e.Journal.QueueObservation(ctx, claim.RunID, observation); err != nil {
		return err
	}
	return e.flush(ctx, claim.RunID)
}

func (e *Engine) flush(ctx context.Context, runID string) error {
	if e.ObserveTimeout <= 0 {
		e.ObserveTimeout = 15 * time.Second
	}
	pending, err := e.Journal.PendingObservations(ctx, runID)
	if err != nil {
		return err
	}
	for _, queued := range pending {
		sendCtx, cancel := context.WithTimeout(ctx, e.ObserveTimeout)
		err := e.Control.Observe(sendCtx, queued.RunID, queued.Body)
		cancel()
		if err != nil {
			return fmt.Errorf("submit durable runner observation: %w", err)
		}
		if err := e.Journal.AcknowledgeObservation(ctx, queued.Body.ObservationID); err != nil {
			return err
		}
	}
	return nil
}

type finalResult struct {
	TurnID string
	Output string
}

func finalTurn(conversation ao.Conversation) (finalResult, string, bool) {
	var latest *ao.Turn
	for i := range conversation.Turns {
		turn := &conversation.Turns[i]
		if latest == nil || turnRequestedAt(*turn).After(turnRequestedAt(*latest)) {
			latest = turn
		}
	}
	if latest == nil || conversation.Controller == "busy" || conversation.Controller == "connecting" || conversation.Controller == "recovering" {
		return finalResult{}, "", false
	}
	if latest.State == "failed" || latest.State == "interrupted" || latest.State == "cancelled" {
		return finalResult{TurnID: latest.ID}, latest.State, true
	}
	if latest.State != "completed" && latest.State != "recovered" {
		return finalResult{}, "", false
	}
	var answer ao.Message
	for _, message := range conversation.Messages {
		if message.Role == "assistant" && message.TurnID == latest.ID && !message.Streaming && message.Sequence > answer.Sequence {
			answer = message
		}
	}
	if answer.ID == "" || answer.Text == "" {
		return finalResult{}, "", false
	}
	return finalResult{TurnID: latest.ID, Output: answer.Text}, "", true
}

func conversationEvidence(conversation ao.Conversation, sessionID string) []contracts.Evidence {
	var evidence []contracts.Evidence
	for _, message := range conversation.Messages {
		if message.Role != "assistant" || message.Streaming {
			continue
		}
		source := fmt.Sprintf("ao://sessions/%s/conversation/messages/%s", sessionID, message.ID)
		evidence = append(evidence, contracts.Evidence{ID: message.ID, Kind: "assistant_message", Summary: fmt.Sprintf("AO assistant message observed at sequence %d; content is carried separately as an unverified result.", message.Sequence), Source: source, CreatedAt: parseAOTime(message.CreatedAt), Verified: false})
	}
	for _, activity := range conversation.Activities {
		if activity.ActivityKind != "command" && activity.ActivityKind != "file_change" && activity.ActivityKind != "mcp_tool" {
			continue
		}
		summary := activity.Summary
		if len(summary) > 1000 {
			summary = summary[:1000]
		}
		source := fmt.Sprintf("ao://sessions/%s/conversation/activities/%s", sessionID, activity.ID)
		evidence = append(evidence, contracts.Evidence{ID: activity.ID, Kind: activity.ActivityKind, Summary: summary, Source: source, CreatedAt: parseAOTime(activity.CreatedAt), Verified: false})
	}
	return evidence
}

func buildPrompt(claim contracts.JobClaim) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Cylo role: %s.\n", claim.Role)
	b.WriteString("Work only on the assigned goal in the AO project workspace. Do not create child runtimes or delegate through AO. Do not send messages, publish changes, deploy services, delete data, or make external irreversible changes. Report what you actually did and cite concrete workspace paths or tool results. Distinguish observed evidence from assumptions.\n\nGoal:\n")
	b.WriteString(claim.Goal)
	if len(claim.Authorization.Scopes) > 0 {
		b.WriteString("\n\nAuthorized capability scopes:\n")
		for _, scope := range claim.Authorization.Scopes {
			fmt.Fprintf(&b, "- %s\n", scope)
		}
	}
	return b.String()
}

func validAOApprovalMode(mode string) bool { return mode == "accept-edits" || mode == "auto" }

func workerName(claim contracts.JobClaim) string {
	return "Cylo " + claim.Role + " " + claim.RunID
}

func observationID(runID, attemptID string, fence uint64, kind, discriminator string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%s\x00%s", runID, attemptID, fence, kind, discriminator)))
	return hex.EncodeToString(sum[:16])
}

// turnRequestedAt parses an AO timestamp; malformed timestamps sort first.
func turnRequestedAt(t ao.Turn) time.Time {
	return parseAOTime(t.RequestedAt)
}

func parseAOTime(raw string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, raw)
	return parsed
}
