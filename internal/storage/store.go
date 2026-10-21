package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	mu         sync.Mutex
	migrations map[int]Migration
}

func Open(ctx context.Context, cfg Config) (*Store, error) {
	if cfg.Path == "" {
		return nil, errors.New("database path is required")
	}
	if cfg.Path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(cfg.Path), 0o700); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{db: db, migrations: make(map[int]Migration)}
	if cfg.Path != ":memory:" {
		lock, err := os.OpenFile(cfg.Path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("open database lock: %w", err)
		}
		if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			_ = lock.Close()
			_ = db.Close()
			return nil, fmt.Errorf("database is already in use: %w", err)
		}
		store.lock = lock
	}
	for _, pragma := range []string{"PRAGMA foreign_keys=ON", "PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL", "PRAGMA busy_timeout=5000"} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			_ = store.Close()
			return nil, fmt.Errorf("configure sqlite (%s): %w", pragma, err)
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
