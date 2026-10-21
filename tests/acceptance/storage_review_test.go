package acceptance

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ronigooja/Cylo/internal/storage"
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
	if e := s.RegisterMigration(storage.Migration{Version: 1, Name: "broken", Up: func(ctx context.Context, tx *sql.Tx) error {
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
	if _, e := s.DB().ExecContext(ctx, "INSERT INTO schema_migrations VALUES(999,'future','now')"); e != nil {
		t.Fatal(e)
	}
	if e := s.Migrate(ctx); e == nil {
		t.Error("old binary accepts unknown future schema")
	}
}
