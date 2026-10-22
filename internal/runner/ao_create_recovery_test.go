package runner

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
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

func TestUnknownAOCreateRecoversOriginalSessionAfterJournalRestart(t *testing.T) {
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
	originalSpec := persisted.Spec
	if err := firstJournal.Close(); err != nil {
		t.Fatal(err)
	}

	secondJournal, err := OpenJournal(ctx, journalPath)
	if err != nil {
		t.Fatalf("reopen runner journal: %v", err)
	}
	t.Cleanup(func() { _ = secondJournal.Close() })
	// AO's idempotent endpoint now has the original committed session available
	// under the same request key. The runner must recover it without changing
	// the persisted request, then require the normal termination proof.
	agent.base.session = ao.Session{ID: "ao-original-session", Status: "terminated"}
	agent.base.stopSuccess = true
	secondControl := &fakeControl{control: contracts.RunControl{DesiredControl: "run", ControlVersion: 1}}
	engine.Journal = secondJournal
	engine.Control = secondControl

	if err := engine.Process(ctx, claim); err != nil {
		t.Fatalf("recovery process: %v", err)
	}
	if len(agent.createSpec) != 2 || !reflect.DeepEqual(agent.createSpec[1], originalSpec) {
		t.Fatalf("recovery changed or skipped the original AO request: creates=%+v want second=%+v", agent.createSpec, originalSpec)
	}
	final, ok, err := secondJournal.Entry(ctx, claim.RunID)
	if err != nil || !ok {
		t.Fatalf("read final journal entry: ok=%v err=%v", ok, err)
	}
	if final.Session != "ao-original-session" || final.Phase != "terminated" {
		t.Fatalf("recovered session was not proof-terminated: session=%q phase=%q", final.Session, final.Phase)
	}
	if len(secondControl.observations) != 2 || secondControl.observations[1].Kind != "runtime_terminated" || secondControl.observations[1].TerminationProof == nil {
		t.Fatalf("recovery did not report proof-backed termination: %+v", secondControl.observations)
	}
}
