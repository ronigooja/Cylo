package runner

import (
	"context"
	"errors"
	"strings"

	"github.com/ronigooja/Cylo/contracts"
)

const CodexWorkspaceWritePolicy = "ao-codex-workspace-write-v1"

// NativeCodexIsolation stays unavailable because the pinned AO process-group
// proof does not cover descendants that escape into new sessions. A revision
// string cannot supply the missing kernel containment boundary.
type NativeCodexIsolation struct{}

func (NativeCodexIsolation) Check(context.Context, contracts.JobClaim) error {
	return errors.New("native AO execution is disabled until process-tree cleanup is proven by a supervised container boundary")
}

// SupervisedCodexIsolation admits only the workspace-write policy after the
// configured runtime image is present on the same Docker daemon the runner
// will supervise. AO applies the actual per-session container profile.
type SupervisedCodexIsolation struct {
	Docker DockerCommand
	Image  string
}

func (i SupervisedCodexIsolation) Check(ctx context.Context, claim contracts.JobClaim) error {
	if i.Docker == nil || strings.TrimSpace(i.Image) == "" || claim.IsolationPolicy != CodexWorkspaceWritePolicy {
		return errors.New("supervised Codex container isolation is unavailable for the selected policy")
	}
	var readScope, writeScope bool
	for _, scope := range claim.Authorization.Scopes {
		readScope = readScope || scope == "workspace:read"
		writeScope = writeScope || scope == "workspace:write"
	}
	if !readScope || !writeScope {
		return errors.New("workspace-write isolation requires explicit read and write grants")
	}
	if _, err := i.Docker.Run(ctx, "image", "inspect", i.Image); err != nil {
		return errors.New("configured AO Codex container image is unavailable")
	}
	return nil
}
