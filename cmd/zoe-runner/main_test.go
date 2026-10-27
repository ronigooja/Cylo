package main

import "testing"

func TestLoadRoutesRequiresServerMatchingProfile(t *testing.T) {
	if _, err := loadRoutes(`{"worker":{"profile_id":"other","runtime_route_id":"runtime","model_route_id":"model","isolation_policy":"ao-codex-workspace-write-v1","harness":"codex","model":"gpt-6-luna"}}`); err == nil {
		t.Fatal("route key mismatch was accepted")
	}
	routes, err := loadRoutes(`{"worker":{"profile_id":"worker","runtime_route_id":"runtime","model_route_id":"model","isolation_policy":"ao-codex-workspace-write-v1","harness":"codex","model":"gpt-6-luna","approval_mode":"accept-edits"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if routes["worker"].ProfileID != "worker" {
		t.Fatalf("route not loaded: %+v", routes)
	}
}

func TestLoadRoutesKeepsOpenCodeRuntimeAndModelRouteExplicit(t *testing.T) {
	routes, err := loadRoutes(`{"worker-opencode":{"profile_id":"worker-opencode","runtime_route_id":"ao-opencode","model_route_id":"cliproxyapi","isolation_policy":"ao-opencode-workspace-write-v1","harness":"opencode","model":"zoe/gpt-6-luna","approval_mode":"auto"}}`)
	if err != nil {
		t.Fatal(err)
	}
	route := routes["worker-opencode"]
	if route.Harness != "opencode" || route.IsolationPolicy != "ao-opencode-workspace-write-v1" || route.ModelRouteID != "cliproxyapi" || route.Model != "zoe/gpt-6-luna" {
		t.Fatalf("OpenCode runtime, isolation, and model route were not preserved independently: %+v", route)
	}
}

func TestLoadRoutesRejectsEmptyOrIncompleteRoutes(t *testing.T) {
	for _, raw := range []string{"", "{}", "[]", `{"worker":{"profile_id":"worker"}}`} {
		if _, err := loadRoutes(raw); err == nil {
			t.Errorf("loadRoutes(%q) succeeded", raw)
		}
	}
}

func TestReconciliationContainerFlagsRequireFullDockerIDs(t *testing.T) {
	for _, name := range []string{"--retain-never-started-container", "--unrelated-preexisting-capability-container"} {
		if err := run([]string{name, "short-id"}); err == nil {
			t.Fatalf("%s accepted a non-full Docker identity", name)
		}
	}
}
