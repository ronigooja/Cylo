package memory

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/control"
)

var (
	modelCallProviderPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	modelCallNamePattern     = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	providerRequestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,160}$`)
)

type modelCallReserveRequest struct {
	Attempt              int    `json:"attempt"`
	RequestKey           string `json:"request_key"`
	Provider             string `json:"provider"`
	Model                string `json:"model"`
	EstimatedInputTokens int    `json:"estimated_input_tokens"`
	MaxOutputTokens      int    `json:"max_output_tokens"`
}

type modelCallReservation struct {
	ID              string `json:"call_id"`
	DispatchAllowed bool   `json:"dispatch_allowed"`
	Status          string `json:"status"`
	ReservedTokens  int    `json:"reserved_tokens"`
}

type modelCallFinalizeRequest struct {
	Attempt           int    `json:"attempt"`
	Outcome           string `json:"outcome"`
	UsageStatus       string `json:"usage_status"`
	InputTokens       *int   `json:"input_tokens"`
	OutputTokens      *int   `json:"output_tokens"`
	ProviderRequestID string `json:"provider_request_id,omitempty"`
}

type modelCallResult struct {
	ID     string `json:"call_id"`
	Status string `json:"status"`
}

func (s *derivedService) handleModelCallReserve(w http.ResponseWriter, r *http.Request) {
	p, ok := control.RunnerPrincipalFromContext(r.Context())
	if !ok || p.RunnerID == "" || !exactRunnerHasScope(p, derivedCompleteScope) {
		control.WriteError(w, control.NewAPIError(http.StatusForbidden, "forbidden", "runner memory job completion scope required", nil))
		return
	}
	var req modelCallReserveRequest
	if err := decodeJSON(r, &req); err != nil {
		control.WriteError(w, err)
		return
	}
	result, err := s.reserveModelCall(r.Context(), p, r.PathValue("job_id"), req)
	if err != nil {
		control.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *derivedService) reserveModelCall(ctx context.Context, p contracts.RunnerPrincipal, jobID string, req modelCallReserveRequest) (modelCallReservation, error) {
	if jobID == "" || req.Attempt < 1 || uuid.Validate(req.RequestKey) != nil ||
		!modelCallProviderPattern.MatchString(req.Provider) || !modelCallNamePattern.MatchString(req.Model) ||
		req.EstimatedInputTokens < 1 || req.MaxOutputTokens < 1 {
		return modelCallReservation{}, control.NewAPIError(http.StatusBadRequest, "invalid_request", "model call reservation fields are invalid", nil)
	}
	if req.EstimatedInputTokens > s.cfg.ModelTokenBudget || req.MaxOutputTokens > s.cfg.ModelTokenBudget {
		return modelCallReservation{}, control.NewAPIError(http.StatusConflict, "model_budget_exhausted", "memory model token reservation exceeds the job budget", nil)
	}
	reserved := req.EstimatedInputTokens + req.MaxOutputTokens
	if reserved > s.cfg.ModelTokenBudget {
		return modelCallReservation{}, control.NewAPIError(http.StatusConflict, "model_budget_exhausted", "memory model token reservation exceeds the job budget", nil)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return modelCallReservation{}, err
	}
	defer tx.Rollback()
	job, err := s.activeModelJob(ctx, tx, p, jobID, req.Attempt, true)
	if err != nil {
		return modelCallReservation{}, err
	}
	var prior modelCallReservation
	var priorAttempt, priorInput, priorOutput int
	var priorProvider, priorModel string
	err = tx.QueryRowContext(ctx, `SELECT id,attempt,provider,model,estimated_input_tokens,max_output_tokens,reserved_tokens,status
FROM memory_model_calls WHERE workspace_id=? AND job_id=? AND request_key=?`, job.WorkspaceID, jobID, req.RequestKey).Scan(
		&prior.ID, &priorAttempt, &priorProvider, &priorModel, &priorInput, &priorOutput, &prior.ReservedTokens, &prior.Status)
	if err == nil {
		if priorAttempt != req.Attempt || priorProvider != req.Provider || priorModel != req.Model || priorInput != req.EstimatedInputTokens || priorOutput != req.MaxOutputTokens {
			return modelCallReservation{}, control.NewAPIError(http.StatusConflict, "idempotency_conflict", "model call request key was already used with different fields", nil)
		}
		prior.DispatchAllowed = false
		if err := tx.Commit(); err != nil {
			return modelCallReservation{}, err
		}
		return prior, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return modelCallReservation{}, err
	}
	var calls, tokens int
	if err := tx.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(max(reserved_tokens,coalesce(input_tokens+output_tokens,reserved_tokens))),0)
FROM memory_model_calls WHERE workspace_id=? AND job_id=?`, job.WorkspaceID, jobID).Scan(&calls, &tokens); err != nil {
		return modelCallReservation{}, err
	}
	if calls >= s.cfg.ModelCallLimit || tokens+reserved > s.cfg.ModelTokenBudget {
		return modelCallReservation{}, control.NewAPIError(http.StatusConflict, "model_budget_exhausted", "memory model call or token budget is exhausted", nil)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	prior = modelCallReservation{ID: uuid.NewString(), DispatchAllowed: true, Status: "reserved", ReservedTokens: reserved}
	if _, err := tx.ExecContext(ctx, `INSERT INTO memory_model_calls(id,workspace_id,job_id,attempt,request_key,runner_id,provider,model,estimated_input_tokens,max_output_tokens,reserved_tokens,status,created_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,'reserved',?)`, prior.ID, job.WorkspaceID, jobID, req.Attempt, req.RequestKey, p.RunnerID, req.Provider, req.Model, req.EstimatedInputTokens, req.MaxOutputTokens, reserved, now); err != nil {
		return modelCallReservation{}, err
	}
	if err := tx.Commit(); err != nil {
		return modelCallReservation{}, err
	}
	return prior, nil
}

// activeModelJob validates a fresh dispatch against the current extraction
// lease and current source. Finalization uses the ledger's owned call record
// instead, so a completed provider response can still be accounted after the
// lease expires or the authority source is deleted.
func (s *derivedService) activeModelJob(ctx context.Context, tx *sql.Tx, p contracts.RunnerPrincipal, id string, attempt int, requireSource bool) (derivedJob, error) {
	if id == "" || attempt < 1 {
		return derivedJob{}, control.NewAPIError(http.StatusBadRequest, "invalid_request", "job ID and positive lease attempt are required", nil)
	}
	var job derivedJob
	var status, owner string
	var leaseUntil sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT id,workspace_id,kind,entity_id,entity_version,target,payload,attempts,status,lease_owner,lease_until FROM memory_jobs WHERE id=?`, id).Scan(
		&job.ID, &job.WorkspaceID, &job.Kind, &job.EntityID, &job.EntityVersion, &job.Target, &job.Payload, &job.Attempt, &status, &owner, &leaseUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return derivedJob{}, control.NewAPIError(http.StatusNotFound, "not_found", "memory job not found", nil)
	}
	if err != nil {
		return derivedJob{}, err
	}
	if !exactRunnerHasWorkspace(p, job.WorkspaceID) || !exactRunnerHasScope(p, derivedCompleteScope) {
		return derivedJob{}, control.NewAPIError(http.StatusForbidden, "forbidden", "runner cannot access this workspace model call", nil)
	}
	if job.Kind != "extract" {
		return derivedJob{}, control.NewAPIError(http.StatusConflict, "invalid_job_kind", "model calls are allowed only for extraction jobs", nil)
	}
	if status != "leased" || owner != p.RunnerID || job.Attempt != attempt || !leaseUntil.Valid {
		return derivedJob{}, control.NewAPIError(http.StatusConflict, "lease_lost", "memory job lease is no longer current", nil)
	}
	deadline, parseErr := time.Parse(time.RFC3339Nano, leaseUntil.String)
	if parseErr != nil || !deadline.After(time.Now().UTC()) {
		return derivedJob{}, control.NewAPIError(http.StatusConflict, "lease_expired", "memory job lease expired", nil)
	}
	if requireSource {
		_, disposition, err := s.materializeTx(ctx, tx, &job)
		if err != nil {
			return derivedJob{}, err
		}
		if disposition != "" {
			return derivedJob{}, control.NewAPIError(http.StatusConflict, "source_unavailable", "memory extraction source is no longer current", nil)
		}
	}
	return job, nil
}

func (s *derivedService) handleModelCallFinalize(w http.ResponseWriter, r *http.Request) {
	p, ok := control.RunnerPrincipalFromContext(r.Context())
	if !ok || p.RunnerID == "" || !exactRunnerHasScope(p, derivedCompleteScope) {
		control.WriteError(w, control.NewAPIError(http.StatusForbidden, "forbidden", "runner memory job completion scope required", nil))
		return
	}
	var req modelCallFinalizeRequest
	if err := decodeJSON(r, &req); err != nil {
		control.WriteError(w, err)
		return
	}
	result, err := s.finalizeModelCall(r.Context(), p, r.PathValue("job_id"), r.PathValue("call_id"), req)
	if err != nil {
		control.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *derivedService) finalizeModelCall(ctx context.Context, p contracts.RunnerPrincipal, jobID, callID string, req modelCallFinalizeRequest) (modelCallResult, error) {
	if jobID == "" || callID == "" || req.Attempt < 1 || (req.Outcome != "succeeded" && req.Outcome != "failed") ||
		(req.UsageStatus != "measured" && req.UsageStatus != "unknown") ||
		(req.UsageStatus == "measured" && (req.InputTokens == nil || req.OutputTokens == nil || *req.InputTokens < 0 || *req.OutputTokens < 0 || *req.InputTokens > 1_000_000 || *req.OutputTokens > 1_000_000)) ||
		(req.UsageStatus == "unknown" && (req.InputTokens != nil || req.OutputTokens != nil)) ||
		(req.ProviderRequestID != "" && !providerRequestIDPattern.MatchString(req.ProviderRequestID)) {
		return modelCallResult{}, control.NewAPIError(http.StatusBadRequest, "invalid_request", "model call finalization fields are invalid", nil)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return modelCallResult{}, err
	}
	defer tx.Rollback()
	var workspace, runnerID, status, priorOutcome, priorUsage string
	var attempt int
	var priorInput, priorOutput sql.NullInt64
	var priorRequestID string
	err = tx.QueryRowContext(ctx, `SELECT workspace_id,runner_id,attempt,status,outcome,usage_status,input_tokens,output_tokens,provider_request_id
FROM memory_model_calls WHERE id=? AND job_id=?`, callID, jobID).Scan(&workspace, &runnerID, &attempt, &status, &priorOutcome, &priorUsage, &priorInput, &priorOutput, &priorRequestID)
	if errors.Is(err, sql.ErrNoRows) {
		return modelCallResult{}, control.NewAPIError(http.StatusNotFound, "not_found", "memory model call not found", nil)
	}
	if err != nil {
		return modelCallResult{}, err
	}
	if !exactRunnerHasWorkspace(p, workspace) || runnerID != p.RunnerID || !exactRunnerHasScope(p, derivedCompleteScope) {
		return modelCallResult{}, control.NewAPIError(http.StatusForbidden, "forbidden", "runner cannot finalize this model call", nil)
	}
	if attempt != req.Attempt {
		return modelCallResult{}, control.NewAPIError(http.StatusConflict, "attempt_conflict", "model call belongs to a different lease attempt", nil)
	}
	if status == "finalized" {
		if sameModelCallFinalization(req, priorOutcome, priorUsage, priorInput, priorOutput, priorRequestID) {
			if err := tx.Commit(); err != nil {
				return modelCallResult{}, err
			}
			return modelCallResult{ID: callID, Status: status}, nil
		}
		return modelCallResult{}, control.NewAPIError(http.StatusConflict, "finalization_conflict", "model call was finalized with different usage", nil)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var inputTokens, outputTokens any
	if req.UsageStatus == "measured" {
		inputTokens, outputTokens = *req.InputTokens, *req.OutputTokens
	}
	if _, err := tx.ExecContext(ctx, `UPDATE memory_model_calls SET status='finalized',outcome=?,usage_status=?,input_tokens=?,output_tokens=?,provider_request_id=?,finalized_at=? WHERE id=? AND status='reserved'`, req.Outcome, req.UsageStatus, inputTokens, outputTokens, strings.TrimSpace(req.ProviderRequestID), now, callID); err != nil {
		return modelCallResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return modelCallResult{}, err
	}
	return modelCallResult{ID: callID, Status: "finalized"}, nil
}

func sameModelCallFinalization(req modelCallFinalizeRequest, outcome, usage string, input, output sql.NullInt64, requestID string) bool {
	if req.Outcome != outcome || req.UsageStatus != usage || strings.TrimSpace(req.ProviderRequestID) != requestID {
		return false
	}
	if usage == "unknown" {
		return req.InputTokens == nil && req.OutputTokens == nil && !input.Valid && !output.Valid
	}
	return req.InputTokens != nil && req.OutputTokens != nil && input.Valid && output.Valid && *req.InputTokens == int(input.Int64) && *req.OutputTokens == int(output.Int64)
}
