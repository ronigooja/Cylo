package control

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/internal/storage"
)

func exportTempTestStore(t *testing.T) *storage.Store {
	t.Helper()
	ctx := context.Background()
	store, err := storage.Open(ctx, storage.Config{Path: filepath.Join(t.TempDir(), "control.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err = storage.RegisterCoreMigrations(store); err != nil {
		t.Fatal(err)
	}
	if err = RegisterInstanceMaintenanceMigration(store); err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().Exec(`INSERT INTO workspaces(id,name,created_at) VALUES('other','Other',?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	return store
}

func addExportTempFixture(t *testing.T, store *storage.Store, id, workspace string, sequence int64) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.DB().Exec(`INSERT INTO cylo_export_temp_registry(export_id,workspace_id,owner_id,export_scope,created_deletion_sequence,status,invalidation_sequence,created_at,updated_at) VALUES(?,?,?,'workspace',?,'active',0,?,?)`, id, workspace, "owner", sequence, now, now); err != nil {
		t.Fatal(err)
	}
}

func createExportTempFixture(t *testing.T, store *storage.Store, id string) string {
	t.Helper()
	root, err := openPrivateExportRoot(store, true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, name := range []string{exportDataName(id), exportArchiveName(id)} {
		file, openErr := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
		if openErr != nil {
			t.Fatal(openErr)
		}
		if _, openErr = file.Write([]byte("fixture")); openErr != nil {
			t.Fatal(openErr)
		}
		if openErr = file.Close(); openErr != nil {
			t.Fatal(openErr)
		}
	}
	path, err := exportTempRootPath(store)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExportTempCleanupIsIdempotentAndWorkspaceScoped(t *testing.T) {
	store := exportTempTestStore(t)
	server, err := NewServer(store, Config{})
	if err != nil {
		t.Fatal(err)
	}
	addExportTempFixture(t, store, "workspace-a-old", "default", 1)
	addExportTempFixture(t, store, "workspace-b-old", "other", 1)
	rootPath := createExportTempFixture(t, store, "workspace-a-old")
	createExportTempFixture(t, store, "workspace-b-old")
	done, code, err := server.cleanExportTemps(context.Background(), "default", 2)
	if err != nil || !done || code != "" {
		t.Fatalf("first cleanup completed=%t code=%q err=%v", done, code, err)
	}
	if _, err = os.Stat(filepath.Join(rootPath, exportDataName("workspace-a-old"))); !os.IsNotExist(err) {
		t.Fatalf("workspace A artifact remains or stat failed: %v", err)
	}
	if _, err = os.Stat(filepath.Join(rootPath, exportDataName("workspace-b-old"))); err != nil {
		t.Fatalf("workspace B artifact was touched: %v", err)
	}
	if done, code, err = server.cleanExportTemps(context.Background(), "default", 2); err != nil || !done || code != "" {
		t.Fatalf("idempotent cleanup completed=%t code=%q err=%v", done, code, err)
	}
	var otherStatus string
	if err = store.DB().QueryRow(`SELECT status FROM cylo_export_temp_registry WHERE export_id='workspace-b-old'`).Scan(&otherStatus); err != nil || otherStatus != "active" {
		t.Fatalf("other workspace registry status=%q err=%v", otherStatus, err)
	}
	var checkpoint int64
	if err = store.DB().QueryRow(`SELECT completed_sequence FROM cylo_deletion_reconciliation_checkpoints WHERE target='export_temp'`).Scan(&checkpoint); err != nil || checkpoint != 0 {
		t.Fatalf("file cleanup alone advanced reconciliation checkpoint=%d err=%v", checkpoint, err)
	}
}

func TestExportTempCleanupReturnsPendingForActiveExporter(t *testing.T) {
	store := exportTempTestStore(t)
	server, err := NewServer(store, Config{})
	if err != nil {
		t.Fatal(err)
	}
	addExportTempFixture(t, store, "active-export", "default", 1)
	rootPath := createExportTempFixture(t, store, "active-export")
	lock, ok := acquireExportTempLock("active-export", true)
	if !ok {
		t.Fatal("could not acquire test exporter lock")
	}
	done, code, err := server.cleanExportTemps(context.Background(), "default", 2)
	if err != nil || done || code != "export_active" {
		t.Fatalf("active cleanup completed=%t code=%q err=%v", done, code, err)
	}
	releaseExportTempLock("active-export", lock)
	if _, err = os.Stat(filepath.Join(rootPath, exportDataName("active-export"))); err != nil {
		t.Fatalf("pending cleanup removed an active artifact: %v", err)
	}
	if done, code, err = server.cleanExportTemps(context.Background(), "default", 2); err != nil || !done || code != "" {
		t.Fatalf("retry cleanup completed=%t code=%q err=%v", done, code, err)
	}
}

func TestExportTempCleanupFailsClosedForUnregisteredOrUnsafeArtifacts(t *testing.T) {
	t.Run("unregistered artifact", func(t *testing.T) {
		store := exportTempTestStore(t)
		server, err := NewServer(store, Config{})
		if err != nil {
			t.Fatal(err)
		}
		rootPath := createExportTempFixture(t, store, "registered-export")
		root, err := openPrivateExportRoot(store, false)
		if err != nil {
			t.Fatal(err)
		}
		file, err := root.OpenFile("unregistered.bin", os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_ = file.Close()
		_ = root.Close()
		addExportTempFixture(t, store, "registered-export", "default", 1)
		if done, code, err := server.cleanExportTemps(context.Background(), "default", 2); err == nil || done || code != "adapter_missing" {
			t.Fatalf("unregistered cleanup completed=%t code=%q err=%v", done, code, err)
		}
		if _, err = os.Stat(filepath.Join(rootPath, "unregistered.bin")); err != nil {
			t.Fatalf("unregistered artifact was removed: %v", err)
		}
	})

	t.Run("symlink root", func(t *testing.T) {
		store := exportTempTestStore(t)
		server, err := NewServer(store, Config{})
		if err != nil {
			t.Fatal(err)
		}
		addExportTempFixture(t, store, "symlink-export", "default", 1)
		rootPath, err := exportTempRootPath(store)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.Symlink(t.TempDir(), rootPath); err != nil {
			t.Fatal(err)
		}
		if done, code, err := server.cleanExportTemps(context.Background(), "default", 2); err == nil || done || code != "unsafe_private_root" {
			t.Fatalf("symlink cleanup completed=%t code=%q err=%v", done, code, err)
		}
	})

	t.Run("symlink artifact", func(t *testing.T) {
		store := exportTempTestStore(t)
		server, err := NewServer(store, Config{})
		if err != nil {
			t.Fatal(err)
		}
		addExportTempFixture(t, store, "symlink-artifact", "default", 1)
		rootPath := createExportTempFixture(t, store, "symlink-artifact")
		outside := filepath.Join(t.TempDir(), "outside.txt")
		if err = os.WriteFile(outside, []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
		if err = os.Remove(filepath.Join(rootPath, exportDataName("symlink-artifact"))); err != nil {
			t.Fatal(err)
		}
		if err = os.Symlink(outside, filepath.Join(rootPath, exportDataName("symlink-artifact"))); err != nil {
			t.Fatal(err)
		}
		if done, code, err := server.cleanExportTemps(context.Background(), "default", 2); err == nil || done || code != "unsafe_or_unremovable_artifact" {
			t.Fatalf("symlink artifact cleanup completed=%t code=%q err=%v", done, code, err)
		}
		if payload, err := os.ReadFile(outside); err != nil || string(payload) != "keep" {
			t.Fatalf("symlink target was changed: data=%q err=%v", payload, err)
		}
	})
}
