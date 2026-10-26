package storage

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func independentGuardResult(t *testing.T, s *Store) error {
	t.Helper()
	tx, err := s.DB().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	return CheckRestoreDeletionReconciliationTx(context.Background(), tx)
}
func TestIndependentRestoreGuardHandlesAliasesMarkersAndFreshStores(t *testing.T) {
	for _, kind := range []string{"fresh", "pending_alias", "marker_alias", "truncated_marker", "directory_marker", "dangling_marker", "no_instance_marker"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			actual := filepath.Join(root, "actual")
			alias := filepath.Join(root, "alias")
			if err := os.Mkdir(actual, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(alias, 0700); err != nil {
				t.Fatal(err)
			}
			database := filepath.Join(actual, "authority.db")
			openPath := database
			switch kind {
			case "pending_alias":
				openPath = filepath.Join(alias, "authority.restore-pending")
				s, err := Open(context.Background(), Config{Path: database})
				if err != nil {
					t.Fatal(err)
				}
				s.Close()
				if err = os.Symlink(database, openPath); err != nil {
					t.Fatal(err)
				}
			case "marker_alias":
				if err := os.WriteFile(filepath.Join(alias, "restore-state.json"), []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
				openPath = filepath.Join(alias, "authority.db")
				s, err := Open(context.Background(), Config{Path: database})
				if err != nil {
					t.Fatal(err)
				}
				s.Close()
				if err = os.Symlink(database, openPath); err != nil {
					t.Fatal(err)
				}
			case "truncated_marker":
				if err := os.WriteFile(filepath.Join(actual, "restore-state.json"), []byte(`{`), 0600); err != nil {
					t.Fatal(err)
				}
			case "directory_marker":
				if err := os.Mkdir(filepath.Join(actual, "restore-state.json"), 0700); err != nil {
					t.Fatal(err)
				}
			case "dangling_marker":
				if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(actual, "restore-state.json")); err != nil {
					t.Fatal(err)
				}
			case "no_instance_marker":
				if err := os.WriteFile(filepath.Join(actual, ".restore-pending"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			store, err := Open(context.Background(), Config{Path: openPath})
			if err != nil {
				if kind == "fresh" {
					t.Fatal(err)
				}
				return
			}
			defer store.Close()
			gateErr := independentGuardResult(t, store)
			if kind == "fresh" {
				if gateErr != nil {
					t.Fatal(gateErr)
				}
			} else if !errors.Is(gateErr, ErrRestoreDeletionReconciliationRequired) {
				t.Fatalf("staged alias/marker not closed: %v", gateErr)
			}
		})
	}
}
func TestIndependentMalformedRestoreGuardCannotAuthorizeRead(t *testing.T) {
	for _, setup := range []string{`CREATE TABLE cylo_restore_stage_guard(singleton INTEGER,pending INTEGER)`, `CREATE TABLE cylo_restore_stage_guard(singleton INTEGER,pending INTEGER); INSERT INTO cylo_restore_stage_guard VALUES(1,7)`, `CREATE TABLE cylo_restore_stage_guard(singleton INTEGER,pending TEXT); INSERT INTO cylo_restore_stage_guard VALUES(1,'bad')`, `CREATE TABLE cylo_restore_stage_guard(singleton INTEGER,pending INTEGER); INSERT INTO cylo_restore_stage_guard VALUES(1,NULL)`} {
		store, err := Open(context.Background(), Config{Path: filepath.Join(t.TempDir(), "authority.db")})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.DB().Exec(setup); err != nil {
			t.Fatal(err)
		}
		if err = independentGuardResult(t, store); err == nil {
			t.Fatal("malformed guard allowed authority read")
		}
		store.Close()
	}
}
func TestIndependentStagingMarkerMustOverridePriorFalseGuard(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "authority.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec(`CREATE TABLE cylo_restore_stage_guard(singleton INTEGER PRIMARY KEY,pending INTEGER NOT NULL);INSERT INTO cylo_restore_stage_guard VALUES(1,0)`); err != nil {
		t.Fatal(err)
	}
	database.Close()
	if err = os.WriteFile(filepath.Join(root, "restore-state.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(context.Background(), Config{Path: path})
	if err != nil {
		return
	}
	defer store.Close()
	if err = independentGuardResult(t, store); !errors.Is(err, ErrRestoreDeletionReconciliationRequired) {
		t.Fatalf("staging marker failed to close prior false guard: %v", err)
	}
}
