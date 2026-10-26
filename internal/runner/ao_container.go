package runner

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/ronigooja/Cylo/contracts"
)

const (
	aoContainerManagedLabel    = "io.cylo.managed"
	aoContainerRuntimeLabel    = "io.cylo.runtime"
	aoContainerSessionLabel    = "io.cylo.ao.session"
	aoContainerCodexRuntime    = "ao-codex-chat"
	aoContainerOpenCodeRuntime = "ao-opencode-acp"
	aoContainerRuntimeValue    = aoContainerCodexRuntime
)

type AOContainerSupervisor interface {
	Find(context.Context, string, string) (containerID string, found bool, err error)
	StopAndVerify(context.Context, string, string, string) (contracts.CapabilityTerminationProof, error)
}

// DockerAOContainerSupervisor owns the narrow cleanup boundary for AO's
// per-session Codex container. It discovers only containers with the fixed AO
// runtime labels and removes only the exact ID durably recorded by the runner.
type DockerAOContainerSupervisor struct {
	Docker DockerCommand
}

type aoContainerInspect struct {
	ID           string     `json:"Id"`
	Created      *time.Time `json:"Created"`
	RestartCount *int       `json:"RestartCount"`
	Config       struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	HostConfig struct {
		NetworkMode   string `json:"NetworkMode"`
		RestartPolicy struct {
			Name *string `json:"Name"`
		} `json:"RestartPolicy"`
	} `json:"HostConfig"`
	State struct {
		Status     *string    `json:"Status"`
		Running    *bool      `json:"Running"`
		Paused     *bool      `json:"Paused"`
		Restarting *bool      `json:"Restarting"`
		OOMKilled  *bool      `json:"OOMKilled"`
		Dead       *bool      `json:"Dead"`
		Pid        *int       `json:"Pid"`
		ExitCode   *int       `json:"ExitCode"`
		Error      *string    `json:"Error"`
		StartedAt  *time.Time `json:"StartedAt"`
		FinishedAt *time.Time `json:"FinishedAt"`
	} `json:"State"`
}

func (s DockerAOContainerSupervisor) Find(ctx context.Context, harness, sessionID string) (string, bool, error) {
	runtimeValue, ok := aoContainerRuntime(harness)
	if s.Docker == nil || !ok || !validAOContainerSessionID(sessionID) {
		return "", false, errors.New("AO container supervisor is not configured")
	}
	output, err := s.Docker.Run(ctx, "ps", "-aq", "--no-trunc", "--filter", "label="+aoContainerManagedLabel+"=true", "--filter", "label="+aoContainerRuntimeLabel+"="+runtimeValue, "--filter", "label="+aoContainerSessionLabel+"="+sessionID)
	if err != nil {
		return "", false, errors.New("AO container inventory is unavailable")
	}
	ids := splitContainerIDs(string(output))
	if len(ids) == 0 {
		return "", false, nil
	}
	ids, err = s.runtimeContainerIDs(ctx, ids, harness, sessionID)
	if err != nil {
		return "", false, err
	}
	if len(ids) == 0 {
		return "", false, nil
	}
	if len(ids) != 1 {
		return "", false, errors.New("AO session has an ambiguous supervised container set")
	}
	inspection, err := s.inspect(ctx, ids[0], harness, sessionID)
	if err != nil {
		return "", false, err
	}
	if inspection.State.Running == nil || !*inspection.State.Running || inspection.State.Pid == nil || *inspection.State.Pid < 1 || inspection.HostConfig.NetworkMode != "none" {
		return "", false, errors.New("AO session container is not running with its network boundary")
	}
	return inspection.ID, true, nil
}

func (s DockerAOContainerSupervisor) StopAndVerify(ctx context.Context, harness, sessionID, expectedID string) (contracts.CapabilityTerminationProof, error) {
	runtimeValue, ok := aoContainerRuntime(harness)
	if s.Docker == nil || !ok || !validAOContainerSessionID(sessionID) {
		return contracts.CapabilityTerminationProof{}, errors.New("AO container supervisor is not configured")
	}
	idsOutput, err := s.Docker.Run(ctx, "ps", "-aq", "--no-trunc", "--filter", "label="+aoContainerManagedLabel+"=true", "--filter", "label="+aoContainerRuntimeLabel+"="+runtimeValue, "--filter", "label="+aoContainerSessionLabel+"="+sessionID)
	if err != nil {
		return contracts.CapabilityTerminationProof{}, errors.New("AO container inventory is unavailable during termination")
	}
	ids := splitContainerIDs(string(idsOutput))
	ids, err = s.runtimeContainerIDs(ctx, ids, harness, sessionID)
	if err != nil {
		return contracts.CapabilityTerminationProof{}, err
	}
	if expectedID == "" && len(ids) == 0 {
		return contracts.CapabilityTerminationProof{EffectRef: "docker:ao-session:" + sessionID, TerminatedAt: time.Now().UTC(), CleanupComplete: true}, nil
	}
	if expectedID == "" || len(ids) > 1 {
		return contracts.CapabilityTerminationProof{}, errors.New("AO container termination identity is unresolved")
	}
	inspection, err := s.inspect(ctx, expectedID, harness, sessionID)
	if err != nil {
		if errors.Is(err, ErrDockerObjectNotFound) && len(ids) == 0 {
			return contracts.CapabilityTerminationProof{EffectRef: "docker:" + expectedID, TerminatedAt: time.Now().UTC(), CleanupComplete: true}, nil
		}
		return contracts.CapabilityTerminationProof{}, err
	}
	if inspection.neverStarted() {
		return contracts.CapabilityTerminationProof{}, errors.New("retained never-started AO record cannot be stopped")
	}
	if len(ids) != 1 || ids[0] != expectedID || inspection.ID != expectedID {
		return contracts.CapabilityTerminationProof{}, errors.New("AO container identity does not match the durable runner journal")
	}
	_, stopErr := s.Docker.Run(ctx, "stop", "--time", "5", expectedID)
	if stopErr != nil {
		if _, err := s.inspect(ctx, expectedID, harness, sessionID); err == nil {
			if _, removeErr := s.Docker.Run(ctx, "rm", "--force", expectedID); removeErr != nil {
				return contracts.CapabilityTerminationProof{}, errors.New("AO container could not be force-removed")
			}
		} else if !errors.Is(err, ErrDockerObjectNotFound) {
			return contracts.CapabilityTerminationProof{}, err
		}
	}
	if _, err := s.inspect(ctx, expectedID, harness, sessionID); !errors.Is(err, ErrDockerObjectNotFound) {
		if err == nil {
			return contracts.CapabilityTerminationProof{}, errors.New("AO container remains after stop")
		}
		return contracts.CapabilityTerminationProof{}, err
	}
	remaining, err := s.Docker.Run(ctx, "ps", "-aq", "--no-trunc", "--filter", "label="+aoContainerManagedLabel+"=true", "--filter", "label="+aoContainerRuntimeLabel+"="+runtimeValue, "--filter", "label="+aoContainerSessionLabel+"="+sessionID)
	if err != nil {
		return contracts.CapabilityTerminationProof{}, errors.New("AO container inventory is unavailable after termination")
	}
	remainingIDs, err := s.runtimeContainerIDs(ctx, splitContainerIDs(string(remaining)), harness, sessionID)
	if err != nil || len(remainingIDs) != 0 {
		return contracts.CapabilityTerminationProof{}, errors.New("AO session still has a container in its PID namespace")
	}
	return contracts.CapabilityTerminationProof{EffectRef: "docker:" + expectedID, TerminatedAt: time.Now().UTC(), CleanupComplete: true}, nil
}

func (s DockerAOContainerSupervisor) inspect(ctx context.Context, id, harness, sessionID string) (aoContainerInspect, error) {
	runtimeValue, ok := aoContainerRuntime(harness)
	if !ok {
		return aoContainerInspect{}, errors.New("AO container runtime is unsupported")
	}
	body, err := s.Docker.Run(ctx, "inspect", "--format", "{{json .}}", id)
	if err != nil {
		return aoContainerInspect{}, err
	}
	var inspection aoContainerInspect
	if err := json.Unmarshal(body, &inspection); err != nil || inspection.ID == "" || inspection.ID != id {
		return aoContainerInspect{}, errors.New("AO container identity could not be decoded")
	}
	labels := inspection.Config.Labels
	if labels[aoContainerManagedLabel] != "true" || labels[aoContainerRuntimeLabel] != runtimeValue || labels[aoContainerSessionLabel] != sessionID {
		return aoContainerInspect{}, errors.New("AO container ownership labels do not match")
	}
	return inspection, nil
}

func aoContainerRuntime(harness string) (string, bool) {
	switch harness {
	case "codex":
		return aoContainerCodexRuntime, true
	case "opencode":
		return aoContainerOpenCodeRuntime, true
	default:
		return "", false
	}
}

func splitContainerIDs(output string) []string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	ids := make([]string, 0, len(lines))
	for _, id := range lines {
		id = strings.TrimSpace(id)
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func validAOContainerSessionID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// neverStarted permits preserving an inert Docker creation record only when
// every lifecycle fact is explicitly present. Missing fields fail closed.
func (i aoContainerInspect) neverStarted() bool {
	s := i.State
	if len(i.ID) != 64 || strings.ToLower(i.ID) != i.ID {
		return false
	}
	if _, err := hex.DecodeString(i.ID); err != nil {
		return false
	}
	if i.Created == nil || i.Created.IsZero() || i.Created.After(time.Now().UTC()) {
		return false
	}
	_, offset := i.Created.Zone()
	if offset != 0 {
		return false
	}
	return i.Created != nil && !i.Created.IsZero() && i.RestartCount != nil && *i.RestartCount == 0 &&
		i.HostConfig.RestartPolicy.Name != nil && *i.HostConfig.RestartPolicy.Name == "no" &&
		s.Status != nil && *s.Status == "created" && s.Running != nil && !*s.Running &&
		s.Paused != nil && !*s.Paused && s.Restarting != nil && !*s.Restarting &&
		s.OOMKilled != nil && !*s.OOMKilled && s.Dead != nil && !*s.Dead &&
		s.Pid != nil && *s.Pid == 0 && s.ExitCode != nil && *s.ExitCode == 0 &&
		s.Error != nil && *s.Error == "" && s.StartedAt != nil && s.StartedAt.IsZero() &&
		s.FinishedAt != nil && s.FinishedAt.IsZero()
}

// runtimeContainerIDs excludes only verified never-started records. Re-read
// those records before accepting an inventory so an observed transition fails
// closed. These records are never stopped or removed by the supervisor.
func (s DockerAOContainerSupervisor) runtimeContainerIDs(ctx context.Context, ids []string, harness, sessionID string) ([]string, error) {
	out := make([]string, 0, len(ids))
	seen := make(map[string]bool)
	for _, id := range ids {
		if seen[id] {
			return nil, errors.New("AO container inventory has duplicate identities")
		}
		seen[id] = true
		i, err := s.inspect(ctx, id, harness, sessionID)
		if err != nil {
			return nil, err
		}
		if i.neverStarted() {
			verified, err := s.inspect(ctx, id, harness, sessionID)
			if err != nil || !verified.neverStarted() || !verified.Created.Equal(*i.Created) {
				return nil, errors.New("retained AO record changed during verification")
			}
			continue
		}
		out = append(out, id)
	}
	return out, nil
}
