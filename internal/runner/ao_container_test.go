package runner

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type shortIDDocker struct {
	id      string
	removed bool
	runtime string
}

func (d *shortIDDocker) Run(_ context.Context, args ...string) ([]byte, error) {
	switch args[0] {
	case "ps":
		if d.removed {
			return nil, nil
		}
		for _, arg := range args {
			if arg == "--no-trunc" {
				return []byte(d.id + "\n"), nil
			}
		}
		return []byte(d.id[:12] + "\n"), nil
	case "inspect":
		if d.removed {
			return nil, ErrDockerObjectNotFound
		}
		body, _ := json.Marshal(map[string]any{
			"Id": d.id,
			"Config": map[string]any{"Labels": map[string]string{
				aoContainerManagedLabel: "true",
				aoContainerRuntimeLabel: d.runtimeValue(),
				aoContainerSessionLabel: "session-review",
			}},
			"HostConfig": map[string]any{"NetworkMode": "none"},
			"State":      map[string]any{"Running": true, "Pid": 42},
		})
		return body, nil
	case "stop", "rm":
		d.removed = true
		return nil, nil
	default:
		panic("unexpected docker command: " + strings.Join(args, " "))
	}
}

func TestDockerAOContainerSupervisorUsesFullIDForStop(t *testing.T) {
	docker := &shortIDDocker{id: strings.Repeat("a", 64)}
	supervisor := DockerAOContainerSupervisor{Docker: docker}
	id, found, err := supervisor.Find(context.Background(), "codex", "session-review")
	if err != nil || !found || id != docker.id {
		t.Fatalf("Find() = %q, %v, %v", id, found, err)
	}
	proof, err := supervisor.StopAndVerify(context.Background(), "codex", "session-review", id)
	if err != nil || !proof.CleanupComplete {
		t.Fatalf("StopAndVerify() failed for full Docker ID: proof=%+v err=%v", proof, err)
	}
}

func TestDockerAOContainerSupervisorUsesOpenCodeRuntimeLabel(t *testing.T) {
	docker := &shortIDDocker{id: strings.Repeat("b", 64), runtime: aoContainerOpenCodeRuntime}
	supervisor := DockerAOContainerSupervisor{Docker: docker}
	id, found, err := supervisor.Find(context.Background(), "opencode", "session-review")
	if err != nil || !found || id != docker.id {
		t.Fatalf("Find(OpenCode) = %q, %v, %v", id, found, err)
	}
	proof, err := supervisor.StopAndVerify(context.Background(), "opencode", "session-review", id)
	if err != nil || !proof.CleanupComplete || proof.EffectRef != "docker:"+id {
		t.Fatalf("StopAndVerify(OpenCode) failed for full Docker ID: proof=%+v err=%v", proof, err)
	}
}

func TestDockerAOContainerSupervisorRejectsUnregisteredHarness(t *testing.T) {
	supervisor := DockerAOContainerSupervisor{Docker: &shortIDDocker{id: strings.Repeat("c", 64)}}
	if _, _, err := supervisor.Find(context.Background(), "registered-by-AO-only", "session-review"); err == nil {
		t.Fatal("container inventory accepted a harness without an explicit supervisor profile")
	}
}

func (d *shortIDDocker) runtimeValue() string {
	if d.runtime != "" {
		return d.runtime
	}
	return aoContainerCodexRuntime
}
