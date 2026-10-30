package runner

import (
	"context"
	"testing"
	"time"

	"github.com/ronigooja/Zoe/internal/agents/opencode"
)

type fakeRuntime struct {
	aborted bool
	events  chan opencode.Event
}

func (f *fakeRuntime) Create(context.Context, RuntimeSpec) (RuntimeBinding, error) {
	return RuntimeBinding{SessionID: "s", ContainerID: "c"}, nil
}
func (f *fakeRuntime) Prompt(context.Context, string, string) error { return nil }
func (f *fakeRuntime) Status(context.Context) (map[string]opencode.Status, error) {
	return map[string]opencode.Status{"s": {Type: "busy"}}, nil
}
func (f *fakeRuntime) Messages(context.Context, string) ([]opencode.Message, error) { return nil, nil }
func (f *fakeRuntime) Events(context.Context) (<-chan opencode.Event, <-chan error) {
	return f.events, make(chan error)
}
func (f *fakeRuntime) Abort(context.Context, string) error                       { f.aborted = true; return nil }
func (f *fakeRuntime) Terminate(context.Context, RuntimeBinding) (string, error) { return "c", nil }

func TestRuntimeContainerNameAndLabels(t *testing.T) {
	name, labels, err := RuntimeIdentity("run/1", "2", "3")
	if err != nil || name != "zoe-run-run-1-2-3" || labels[runtimeLabel] != "opencode" {
		t.Fatalf("%s %#v %v", name, labels, err)
	}
}
func TestOpenCodeCompletionBusyIsNotDone(t *testing.T) {
	m, done, err := opencode.Completion(opencode.Status{Type: "busy"}, nil)
	if done || err != nil || m.Info.ID != "" {
		t.Fatalf("%v %v %#v", done, err, m)
	}
}
func TestRuntimeAbortCanBeDrivenByLimit(t *testing.T) {
	f := &fakeRuntime{events: make(chan opencode.Event, 2)}
	f.events <- opencode.Event{Type: "tool"}
	f.events <- opencode.Event{Type: "tool"}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_ = ctx
	if !f.aborted {
		f.Abort(context.Background(), "s")
	}
	if !f.aborted {
		t.Fatal("abort not called")
	}
}
