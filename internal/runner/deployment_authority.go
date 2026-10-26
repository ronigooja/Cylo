package runner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	DeploymentHostLockPath  = "/run/lock/cylo-runner-deployments.lock"
	DeploymentAuthorityPath = "/var/lib/cylo/runner-authority/runner-deployment-authority.db"
)

// DeploymentAuthority is the trusted Docker-host-wide fence store. It must be
// opened from the fixed deployment authority path while the singleton host
// lock is held, so changing a runner journal cannot reset deployment fences.
type DeploymentAuthority struct {
	db      *sql.DB
	lock    *os.File
	locksMu sync.Mutex
	locks   map[string]chan struct{}
}

func OpenDeploymentAuthority(ctx context.Context, filename string) (*DeploymentAuthority, error) {
	if strings.TrimSpace(filename) == "" || filename == ":memory:" {
		return nil, errors.New("deployment authority requires a persistent host-wide file")
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return nil, fmt.Errorf("create deployment authority directory: %w", err)
	}
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return nil, err
	}
	if info, err := os.Lstat(absolute); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("deployment authority path must not be a symlink")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	file, err := os.OpenFile(absolute, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open deployment authority: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("deployment authority is already in use: %w", err)
	}
	db, err := sql.Open("sqlite", absolute)
	if err != nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{"PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL", "PRAGMA busy_timeout=5000"} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			_ = db.Close()
			_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
			_ = file.Close()
			return nil, fmt.Errorf("configure deployment authority: %w", err)
		}
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS deployment_fences (deployment_id TEXT PRIMARY KEY, fence INTEGER NOT NULL, fingerprint TEXT NOT NULL, effect_started INTEGER NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		_ = db.Close()
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
		return nil, err
	}
	if err := protectSQLiteFiles(absolute); err != nil {
		_ = db.Close()
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
		return nil, err
	}
	return &DeploymentAuthority{db: db, lock: file, locks: make(map[string]chan struct{})}, nil
}

func (a *DeploymentAuthority) Close() error {
	if a == nil || a.db == nil {
		return nil
	}
	err := a.db.Close()
	a.db = nil
	if a.lock != nil {
		_ = syscall.Flock(int(a.lock.Fd()), syscall.LOCK_UN)
		if closeErr := a.lock.Close(); err == nil {
			err = closeErr
		}
		a.lock = nil
	}
	return err
}

func (a *DeploymentAuthority) LockDeployment(ctx context.Context, id string) (func(), error) {
	if id == "" {
		return nil, errors.New("deployment id is required for effect lock")
	}
	a.locksMu.Lock()
	lock := a.locks[id]
	if lock == nil {
		lock = make(chan struct{}, 1)
		lock <- struct{}{}
		a.locks[id] = lock
	}
	a.locksMu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-lock:
		return func() { lock <- struct{}{} }, nil
	}
}

// BeginDeploymentEffect commits the host-wide monotonic fence and intent
// before Docker. A repeated exact claim returns replay=false so callers only
// inspect/reconcile its outcome and never repeat its write effects.
func (a *DeploymentAuthority) BeginDeploymentEffect(ctx context.Context, id string, fence int64, fingerprint string) (bool, error) {
	if id == "" || fence < 1 || fingerprint == "" {
		return false, errors.New("deployment fence reservation is incomplete")
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var prior int64
	var priorFingerprint string
	var started int
	err = tx.QueryRowContext(ctx, `SELECT fence,fingerprint,effect_started FROM deployment_fences WHERE deployment_id=?`, id).Scan(&prior, &priorFingerprint, &started)
	if err == nil {
		if fence < prior {
			return false, errors.New("deployment claim fence is stale")
		}
		if fence == prior && fingerprint != priorFingerprint {
			return false, errors.New("deployment claim changed under the same fence")
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if fence > prior {
		_, err = tx.ExecContext(ctx, `INSERT INTO deployment_fences(deployment_id,fence,fingerprint,effect_started,updated_at) VALUES(?,?,?,1,?) ON CONFLICT(deployment_id) DO UPDATE SET fence=excluded.fence,fingerprint=excluded.fingerprint,effect_started=1,updated_at=excluded.updated_at`, id, fence, fingerprint, time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			return false, err
		}
		started = 0
	}
	if started != 0 {
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}
	if fence == prior {
		_, err = tx.ExecContext(ctx, `UPDATE deployment_fences SET effect_started=1,updated_at=? WHERE deployment_id=?`, time.Now().UTC().Format(time.RFC3339Nano), id)
		if err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
