package acceptance

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ronigooja/Zoe/internal/control"
	"github.com/ronigooja/Zoe/internal/storage"
)

func TestProfileForRoleUsesExplicitMappingAndUnambiguousFallback(t *testing.T) {
	workerA := control.RuntimeProfile{ID: "worker-a", RuntimeRouteID: "native", ModelRouteID: "model", Role: "worker", IsolationPolicy: "native-sandbox"}
	workerB := control.RuntimeProfile{ID: "worker-b", RuntimeRouteID: "native", ModelRouteID: "model", Role: "worker", IsolationPolicy: "native-sandbox"}
	reader := control.RuntimeProfile{ID: "reader", RuntimeRouteID: "native", ModelRouteID: "model", Role: "reader", IsolationPolicy: "native-sandbox"}
	profiles := map[string]control.RuntimeProfile{"worker-a": workerA, "worker-b": workerB, "reader": reader}
	var explicit *control.Server
	_, _ = reviewServer(t, control.Config{Profiles: profiles, RoleProfiles: map[string]string{"worker": "worker-b"}}, func(s *control.Server) { explicit = s })
	if got, ok := explicit.ProfileForRole("worker"); !ok || got != "worker-b" {
		t.Fatalf("explicit role mapping = (%q,%v), want (worker-b,true)", got, ok)
	}
	if got, ok := explicit.ProfileForRole(" reader "); !ok || got != "reader" {
		t.Fatalf("unique role fallback = (%q,%v), want (reader,true)", got, ok)
	}
	if got, ok := explicit.ProfileForRole("missing"); ok || got != "" {
		t.Fatalf("missing role fallback = (%q,%v), want empty,false", got, ok)
	}

	var ambiguous *control.Server
	_, _ = reviewServer(t, control.Config{Profiles: profiles}, func(s *control.Server) { ambiguous = s })
	if got, ok := ambiguous.ProfileForRole("worker"); ok || got != "" {
		t.Fatalf("ambiguous role fallback = (%q,%v), want empty,false", got, ok)
	}
}

func TestRoleProfileMappingMustReferenceMatchingWorkerProfile(t *testing.T) {
	profile := control.RuntimeProfile{ID: "worker", RuntimeRouteID: "native", ModelRouteID: "model", Role: "worker", IsolationPolicy: "native-sandbox"}
	store := openStore(t, filepath.Join(t.TempDir(), "control.db"))
	if err := storage.RegisterCoreMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err := control.NewServer(store, control.Config{Profiles: map[string]control.RuntimeProfile{"worker": profile}, RoleProfiles: map[string]string{"reader": "worker"}})
	if err == nil {
		t.Fatal("role mapping with mismatched profile role was accepted")
	}
}
