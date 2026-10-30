package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ronigooja/Zoe/contracts"
	"github.com/ronigooja/Zoe/internal/agents/opencode"
)

var ErrNoClaim = errors.New("no runner claim available")
var ErrOutcomePending = errors.New("external outcome remains unresolved and fenced")

type WorkIsolation interface {
	Check(context.Context, contracts.JobClaim) error
}
type RunnerAPI interface {
	Claim(context.Context) (*contracts.JobClaim, error)
	Current(context.Context) ([]contracts.JobClaim, error)
	Control(context.Context, string) (contracts.RunControl, error)
	Observe(context.Context, string, contracts.RunnerObservation) error
}
type runBudgetControl interface {
	ReserveBudget(context.Context, string, contracts.RunBudgetReserveRequest) (contracts.RunBudgetReceipt, error)
	SettleBudget(context.Context, string, string, contracts.RunBudgetSettleRequest) (contracts.RunBudgetReceipt, error)
}
type dispatchAuthorizationControl interface {
	CheckDispatchAuthorization(context.Context, string) (bool, string, error)
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
	Control                      RunnerAPI
	Instance                     InstanceAuthority
	Runtime                      Runtime
	Journal                      *Journal
	Routes                       map[string]Route
	Isolation                    WorkIsolation
	PollInterval, ObserveTimeout time.Duration
}

func (e *Engine) RunOnce(ctx context.Context) error {
	if e.Control == nil || e.Journal == nil {
		return errors.New("runner engine requires control client and journal")
	}
	c, err := e.Control.Claim(ctx)
	if err != nil {
		return err
	}
	if c == nil {
		return ErrNoClaim
	}
	return e.Process(ctx, *c)
}
func (e *Engine) Process(ctx context.Context, claim contracts.JobClaim) error {
	if err := validateClaim(claim); err != nil {
		return err
	}
	if e.Runtime == nil {
		return errors.New("OpenCode runtime is required")
	}
	entry, ok, err := e.Journal.Entry(ctx, claim.RunID)
	if err != nil {
		return err
	}
	if ok && entry.Session != "" {
		return e.monitor(ctx, claim, RuntimeBinding{SessionID: entry.Session, ContainerID: entry.Container})
	}
	route, exists := e.Routes[claim.ProfileID]
	if !exists || route.Harness != "opencode" || route.IsolationPolicy != OpenCodeWorkspaceWritePolicy {
		return e.terminal(ctx, claim, "runtime_unavailable", "OpenCode route unavailable")
	}
	if e.Isolation != nil {
		if err := e.Isolation.Check(ctx, claim); err != nil {
			return e.terminal(ctx, claim, "isolation_denied", err.Error())
		}
	}
	spec := RuntimeSpec{RunID: claim.RunID, AttemptID: claim.AttemptID, Fence: fmt.Sprint(claim.Fence), Model: route.Model, Prompt: buildPrompt(claim), Workspace: claim.WorkspaceID}
	entry, err = e.Journal.SaveClaim(ctx, claim, spec, "create_attempted")
	if err != nil {
		return err
	}
	binding, err := e.Runtime.Create(ctx, spec)
	if err != nil {
		return err
	}
	if binding.SessionID == "" || binding.ContainerID == "" {
		return fmt.Errorf("%w: incomplete runtime binding", ErrRuntimeOutcomeUnknown)
	}
	if err = e.Journal.SetSession(ctx, claim.RunID, claim.AttemptID, claim.Fence, binding.SessionID, "bound"); err != nil {
		return err
	}
	if err = e.Journal.SetContainer(ctx, claim.RunID, claim.AttemptID, claim.Fence, binding.ContainerID); err != nil {
		return err
	}
	_ = entry
	if err = e.Runtime.Prompt(ctx, binding.SessionID, spec.Prompt); err != nil {
		return err
	}
	return e.monitor(ctx, claim, binding)
}
func (e *Engine) monitor(ctx context.Context, claim contracts.JobClaim, b RuntimeBinding) error {
	steps := int64(0)
	maxSteps := claim.Budget.MaxToolSteps
	events, errs := e.Runtime.Events(ctx)
	stepEvents := make(chan opencode.Event, 16)
	go func() {
		defer close(stepEvents)
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				select {
				case stepEvents <- event:
				case <-ctx.Done():
					return
				}
			case err, ok := <-errs:
				if ok && err != nil {
					return
				}
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case _, ok := <-stepEvents:
			if !ok {
				stepEvents = nil
				continue
			}
			steps++
			if maxSteps > 0 && steps > maxSteps {
				_ = e.Runtime.Abort(ctx, b.SessionID)
				return e.stop(ctx, claim, b, "tool_step_limit")
			}
		default:
		}
		status, err := e.Runtime.Status(ctx)
		if err != nil {
			time.Sleep(e.interval())
			continue
		}
		msgs, err := e.Runtime.Messages(ctx, b.SessionID)
		if err != nil {
			time.Sleep(e.interval())
			continue
		}
		m, done, term := opencode.Completion(status[b.SessionID], msgs)
		if done {
			if term != nil {
				_ = e.Runtime.Abort(ctx, b.SessionID)
				return e.stop(ctx, claim, b, "runtime_error")
			}
			return e.stopWithResult(ctx, claim, b, "completed", m)
		}
		if !claim.Budget.HardDeadline.IsZero() && time.Now().After(claim.Budget.HardDeadline) {
			_ = e.Runtime.Abort(ctx, b.SessionID)
			return e.stop(ctx, claim, b, "run_deadline_exceeded")
		}
		time.Sleep(e.interval())
	}
}
func (e *Engine) stop(ctx context.Context, claim contracts.JobClaim, b RuntimeBinding, reason string) error {
	return e.stopWithResult(ctx, claim, b, reason, opencode.Message{})
}
func (e *Engine) stopWithResult(ctx context.Context, claim contracts.JobClaim, b RuntimeBinding, reason string, message opencode.Message) error {
	id, err := e.Runtime.Terminate(ctx, b)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrOutcomePending, err)
	}
	proof := &contracts.TerminationProof{Source: "runner_container_removal", Method: "container_removed", RuntimeSessionID: b.SessionID, RuntimeContainerID: id, VerifiedAt: time.Now().UTC(), CleanupComplete: true}
	return e.emitResult(ctx, claim, "runtime_terminated", "runtime", reason, proof, message)
}
func (e *Engine) terminal(ctx context.Context, c contracts.JobClaim, code, msg string) error {
	return e.emit(ctx, c, "failure", "runner", code, nil)
}
func (e *Engine) emit(ctx context.Context, c contracts.JobClaim, kind, source, summary string, proof *contracts.TerminationProof) error {
	return e.emitResult(ctx, c, kind, source, summary, proof, opencode.Message{})
}
func (e *Engine) emitResult(ctx context.Context, c contracts.JobClaim, kind, source, summary string, proof *contracts.TerminationProof, message opencode.Message) error {
	raw, _ := json.Marshal(map[string]string{"summary": summary})
	if message.Info.ID != "" {
		raw, _ = json.Marshal(map[string]any{"summary": summary, "assistant_message": message})
	}
	sum := sha256.Sum256(raw)
	obs := contracts.RunnerObservation{ObservationID: hex.EncodeToString(sum[:16]), RunID: c.RunID, AttemptID: c.AttemptID, Fence: c.Fence, Kind: kind, OccurredAt: time.Now().UTC(), RuntimeSessionID: c.RuntimeSessionID, TerminationProof: proof, Result: raw}
	if e.Journal != nil {
		_ = e.Journal.QueueObservation(ctx, c.RunID, obs)
	}
	if e.Control != nil {
		return e.Control.Observe(ctx, c.RunID, obs)
	}
	return nil
}
func (e *Engine) flush(ctx context.Context, runID string) error {
	pending, err := e.Journal.PendingObservations(ctx, runID)
	if err != nil {
		return err
	}
	for _, q := range pending {
		if err := e.Control.Observe(ctx, runID, q.Body); err != nil {
			return err
		}
		if err := e.Journal.AcknowledgeObservation(ctx, q.Body.ObservationID); err != nil {
			return err
		}
	}
	return nil
}
func (e *Engine) emitNoEffectTerminal(ctx context.Context, c contracts.JobClaim, reason, message string, p *contracts.NoEffectProof) error {
	return e.emit(ctx, c, "no_effect_terminal", reason, message, nil)
}
func sameRecoveredClaim(a, b contracts.JobClaim, _ string) bool {
	a.RuntimeSessionID = ""
	b.RuntimeSessionID = ""
	return a.RunID == b.RunID && a.AttemptID == b.AttemptID && a.Fence == b.Fence && a.Harness == b.Harness
}
func (e *Engine) interval() time.Duration {
	if e.PollInterval > 0 {
		return e.PollInterval
	}
	return time.Second
}
func validateClaim(c contracts.JobClaim) error {
	if c.RunID == "" || c.TaskID == "" || c.AttemptID == "" || c.WorkspaceID == "" || c.Fence == 0 || c.ProfileID == "" || c.RuntimeRouteID == "" || c.ModelRouteID == "" || c.IsolationPolicy == "" {
		return errors.New("runner claim is missing required identity")
	}
	if c.Harness != "opencode" {
		return errors.New("runner claim selects unsupported runtime")
	}
	return nil
}
func buildPrompt(c contracts.JobClaim) string {
	return "Work only on the assigned goal. Do not delegate or perform external irreversible changes.\n\nGoal:\n" + c.Goal
}
func observationID(run, attempt string, fence uint64, kind, disc string) string {
	h := sha256.Sum256([]byte(strings.Join([]string{run, attempt, fmt.Sprint(fence), kind, disc}, "\x00")))
	return hex.EncodeToString(h[:16])
}
