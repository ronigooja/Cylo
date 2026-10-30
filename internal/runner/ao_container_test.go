package runner

import (
	"context"
	"strings"
	"testing"
)

type removalDocker struct{ removed bool }

func (d *removalDocker) Run(_ context.Context, args ...string) ([]byte, error) {
	if args[0] == "rm" {
		d.removed = true
		return nil, nil
	}
	if args[0] == "inspect" && d.removed {
		return nil, ErrDockerObjectNotFound
	}
	return []byte(strings.Repeat("a", 64)), nil
}
func TestRuntimeRemovalUsesExactFullIDAndVerifiesAbsence(t *testing.T) {
	d := &removalDocker{}
	if err := (DockerRuntimeSupervisor{Docker: d}).RemoveExact(context.Background(), strings.Repeat("a", 64)); err != nil || !d.removed {
		t.Fatalf("remove err=%v removed=%v", err, d.removed)
	}
}

type createArgsDocker struct{ args []string }

func (d *createArgsDocker) Run(_ context.Context, args ...string) ([]byte, error) {
	if len(args) > 0 && args[0] == "create" {
		d.args = append([]string(nil), args...)
	}
	return []byte(strings.Repeat("a", 64)), nil
}
func TestRuntimeCreateMountsScopedWorkspaceReadWrite(t *testing.T) {
	d := &createArgsDocker{}
	_, _ = (DockerRuntimeSupervisor{Docker: d, RandomPassword: func() (string, error) { return "secret", nil }}).Create(context.Background(), RuntimeContainerSpec{RunID: "run", Attempt: "attempt", Fence: "1", Image: "image", Workspace: "/root/run", ContainerWorkspace: "/workspace", ProviderSocket: "/provider.sock", PidsLimit: 256})
	joined := strings.Join(d.args, " ")
	if !strings.Contains(joined, "type=bind,src=/root/run,dst=/workspace") {
		t.Fatalf("workspace mount missing or readonly: %s", joined)
	}
	if strings.Contains(joined, ",readonly") {
		t.Fatalf("workspace unexpectedly readonly: %s", joined)
	}
	if !strings.Contains(joined, "OPENCODE_SERVER_PASSWORD=secret") || !strings.Contains(joined, "--network none") {
		t.Fatalf("runtime isolation args missing: %s", joined)
	}
}
