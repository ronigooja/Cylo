package runner

import (
	"context"
	"testing"
)

func TestOpenCodeIsolationRequiresDockerAndImage(t *testing.T) {
	i := SupervisedRuntimeIsolation{}
	if err := i.Check(context.Background(), testClaim()); err == nil {
		t.Fatal("isolation admitted without Docker")
	}
}
