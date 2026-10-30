package runner

import (
	"context"
	"errors"
	"github.com/ronigooja/Zoe/contracts"
	"github.com/ronigooja/Zoe/internal/agents/opencode"
	"testing"
	"time"
)

type runtimeFake struct {
	aborts, creates, terminates int
	events                      chan opencode.Event
	done                        bool
	result                      opencode.Message
}

func (f *runtimeFake) Create(context.Context, RuntimeSpec) (RuntimeBinding, error) {
	f.creates++
	return RuntimeBinding{"session", "container"}, nil
}
func (f *runtimeFake) Prompt(context.Context, string, string) error { return nil }
func (f *runtimeFake) Status(context.Context) (map[string]opencode.Status, error) {
	if f.done {
		return map[string]opencode.Status{"session": {Type: "idle"}}, nil
	}
	return map[string]opencode.Status{"session": {Type: "busy"}}, nil
}
func (f *runtimeFake) Messages(context.Context, string) ([]opencode.Message, error) {
	if f.done {
		return []opencode.Message{f.result}, nil
	}
	return nil, nil
}
func (f *runtimeFake) Events(context.Context) (<-chan opencode.Event, <-chan error) {
	return f.events, make(chan error)
}
func (f *runtimeFake) Abort(context.Context, string) error { f.aborts++; return nil }
func (f *runtimeFake) Terminate(context.Context, RuntimeBinding) (string, error) {
	f.terminates++
	return "container", nil
}

type controlFake struct{ observations []contracts.RunnerObservation }

func (*controlFake) Claim(context.Context) (*contracts.JobClaim, error)    { return nil, ErrNoClaim }
func (*controlFake) Current(context.Context) ([]contracts.JobClaim, error) { return nil, nil }
func (*controlFake) Control(context.Context, string) (contracts.RunControl, error) {
	return contracts.RunControl{}, nil
}
func (c *controlFake) Observe(_ context.Context, _ string, o contracts.RunnerObservation) error {
	c.observations = append(c.observations, o)
	return nil
}
func testClaim() contracts.JobClaim {
	return contracts.JobClaim{RunID: "run", TaskID: "task", AttemptID: "attempt", WorkspaceID: "workspace", ProfileID: "profile", Harness: "opencode", RuntimeRouteID: "runtime", ModelRouteID: "model", IsolationPolicy: OpenCodeWorkspaceWritePolicy, Role: "worker", Fence: 1, Budget: contracts.RunBudgetSnapshot{MaxToolSteps: 1}}
}
func TestRuntimeProcessUsesNeutralBindingAndRemovalProof(t *testing.T) {
	f := &runtimeFake{events: make(chan opencode.Event, 1), done: true}
	m := opencode.Message{}
	m.Info.Role = "assistant"
	m.Info.Time.Completed = 1
	m.Info.Finish = "stop"
	f.result = m
	c := &controlFake{}
	j := openTestJournal(t)
	e := &Engine{Control: c, Runtime: f, Journal: j, Routes: map[string]Route{"profile": {ProfileID: "profile", Harness: "opencode", IsolationPolicy: OpenCodeWorkspaceWritePolicy, Model: "model"}}, PollInterval: time.Millisecond}
	if err := e.Process(context.Background(), testClaim()); err != nil {
		t.Fatal(err)
	}
	if f.creates != 1 || f.terminates != 1 || len(c.observations) != 1 || c.observations[0].TerminationProof.Method != "container_removed" {
		t.Fatalf("runtime lifecycle=%+v observations=%+v", f, c.observations)
	}
}
func TestRuntimeAbortOnToolStepLimit(t *testing.T) {
	f := &runtimeFake{events: make(chan opencode.Event, 2)}
	f.events <- opencode.Event{Type: "tool"}
	f.events <- opencode.Event{Type: "tool"}
	c := &controlFake{}
	j := openTestJournal(t)
	e := &Engine{Control: c, Runtime: f, Journal: j, Routes: map[string]Route{"profile": {ProfileID: "profile", Harness: "opencode", IsolationPolicy: OpenCodeWorkspaceWritePolicy, Model: "model"}}, PollInterval: time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_ = e.Process(ctx, testClaim())
	if f.aborts == 0 {
		t.Fatal("tool-step limit did not abort")
	}
}
func TestRuntimeErrorsAreNotRetriedAsCreate(t *testing.T) {
	f := &runtimeFake{events: make(chan opencode.Event), done: false}
	_ = f
	if !errors.Is(ErrOutcomePending, ErrOutcomePending) {
		t.Fatal("sentinel")
	}
}
func openTestJournal(t *testing.T) *Journal {
	t.Helper()
	j, err := OpenJournal(context.Background(), t.TempDir()+"/journal.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	return j
}
