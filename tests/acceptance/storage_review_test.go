package acceptance

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/internal/memory"
	"github.com/ronigooja/Cylo/internal/storage"
	"github.com/ronigooja/Cylo/internal/voice"
)

func openStore(t *testing.T, path string) *storage.Store {
	t.Helper()
	s, e := storage.Open(context.Background(), storage.Config{Path: path})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func TestReviewPragmasAndReconnect(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "authority.db"))
	ctx := context.Background()
	check := func() {
		t.Helper()
		for p, w := range map[string]string{"journal_mode": "wal", "synchronous": "2", "foreign_keys": "1", "busy_timeout": "5000"} {
			var got string
			if e := s.DB().QueryRowContext(ctx, "PRAGMA "+p).Scan(&got); e != nil {
				t.Fatal(e)
			}
			if got != w {
				t.Errorf("%s=%s want %s", p, got, w)
			}
		}
	}
	check()
	s.DB().SetConnMaxLifetime(time.Nanosecond)
	time.Sleep(time.Millisecond)
	check()
}
func TestReviewLockAliases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "authority.db")
	openStore(t, path)
	if other, e := storage.Open(context.Background(), storage.Config{Path: path}); e == nil {
		other.Close()
		t.Error("same path accepted twice")
	}
	alias := filepath.Join(dir, "alias.db")
	if e := os.Symlink(path, alias); e != nil {
		t.Fatal(e)
	}
	if other, e := storage.Open(context.Background(), storage.Config{Path: alias}); e == nil {
		other.Close()
		t.Error("symlink alias bypasses active database lock")
	}
}
func TestReviewFilePermissions(t *testing.T) {
	dir := t.TempDir()
	if e := os.Chmod(dir, 0755); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "authority.db")
	s := openStore(t, path)
	if e := storage.RegisterCoreMigrations(s); e != nil {
		t.Fatal(e)
	}
	if e := s.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		info, e := os.Stat(path + suffix)
		if e != nil {
			t.Fatal(e)
		}
		if info.Mode().Perm()&0077 != 0 {
			t.Errorf("%s mode=%04o exposes authority data to other users", suffix, info.Mode().Perm())
		}
	}
}
func TestReviewMigrationAtomicity(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "authority.db"))
	ctx := context.Background()
	boom := errors.New("injected failure")
	if e := s.RegisterMigration(storage.Migration{Namespace: "test", Version: 1, Name: "broken", Up: func(ctx context.Context, tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx, "CREATE TABLE partial(id INTEGER)"); e != nil {
			return e
		}
		return boom
	}}); e != nil {
		t.Fatal(e)
	}
	if e := s.Migrate(ctx); !errors.Is(e, boom) {
		t.Fatalf("want injected failure: %v", e)
	}
	var count int
	if e := s.DB().QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE name='partial'").Scan(&count); e != nil {
		t.Fatal(e)
	}
	if count != 0 {
		t.Error("partial migration persisted")
	}
	if e := s.DB().QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations").Scan(&count); e != nil {
		t.Fatal(e)
	}
	if count != 0 {
		t.Error("failed migration marked applied")
	}
}
func TestReviewRejectFutureSchema(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "authority.db"))
	ctx := context.Background()
	if e := storage.RegisterCoreMigrations(s); e != nil {
		t.Fatal(e)
	}
	if e := s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := s.DB().ExecContext(ctx, "INSERT INTO schema_migrations(version,name,applied_at) VALUES(999,'future','now')"); e != nil {
		t.Fatal(e)
	}
	if e := s.Migrate(ctx); e == nil {
		t.Error("old binary accepts unknown future schema")
	}
}

func TestReviewLateCoreMigrationAfterFeatureHistory(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "feature-history.db"))
	ctx := context.Background()
	for _, register := range []func(*storage.Store) error{storage.RegisterCoreMigrations, memory.RegisterMigrations, voice.RegisterMigrations} {
		if err := register(s); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	// Recreate the valid pre-v3 database history: core 1/2 and feature 100/200/201
	// have already run, but the newly added core isolation snapshot has not.
	if _, err := s.DB().ExecContext(ctx, `ALTER TABLE runs DROP COLUMN isolation_policy`); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{`DROP INDEX push_deliveries_token`, `DROP INDEX push_deliveries_due`, `DROP INDEX device_push_tokens_subject`, `DROP INDEX device_push_tokens_active_device_platform`, `DROP TABLE push_deliveries`, `DROP TABLE device_push_tokens`} {
		if _, err := s.DB().ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.DB().ExecContext(ctx, `DELETE FROM schema_migrations WHERE version=3`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `DELETE FROM schema_migrations WHERE version=4`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `CREATE TABLE schema_migrations_legacy (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO schema_migrations_legacy(version,name,applied_at) SELECT version,name,applied_at FROM schema_migrations`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `DROP TABLE schema_migrations`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `ALTER TABLE schema_migrations_legacy RENAME TO schema_migrations`); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("new core migration was blocked by applied feature migrations: %v", err)
	}
	var name, namespace string
	if err := s.DB().QueryRowContext(ctx, `SELECT name,namespace FROM schema_migrations WHERE version=3`).Scan(&name, &namespace); err != nil {
		t.Fatal(err)
	}
	if name != "run_isolation_policy_snapshot" || namespace != "core" {
		t.Fatalf("late migration identity=%q/%q", namespace, name)
	}
	var featureRows int
	if err := s.DB().QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE version IN (100,200,201)`).Scan(&featureRows); err != nil {
		t.Fatal(err)
	}
	if featureRows != 3 {
		t.Fatalf("feature migrations were lost or rerun: %d records", featureRows)
	}
	var blankNamespaces int
	if err := s.DB().QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE namespace=''`).Scan(&blankNamespaces); err != nil {
		t.Fatal(err)
	}
	if blankNamespaces != 0 {
		t.Fatalf("valid legacy migration history was not backfilled: %d rows remain blank", blankNamespaces)
	}
}

func TestReviewMigrationHistoryRequiresKnownNamespaceSequence(t *testing.T) {
	for _, tc := range []struct {
		name         string
		breakHistory string
		want         string
	}{
		{name: "missing earlier migration", breakHistory: `DELETE FROM schema_migrations WHERE version=1`, want: `gap in namespace "core" at version 1`},
		{name: "wrong namespace", breakHistory: `UPDATE schema_migrations SET namespace='memory' WHERE version=1`, want: `namespace mismatch`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openStore(t, filepath.Join(t.TempDir(), "namespace-history.db"))
			ctx := context.Background()
			if err := storage.RegisterCoreMigrations(s); err != nil {
				t.Fatal(err)
			}
			if err := s.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := s.DB().ExecContext(ctx, tc.breakHistory); err != nil {
				t.Fatal(err)
			}
			if err := s.Migrate(ctx); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("migration history should fail with %q, got %v", tc.want, err)
			}
		})
	}
}

func TestReviewLegacyNamespaceBackfillRollsBackOnHistoryGap(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "legacy-gap.db"))
	ctx := context.Background()
	if err := storage.RegisterCoreMigrations(s); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `DELETE FROM schema_migrations WHERE version=2`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `CREATE TABLE schema_migrations_legacy (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO schema_migrations_legacy(version,name,applied_at) SELECT version,name,applied_at FROM schema_migrations`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `DROP TABLE schema_migrations`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `ALTER TABLE schema_migrations_legacy RENAME TO schema_migrations`); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err == nil || !strings.Contains(err.Error(), `gap in namespace "core" at version 2`) {
		t.Fatalf("migration should reject the legacy gap, got %v", err)
	}
	var blank, named int
	if err := s.DB().QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE namespace=''`).Scan(&blank); err != nil {
		t.Fatal(err)
	}
	if err := s.DB().QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE namespace<>''`).Scan(&named); err != nil {
		t.Fatal(err)
	}
	if blank != 2 || named != 0 {
		t.Fatalf("failed validation partially backfilled legacy history: blank=%d named=%d", blank, named)
	}
}
