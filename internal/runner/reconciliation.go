package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/agents/ao"
)

type SessionInventoryReader interface {
	ListSessions(context.Context) (ao.SessionInventory, error)
}

// ReconcileNoEffect never creates, stops, or removes an AO or Docker runtime.
// The caller must hold the normal deployment-host and journal process locks.
func (e *Engine) ReconcileNoEffect(ctx context.Context, runIDs []string, actor string, confirmed bool, inventory SessionInventoryReader, docker DockerCommand) error {
	actor = strings.TrimSpace(actor)
	if len(runIDs) == 0 || actor == "" || len(actor) > 200 || strings.ContainsAny(actor, "\r\n") || !confirmed {
		return errors.New("explicit run IDs, actor, and no-effect confirmation are required")
	}
	if e.Journal == nil || e.Control == nil || e.Instance == nil || inventory == nil || docker == nil {
		return errors.New("reconciliation requires journal, control, instance fence, AO and Docker inventory")
	}
	seen := map[string]bool{}
	for _, id := range runIDs {
		if id == "" || strings.TrimSpace(id) != id || seen[id] {
			return errors.New("run IDs must be explicit, nonempty and unique")
		}
		seen[id] = true
	}
	for _, id := range runIDs {
		if err := e.reconcileNoEffect(ctx, id, actor, inventory, docker); err != nil {
			return fmt.Errorf("reconcile run %s: %w", id, err)
		}
	}
	return nil
}

func (e *Engine) reconcileNoEffect(ctx context.Context, id, actor string, inventory SessionInventoryReader, docker DockerCommand) (resultErr error) {
	checkedAt := time.Now().UTC()
	auditID := observationID(id, actor, 0, "reconciliation_audit", checkedAt.Format(time.RFC3339Nano))
	audit := map[string]any{"confirmed": true, "outcome": "started", "ao_query": "GET /api/v1/sessions/inventory", "ao_result": "not_checked", "docker_query": "docker ps -aq --no-trunc; docker inspect all IDs", "docker_result": "not_checked", "stage": "journal"}
	if err := e.Journal.SaveReconciliationAudit(ctx, auditID, id, actor, checkedAt, audit); err != nil {
		return err
	}
	defer func() {
		audit["outcome"] = "refused"
		if resultErr == nil {
			audit["outcome"] = "completed"
		}
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		if err := e.Journal.SaveReconciliationAudit(auditCtx, auditID, id, actor, checkedAt, audit); err != nil {
			resultErr = fmt.Errorf("persist reconciliation audit: %w", err)
		}
	}()

	entry, ok, err := e.Journal.Entry(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("run has no durable journal claim")
	}
	if err := e.checkInstance(ctx, entry.Claim, false); err != nil {
		return err
	}
	audit["stage"] = "receipt_replay"
	receipt, err := e.Journal.NoEffectReceipt(ctx, id)
	if err != nil {
		return err
	}
	if receipt != nil {
		if receipt.NoEffectProof == nil || receipt.NoEffectProof.Reconciliation == nil {
			return errors.New("run has a different terminal receipt")
		}
		if err := e.Journal.SetPhase(ctx, id, entry.Claim.AttemptID, entry.Claim.Fence, "no_effect_terminal"); err != nil {
			return err
		}
		return e.flush(ctx, id)
	}
	if entry.Phase != "effect_unknown" || entry.Session != "" || entry.Container != "" || entry.Claim.AOSessionID != "" || entry.Spec.ClientRequestID != id || entry.Spec.ProjectID != entry.Claim.AOProjectID {
		return errors.New("only an effect_unknown run with an exact create request and no session or container binding can be reconciled")
	}
	audit["stage"] = "server_claim"
	current, err := e.Control.Current(ctx)
	if err != nil {
		return err
	}
	matches := 0
	for _, claim := range current {
		if claim.RunID == id {
			if !sameRecoveredClaim(entry.Claim, claim, "") {
				return errors.New("server claim differs from durable fence")
			}
			matches++
		}
	}
	if matches != 1 {
		return errors.New("run is not a current server-owned claim")
	}
	audit["stage"] = "ao_inventory"
	audit["ao_result"] = "unavailable"
	sessions, err := inventory.ListSessions(ctx)
	if err != nil {
		return err
	}
	audit["ao_result"] = map[string]any{"complete": sessions.Complete, "client_request_id_complete": sessions.ClientRequestIDComplete, "session_count": len(sessions.Sessions), "matches": 0}
	if !sessions.Complete || !sessions.ClientRequestIDComplete {
		return errors.New("AO inventory cannot prove complete clientRequestId absence")
	}
	for _, session := range sessions.Sessions {
		if session.ClientRequestID == "" || session.ClientRequestID == id {
			audit["ao_result"] = "matching_or_uncorrelated_session"
			return errors.New("AO inventory includes matching or uncorrelated session")
		}
	}
	audit["stage"] = "docker_inventory"
	audit["docker_result"] = "unavailable_or_runtime_present"
	if err := verifyNoAORuntimeContainers(ctx, docker); err != nil {
		audit["docker_result"] = err.Error()
		return err
	}
	if err := e.checkInstance(ctx, entry.Claim, false); err != nil {
		return err
	}
	audit["docker_result"] = "complete_no_runtime"
	audit["stage"] = "terminal_receipt"
	proof := &contracts.NoEffectProof{JournalPhase: "effect_unknown", AOCreateStarted: true, Outcome: "failed", Reconciliation: &contracts.NoEffectReconciliation{
		RunID: id, Actor: actor, Confirmed: true, VerifiedAt: time.Now().UTC(), AOQuery: "GET /api/v1/sessions/inventory", AOInventoryComplete: true, AOClientRequestIDComplete: true,
		DockerQuery: "docker ps -aq --no-trunc; docker inspect all IDs", DockerInventoryComplete: true, InstanceID: entry.InstanceID, InstanceEpoch: entry.Epoch,
	}}
	// Persist the complete immutable receipt before changing phase or sending it.
	// emit queues before delivery; any crash is recovered by NoEffectReceipt.
	err = e.emitNoEffectTerminal(ctx, entry.Claim, "operator_reconciled_absence", "AO and Docker read-only inventories proved no runtime; the operator reconciled the unknown create outcome.", proof)
	durable, receiptErr := e.Journal.NoEffectReceipt(ctx, id)
	if receiptErr != nil {
		return receiptErr
	}
	if durable == nil {
		return err
	}
	if phaseErr := e.Journal.SetPhase(ctx, id, entry.Claim.AttemptID, entry.Claim.Fence, "no_effect_terminal"); phaseErr != nil {
		return phaseErr
	}
	return err
}

func verifyNoAORuntimeContainers(ctx context.Context, docker DockerCommand) error {
	output, err := docker.Run(ctx, "ps", "-aq", "--no-trunc")
	if err != nil {
		return errors.New("complete Docker inventory unavailable")
	}
	for _, id := range splitContainerIDs(string(output)) {
		body, err := docker.Run(ctx, "inspect", "--format", "{{json .}}", id)
		if err != nil {
			return errors.New("Docker inventory changed or inspection failed")
		}
		var container struct {
			ID     string `json:"Id"`
			Name   string `json:"Name"`
			Config struct {
				Image  string            `json:"Image"`
				Labels map[string]string `json:"Labels"`
			} `json:"Config"`
		}
		if json.Unmarshal(body, &container) != nil || container.ID != id || len(id) != 64 {
			return errors.New("Docker inventory identity is incomplete")
		}
		labels := container.Config.Labels
		legacy := strings.ToLower(container.Name + " " + container.Config.Image)
		if labels[aoContainerRuntimeLabel] != "" || labels[aoContainerSessionLabel] != "" || labels[aoContainerManagedLabel] == "true" || strings.Contains(legacy, "codex") || strings.Contains(legacy, "opencode") || strings.Contains(legacy, "ao-worker") {
			return errors.New("AO or legacy uncorrelated managed runtime container exists; absence is unproven")
		}
	}
	return nil
}
