package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var ErrRuntimeOutcomeUnknown = errors.New("runtime container operation outcome is unknown")

const (
	runtimeManagedLabel = "io.zoe.managed"
	runtimeLabel        = "io.zoe.runtime"
	runtimeRunLabel     = "io.zoe.run"
	runtimeAttemptLabel = "io.zoe.attempt"
	runtimeFenceLabel   = "io.zoe.fence"
)

var runtimePart = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)

type RuntimeContainerSpec struct {
	RunID, Attempt, Fence string
	Image                 string
	Workspace             string
	ContainerWorkspace    string
	ServerPassword        string
	ServerUsername        string
	ProviderSocket        string
	PidsLimit             int
}

type RuntimeContainer struct {
	ID, Name, Password string
	Labels             map[string]string
}

// DockerRuntimeSupervisor owns creation and removal of exactly one OpenCode
// container. It never removes by name or by a label-selected set.
type DockerRuntimeSupervisor struct {
	Docker         DockerCommand
	RandomPassword func() (string, error)
}

func RuntimeIdentity(runID, attempt, fence string) (string, map[string]string, error) {
	if strings.TrimSpace(runID) == "" || strings.TrimSpace(attempt) == "" || strings.TrimSpace(fence) == "" {
		return "", nil, errors.New("runtime identity is incomplete")
	}
	parts := []string{sanitizeRuntimePart(runID), sanitizeRuntimePart(attempt), sanitizeRuntimePart(fence)}
	for _, p := range parts {
		if p == "" {
			return "", nil, errors.New("runtime identity contains no usable characters")
		}
	}
	name := "zoe-run-" + strings.Join(parts, "-")
	if len(name) > 128 {
		return "", nil, errors.New("runtime container name is too long")
	}
	return name, map[string]string{runtimeManagedLabel: "true", runtimeLabel: "opencode", runtimeRunLabel: runID, runtimeAttemptLabel: attempt, runtimeFenceLabel: fence}, nil
}

func (s DockerRuntimeSupervisor) Create(ctx context.Context, spec RuntimeContainerSpec) (RuntimeContainer, error) {
	if s.Docker == nil || strings.TrimSpace(spec.Image) == "" || spec.Workspace == "" || spec.ContainerWorkspace == "" {
		return RuntimeContainer{}, errors.New("runtime container configuration is incomplete")
	}
	name, labels, err := RuntimeIdentity(spec.RunID, spec.Attempt, spec.Fence)
	if err != nil {
		return RuntimeContainer{}, err
	}
	password := spec.ServerPassword
	if password == "" {
		if s.RandomPassword != nil {
			password, err = s.RandomPassword()
		} else {
			password, err = randomRuntimePassword()
		}
		if err != nil {
			return RuntimeContainer{}, err
		}
	}
	args := []string{"create", "--name", name, "--network", "none", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--pids-limit", fmt.Sprint(max(1, spec.PidsLimit)), "--mount", "type=bind,src=" + spec.Workspace + ",dst=" + spec.ContainerWorkspace + ",readonly"}
	for k, v := range labels {
		args = append(args, "--label", k+"="+v)
	}
	args = append(args, "--env", "OPENCODE_SERVER_PASSWORD="+password)
	if spec.ServerUsername != "" {
		args = append(args, "--env", "OPENCODE_SERVER_USERNAME="+spec.ServerUsername)
	}
	if spec.ProviderSocket != "" {
		args = append(args, "--mount", "type=bind,src="+spec.ProviderSocket+",dst=/run/zoe/provider.sock,readonly")
	}
	args = append(args, spec.Image)
	out, err := s.Docker.Run(ctx, args...)
	if err != nil {
		if found, lookupErr := s.findByLabels(ctx, labels); lookupErr == nil && found.ID != "" {
			return RuntimeContainer{}, fmt.Errorf("%w: create failed but container exists", ErrRuntimeOutcomeUnknown)
		}
		return RuntimeContainer{}, fmt.Errorf("%w: container create outcome unresolved: %v", ErrRuntimeOutcomeUnknown, err)
	}
	id := strings.TrimSpace(string(out))
	if !validDockerID(id) {
		return RuntimeContainer{}, fmt.Errorf("%w: docker create returned invalid id", ErrRuntimeOutcomeUnknown)
	}
	if _, err := s.Docker.Run(ctx, "start", id); err != nil {
		return RuntimeContainer{}, fmt.Errorf("%w: container start outcome unresolved: %v", ErrRuntimeOutcomeUnknown, err)
	}
	return RuntimeContainer{ID: id, Name: name, Password: password, Labels: labels}, nil
}

func (s DockerRuntimeSupervisor) Find(ctx context.Context, runID, attempt, fence string) (RuntimeContainer, bool, error) {
	_, labels, err := RuntimeIdentity(runID, attempt, fence)
	if err != nil {
		return RuntimeContainer{}, false, err
	}
	c, err := s.findByLabels(ctx, labels)
	if err != nil {
		return RuntimeContainer{}, false, err
	}
	return c, c.ID != "", nil
}

func (s DockerRuntimeSupervisor) RemoveExact(ctx context.Context, id string) error {
	if !validDockerID(id) {
		return errors.New("runtime container id must be a full Docker id")
	}
	if _, err := s.Docker.Run(ctx, "rm", "--force", id); err != nil && !errors.Is(err, ErrDockerObjectNotFound) {
		return err
	}
	if _, err := s.Docker.Run(ctx, "inspect", id); !errors.Is(err, ErrDockerObjectNotFound) {
		if err == nil {
			return errors.New("runtime container remains after removal")
		}
		return err
	}
	return nil
}

func (s DockerRuntimeSupervisor) findByLabels(ctx context.Context, labels map[string]string) (RuntimeContainer, error) {
	if s.Docker == nil {
		return RuntimeContainer{}, errors.New("runtime container supervisor is not configured")
	}
	args := []string{"ps", "-aq", "--no-trunc"}
	for k, v := range labels {
		args = append(args, "--filter", "label="+k+"="+v)
	}
	out, err := s.Docker.Run(ctx, args...)
	if err != nil {
		return RuntimeContainer{}, err
	}
	ids := splitContainerIDs(string(out))
	if len(ids) > 1 {
		return RuntimeContainer{}, errors.New("runtime labels match multiple containers")
	}
	if len(ids) == 0 {
		return RuntimeContainer{}, nil
	}
	if !validDockerID(ids[0]) {
		return RuntimeContainer{}, errors.New("runtime inventory returned a non-full id")
	}
	return RuntimeContainer{ID: ids[0], Labels: labels}, nil
}

func randomRuntimePassword() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func sanitizeRuntimePart(s string) string {
	s = runtimePart.ReplaceAllString(strings.TrimSpace(s), "-")
	s = strings.Trim(s, "-_.")
	return s
}
func validDockerID(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
