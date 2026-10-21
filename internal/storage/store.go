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
	"strings"
	"sync"
	"syscall"

	_ "modernc.org/sqlite"
)

type Config struct {
	Path string
}

type Migration struct {
	Namespace string
	Version   int
	Name      string
	Up        func(context.Context, *sql.Tx) error
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
	if strings.TrimSpace(m.Namespace) == "" || m.Version < 1 || m.Name == "" || m.Up == nil {
		return errors.New("migration requires namespace, positive version, name, and Up function")
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
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL, namespace TEXT NOT NULL DEFAULT '')`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	columns, err := s.db.QueryContext(ctx, `PRAGMA table_info(schema_migrations)`)
	if err != nil {
		return fmt.Errorf("inspect migration table: %w", err)
	}
	hasNamespace := false
	for columns.Next() {
		var cid, notNull, primaryKey int
		var name, kind string
		var defaultValue any
		if err = columns.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			columns.Close()
			return fmt.Errorf("inspect migration table columns: %w", err)
		}
		if name == "namespace" {
			hasNamespace = true
		}
	}
	if err = columns.Err(); err != nil {
		columns.Close()
		return fmt.Errorf("inspect migration table columns: %w", err)
	}
	if err = columns.Close(); err != nil {
		return err
	}
	if !hasNamespace {
		if _, err = s.db.ExecContext(ctx, `ALTER TABLE schema_migrations ADD COLUMN namespace TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add migration namespace: %w", err)
		}
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
	type appliedMigration struct {
		version       int
		namespace     string
		name          string
		needsBackfill bool
	}
	rows, err := s.db.QueryContext(ctx, `SELECT version,name,namespace FROM schema_migrations ORDER BY version`)
	if err != nil {
		return fmt.Errorf("read migration history: %w", err)
	}
	applied := make(map[int]appliedMigration)
	for rows.Next() {
		var record appliedMigration
		if err = rows.Scan(&record.version, &record.name, &record.namespace); err != nil {
			rows.Close()
			return fmt.Errorf("read migration history: %w", err)
		}
		migration, ok := migrations[record.version]
		if !ok {
			rows.Close()
			return fmt.Errorf("database migration %d is newer than or unknown to this binary", record.version)
		}
		if record.name != migration.Name {
			rows.Close()
			return fmt.Errorf("migration %d name mismatch: database=%q binary=%q", record.version, record.name, migration.Name)
		}
		if record.namespace != "" && record.namespace != migration.Namespace {
			rows.Close()
			return fmt.Errorf("migration %d namespace mismatch: database=%q binary=%q", record.version, record.namespace, migration.Namespace)
		}
		if record.namespace == "" {
			record.needsBackfill = true
			record.namespace = migration.Namespace
		}
		applied[record.version] = record
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read migration history: %w", err)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	maxApplied := make(map[string]int)
	for _, record := range applied {
		if record.version > maxApplied[record.namespace] {
			maxApplied[record.namespace] = record.version
		}
	}
	for version, migration := range migrations {
		if version <= maxApplied[migration.Namespace] {
			if _, ok := applied[version]; !ok {
				return fmt.Errorf("database migration history has a gap in namespace %q at version %d", migration.Namespace, version)
			}
		}
	}
	needsBackfill := false
	for _, record := range applied {
		if record.needsBackfill {
			needsBackfill = true
			break
		}
	}
	if needsBackfill {
		tx, txErr := s.db.BeginTx(ctx, nil)
		if txErr != nil {
			return fmt.Errorf("begin migration namespace backfill: %w", txErr)
		}
		for version, record := range applied {
			if !record.needsBackfill {
				continue
			}
			if _, txErr = tx.ExecContext(ctx, `UPDATE schema_migrations SET namespace=? WHERE version=? AND namespace=''`, record.namespace, version); txErr != nil {
				_ = tx.Rollback()
				return fmt.Errorf("backfill migration namespace: %w", txErr)
			}
		}
		if txErr = tx.Commit(); txErr != nil {
			return fmt.Errorf("commit migration namespace backfill: %w", txErr)
		}
	}
	sort.Ints(versions)
	for _, version := range versions {
		if _, ok := applied[version]; ok {
			continue
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", version, err)
		}
		if err := migrations[version].Up(ctx, tx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,name,applied_at,namespace) VALUES(?,?,strftime('%Y-%m-%dT%H:%M:%fZ','now'),?)`, version, migrations[version].Name, migrations[version].Namespace); err != nil {
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
