package runner

import (
	"context"
	"errors"
	"strings"

	"github.com/ronigooja/Zoe/contracts"
)

const CodexWorkspaceWritePolicy = "ao-codex-workspace-write-v1"
const OpenCodeWorkspaceWritePolicy = "ao-opencode-workspace-write-v1"

// NativeCodexIsolation stays unavailable because the pinned AO process-group
// proof does not cover descendants that escape into new sessions. A revision
// string cannot supply the missing kernel containment boundary.
type NativeCodexIsolation struct{}

func (NativeCodexIsolation) Check(context.Context, contracts.JobClaim) error {
	return errors.New("native AO execution is disabled until process-tree cleanup is proven by a supervised container boundary")
}

type SupervisedRuntimeProfile struct {
	Harness         string
	IsolationPolicy string
	Image           string
}

// SupervisedRuntimeIsolation admits an explicit pair of AO harness and
// workspace-write policy only when its pinned image exists on the Docker daemon
// the runner supervises. AO applies the per-session container profile itself.
type SupervisedRuntimeIsolation struct {
	Docker   DockerCommand
	Profiles map[string]SupervisedRuntimeProfile
}

func (i SupervisedRuntimeIsolation) Check(ctx context.Context, claim contracts.JobClaim) error {
	if i.Docker == nil {
		return errors.New("supervised runtime container isolation is unavailable")
	}
	var profile SupervisedRuntimeProfile
	matched := false
	for _, candidate := range i.Profiles {
		if candidate.IsolationPolicy == claim.IsolationPolicy {
			if matched {
				return errors.New("supervised runtime isolation policy is ambiguous")
			}
			profile, matched = candidate, true
		}
	}
	if !matched || strings.TrimSpace(profile.Harness) == "" || strings.TrimSpace(profile.Image) == "" {
		return errors.New("supervised runtime container isolation is unavailable for the selected policy")
	}
	if effectiveHarness(profile.Harness) != effectiveHarness(claim.Harness) {
		return errors.New("supervised runtime harness does not match the server-selected run harness")
	}
	return i.checkProfile(ctx, claim, profile)
}

func (i SupervisedRuntimeIsolation) CheckRoute(ctx context.Context, claim contracts.JobClaim, route Route) error {
	if i.Docker == nil || route.IsolationPolicy != claim.IsolationPolicy || effectiveHarness(route.Harness) != effectiveHarness(claim.Harness) {
		return errors.New("supervised runtime container isolation is unavailable for the selected route")
	}
	profile, ok := i.Profiles[route.Harness]
	if !ok || profile.Harness != route.Harness || profile.IsolationPolicy != route.IsolationPolicy || profile.Image == "" {
		return errors.New("supervised runtime container isolation is unavailable for the selected route")
	}
	return i.checkProfile(ctx, claim, profile)
}

func (i SupervisedRuntimeIsolation) checkProfile(ctx context.Context, claim contracts.JobClaim, profile SupervisedRuntimeProfile) error {
	var readScope, writeScope bool
	for _, scope := range claim.Authorization.Scopes {
		readScope = readScope || scope == "workspace:read"
		writeScope = writeScope || scope == "workspace:write"
	}
	if !readScope || !writeScope {
		return errors.New("supervised workspace-write isolation requires explicit read and write grants")
	}
	if _, err := i.Docker.Run(ctx, "image", "inspect", profile.Image); err != nil {
		return errors.New("configured AO runtime container image is unavailable")
	}
	return nil
}

// SupervisedCodexIsolation preserves the existing Codex adapter surface for
// callers that configure only the first approved runtime profile.
type SupervisedCodexIsolation struct {
	Docker DockerCommand
	Image  string
}

func (i SupervisedCodexIsolation) Check(ctx context.Context, claim contracts.JobClaim) error {
	return (SupervisedRuntimeIsolation{Docker: i.Docker, Profiles: map[string]SupervisedRuntimeProfile{
		"codex": {Harness: "codex", IsolationPolicy: CodexWorkspaceWritePolicy, Image: i.Image},
	}}).Check(ctx, claim)
}

func (i SupervisedCodexIsolation) CheckRoute(ctx context.Context, claim contracts.JobClaim, route Route) error {
	if route.Harness != "codex" || route.IsolationPolicy != CodexWorkspaceWritePolicy {
		return errors.New("supervised Codex container isolation is unavailable for the selected route")
	}
	return i.Check(ctx, claim)
}
