package acceptance

import (
	"testing"

	"github.com/ronigooja/Zoe/internal/control"
)

func TestReviewRoleMappingRejectsConflictingNormalizedRoles(t *testing.T) {
	profiles := map[string]control.RuntimeProfile{
		"a": {ID: "a", RuntimeRouteID: "native", ModelRouteID: "model", Role: "worker", IsolationPolicy: "opencode-workspace-write-v1"},
		"b": {ID: "b", RuntimeRouteID: "native", ModelRouteID: "model", Role: "worker", IsolationPolicy: "opencode-workspace-write-v1"},
	}
	store := openStore(t, t.TempDir()+"/control.db")
	_, err := control.NewServer(store, control.Config{Profiles: profiles, RoleProfiles: map[string]string{"worker": "a", " worker ": "b"}})
	if err == nil {
		t.Fatal("conflicting mappings collapse to one normalized role in nondeterministic map iteration order")
	}
}
