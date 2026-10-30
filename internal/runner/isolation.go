package runner

import (
	"context"
	"errors"
	"strings"

	"github.com/ronigooja/Zoe/contracts"
)

const OpenCodeWorkspaceWritePolicy = "opencode-workspace-write-v1"

type SupervisedRuntimeProfile struct{ Harness, IsolationPolicy, Image string }
type SupervisedRuntimeIsolation struct {
	Docker   DockerCommand
	Profiles map[string]SupervisedRuntimeProfile
}

func (i SupervisedRuntimeIsolation) Check(ctx context.Context, claim contracts.JobClaim) error {
	if i.Docker == nil {
		return errors.New("supervised runtime isolation unavailable")
	}
	if claim.Harness != "opencode" || claim.IsolationPolicy != OpenCodeWorkspaceWritePolicy {
		return errors.New("OpenCode isolation policy required")
	}
	p, ok := i.Profiles["opencode"]
	if !ok || p.Image == "" || strings.TrimSpace(p.Image) == "" {
		return errors.New("OpenCode image unavailable")
	}
	if _, err := i.Docker.Run(ctx, "image", "inspect", p.Image); err != nil {
		return errors.New("configured OpenCode image unavailable")
	}
	return nil
}
func (i SupervisedRuntimeIsolation) CheckRoute(ctx context.Context, claim contracts.JobClaim, route Route) error {
	return i.Check(ctx, claim)
}
