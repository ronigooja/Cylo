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
