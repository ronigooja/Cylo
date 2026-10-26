package acceptance

import (
	"testing"

	"github.com/ronigooja/Cylo/internal/control"
)

func TestReviewRoleMappingRejectsConflictingNormalizedRoles(t *testing.T) {
	profiles := map[string]control.RuntimeProfile{
		"a": {ID: "a", AOProjectID: "workers-a", RuntimeRouteID: "native", ModelRouteID: "model", Role: "worker", AOAgentKind: "worker", IsolationPolicy: "native-sandbox"},
		"b": {ID: "b", AOProjectID: "workers-b", RuntimeRouteID: "native", ModelRouteID: "model", Role: "worker", AOAgentKind: "worker", IsolationPolicy: "native-sandbox"},
	}
	store := openStore(t, t.TempDir()+"/control.db")
	_, err := control.NewServer(store, control.Config{Profiles: profiles, RoleProfiles: map[string]string{"worker": "a", " worker ": "b"}})
	if err == nil {
		t.Fatal("conflicting mappings collapse to one normalized role in nondeterministic map iteration order")
	}
}
