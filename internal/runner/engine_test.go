package runner

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/agents/ao"
)

type fakeControl struct {
	order                   *[]string
	control                 contracts.RunControl
	controls                []string
	controlIndex            int
	observations            []contracts.RunnerObservation
	loseFirstObservationAck bool
}

func (f *fakeControl) Claim(context.Context) (*contracts.JobClaim, error)    { return nil, ErrNoClaim }
func (f *fakeControl) Current(context.Context) ([]contracts.JobClaim, error) { return nil, nil }
func (f *fakeControl) Control(_ context.Context, runID string) (contracts.RunControl, error) {
	if f.order != nil {
		*f.order = append(*f.order, "control")
	}
	result := f.control
	if f.controlIndex < len(f.controls) {
		result.DesiredControl = f.controls[f.controlIndex]
		f.controlIndex++
	}
	result.RunID = runID
	return result, nil
}
func (f *fakeControl) Observe(_ context.Context, _ string, observation contracts.RunnerObservation) error {
	f.observations = append(f.observations, observation)
	if f.order != nil {
		*f.order = append(*f.order, "observe:"+observation.Kind)
	}
	if f.loseFirstObservationAck {
		f.loseFirstObservationAck = false
		return errors.New("simulated lost observation acknowledgement")
	}
	return nil
}

type fakeAO struct {
	order        *[]string
	createErr    error
	createSpec   ao.WorkerSpec
	createN      int
	getN         int
	stopN        int
	stopSuccess  bool
	stopPending  bool
	proofSession string
	proofMethod  string
	session      ao.Session
	conversation ao.Conversation
}

func (f *fakeAO) CreateWorker(_ context.Context, spec ao.WorkerSpec) (ao.Session, error) {
	f.createN++
	f.createSpec = spec
	if f.order != nil {
		*f.order = append(*f.order, "create")
	}
	if f.createErr != nil {
		return ao.Session{}, f.createErr
	}
	return ao.Session{ID: "ao-1"}, nil
}
func (f *fakeAO) GetSession(_ context.Context, id string) (ao.Session, error) {
	f.getN++
	if f.order != nil {
		*f.order = append(*f.order, "get-session")
	}
	if f.session.ID == "" {
		f.session.ID = id
	}
	return f.session, nil
}
func (f *fakeAO) GetConversation(context.Context, string) (ao.Conversation, error) {
	return f.conversation, nil
}
func (f *fakeAO) StopWorker(_ context.Context, id string) (ao.Session, ao.KillResponse, error) {
	f.stopN++
	if f.order != nil {
		*f.order = append(*f.order, "stop")
	}
	if f.stopSuccess {
		proofSession := f.proofSession
		if proofSession == "" {
			proofSession = id
		}
		method := f.proofMethod
		if method == "" {
			method = "native_process_exit_waited"
		}
		proof := &ao.TerminationProof{Source: "ao_chat_stop", Method: method, SessionID: proofSession, VerifiedAt: time.Now().UTC(), CleanupComplete: !f.stopPending}
		return ao.Session{ID: id, Status: "terminated"}, ao.KillResponse{SessionID: id, Freed: !f.stopPending, CleanupPending: f.stopPending, TerminationProof: proof}, nil
	}
	return ao.Session{}, ao.KillResponse{}, errors.New("kill response unknown")
}

type permitIsolation struct{}

func (permitIsolation) Check(context.Context, contracts.JobClaim) error { return nil }

func makeEngine(t *testing.T, claim contracts.JobClaim, control contracts.RunControl, agent AO, isolation WorkIsolation, order *[]string) (*Engine, *fakeControl) {
	t.Helper()
	journal, err := OpenJournal(context.Background(), filepath.Join(t.TempDir(), "runner.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	fake := &fakeControl{order: order, control: control}
	engine := &Engine{
		Control:      fake,
		AO:           agent,
		Journal:      journal,
		Routes:       map[string]Route{"worker-safe": {ProfileID: "worker-safe", RuntimeRouteID: "codex", ModelRouteID: "sol", IsolationPolicy: CodexWorkspaceWritePolicy, Harness: "codex", Model: "gpt-6.1-sol", ApprovalMode: "accept-edits"}},
		Isolation:    isolation,
		PollInterval: time.Millisecond,
	}
	return engine, fake
}

func validClaim() contracts.JobClaim {
	return contracts.JobClaim{RunID: "run-1", TaskID: "task-1", AttemptID: "attempt-1", WorkspaceID: "workspace-1", TaskVersion: 2, Goal: "Inspect a local fixture.", ProfileID: "worker-safe", Harness: "codex", RuntimeRouteID: "codex", ModelRouteID: "sol", IsolationPolicy: CodexWorkspaceWritePolicy, Role: "worker", AOProjectID: "fixture-project", Authorization: contracts.JobAuthorization{Scopes: []string{"workspace:read", "workspace:write"}}, Fence: 7}
}

func TestProcessJournalsDispatchBeforeAOAndReportsUnverifiedTermination(t *testing.T) {
	var order []string
	claim := validClaim()
	agent := &fakeAO{order: &order, session: ao.Session{Status: "terminated"}, stopSuccess: true}
	engine, control := makeEngine(t, claim, contracts.RunControl{DesiredControl: "run", ControlVersion: 1}, agent, permitIsolation{}, &order)
	if err := engine.Process(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	if agent.createSpec.ClientRequestID != claim.RunID || agent.createSpec.ProjectID != claim.AOProjectID || agent.createSpec.ApprovalMode != "auto" {
		t.Fatalf("unsafe AO spec: %+v", agent.createSpec)
	}
	start, create, bound := indexOf(order, "observe:dispatch_started"), indexOf(order, "create"), indexOf(order, "observe:dispatch_bound")
	if start < 0 || create <= start || bound <= create {
		t.Fatalf("AO side effect was not fenced by a durable dispatch observation: %v", order)
	}
	var kinds []string
	for _, observation := range control.observations {
		kinds = append(kinds, observation.Kind)
		if observation.AttemptID != claim.AttemptID || observation.Fence != claim.Fence {
			t.Fatalf("observation lost run fence: %+v", observation)
		}
	}
	if !reflect.DeepEqual(kinds, []string{"dispatch_started", "dispatch_bound", "runtime_terminated"}) {
		t.Fatalf("observation kinds = %v", kinds)
	}
	terminal := control.observations[len(control.observations)-1]
	if terminal.TerminationProof == nil || terminal.TerminationProof.Method != "native_process_exit_waited" || terminal.TerminationProof.SessionID != "ao-1" || !terminal.TerminationProof.CleanupComplete {
		t.Fatalf("terminal observation lacks native termination proof: %+v", terminal.TerminationProof)
	}
}

func TestProcessFailsClosedWithoutIsolationVerifier(t *testing.T) {
	claim := validClaim()
	agent := &fakeAO{}
	engine, control := makeEngine(t, claim, contracts.RunControl{DesiredControl: "run"}, agent, nil, nil)
	if err := engine.Process(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	if agent.createN != 0 {
		t.Fatal("AO runtime was created without execution isolation")
	}
	if len(control.observations) != 1 || control.observations[0].Kind != "no_effect_terminal" || control.observations[0].NoEffectProof == nil || control.observations[0].NoEffectProof.Outcome != "failed" || control.observations[0].Evidence[0].Verified {
		t.Fatalf("expected explicit no-effect terminal observation: %+v", control.observations)
	}
}

func TestRouteUnavailableFailsRunBeforeAOCreate(t *testing.T) {
	claim := validClaim()
	agent := &fakeAO{}
	engine, control := makeEngine(t, claim, contracts.RunControl{DesiredControl: "run"}, agent, permitIsolation{}, nil)
	engine.Routes = map[string]Route{}
	if err := engine.Process(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	if agent.createN != 0 {
		t.Fatal("AO runtime was created without the server-selected route")
	}
	if len(control.observations) != 1 || control.observations[0].Kind != "no_effect_terminal" || control.observations[0].NoEffectProof == nil || control.observations[0].NoEffectProof.Outcome != "failed" {
		t.Fatalf("route rejection did not report a no-effect terminal observation: %+v", control.observations)
	}
}

func TestServerSelectedHarnessMustMatchRunnerRoute(t *testing.T) {
	claim := validClaim()
	claim.Harness = "opencode"
	agent := &fakeAO{}
	engine, control := makeEngine(t, claim, contracts.RunControl{DesiredControl: "run"}, agent, permitIsolation{}, nil)
	if err := engine.Process(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	if agent.createN != 0 {
		t.Fatalf("runner dispatched %s claim through its %s route", claim.Harness, engine.Routes[claim.ProfileID].Harness)
	}
	if len(control.observations) != 1 || control.observations[0].Kind != "no_effect_terminal" {
		t.Fatalf("harness mismatch was not reported as no-effect: %+v", control.observations)
	}
}

func TestOpenCodeACPProofIsAcceptedAndNativeProofIsRejected(t *testing.T) {
	for _, tc := range []struct {
		name, method string
		wantTerminal bool
	}{
		{name: "ACP proof", method: "acp_process_exit_waited", wantTerminal: true},
		{name: "native proof", method: "native_process_exit_waited", wantTerminal: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claim := validClaim()
			claim.Harness = "opencode"
			claim.RuntimeRouteID = "opencode"
			claim.IsolationPolicy = OpenCodeWorkspaceWritePolicy
			agent := &fakeAO{session: ao.Session{Status: "terminated"}, stopSuccess: true, proofMethod: tc.method}
			engine, control := makeEngine(t, claim, contracts.RunControl{DesiredControl: "run"}, agent, permitIsolation{}, nil)
			route := engine.Routes[claim.ProfileID]
			route.Harness = "opencode"
			route.RuntimeRouteID = "opencode"
			route.IsolationPolicy = OpenCodeWorkspaceWritePolicy
			engine.Routes[claim.ProfileID] = route
			err := engine.Process(context.Background(), claim)
			if tc.wantTerminal {
				if err != nil {
					t.Fatal(err)
				}
				last := control.observations[len(control.observations)-1]
				if last.Kind != "runtime_terminated" || last.TerminationProof == nil || last.TerminationProof.Method != tc.method {
					t.Fatalf("ACP proof was not accepted: %+v", control.observations)
				}
			} else {
				if !errors.Is(err, ErrOutcomePending) {
					t.Fatalf("native proof unexpectedly verified OpenCode stop: %v", err)
				}
				for _, observation := range control.observations {
					if observation.Kind == "runtime_terminated" || observation.Kind == "stop_confirmed" {
						t.Fatalf("native proof falsely verified ACP shutdown: %+v", observation)
					}
				}
			}
		})
	}
}

func TestLegacyCodexJournalClaimRecoversWithExplicitServerHarness(t *testing.T) {
	ctx := context.Background()
	claim := validClaim() // Simulates a pre-harness server claim serialized in an old journal.
	claim.Harness = ""
	agent := &fakeAO{session: ao.Session{Status: "terminated"}, stopSuccess: true}
	engine, _ := makeEngine(t, claim, contracts.RunControl{DesiredControl: "run"}, agent, permitIsolation{}, nil)
	spec := ao.WorkerSpec{ClientRequestID: claim.RunID, ProjectID: claim.AOProjectID, Harness: "codex", Model: "gpt-6.1-sol", ApprovalMode: "auto", DisplayName: "worker", Prompt: "fixture"}
	if _, err := engine.Journal.SaveClaim(ctx, claim, spec, "claimed"); err != nil {
		t.Fatal(err)
	}
	claim.Harness = "codex"
	if err := engine.Process(ctx, claim); err != nil {
		t.Fatalf("legacy Codex journal did not recover: %v", err)
	}
	if agent.createN != 1 || agent.createSpec.Harness != "codex" {
		t.Fatalf("legacy Codex journal replay changed harness: %+v", agent.createSpec)
	}
}

func TestJournaledAOHarnessMustMatchCurrentClaim(t *testing.T) {
	ctx := context.Background()
	claim := validClaim()
	agent := &fakeAO{}
	engine, _ := makeEngine(t, claim, contracts.RunControl{DesiredControl: "run"}, agent, permitIsolation{}, nil)
	spec := ao.WorkerSpec{ClientRequestID: claim.RunID, ProjectID: claim.AOProjectID, Harness: "opencode", Model: "cylo/gpt-6-luna", ApprovalMode: "auto", DisplayName: "worker", Prompt: "fixture"}
	if _, err := engine.Journal.SaveClaim(ctx, claim, spec, "claimed"); err != nil {
		t.Fatal(err)
	}
	if err := engine.Process(ctx, claim); err == nil {
		t.Fatal("mismatched persisted AO harness was accepted")
	}
	if agent.createN != 0 {
		t.Fatal("AO create ran with a mismatched persisted harness")
	}
}

func TestRecoveredClaimSessionMustMatchJournalSession(t *testing.T) {
	ctx := context.Background()
	claim := validClaim()
	agent := &fakeAO{}
	engine, _ := makeEngine(t, claim, contracts.RunControl{DesiredControl: "run"}, agent, permitIsolation{}, nil)
	spec := ao.WorkerSpec{ClientRequestID: claim.RunID, ProjectID: claim.AOProjectID, Harness: "codex", Model: "gpt-6.1-sol", ApprovalMode: "auto", DisplayName: "worker", Prompt: "fixture"}
	if _, err := engine.Journal.SaveClaim(ctx, claim, spec, "claimed"); err != nil {
		t.Fatal(err)
	}
	if err := engine.Journal.SetSession(ctx, claim.RunID, claim.AttemptID, claim.Fence, "journal-session", "bound"); err != nil {
		t.Fatal(err)
	}
	claim.AOSessionID = "foreign-session"
	if err := engine.Process(ctx, claim); err == nil {
		t.Fatal("server claim with a foreign AO session matched the journal")
	}
	if agent.createN != 0 {
		t.Fatal("AO create ran after a foreign session claim")
	}
}

func TestNoEffectReceiptReplaysExactlyAfterLostAcknowledgement(t *testing.T) {
	claim := validClaim()
	agent := &fakeAO{}
	engine, control := makeEngine(t, claim, contracts.RunControl{DesiredControl: "run"}, agent, permitIsolation{}, nil)
	engine.Routes = map[string]Route{}
	control.loseFirstObservationAck = true

	if err := engine.Process(context.Background(), claim); err == nil {
		t.Fatal("expected simulated lost observation acknowledgement")
	}
	pending, err := engine.Journal.PendingObservations(context.Background(), claim.RunID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending no-effect receipts after lost acknowledgement = %d, err=%v", len(pending), err)
	}
	first := pending[0].Body
	if first.Kind != "no_effect_terminal" {
		t.Fatalf("queued receipt kind = %q", first.Kind)
	}

	if err := engine.Process(context.Background(), claim); err != nil {
		t.Fatalf("replay lost-acknowledgement receipt: %v", err)
	}
	if agent.createN != 0 {
		t.Fatal("runner created AO worker while replaying a terminal no-effect receipt")
	}
	if len(control.observations) != 2 || !reflect.DeepEqual(control.observations[0], first) || !reflect.DeepEqual(control.observations[1], first) {
		t.Fatalf("runner did not replay the exact queued receipt: observations=%+v queued=%+v", control.observations, first)
	}
	pending, err = engine.Journal.PendingObservations(context.Background(), claim.RunID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending no-effect receipts after replay acknowledgement = %d, err=%v", len(pending), err)
	}
}

func TestStopBeforeAOCreateReportsJournalBackedNoEffect(t *testing.T) {
	claim := validClaim()
	agent := &fakeAO{}
	engine, control := makeEngine(t, claim, contracts.RunControl{DesiredControl: "stop"}, agent, permitIsolation{}, nil)
	if err := engine.Process(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	if agent.createN != 0 || agent.stopN != 0 {
		t.Fatalf("pre-dispatch stop called AO create/stop: create=%d stop=%d", agent.createN, agent.stopN)
	}
	if len(control.observations) != 1 {
		t.Fatalf("pre-dispatch stop observations=%+v", control.observations)
	}
	observation := control.observations[0]
	if observation.Kind != "no_effect_terminal" || observation.NoEffectProof == nil || observation.NoEffectProof.Outcome != "stopped" || observation.NoEffectProof.JournalPhase != "claimed" || observation.NoEffectProof.AOCreateStarted || observation.NoEffectProof.AOSessionID != "" || observation.NoEffectProof.AOContainerID != "" {
		t.Fatalf("pre-dispatch stop lacks the exact journal-backed no-effect proof: %+v", observation)
	}
}

func TestUnknownAOCreateUsesOneAdapterRequestAndStaysFenced(t *testing.T) {
	claim := validClaim()
	agent := &fakeAO{createErr: fmtWrapUnknown()}
	engine, control := makeEngine(t, claim, contracts.RunControl{DesiredControl: "run"}, agent, permitIsolation{}, nil)
	if err := engine.Process(context.Background(), claim); err == nil {
		t.Fatal("expected an unknown create to remain unresolved")
	}
	if agent.createN != 1 {
		t.Fatalf("engine blindly repeated AO create %d times", agent.createN)
	}
	if len(control.observations) != 2 || control.observations[1].Kind != "effect_unknown" || control.observations[1].ExternalEffects[0].Status != "unknown" {
		t.Fatalf("unknown AO effect was not reported: %+v", control.observations)
	}
}

func TestUncertainKillIsNotRepeatedAcrossRunnerRestart(t *testing.T) {
	claim := validClaim()
	order := []string{}
	agent := &fakeAO{order: &order, session: ao.Session{Status: "working"}}
	engine, control := makeEngine(t, claim, contracts.RunControl{DesiredControl: "stop", ControlVersion: 2}, agent, permitIsolation{}, &order)
	control.controls = []string{"run", "stop"}
	if err := engine.Process(context.Background(), claim); err == nil {
		t.Fatal("expected unknown kill outcome")
	}
	entry, ok, err := engine.Journal.Entry(context.Background(), claim.RunID)
	if err != nil || !ok || entry.Session != "ao-1" {
		t.Fatalf("missing persisted AO session: %+v %v", entry, err)
	}
	claim.AOSessionID = entry.Session
	if err := engine.Process(context.Background(), claim); !errors.Is(err, ErrOutcomePending) {
		t.Fatalf("replaying the unresolved stop should remain fenced: %v", err)
	}
	if agent.stopN != 1 {
		t.Fatalf("AO kill retried %d times", agent.stopN)
	}
	if agent.getN < 2 {
		t.Fatalf("AO stop was not reconciled through GET: %d reads", agent.getN)
	}
}

func TestCleanupPendingKillRemainsFencedAcrossRestart(t *testing.T) {
	claim := validClaim()
	agent := &fakeAO{session: ao.Session{Status: "working"}, stopSuccess: true, stopPending: true}
	engine, control := makeEngine(t, claim, contracts.RunControl{DesiredControl: "stop"}, agent, permitIsolation{}, nil)
	control.controls = []string{"run", "stop"}
	if err := engine.Process(context.Background(), claim); !errors.Is(err, ErrOutcomePending) {
		t.Fatalf("expected cleanup pending to remain unresolved, got %v", err)
	}
	entry, ok, err := engine.Journal.Entry(context.Background(), claim.RunID)
	if err != nil || !ok || entry.Phase != "stop_uncertain" {
		t.Fatalf("cleanup pending phase=%q exists=%v err=%v", entry.Phase, ok, err)
	}
	claim.AOSessionID = entry.Session
	if err := engine.Process(context.Background(), claim); err == nil {
		t.Fatal("cleanup pending was incorrectly resolved by reading terminated status")
	}
	if agent.stopN != 1 {
		t.Fatalf("AO kill was repeated %d times", agent.stopN)
	}
	for _, observation := range control.observations {
		if observation.Kind == "runtime_terminated" || observation.Kind == "stop_confirmed" {
			t.Fatalf("reported completion without cleanup confirmation: %+v", observation)
		}
	}
}

func TestStopWithoutNativeTerminationProofRemainsFenced(t *testing.T) {
	claim := validClaim()
	agent := &fakeAO{session: ao.Session{Status: "terminated"}, stopSuccess: true, proofSession: "other-session"}
	// A mismatched session ID cannot prove termination of the bound AO worker.
	engine, control := makeEngine(t, claim, contracts.RunControl{DesiredControl: "stop"}, agent, permitIsolation{}, nil)
	control.controls = []string{"run", "stop"}
	if err := engine.Process(context.Background(), claim); !errors.Is(err, ErrOutcomePending) {
		t.Fatalf("expected malformed proof to remain fenced, got %v", err)
	}
	for _, observation := range control.observations {
		if observation.Kind == "runtime_terminated" || observation.Kind == "stop_confirmed" {
			t.Fatalf("accepted malformed native termination proof: %+v", observation)
		}
	}
}

func fmtWrapUnknown() error { return fmt.Errorf("%w: timeout", ao.ErrOutcomeUnknown) }

func indexOf(values []string, value string) int {
	for i, item := range values {
		if item == value {
			return i
		}
	}
	return -1
}
