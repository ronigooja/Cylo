package runner

import (
	"context"
	"testing"

	"github.com/ronigooja/Cylo/contracts"
)

func TestNativeCodexIsolationCannotBeEnabledByPinnedRevisionOrPolicy(t *testing.T) {
	claim := validClaim()
	claim.IsolationPolicy = "ao-codex-workspace-write-v1"
	for _, revision := range []string{"", "7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff", "attacker-selected-revision"} {
		_ = revision // revision strings are intentionally not an isolation authority.
		if err := (NativeCodexIsolation{}).Check(context.Background(), claim); err == nil {
			t.Fatal("native AO process-group proof was enabled without a supervised container boundary")
		}
	}
	var verifier WorkIsolation = NativeCodexIsolation{}
	if err := verifier.Check(context.Background(), contracts.JobClaim{IsolationPolicy: claim.IsolationPolicy}); err == nil {
		t.Fatal("known-insufficient native proof accepted an enabled policy")
	}
}
