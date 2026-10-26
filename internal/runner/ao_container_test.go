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

// retainedDocker keeps the inert creation artifact present throughout the
// current worker's stop, so any accidental mutation is visible to the test.
type retainedDocker struct {
	bodies      map[string]map[string]any
	order       []string
	mutations   []string
	inspectHook func(string, int)
	reads       map[string]int
}

func (d *retainedDocker) Run(_ context.Context, args ...string) ([]byte, error) {
	switch args[0] {
	case "ps":
		var ids []string
		for _, id := range d.order {
			if _, ok := d.bodies[id]; ok {
				ids = append(ids, id)
			}
		}
		return []byte(strings.Join(ids, "\n")), nil
	case "inspect":
		id := args[len(args)-1]
		if d.reads == nil {
			d.reads = make(map[string]int)
		}
		d.reads[id]++
		if d.inspectHook != nil {
			d.inspectHook(id, d.reads[id])
		}
		body, ok := d.bodies[id]
		if !ok {
			return nil, ErrDockerObjectNotFound
		}
		return json.Marshal(body)
	case "stop", "rm":
		id := args[len(args)-1]
		d.mutations = append(d.mutations, id)
		delete(d.bodies, id)
		return nil, nil
	}
	panic("unexpected docker command")
}
func retainedCreationFixture(id string) map[string]any {
	return map[string]any{"Id": id, "Created": "2026-01-01T00:00:00Z", "RestartCount": 0,
		"Config":     map[string]any{"Labels": map[string]string{aoContainerManagedLabel: "true", aoContainerRuntimeLabel: aoContainerCodexRuntime, aoContainerSessionLabel: "session-review"}},
		"HostConfig": map[string]any{"NetworkMode": "none", "RestartPolicy": map[string]any{"Name": "no"}},
		"State":      map[string]any{"Status": "created", "Running": false, "Paused": false, "Restarting": false, "OOMKilled": false, "Dead": false, "Pid": 0, "ExitCode": 0, "Error": "", "StartedAt": "0001-01-01T00:00:00Z", "FinishedAt": "0001-01-01T00:00:00Z"}}
}
func TestSupervisorPreservesNeverStartedRecordAlongsideActualWorker(t *testing.T) {
	oldID, activeID := strings.Repeat("a", 64), strings.Repeat("b", 64)
	active := retainedCreationFixture(activeID)
	active["State"] = map[string]any{"Status": "running", "Running": true, "Pid": 42}
	d := &retainedDocker{bodies: map[string]map[string]any{oldID: retainedCreationFixture(oldID), activeID: active}, order: []string{oldID, activeID}}
	s := DockerAOContainerSupervisor{Docker: d}
	id, found, err := s.Find(context.Background(), "codex", "session-review")
	if err != nil || !found || id != activeID {
		t.Fatalf("Find=%s %t %v", id, found, err)
	}
	proof, err := s.StopAndVerify(context.Background(), "codex", "session-review", id)
	if err != nil || !proof.CleanupComplete {
		t.Fatalf("Stop=%+v %v", proof, err)
	}
	if _, ok := d.bodies[oldID]; !ok {
		t.Fatal("retained creation record was removed")
	}
	for _, id := range d.mutations {
		if id != activeID {
			t.Fatalf("mutated retained record: %s", id)
		}
	}
	if _, err := s.StopAndVerify(context.Background(), "codex", "session-review", oldID); err == nil {
		t.Fatal("explicit stop accepted retained record")
	}
}
func TestSupervisorDoesNotIgnoreIncompleteOrPreviouslyStartedRecord(t *testing.T) {
	for _, field := range []string{"Status", "Running", "Paused", "Restarting", "OOMKilled", "Dead", "Pid", "ExitCode", "Error", "StartedAt", "FinishedAt", "RestartCount", "RestartPolicy", "Created", "started", "restarting"} {
		t.Run(field, func(t *testing.T) {
			id := strings.Repeat("a", 64)
			body := retainedCreationFixture(id)
			state := body["State"].(map[string]any)
			switch field {
			case "RestartCount", "Created":
				delete(body, field)
			case "RestartPolicy":
				delete(body["HostConfig"].(map[string]any), field)
			case "started":
				state["StartedAt"] = "2026-01-01T00:00:00Z"
			case "restarting":
				state["Restarting"] = true
			default:
				delete(state, field)
			}
			d := &retainedDocker{bodies: map[string]map[string]any{id: body}, order: []string{id}}
			if _, _, err := (DockerAOContainerSupervisor{Docker: d}).Find(context.Background(), "codex", "session-review"); err == nil {
				t.Fatal("unproven record was ignored")
			}
			if _, err := (DockerAOContainerSupervisor{Docker: d}).StopAndVerify(context.Background(), "codex", "session-review", ""); err == nil {
				t.Fatal("unproven record accepted as absent")
			}
			if len(d.mutations) != 0 {
				t.Fatal("unproven record mutated")
			}
		})
	}
}
func TestSupervisorRejectsRetainedRecordChangingDuringInspection(t *testing.T) {
	id := strings.Repeat("a", 64)
	body := retainedCreationFixture(id)
	d := &retainedDocker{bodies: map[string]map[string]any{id: body}, order: []string{id}}
	d.inspectHook = func(_ string, n int) {
		if n == 2 {
			body["State"].(map[string]any)["Running"] = true
		}
	}
	if _, _, err := (DockerAOContainerSupervisor{Docker: d}).Find(context.Background(), "codex", "session-review"); err == nil {
		t.Fatal("transitioning retained record ignored")
	}
}

func TestSupervisorRejectsTwoActualWorkersSharingSession(t *testing.T) {
	first, second := strings.Repeat("a", 64), strings.Repeat("b", 64)
	bodies := make(map[string]map[string]any)
	for _, id := range []string{first, second} {
		body := retainedCreationFixture(id)
		body["State"] = map[string]any{"Status": "running", "Running": true, "Pid": 42}
		bodies[id] = body
	}
	d := &retainedDocker{bodies: bodies, order: []string{first, second}}
	s := DockerAOContainerSupervisor{Docker: d}
	if _, _, err := s.Find(context.Background(), "codex", "session-review"); err == nil {
		t.Fatal("multiple actual worker runtimes accepted")
	}
	if _, err := s.StopAndVerify(context.Background(), "codex", "session-review", first); err == nil {
		t.Fatal("ambiguous actual worker set accepted for stop")
	}
	if len(d.mutations) != 0 {
		t.Fatal("ambiguous runtime set mutated")
	}
}
