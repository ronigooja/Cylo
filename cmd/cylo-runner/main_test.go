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

func TestLoadRoutesRejectsEmptyOrIncompleteRoutes(t *testing.T) {
	for _, raw := range []string{"", "{}", "[]", `{"worker":{"profile_id":"worker"}}`} {
		if _, err := loadRoutes(raw); err == nil {
			t.Errorf("loadRoutes(%q) succeeded", raw)
		}
	}
}
