package runner

import (
	"context"
	"strings"
	"testing"
)

func TestRuntimeIdentityLookupIsDeterministic(t *testing.T) {
	name, labels, err := RuntimeIdentity("run", "attempt", "fence")
	if err != nil || name != "zoe-run-run-attempt-fence" || labels[runtimeRunLabel] != "run" {
		t.Fatalf("%s %#v %v", name, labels, err)
	}
}
func TestLegacyRetainedRuntimeLabelsRemainNamedForCompatibility(t *testing.T) {
	if !strings.Contains(aoContainerRuntimeLabel, "runtime") || aoContainerSessionLabel == "" {
		t.Fatal("legacy labels not isolated")
	}
}
func TestReconciliationRequiresDocker(t *testing.T) {
	e := &Engine{}
	if err := e.ReconcileNoEffect(context.Background(), []string{"run"}, "operator", true, nil, nil); err == nil {
		t.Fatal("reconciliation accepted missing Docker inventory")
	}
}
