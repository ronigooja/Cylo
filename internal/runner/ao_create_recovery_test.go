package runner

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/agents/ao"
)

type recoveringAO struct {
	base       *fakeAO
	createSpec []ao.WorkerSpec
}

func (a *recoveringAO) CreateWorker(_ context.Context, spec ao.WorkerSpec) (ao.Session, error) {
	a.createSpec = append(a.createSpec, spec)
	if len(a.createSpec) == 1 {
		return ao.Session{}, fmtWrapUnknown()
	}
	return ao.Session{ID: "ao-original-session", Status: "terminated"}, nil
}

func (a *recoveringAO) GetSession(ctx context.Context, id string) (ao.Session, error) {
	return a.base.GetSession(ctx, id)
}

func (a *recoveringAO) GetConversation(ctx context.Context, id string) (ao.Conversation, error) {
	return a.base.GetConversation(ctx, id)
}

func (a *recoveringAO) StopWorker(ctx context.Context, id string) (ao.Session, ao.KillResponse, error) {
	return a.base.StopWorker(ctx, id)
}

func TestUnknownAOCreateRemainsFencedAfterJournalRestartWithoutLookup(t *testing.T) {
	ctx := context.Background()
	claim := validClaim()
	journalPath := filepath.Join(t.TempDir(), "runner.db")
	firstJournal, err := OpenJournal(ctx, journalPath)
	if err != nil {
		t.Fatal(err)
	}
	firstControl := &fakeControl{control: contracts.RunControl{DesiredControl: "run", ControlVersion: 1}}
	agent := &recoveringAO{base: &fakeAO{}}
	engine := &Engine{
		Control: firstControl,
		AO:      agent,
		Journal: firstJournal,
		Routes: map[string]Route{
			"worker-safe": {ProfileID: "worker-safe", RuntimeRouteID: "codex", ModelRouteID: "sol", IsolationPolicy: CodexWorkspaceWritePolicy, Harness: "codex", Model: "gpt-6.1-sol", ApprovalMode: "accept-edits"},
		},
		Isolation:    permitIsolation{},
		PollInterval: time.Millisecond,
	}

	if err := engine.Process(ctx, claim); !errors.Is(err, ErrOutcomePending) {
		t.Fatalf("first create error = %v, want unresolved external outcome", err)
	}
	if len(agent.createSpec) != 1 {
		t.Fatalf("first process made %d AO create calls, want one adapter call", len(agent.createSpec))
	}
	persisted, ok, err := firstJournal.Entry(ctx, claim.RunID)
	if err != nil || !ok {
		t.Fatalf("read unknown create journal entry: ok=%v err=%v", ok, err)
	}
	if persisted.Phase != "effect_unknown" || persisted.Session != "" {
		t.Fatalf("unknown create journal state = phase %q session %q", persisted.Phase, persisted.Session)
	}
	if persisted.Spec.ClientRequestID != claim.RunID || persisted.Spec.ProjectID != claim.AOProjectID || persisted.Spec.Harness != "codex" {
		t.Fatalf("journal did not retain original AO request identity: %+v", persisted.Spec)
	}
	if err := firstJournal.Close(); err != nil {
		t.Fatal(err)
	}

	secondJournal, err := OpenJournal(ctx, journalPath)
	if err != nil {
		t.Fatalf("reopen runner journal: %v", err)
	}
	t.Cleanup(func() { _ = secondJournal.Close() })
	// AO exposes no read-only lookup by clientRequestId. Repeating its create
	// endpoint could launch a worker if the first POST never reached AO, so the
	// restart must remain uncertain and make no second POST.
	agent.base.session = ao.Session{ID: "ao-original-session", Status: "terminated"}
	agent.base.stopSuccess = true
	secondControl := &fakeControl{control: contracts.RunControl{DesiredControl: "run", ControlVersion: 1}}
	engine.Journal = secondJournal
	engine.Control = secondControl

	if err := engine.Process(ctx, claim); !errors.Is(err, ErrOutcomePending) {
		t.Fatalf("recovery process error=%v, want unresolved external outcome", err)
	}
	if len(agent.createSpec) != 1 {
		t.Fatalf("runner replayed an uncertain AO create after restart: calls=%d", len(agent.createSpec))
	}
	final, ok, err := secondJournal.Entry(ctx, claim.RunID)
	if err != nil || !ok {
		t.Fatalf("read final journal entry: ok=%v err=%v", ok, err)
	}
	if final.Session != "" || final.Phase != "effect_unknown" {
		t.Fatalf("unknown create was incorrectly resolved: session=%q phase=%q", final.Session, final.Phase)
	}
	if len(secondControl.observations) != 1 || secondControl.observations[0].Kind != "effect_unknown" || secondControl.observations[0].NoEffectProof != nil {
		t.Fatalf("recovery did not preserve uncertain external outcome: %+v", secondControl.observations)
	}
}

func TestDefiniteCreateRejectionReplaysJournalReceiptWithoutCallingAO(t *testing.T) {
	ctx := context.Background()
	claim := validClaim()
	path := filepath.Join(t.TempDir(), "runner.db")
	firstJournal, err := OpenJournal(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer firstJournal.Close()
	agent := &fakeAO{createErr: ao.CreateRejectedError{Status: 400, Code: "INVALID_REQUEST"}}
	firstControl := &fakeControl{control: contracts.RunControl{DesiredControl: "run", ControlVersion: 1}, loseObservationNumber: 2}
	engine := &Engine{Control: firstControl, AO: agent, Journal: firstJournal, Routes: map[string]Route{"worker-safe": {ProfileID: "worker-safe", RuntimeRouteID: "codex", ModelRouteID: "sol", IsolationPolicy: CodexWorkspaceWritePolicy, Harness: "codex", Model: "gpt-6.1-sol", ApprovalMode: "accept-edits"}}, Isolation: permitIsolation{}}
	if err := engine.Process(ctx, claim); err == nil {
		t.Fatal("expected simulated lost receipt acknowledgement")
	}
	if agent.createN != 1 || len(firstControl.observations) != 2 || firstControl.observations[1].Kind != "create_rejected" {
		t.Fatalf("create calls=%d observations=%+v", agent.createN, firstControl.observations)
	}
	if err := firstJournal.Close(); err != nil {
		t.Fatal(err)
	}
	secondJournal, err := OpenJournal(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer secondJournal.Close()
	engine.Journal = secondJournal
	secondControl := &fakeControl{control: contracts.RunControl{DesiredControl: "run", ControlVersion: 1}}
	engine.Control = secondControl
	if err := engine.Process(ctx, claim); err != nil {
		t.Fatal(err)
	}
	if agent.createN != 1 || len(secondControl.observations) != 1 || secondControl.observations[0].Kind != "create_rejected" {
		t.Fatalf("replayed AO create after restart: calls=%d observations=%+v", agent.createN, secondControl.observations)
	}
}

func TestCreateRejectionReceiptQueuedBeforePhaseWritePreventsReplay(t *testing.T) {
	ctx := context.Background()
	claim := validClaim()
	journal, err := OpenJournal(ctx, filepath.Join(t.TempDir(), "runner.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	spec := ao.WorkerSpec{ClientRequestID: claim.RunID, ProjectID: claim.AOProjectID, Harness: "codex", DisplayName: "test", Prompt: "fixture"}
	if _, err := journal.SaveClaim(ctx, claim, spec, "create_pending"); err != nil {
		t.Fatal(err)
	}
	proof := &contracts.CreateRejectionProof{HTTPStatus: 400, ErrorCode: "INVALID_REQUEST"}
	receipt := contracts.RunnerObservation{ObservationID: "durable-create-rejection", RunID: claim.RunID, AttemptID: claim.AttemptID, Fence: claim.Fence, Kind: "create_rejected", OccurredAt: time.Now().UTC(), AOProjectID: claim.AOProjectID, CreateRejection: proof, Evidence: []contracts.Evidence{}}
	if err := journal.QueueObservation(ctx, claim.RunID, receipt); err != nil {
		t.Fatal(err)
	}
	agent := &fakeAO{}
	control := &fakeControl{control: contracts.RunControl{DesiredControl: "run"}}
	engine := &Engine{Control: control, AO: agent, Journal: journal, Isolation: permitIsolation{}}
	if err := engine.Process(ctx, claim); err != nil {
		t.Fatal(err)
	}
	if agent.createN != 0 || len(control.observations) != 1 || control.observations[0].Kind != "create_rejected" {
		t.Fatalf("create=%d observations=%+v", agent.createN, control.observations)
	}
}
