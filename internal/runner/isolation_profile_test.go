package runner

import (
	"context"
	"errors"
	"testing"
)

type inspectImageDocker struct {
	image string
	err   error
}

func (d *inspectImageDocker) Run(_ context.Context, args ...string) ([]byte, error) {
	if len(args) != 3 || args[0] != "image" || args[1] != "inspect" {
		return nil, errors.New("unexpected Docker command")
	}
	d.image = args[2]
	return nil, d.err
}

func TestSupervisedRuntimeIsolationRequiresExactHarnessPolicyProfile(t *testing.T) {
	docker := &inspectImageDocker{}
	isolation := SupervisedRuntimeIsolation{Docker: docker, Profiles: map[string]SupervisedRuntimeProfile{
		"codex":    {Harness: "codex", IsolationPolicy: CodexWorkspaceWritePolicy, Image: "cylo-codex:pinned"},
		"opencode": {Harness: "opencode", IsolationPolicy: OpenCodeWorkspaceWritePolicy, Image: "cylo-opencode:pinned"},
	}}
	claim := validClaim()
	claim.Harness = "opencode"
	claim.IsolationPolicy = OpenCodeWorkspaceWritePolicy
	route := Route{Harness: "opencode", IsolationPolicy: OpenCodeWorkspaceWritePolicy}
	if err := isolation.CheckRoute(context.Background(), claim, route); err != nil {
		t.Fatalf("CheckRoute(OpenCode) failed: %v", err)
	}
	if docker.image != "cylo-opencode:pinned" {
		t.Fatalf("inspected image = %q, want the OpenCode profile image", docker.image)
	}
	for _, tc := range []struct {
		name  string
		route Route
	}{
		{name: "codex harness with OpenCode policy", route: Route{Harness: "codex", IsolationPolicy: OpenCodeWorkspaceWritePolicy}},
		{name: "unregistered AO harness", route: Route{Harness: "aider", IsolationPolicy: OpenCodeWorkspaceWritePolicy}},
		{name: "wrong policy", route: Route{Harness: "opencode", IsolationPolicy: CodexWorkspaceWritePolicy}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := isolation.CheckRoute(context.Background(), claim, tc.route); err == nil {
				t.Fatal("route without an exact configured harness/policy profile was accepted")
			}
		})
	}
}

func TestSupervisedRuntimeIsolationRequiresWorkspaceReadAndWrite(t *testing.T) {
	docker := &inspectImageDocker{}
	isolation := SupervisedRuntimeIsolation{Docker: docker, Profiles: map[string]SupervisedRuntimeProfile{
		"opencode": {Harness: "opencode", IsolationPolicy: OpenCodeWorkspaceWritePolicy, Image: "cylo-opencode:pinned"},
	}}
	claim := validClaim()
	claim.IsolationPolicy = OpenCodeWorkspaceWritePolicy
	claim.Authorization.Scopes = []string{"workspace:read"}
	route := Route{Harness: "opencode", IsolationPolicy: OpenCodeWorkspaceWritePolicy}
	if err := isolation.CheckRoute(context.Background(), claim, route); err == nil {
		t.Fatal("OpenCode container profile was accepted without an explicit write grant")
	}
	if docker.image != "" {
		t.Fatal("image inspection happened before authorization was checked")
	}
}

func TestSupervisedRuntimeIsolationRejectsAmbiguousPolicy(t *testing.T) {
	isolation := SupervisedRuntimeIsolation{Docker: &inspectImageDocker{}, Profiles: map[string]SupervisedRuntimeProfile{
		"codex":    {Harness: "codex", IsolationPolicy: CodexWorkspaceWritePolicy, Image: "cylo-codex:pinned"},
		"opencode": {Harness: "opencode", IsolationPolicy: CodexWorkspaceWritePolicy, Image: "cylo-opencode:pinned"},
	}}
	claim := validClaim()
	claim.IsolationPolicy = CodexWorkspaceWritePolicy
	if err := isolation.Check(context.Background(), claim); err == nil {
		t.Fatal("ambiguous isolation policy was accepted")
	}
}

func TestRunnerRecognizesOnlyExplicitSupervisedHarnessPolicies(t *testing.T) {
	for _, tc := range []struct {
		name  string
		route Route
		want  bool
	}{
		{name: "Codex profile", route: Route{Harness: "codex", IsolationPolicy: CodexWorkspaceWritePolicy}, want: true},
		{name: "OpenCode profile", route: Route{Harness: "opencode", IsolationPolicy: OpenCodeWorkspaceWritePolicy}, want: true},
		{name: "registered but unapproved adapter", route: Route{Harness: "aider", IsolationPolicy: OpenCodeWorkspaceWritePolicy}},
		{name: "crossed harness and policy", route: Route{Harness: "codex", IsolationPolicy: OpenCodeWorkspaceWritePolicy}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := supportedSupervisedRoute(tc.route); got != tc.want {
				t.Fatalf("supportedSupervisedRoute(%+v) = %v, want %v", tc.route, got, tc.want)
			}
		})
	}
}
