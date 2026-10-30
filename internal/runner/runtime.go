package runner

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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

type RuntimeBinding struct{ SessionID, ContainerID, Password string }

type Runtime interface {
	Create(context.Context, RuntimeSpec) (RuntimeBinding, error)
	Prompt(context.Context, RuntimeBinding, string) error
	Status(context.Context, RuntimeBinding) (map[string]opencode.Status, error)
	Messages(context.Context, RuntimeBinding) ([]opencode.Message, error)
	Events(context.Context, RuntimeBinding) (<-chan opencode.Event, <-chan error)
	Abort(context.Context, RuntimeBinding) error
	Terminate(context.Context, RuntimeBinding) (string, error)
}

type OpenCodeRuntime struct {
	BaseURL    string
	Username   string
	Client     *opencode.Client
	Docker     DockerCommand
	Supervisor DockerRuntimeSupervisor
	Container  RuntimeContainerSpec
}

func (r OpenCodeRuntime) Create(ctx context.Context, spec RuntimeSpec) (RuntimeBinding, error) {
	cfg := r.Container
	cfg.RunID, cfg.Attempt, cfg.Fence = spec.RunID, spec.AttemptID, spec.Fence
	if spec.Image != "" {
		cfg.Image = spec.Image
	}
	if cfg.WorkspaceRoot != "" {
		cfg.Workspace = filepath.Join(cfg.WorkspaceRoot, sanitizeRuntimePart(spec.RunID))
		if err := os.MkdirAll(cfg.Workspace, 0o700); err != nil {
			return RuntimeBinding{}, err
		}
	} else {
		cfg.Workspace = spec.Workspace
	}
	cfg.Model = spec.Model
	container, err := r.Supervisor.Create(ctx, cfg)
	if err != nil {
		return RuntimeBinding{}, err
	}
	client, err := opencode.New(opencode.Config{BaseURL: r.BaseURL, Username: r.Username, Password: container.Password, Client: r.clientHTTP(container.ID)})
	if err != nil {
		return RuntimeBinding{ContainerID: container.ID, Password: container.Password}, err
	}
	session, err := client.CreateSession(ctx, spec.RunID)
	if err != nil {
		return RuntimeBinding{ContainerID: container.ID, Password: container.Password}, err
	}
	return RuntimeBinding{SessionID: session.ID, ContainerID: container.ID, Password: container.Password}, nil
}
func (r OpenCodeRuntime) client(binding RuntimeBinding) (*opencode.Client, error) {
	return opencode.New(opencode.Config{BaseURL: r.BaseURL, Username: r.Username, Password: binding.Password, Client: r.clientHTTP(binding.ContainerID)})
}
func (r OpenCodeRuntime) clientHTTP(containerID string) *http.Client {
	return &http.Client{Timeout: 20 * time.Second, Transport: DockerExecTransport{Docker: r.Docker, ContainerID: containerID}}
}
func (r OpenCodeRuntime) Prompt(ctx context.Context, b RuntimeBinding, prompt string) error {
	c, err := r.client(b)
	if err != nil {
		return err
	}
	return c.PromptAsync(ctx, b.SessionID, map[string]any{"model": r.Container.Model, "parts": []map[string]string{{"type": "text", "text": prompt}}})
}
func (r OpenCodeRuntime) Status(ctx context.Context, b RuntimeBinding) (map[string]opencode.Status, error) {
	c, err := r.client(b)
	if err != nil {
		return nil, err
	}
	return c.Status(ctx)
}
func (r OpenCodeRuntime) Messages(ctx context.Context, b RuntimeBinding) ([]opencode.Message, error) {
	c, err := r.client(b)
	if err != nil {
		return nil, err
	}
	return c.Messages(ctx, b.SessionID)
}
func (r OpenCodeRuntime) Events(ctx context.Context, b RuntimeBinding) (<-chan opencode.Event, <-chan error) {
	c, err := r.client(b)
	if err != nil {
		e := make(chan error, 1)
		e <- err
		close(e)
		return nil, e
	}
	return c.Events(ctx)
}
func (r OpenCodeRuntime) Abort(ctx context.Context, b RuntimeBinding) error {
	c, err := r.client(b)
	if err != nil {
		return err
	}
	return c.Abort(ctx, b.SessionID)
}
func (r OpenCodeRuntime) Terminate(ctx context.Context, b RuntimeBinding) (string, error) {
	if b.SessionID != "" {
		_ = r.Abort(ctx, b)
	}
	if b.ContainerID == "" {
		return "", errors.New("runtime container identity is required")
	}
	if err := r.Supervisor.RemoveExact(ctx, b.ContainerID); err != nil {
		return "", err
	}
	return b.ContainerID, nil
}

func waitRuntime(ctx context.Context, r Runtime, binding RuntimeBinding, interval time.Duration, onStep func() error) (opencode.Message, error) {
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
		statuses, err := r.Status(ctx, binding)
		if err != nil {
			continue
		}
		messages, err := r.Messages(ctx, binding)
		if err != nil {
			continue
		}
		m, done, terminalErr := opencode.Completion(statuses[binding.SessionID], messages)
		if done {
			return m, terminalErr
		}
	}
}

func runtimeError(code string, err error) error { return fmt.Errorf("runtime %s: %w", code, err) }
