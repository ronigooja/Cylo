// Package ops provides local deployment inspection and service lifecycle helpers
// for the cylo operator CLI. It never reads or prints secret contents.
package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Mode string

const (
	ModeSystemd Mode = "systemd"
	ModeCompose Mode = "compose"
	ModeUnknown Mode = "unknown"
)

type CommandRunner func(context.Context, string, ...string) ([]byte, error)

func defaultRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type Manager struct {
	Run CommandRunner
}

func NewManager(run CommandRunner) Manager {
	if run == nil {
		run = defaultRunner
	}
	return Manager{Run: run}
}

type ServiceStatus struct{ Name, State, Source string }
type SecretStatus struct {
	Path   string
	Exists bool
	Mode   string
	Owner  string
}
type ProviderStatus struct{ BaseURL, Model, ConfigSource, LastCheck string }
type Status struct {
	Mode     Mode
	Services []ServiceStatus
	Provider ProviderStatus
	Secret   SecretStatus
}

type InspectConfig struct {
	Mode                 Mode
	Services             []string
	ComposeProject       string
	ComposeFile          string
	SecretPath           string
	ProviderBaseURL      string
	ProviderModel        string
	ProviderConfigSource string
	ProviderLastCheck    string
}

func DetectMode() Mode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CYLO_DEPLOYMENT_MODE"))) {
	case "systemd":
		return ModeSystemd
	case "compose":
		return ModeCompose
	}
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		return ModeSystemd
	}
	return ModeCompose
}

func (m Manager) Inspect(ctx context.Context, cfg InspectConfig) Status {
	mode := cfg.Mode
	if mode == ModeUnknown || mode == "" {
		mode = DetectMode()
	}
	services := cfg.Services
	if len(services) == 0 {
		services = []string{"cylo-compose.service", "cylo-ao.service", "cylo-runner.service"}
	}
	result := Status{Mode: mode, Provider: ProviderStatus{BaseURL: redactURL(cfg.ProviderBaseURL), Model: cfg.ProviderModel, ConfigSource: cfg.ProviderConfigSource, LastCheck: cfg.ProviderLastCheck}}
	for _, name := range services {
		result.Services = append(result.Services, m.service(ctx, mode, name, cfg))
	}
	result.Secret = inspectSecret(cfg.SecretPath)
	return result
}

func (m Manager) service(ctx context.Context, mode Mode, name string, cfg InspectConfig) ServiceStatus {
	s := ServiceStatus{Name: name, Source: string(mode), State: "unknown"}
	var out []byte
	var err error
	if mode == ModeCompose {
		args := []string{"compose"}
		if cfg.ComposeProject != "" {
			args = append(args, "--project-name", cfg.ComposeProject)
		}
		if cfg.ComposeFile != "" {
			args = append(args, "--file", cfg.ComposeFile)
		}
		args = append(args, "ps", "--status", "running")
		out, err = m.Run(ctx, "docker", args...)
	} else {
		out, err = m.Run(ctx, "systemctl", "is-active", name)
	}
	if err == nil && strings.TrimSpace(string(out)) != "" {
		s.State = strings.TrimSpace(strings.Split(string(out), "\n")[0])
	}
	if err != nil && strings.TrimSpace(string(out)) != "" {
		s.State = strings.TrimSpace(strings.Split(string(out), "\n")[0])
	}
	return s
}

func inspectSecret(path string) SecretStatus {
	s := SecretStatus{Path: path}
	if path == "" {
		return s
	}
	info, err := os.Stat(path)
	if err != nil {
		return s
	}
	s.Exists = info.Mode().IsRegular()
	s.Mode = fmt.Sprintf("%04o", info.Mode().Perm())
	s.Owner = "current-user" // owner identity is intentionally not resolved into status output
	return s
}

func (m Manager) Reload(ctx context.Context, mode Mode, services []string, composeProject, composeFile string) error {
	if mode == ModeUnknown || mode == "" {
		mode = DetectMode()
	}
	if len(services) == 0 {
		services = []string{"cylo-ao.service", "cylo-runner.service"}
	}
	switch mode {
	case ModeSystemd:
		if _, err := m.Run(ctx, "systemctl", "daemon-reload"); err != nil {
			return commandError("systemctl daemon-reload", err)
		}
		args := append([]string{"reload-or-restart"}, services...)
		if _, err := m.Run(ctx, "systemctl", args...); err != nil {
			return commandError("systemctl reload-or-restart", err)
		}
		return nil
	case ModeCompose:
		args := []string{"compose"}
		if composeProject != "" {
			args = append(args, "--project-name", composeProject)
		}
		if composeFile != "" {
			args = append(args, "--file", composeFile)
		}
		args = append(args, "up", "-d", "--no-build")
		if _, err := m.Run(ctx, "docker", args...); err != nil {
			return commandError("docker compose up", err)
		}
		return nil
	default:
		return errors.New("unknown deployment mode")
	}
}

func commandError(action string, err error) error { return fmt.Errorf("%s failed: %w", action, err) }

func redactURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Keep the operator-visible endpoint while removing credentials/query/fragment.
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	if i := strings.Index(raw, "@"); i >= 0 {
		raw = raw[i+1:]
	}
	return raw
}

func DefaultSecretPath(root string) string {
	if strings.TrimSpace(root) == "" {
		return "/etc/cylo/runner-secrets/cliproxyapi-api-key"
	}
	return filepath.Join(root, "runner-secrets", "cliproxyapi-api-key")
}
