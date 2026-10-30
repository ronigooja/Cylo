package runner

import (
	"context"
	"github.com/ronigooja/Zoe/contracts"
	"testing"
)

type imageDocker struct{}

func (imageDocker) Run(context.Context, ...string) ([]byte, error) { return nil, nil }
func TestOnlyOpenCodeProfileIsAdmitted(t *testing.T) {
	i := SupervisedRuntimeIsolation{Docker: imageDocker{}, Profiles: map[string]SupervisedRuntimeProfile{"opencode": {Harness: "opencode", IsolationPolicy: OpenCodeWorkspaceWritePolicy, Image: "image"}}}
	if err := i.Check(context.Background(), testClaim()); err != nil {
		t.Fatal(err)
	}
	bad := testClaim()
	bad.Harness = "other"
	if err := i.Check(context.Background(), bad); err == nil {
		t.Fatal("non-OpenCode runtime admitted")
	}
	_ = contracts.JobClaim{Harness: "opencode"}
}
