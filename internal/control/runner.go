package control

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ronigooja/Cylo/contracts"
)

func (s *Server) handleClaims(w http.ResponseWriter, r *http.Request) {
	p, ok := RunnerPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, apiError(401, "unauthenticated", "runner authentication required", nil))
		return
	}
	if !runnerHasAnyWorkspaceScope(p, "runner:claim") {
		writeError(w, apiError(403, "forbidden", "runner lacks claim scope", nil))
		return
	}
	tx, err := s.store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, err)
		return
	}
	defer tx.Rollback()
	// A lost claim response is recovered by returning the same dispatching assignment.
	var runID string
	err = tx.QueryRowContext(r.Context(), `SELECT id FROM runs WHERE runner_id=? AND status='dispatching' ORDER BY started_at LIMIT 1`, p.RunnerID).Scan(&runID)
	if err == nil {
		claim, e := loadClaim(r.Context(), tx, runID, s.cfg)
		if e != nil {
			writeError(w, e)
			return
		}
		if e = tx.Commit(); e != nil {
			writeError(w, e)
			return
		}
		writeJSON(w, 200, claim)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		writeError(w, err)
		return
	}
	var active int
	if err = tx.QueryRowContext(r.Context(), `SELECT count(*) FROM runs WHERE status IN ('dispatching','active','uncertain')`).Scan(&active); err != nil {
		writeError(w, err)
		return
	}
	if active >= 4 {
		_ = tx.Rollback()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	rows, err := tx.QueryContext(r.Context(), `SELECT id,workspace_id,title,goal,profile_id,version,created_by_device_id,command_id,coalesce(lineage_root_task_id,id),coalesce(lineage_parent_task_id,''),lineage_depth,child_policy_json,pregrant_capabilities_json FROM tasks WHERE status='queued' AND NOT EXISTS (SELECT 1 FROM task_dependencies d WHERE d.task_id=tasks.id AND d.status<>'satisfied') ORDER BY created_at,id`)
	if err != nil {
		writeError(w, err)
		return
	}
	type candidate struct {
		id, workspace, title, goal, profile, device, command string
		root, parent                                         string
		depth                                                int
		childPolicyJSON, pregrantJSON                        []byte
		version                                              int64
	}
	var chosen *candidate
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.id, &c.workspace, &c.title, &c.goal, &c.profile, &c.version, &c.device, &c.command, &c.root, &c.parent, &c.depth, &c.childPolicyJSON, &c.pregrantJSON); err != nil {
			rows.Close()
			writeError(w, err)
			return
		}
		if !HasRunnerScope(p, "runner:claim", c.workspace) {
			continue
		}
		profile, exists := s.cfg.Profiles[c.profile]
		if !exists {
			continue
		}
		if profile.AOAgentKind != "worker" || profile.IsolationPolicy == "" {
			continue
		}
		chosen = &c
		break
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		writeError(w, err)
		return
	}
	rows.Close()
	if chosen == nil {
		_ = tx.Rollback()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	profile := s.cfg.Profiles[chosen.profile]
	runID, err = randomID()
	if err != nil {
		writeError(w, err)
		return
	}
	attemptID, err := randomID()
	if err != nil {
		writeError(w, err)
		return
	}
	var fence uint64
	if err = tx.QueryRowContext(r.Context(), `SELECT coalesce(max(fence),0)+1 FROM runs`).Scan(&fence); err != nil {
		writeError(w, err)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	newVersion := chosen.version + 1
	policyBytes := chosen.childPolicyJSON
	if len(policyBytes) == 0 {
		policyBytes = []byte(`{}`)
	}
	pregrant := []string{}
	if err = json.Unmarshal(chosen.pregrantJSON, &pregrant); err != nil {
		writeError(w, apiError(500, "internal", "persisted child pregrant is invalid", nil))
		return
	}
	capabilityIDs := profile.CapabilityIDs
	if chosen.parent != "" {
		capabilityIDs = intersectStrings(capabilityIDs, pregrant)
	}
	authorization := contracts.JobAuthorization{CapabilityIDs: append([]string{}, capabilityIDs...), Scopes: append([]string{}, profile.Scopes...)}
	authorizationJSON, err := json.Marshal(authorization)
	if err != nil {
		writeError(w, err)
		return
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO runs(id,workspace_id,task_id,attempt_id,status,desired_control,control_version,fence,runner_id,authorized_device_id,command_id,goal,profile_id,runtime_route_id,model_route_id,role,isolation_policy,ao_project_id,started_at,task_version,authorization_json,child_policy_json,lineage_root_task_id,lineage_parent_task_id,lineage_depth) VALUES(?,?,?,?,'dispatching','run',1,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, runID, chosen.workspace, chosen.id, attemptID, fence, p.RunnerID, chosen.device, chosen.command, chosen.goal, profile.ID, profile.RuntimeRouteID, profile.ModelRouteID, profile.Role, profile.IsolationPolicy, profile.AOProjectID, now, newVersion, authorizationJSON, policyBytes, chosen.root, nullString(chosen.parent), chosen.depth)
	if err != nil {
		writeError(w, err)
		return
	}
	_, err = tx.ExecContext(r.Context(), `UPDATE tasks SET status='active',version=?,started_at=coalesce(started_at,?),updated_at=? WHERE id=? AND workspace_id=? AND status='queued' AND version=?`, newVersion, now, now, chosen.id, chosen.workspace, chosen.version)
	if err != nil {
		writeError(w, err)
		return
	}
	if err = appendEventTx(r.Context(), tx, contracts.Event{Type: "run.claimed", OccurredAt: time.Now().UTC(), WorkspaceID: chosen.workspace, SubjectID: "", AggregateType: "task", AggregateID: chosen.id, AggregateVersion: newVersion, Payload: raw(map[string]any{"task_id": chosen.id, "run_id": runID, "attempt_id": attemptID})}); err != nil {
		writeError(w, err)
		return
	}
	claim := contracts.JobClaim{RunID: runID, TaskID: chosen.id, AttemptID: attemptID, WorkspaceID: chosen.workspace, TaskVersion: newVersion, Goal: chosen.goal, ProfileID: profile.ID, RuntimeRouteID: profile.RuntimeRouteID, ModelRouteID: profile.ModelRouteID, IsolationPolicy: profile.IsolationPolicy, Role: profile.Role, AOProjectID: profile.AOProjectID, Authorization: authorization, Fence: fence, LineageRootTaskID: chosen.root, LineageParentTaskID: chosen.parent, LineageDepth: chosen.depth, PregrantCapabilityIDs: append([]string{}, pregrant...)}
	if err = tx.Commit(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, claim)
}

func runnerHasAnyWorkspaceScope(p contracts.RunnerPrincipal, scope string) bool {
	if len(p.WorkspaceIDs) == 0 {
		return false
	}
	for _, workspace := range p.WorkspaceIDs {
		if HasRunnerScope(p, scope, workspace) {
			return true
		}
	}
	return false
}

func intersectStrings(granted, requested []string) []string {
	out := make([]string, 0, len(requested))
	for _, value := range requested {
		if containsString(granted, value) && !containsString(out, value) {
			out = append(out, value)
		}
	}
	return out
}

func loadClaim(ctx context.Context, tx *sql.Tx, runID string, cfg Config) (contracts.JobClaim, error) {
	_ = cfg
	var c contracts.JobClaim
	var fence uint64
	var aoProject, aoSession string
	var authJSON []byte
	err := tx.QueryRowContext(ctx, `SELECT r.id,r.task_id,r.attempt_id,r.workspace_id,r.task_version,r.goal,r.profile_id,r.runtime_route_id,r.model_route_id,r.role,r.isolation_policy,r.ao_project_id,r.ao_session_id,r.fence,r.authorization_json,coalesce(r.lineage_root_task_id,''),coalesce(r.lineage_parent_task_id,''),r.lineage_depth FROM runs r WHERE r.id=?`, runID).Scan(&c.RunID, &c.TaskID, &c.AttemptID, &c.WorkspaceID, &c.TaskVersion, &c.Goal, &c.ProfileID, &c.RuntimeRouteID, &c.ModelRouteID, &c.Role, &c.IsolationPolicy, &aoProject, &aoSession, &fence, &authJSON, &c.LineageRootTaskID, &c.LineageParentTaskID, &c.LineageDepth)
	if err != nil {
		return c, err
	}
	c.AOProjectID = aoProject
	c.AOSessionID = aoSession
	c.Fence = fence
	if err := json.Unmarshal(authJSON, &c.Authorization); err != nil {
		return c, fmt.Errorf("decode persisted run authorization: %w", err)
	}
	if c.LineageParentTaskID != "" {
		c.PregrantCapabilityIDs = append([]string{}, c.Authorization.CapabilityIDs...)
	} else {
		c.PregrantCapabilityIDs = []string{}
	}
	return c, nil
}

func (s *Server) handleCurrentClaims(w http.ResponseWriter, r *http.Request) {
	p, ok := RunnerPrincipalFromContext(r.Context())
	if !ok || !runnerHasAnyWorkspaceScope(p, "runner:control:read") {
		writeError(w, apiError(403, "forbidden", "runner lacks current-claim read scope", nil))
		return
	}
	rows, err := s.store.DB().QueryContext(r.Context(), `SELECT id FROM runs WHERE runner_id=? AND status IN ('dispatching','active','uncertain') ORDER BY started_at`, p.RunnerID)
	if err != nil {
		writeError(w, err)
		return
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			writeError(w, err)
			return
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		writeError(w, err)
		return
	}
	if err = rows.Close(); err != nil {
		writeError(w, err)
		return
	}
	recovery := contracts.ClaimRecovery{Claims: []contracts.JobClaim{}}
	for _, id := range ids {
		var claim contracts.JobClaim
		var authJSON []byte
		err = s.store.DB().QueryRowContext(r.Context(), `SELECT r.id,r.task_id,r.attempt_id,r.workspace_id,r.task_version,r.goal,r.profile_id,r.runtime_route_id,r.model_route_id,r.role,r.isolation_policy,r.ao_project_id,r.ao_session_id,r.fence,r.authorization_json,coalesce(r.lineage_root_task_id,''),coalesce(r.lineage_parent_task_id,''),r.lineage_depth FROM runs r WHERE r.id=?`, id).Scan(&claim.RunID, &claim.TaskID, &claim.AttemptID, &claim.WorkspaceID, &claim.TaskVersion, &claim.Goal, &claim.ProfileID, &claim.RuntimeRouteID, &claim.ModelRouteID, &claim.Role, &claim.IsolationPolicy, &claim.AOProjectID, &claim.AOSessionID, &claim.Fence, &authJSON, &claim.LineageRootTaskID, &claim.LineageParentTaskID, &claim.LineageDepth)
		if err != nil {
			writeError(w, err)
			return
		}
		if err = json.Unmarshal(authJSON, &claim.Authorization); err != nil {
			writeError(w, apiError(500, "internal", "persisted run authorization is invalid", nil))
			return
		}
		if claim.LineageParentTaskID != "" {
			claim.PregrantCapabilityIDs = append([]string{}, claim.Authorization.CapabilityIDs...)
		} else {
			claim.PregrantCapabilityIDs = []string{}
		}
		recovery.Claims = append(recovery.Claims, claim)
	}
	writeJSON(w, 200, recovery)
}

func (s *Server) handleRunControl(w http.ResponseWriter, r *http.Request) {
	p, ok := RunnerPrincipalFromContext(r.Context())
	if !ok || !runnerHasAnyWorkspaceScope(p, "runner:control:read") {
		writeError(w, apiError(403, "forbidden", "runner lacks control read scope", nil))
		return
	}
	var control contracts.RunControl
	err := s.store.DB().QueryRowContext(r.Context(), `SELECT id,desired_control,control_version FROM runs WHERE id=? AND runner_id=? AND status IN ('dispatching','active','uncertain')`, r.PathValue("run_id"), p.RunnerID).Scan(&control.RunID, &control.DesiredControl, &control.ControlVersion)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, apiError(404, "not_found", "active run not found", nil))
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, control)
}

func (s *Server) handleObservation(w http.ResponseWriter, r *http.Request) {
	p, ok := RunnerPrincipalFromContext(r.Context())
	if !ok || !runnerHasAnyWorkspaceScope(p, "runner:observe") {
		writeError(w, apiError(403, "forbidden", "runner lacks observation scope", nil))
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" || len(key) > 200 {
		writeError(w, apiError(400, "invalid_request", "Idempotency-Key is required", nil))
		return
	}
	var observation contracts.RunnerObservation
	if err := decodeJSON(r, &observation); err != nil {
		writeError(w, err)
		return
	}
	runID := r.PathValue("run_id")
	if observation.RunID == "" || observation.RunID != runID || observation.ObservationID == "" || observation.AttemptID == "" || observation.Fence == 0 {
		writeError(w, apiError(400, "invalid_request", "run_id, observation_id, attempt_id, and fence are required", nil))
		return
	}
	if !validObservationKind(observation.Kind) {
		writeError(w, apiError(400, "invalid_request", "observation kind is not supported", nil))
		return
	}
	canonical, err := json.Marshal(observation)
	if err != nil {
		writeError(w, err)
		return
	}
	bodyHash := sha256.Sum256(canonical)
	tx, err := s.store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, err)
		return
	}
	defer tx.Rollback()
	var workspace, taskID, attempt, runnerID, status, desired, boundProject, boundSession string
	var fence uint64
	err = tx.QueryRowContext(r.Context(), `SELECT workspace_id,task_id,attempt_id,runner_id,status,desired_control,fence,ao_project_id,ao_session_id FROM runs WHERE id=?`, runID).Scan(&workspace, &taskID, &attempt, &runnerID, &status, &desired, &fence, &boundProject, &boundSession)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, apiError(404, "not_found", "run not found", nil))
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	if runnerID != p.RunnerID || !HasRunnerScope(p, "runner:observe", workspace) {
		writeError(w, apiError(403, "forbidden", "runner does not own this run", nil))
		return
	}
	if observation.AttemptID != attempt || observation.Fence != fence {
		writeError(w, apiError(409, "fence_conflict", "observation does not match active attempt fence", map[string]any{"attempt_id": attempt, "fence": fence}))
		return
	}
	var oldHash []byte
	err = tx.QueryRowContext(r.Context(), `SELECT body_hash FROM runner_observations WHERE run_id=? AND runner_id=? AND idempotency_key=?`, runID, p.RunnerID, key).Scan(&oldHash)
	if err == nil {
		if !sameHash(oldHash, bodyHash[:]) {
			writeError(w, apiError(409, "idempotency_conflict", "observation key reused with different content", nil))
			return
		}
		if err = tx.Commit(); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, 200, map[string]string{"status": "recorded"})
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		writeError(w, err)
		return
	}
	if status == "verified" || status == "stopped" || status == "failed" {
		writeError(w, apiError(409, "state_conflict", "run is terminal", map[string]any{"status": status}))
		return
	}
	if observation.Kind == "stop_confirmed" && desired != "stop" {
		writeError(w, apiError(409, "state_conflict", "stop confirmation requires desired_control=stop", nil))
		return
	}
	if observation.Kind == "stop_confirmed" || observation.Kind == "runtime_terminated" {
		if boundProject == "" || boundSession == "" || observation.AOProjectID != boundProject || observation.AOSessionID != boundSession {
			writeError(w, apiError(409, "session_conflict", "termination observation must match the bound AO project and session", nil))
			return
		}
		proof := observation.TerminationProof
		_, offset := proofTimeZone(proof)
		if proof == nil || proof.Source != "ao_chat_stop" || proof.Method != "native_process_exit_waited" || proof.SessionID != boundSession || !proof.CleanupComplete || proof.VerifiedAt.IsZero() || offset != 0 || proof.VerifiedAt.After(time.Now().UTC().Add(5*time.Minute)) {
			writeError(w, apiError(409, "termination_unproven", "termination requires trusted AO stop proof for the bound session", nil))
			return
		}
	}
	now := time.Now().UTC()
	if observation.OccurredAt.IsZero() {
		observation.OccurredAt = now
	}
	if observation.OccurredAt.After(now.Add(5 * time.Minute)) {
		writeError(w, apiError(400, "invalid_request", "observation timestamp is too far in the future", nil))
		return
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO runner_observations(observation_id,idempotency_key,run_id,attempt_id,fence,runner_id,kind,occurred_at,body_hash,body) VALUES(?,?,?,?,?,?,?,?,?,?)`, observation.ObservationID, key, runID, attempt, fence, p.RunnerID, observation.Kind, now.Format(time.RFC3339Nano), bodyHash[:], canonical)
	if err != nil {
		writeError(w, err)
		return
	}
	var taskVersion int64
	if err = tx.QueryRowContext(r.Context(), `SELECT version FROM tasks WHERE id=? AND workspace_id=?`, taskID, workspace).Scan(&taskVersion); err != nil {
		writeError(w, err)
		return
	}
	eventKind := "run." + observation.Kind
	nextRunStatus := status
	nextTaskStatus := ""
	var taskResult string
	provenance, _ := json.Marshal(observation)
	for _, item := range observation.Evidence {
		eid, e := randomID()
		if e != nil {
			writeError(w, e)
			return
		}
		_, e = tx.ExecContext(r.Context(), `INSERT INTO evidence(id,workspace_id,task_id,run_id,kind,summary,source,provenance,created_at,verified,verifier_id) VALUES(?,?,?,?,?,?, 'runner',?,?,0,'')`, eid, workspace, taskID, runID, item.Kind, item.Summary, provenance, now.Format(time.RFC3339Nano))
		if e != nil {
			writeError(w, e)
			return
		}
	}
	if observation.Kind == "result" {
		summary := string(observation.Result)
		if len(summary) > 20000 {
			summary = summary[:20000]
		}
		taskResult = summary
		if len(observation.Result) == 0 || !json.Valid(observation.Result) {
			writeError(w, apiError(400, "invalid_request", "result must be valid JSON", nil))
			return
		}
		eid, e := randomID()
		if e != nil {
			writeError(w, e)
			return
		}
		_, e = tx.ExecContext(r.Context(), `INSERT INTO evidence(id,workspace_id,task_id,run_id,kind,summary,source,provenance,created_at,verified,verifier_id) VALUES(?,?,?,?, 'agent_result',?,'runner',?,?,0,'')`, eid, workspace, taskID, runID, summary, provenance, now.Format(time.RFC3339Nano))
		if e != nil {
			writeError(w, e)
			return
		}
		_, err = tx.ExecContext(r.Context(), `UPDATE tasks SET result=?,version=version+1,updated_at=? WHERE id=? AND workspace_id=?`, summary, now.Format(time.RFC3339Nano), taskID, workspace)
		if err != nil {
			writeError(w, err)
			return
		}
		taskVersion++
	}
	if observation.Kind == "failure" {
		_, err = tx.ExecContext(r.Context(), `UPDATE tasks SET error=?,version=version+1,updated_at=? WHERE id=? AND workspace_id=?`, observation.Error, now.Format(time.RFC3339Nano), taskID, workspace)
		if err != nil {
			writeError(w, err)
			return
		}
		taskVersion++
	}
	for _, effect := range observation.ExternalEffects {
		details, _ := json.Marshal(effect)
		if effect.ID == "" || effect.Kind == "" {
			writeError(w, apiError(400, "invalid_request", "external effect requires id and kind", nil))
			return
		}
		_, err = tx.ExecContext(r.Context(), `INSERT INTO external_effects(id,workspace_id,task_id,run_id,kind,status,external_reference,first_observed_at,last_observed_at,details) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,external_reference=excluded.external_reference,last_observed_at=excluded.last_observed_at,details=excluded.details WHERE external_effects.run_id=excluded.run_id`, effect.ID, workspace, taskID, runID, effect.Kind, effect.Status, effect.Reference, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), details)
		if err != nil {
			writeError(w, err)
			return
		}
	}
	switch observation.Kind {
	case "dispatch_started":
		if status != "dispatching" {
			writeError(w, apiError(409, "state_conflict", "dispatch_started requires dispatching run", nil))
			return
		}
		nextRunStatus = "active"
	case "dispatch_bound":
		if observation.AOProjectID == "" || observation.AOSessionID == "" {
			writeError(w, apiError(400, "invalid_request", "dispatch_bound requires AO project and session IDs", nil))
			return
		}
		_, err = tx.ExecContext(r.Context(), `UPDATE runs SET ao_project_id=?,ao_session_id=? WHERE id=?`, observation.AOProjectID, observation.AOSessionID, runID)
		if err != nil {
			writeError(w, err)
			return
		}
	case "progress":
	case "result", "failure":
	case "effect_unknown":
		nextRunStatus = "uncertain"
	case "stop_confirmed":
		if desired != "stop" {
			writeError(w, apiError(409, "state_conflict", "stop confirmation requires requested stop", nil))
			return
		}
		// Runner emits this only after independent termination and cleanup confirmation.
		nextRunStatus = "stopped"
		nextTaskStatus = "canceled"
	case "runtime_terminated":
		nextRunStatus = "verification_pending"
		var resultCount int
		if err = tx.QueryRowContext(r.Context(), `SELECT count(*) FROM evidence WHERE run_id=? AND kind='agent_result'`, runID).Scan(&resultCount); err != nil {
			writeError(w, err)
			return
		}
		if desired == "stop" {
			nextRunStatus = "stopped"
			nextTaskStatus = "canceled"
		} else if resultCount > 0 {
			nextTaskStatus = "awaiting_user"
		} else {
			nextRunStatus = "failed"
			nextTaskStatus = "failed"
		}
	}
	if nextRunStatus != status {
		stoppedAt := any(nil)
		if nextRunStatus == "stopped" || nextRunStatus == "verification_pending" || nextRunStatus == "failed" {
			stoppedAt = now.Format(time.RFC3339Nano)
		}
		_, err = tx.ExecContext(r.Context(), `UPDATE runs SET status=?,last_observation_at=?,stopped_at=coalesce(?,stopped_at) WHERE id=? AND fence=?`, nextRunStatus, now.Format(time.RFC3339Nano), stoppedAt, runID, fence)
		if err != nil {
			writeError(w, err)
			return
		}
	}
	if nextTaskStatus != "" {
		if !validTaskTransition(statusForTask(status, nextTaskStatus), nextTaskStatus) { /* runner terminal transitions are validated by explicit observation kind */
		}
		taskVersion++
		finished := any(nil)
		if nextTaskStatus == "canceled" || nextTaskStatus == "failed" {
			finished = now.Format(time.RFC3339Nano)
		}
		_, err = tx.ExecContext(r.Context(), `UPDATE tasks SET status=?,version=?,updated_at=?,finished_at=coalesce(?,finished_at) WHERE id=? AND workspace_id=?`, nextTaskStatus, taskVersion, now.Format(time.RFC3339Nano), finished, taskID, workspace)
		if err != nil {
			writeError(w, err)
			return
		}
		if err = settleDependentChildren(r.Context(), tx, taskID, nextTaskStatus, now); err != nil {
			writeError(w, err)
			return
		}
		if err = cancelPendingChildProposals(r.Context(), tx, taskID, now); err != nil {
			writeError(w, err)
			return
		}
		_, err = tx.ExecContext(r.Context(), `UPDATE approvals SET status='canceled',version=version+1 WHERE task_id=? AND workspace_id=? AND status='pending'`, taskID, workspace)
		if err != nil {
			writeError(w, err)
			return
		}
	}
	if observation.Kind == "runtime_terminated" && nextTaskStatus == "awaiting_user" {
		if err = ensureAcceptanceApprovalPlaceholder(r.Context(), tx, workspace, taskID); err != nil {
			writeError(w, err)
			return
		}
	}
	if err = appendEventTx(r.Context(), tx, contracts.Event{Type: eventKind, OccurredAt: now, WorkspaceID: workspace, SubjectID: "", AggregateType: "run", AggregateID: runID, AggregateVersion: int64(fence), Payload: raw(map[string]any{"run_id": runID, "task_id": taskID, "attempt_id": attempt, "kind": observation.Kind, "task_status": nextTaskStatus, "result": taskResult})}); err != nil {
		writeError(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "recorded"})
}

func proofTimeZone(proof *contracts.TerminationProof) (string, int) {
	if proof == nil || proof.VerifiedAt.IsZero() {
		return "", 1
	}
	return proof.VerifiedAt.Zone()
}

func statusForTask(runStatus, taskStatus string) string {
	_ = runStatus
	switch taskStatus {
	case "canceled", "failed", "awaiting_user":
		return "active"
	}
	return "active"
}
func validObservationKind(k string) bool {
	switch k {
	case "dispatch_started", "dispatch_bound", "progress", "result", "failure", "effect_unknown", "stop_confirmed", "runtime_terminated":
		return true
	}
	return false
}
func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func appendEventTx(ctx context.Context, tx *sql.Tx, e contracts.Event) error {
	if e.EventID == "" {
		id, err := randomID()
		if err != nil {
			return err
		}
		e.EventID = id
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	e.SchemaVersion = 1
	var cursor int64
	if err := tx.QueryRowContext(ctx, `UPDATE event_counters SET current_cursor=current_cursor+1 WHERE workspace_id=? RETURNING current_cursor`, e.WorkspaceID).Scan(&cursor); err != nil {
		return err
	}
	e.Cursor = strconv.FormatInt(cursor, 10)
	payload, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO events(event_id,workspace_id,cursor,schema_version,event_type,occurred_at,subject_id,aggregate_type,aggregate_id,aggregate_version,payload) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, e.EventID, e.WorkspaceID, cursor, e.SchemaVersion, e.Type, e.OccurredAt.Format(time.RFC3339Nano), e.SubjectID, e.AggregateType, e.AggregateID, e.AggregateVersion, payload)
	if err != nil {
		return err
	}
	return enqueueEventNotification(ctx, tx, e)
}
func sameHash(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}
func ensureAcceptanceApprovalPlaceholder(ctx context.Context, tx *sql.Tx, workspace, task string) error {
	return nil
}

var _ = fmt.Sprintf
