package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRestorePendingFilenamePersistsGuardAcrossRename(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	pending := filepath.Join(directory, "cylo.sqlite.restore-pending")
	store, err := Open(ctx, Config{Path: pending})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	activatedName := filepath.Join(directory, "cylo.sqlite")
	if err := os.Rename(pending, activatedName); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, Config{Path: activatedName})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := CheckRestoreDeletionReconciliationTx(ctx, tx); !errors.Is(err, ErrRestoreDeletionReconciliationRequired) {
		t.Fatalf("renamed staged database is not guarded: %v", err)
	}
}

func TestRestoreStateMarkerClosesDatabaseRegardlessOfContents(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "restore-state.json"), []byte(`{"restore_state":{"dispatch_enabled":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, Config{Path: filepath.Join(directory, "cylo.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := CheckRestoreDeletionReconciliationTx(ctx, tx); !errors.Is(err, ErrRestoreDeletionReconciliationRequired) {
		t.Fatalf("restore marker did not close database: %v", err)
	}
}
