package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"

	_ "modernc.org/sqlite"
)

type Config struct {
	Path string
}

type Migration struct {
	Version int
	Name    string
	Up      func(context.Context, *sql.Tx) error
}

type Store struct {
	db         *sql.DB
	lock       *os.File
	path       string
	mu         sync.Mutex
	migrations map[int]Migration
}

func Open(ctx context.Context, cfg Config) (*Store, error) {
	if cfg.Path == "" {
		return nil, errors.New("database path is required")
	}
	path := cfg.Path
	var lock *os.File
	if path != ":memory:" {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve database path: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
		parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
		if err != nil {
			return nil, fmt.Errorf("resolve database directory: %w", err)
		}
		path = filepath.Join(parent, filepath.Base(absolute))
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = resolved
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("resolve database file: %w", err)
		}
		lock, err = os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, fmt.Errorf("open database file: %w", err)
		}
		info, err := lock.Stat()
		if err != nil {
			_ = lock.Close()
			return nil, fmt.Errorf("stat database file: %w", err)
		}
		if !info.Mode().IsRegular() {
			_ = lock.Close()
			return nil, errors.New("database path must be a regular file")
		}
		if err := lock.Chmod(0o600); err != nil {
			_ = lock.Close()
			return nil, fmt.Errorf("protect database file: %w", err)
		}
		if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			_ = lock.Close()
			return nil, fmt.Errorf("database is already in use: %w", err)
		}
	}
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		if lock != nil {
			_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
			_ = lock.Close()
		}
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{db: db, lock: lock, path: path, migrations: make(map[int]Migration)}
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode=WAL`); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("enable sqlite WAL: %w", err)
	}
	for _, sidecar := range []string{path + "-wal", path + "-shm"} {
		if err := os.Chmod(sidecar, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
			_ = store.Close()
			return nil, fmt.Errorf("protect sqlite sidecar: %w", err)
		}
	}
	return store, nil
}

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) Close() error {
	var errs []error
	if s.db != nil {
		if err := s.db.Close(); err != nil {
			errs = append(errs, err)
		}
		s.db = nil
	}
	if s.lock != nil {
		_ = syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
		if err := s.lock.Close(); err != nil {
			errs = append(errs, err)
		}
		s.lock = nil
	}
	return errors.Join(errs...)
}

func (s *Store) RegisterMigration(m Migration) error {
	if m.Version < 1 || m.Name == "" || m.Up == nil {
		return errors.New("migration requires positive version, name, and Up function")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.migrations[m.Version]; exists {
		return fmt.Errorf("migration version %d already registered", m.Version)
	}
	s.migrations[m.Version] = m
	return nil
}

func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	var dbMax int
	if err := s.db.QueryRowContext(ctx, `SELECT coalesce(max(version),0) FROM schema_migrations`).Scan(&dbMax); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	s.mu.Lock()
	versions := make([]int, 0, len(s.migrations))
	for v := range s.migrations {
		versions = append(versions, v)
	}
	migrations := make(map[int]Migration, len(s.migrations))
	for v, m := range s.migrations {
		migrations[v] = m
	}
	s.mu.Unlock()
	if dbMax > 0 {
		if _, ok := migrations[dbMax]; !ok {
			return fmt.Errorf("database schema version %d is newer than this binary supports", dbMax)
		}
	}
	for version := range migrations {
		if version <= dbMax {
			var found int
			if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE version=?`, version).Scan(&found); err != nil {
				return err
			}
			if found != 1 {
				return fmt.Errorf("database migration history has a gap at version %d", version)
			}
		}
	}
	sort.Ints(versions)
	for _, version := range versions {
		var applied string
		err := s.db.QueryRowContext(ctx, `SELECT name FROM schema_migrations WHERE version=?`, version).Scan(&applied)
		if err == nil {
			if applied != migrations[version].Name {
				return fmt.Errorf("migration %d name mismatch: database=%q binary=%q", version, applied, migrations[version].Name)
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("read migration %d: %w", version, err)
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", version, err)
		}
		if err := migrations[version].Up(ctx, tx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,strftime('%Y-%m-%dT%H:%M:%fZ','now'))`, version, migrations[version].Name); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", version, err)
		}
	}
	return nil
}

func sqliteDSN(path string) string {
	if path == ":memory:" {
		path = "file:cylo-memory?mode=memory&cache=shared"
	}
	u, err := url.Parse(path)
	if err != nil || u.Scheme == "" {
		u = &url.URL{Scheme: "file", Path: path}
	}
	q := u.Query()
	for _, pragma := range []string{"foreign_keys(ON)", "synchronous(FULL)", "busy_timeout(5000)"} {
		q.Add("_pragma", pragma)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
