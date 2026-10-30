package runner

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ronigooja/Zoe/internal/agents/opencode"
)

type RuntimeSpec struct {
	RunID, AttemptID, Fence  string
	Model, Prompt, Workspace string
	Image, ProviderSocket    string
	// Legacy request fields are retained only for decoding old runner journals.
	ClientRequestID, ProjectID, Harness, DisplayName, ApprovalMode, Effort string
}

type RuntimeBinding struct{ SessionID, ContainerID string }

type Runtime interface {
	Create(context.Context, RuntimeSpec) (RuntimeBinding, error)
	Prompt(context.Context, string, string) error
	Status(context.Context) (map[string]opencode.Status, error)
	Messages(context.Context, string) ([]opencode.Message, error)
	Events(context.Context) (<-chan opencode.Event, <-chan error)
	Abort(context.Context, string) error
	Terminate(context.Context, RuntimeBinding) (string, error)
}

type OpenCodeRuntime struct {
	Client     *opencode.Client
	Supervisor DockerRuntimeSupervisor
	Container  RuntimeContainerSpec
}

func (r OpenCodeRuntime) Create(ctx context.Context, spec RuntimeSpec) (RuntimeBinding, error) {
	cfg := r.Container
	cfg.RunID, cfg.Attempt, cfg.Fence, cfg.Image = spec.RunID, spec.AttemptID, spec.Fence, spec.Image
	if cfg.WorkspaceRoot != "" {
		cfg.Workspace = cfg.WorkspaceRoot
	} else {
		cfg.Workspace = spec.Workspace
	}
	cfg.Model = spec.Model
	container, err := r.Supervisor.Create(ctx, cfg)
	if err != nil {
		return RuntimeBinding{}, err
	}
	session, err := r.Client.CreateSession(ctx, spec.RunID)
	if err != nil {
		return RuntimeBinding{ContainerID: container.ID}, err
	}
	return RuntimeBinding{SessionID: session.ID, ContainerID: container.ID}, nil
}
func (r OpenCodeRuntime) Prompt(ctx context.Context, id, prompt string) error {
	return r.Client.PromptAsync(ctx, id, map[string]any{"model": r.Container.Model, "parts": []map[string]string{{"type": "text", "text": prompt}}})
}
func (r OpenCodeRuntime) Status(ctx context.Context) (map[string]opencode.Status, error) {
	return r.Client.Status(ctx)
}
func (r OpenCodeRuntime) Messages(ctx context.Context, id string) ([]opencode.Message, error) {
	return r.Client.Messages(ctx, id)
}
func (r OpenCodeRuntime) Events(ctx context.Context) (<-chan opencode.Event, <-chan error) {
	return r.Client.Events(ctx)
}
func (r OpenCodeRuntime) Abort(ctx context.Context, id string) error { return r.Client.Abort(ctx, id) }
func (r OpenCodeRuntime) Terminate(ctx context.Context, b RuntimeBinding) (string, error) {
	if b.SessionID != "" {
		_ = r.Abort(ctx, b.SessionID)
	}
	if b.ContainerID == "" {
		return "", errors.New("runtime container identity is required")
	}
	if err := r.Supervisor.RemoveExact(ctx, b.ContainerID); err != nil {
		return "", err
	}
	return b.ContainerID, nil
}

func waitRuntime(ctx context.Context, r Runtime, session string, interval time.Duration, onStep func() error) (opencode.Message, error) {
	if interval <= 0 {
		interval = time.Second
	}
	for {
		select {
		case <-ctx.Done():
			return opencode.Message{}, ctx.Err()
		case <-time.After(interval):
		}
		if onStep != nil {
			if err := onStep(); err != nil {
				return opencode.Message{}, err
			}
		}
		statuses, err := r.Status(ctx)
		if err != nil {
			continue
		}
		messages, err := r.Messages(ctx, session)
		if err != nil {
			continue
		}
		m, done, terminalErr := opencode.Completion(statuses[session], messages)
		if done {
			return m, terminalErr
		}
	}
}

func runtimeError(code string, err error) error { return fmt.Errorf("runtime %s: %w", code, err) }
