package acceptance

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ronigooja/Zoe/internal/capabilities"
	"github.com/ronigooja/Zoe/internal/control"
	"github.com/ronigooja/Zoe/internal/memory"
	"github.com/ronigooja/Zoe/internal/storage"
	"github.com/ronigooja/Zoe/internal/voice"
)

func TestIndependentStagedRestoreCannotServeAuthorityBeforePrepare(t *testing.T) {
	ctx := context.Background()
	source, _, token, subject, _ := scopedExportServer(t)
	if err := source.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	seedExportMemory(t, source.DB(), "snapshot-claim", "personal", "", subject, "old", "snapshot-only private content", "", false, time.Now().UTC().Format(time.RFC3339Nano))
	root := t.TempDir()
	path := filepath.Join(root, "zoe.sqlite.restore-pending")
	if _, err := source.DB().Exec(`VACUUM INTO ?`, path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "restore-state.json"), []byte(`{"restore_state":{"requires_old_instance_isolation":true,"requires_deletion_reconciliation_before_derived_reads":true}}`), 0600); err != nil {
		t.Fatal(err)
	}
	target, err := storage.Open(ctx, storage.Config{Path: path})
	if err != nil {
		return
	}
	defer target.Close()
	if err = storage.RegisterCoreMigrations(target); err != nil {
		t.Fatal(err)
	}
	if err = control.RegisterInstanceMaintenanceMigration(target); err != nil {
		t.Fatal(err)
	}
	server, err := control.NewServer(target, control.Config{InstanceScopes: exportInstanceScopes})
	if err != nil {
		t.Fatal(err)
	}
	if err = memory.Register(server, target); err != nil {
		t.Fatal(err)
	}
	if err = voice.RegisterMigrations(target); err != nil {
		t.Fatal(err)
	}
	if err = capabilities.RegisterMigrations(target); err != nil {
		t.Fatal(err)
	}
	if err = control.RegisterExportRoutes(server); err != nil {
		t.Fatal(err)
	}
	if err = target.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	response := reviewRequest(server.Handler(), http.MethodGet, "/v1/export?scope=personal", token, "", nil)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("staged restored DB served business data before prepare/reconciliation: HTTP=%d", response.Code)
	}
}
