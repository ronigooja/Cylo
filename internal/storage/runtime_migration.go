package storage

import (
	"context"
	"database/sql"
)

func RegisterRuntimeMigration(s *Store) error {
	return s.RegisterMigration(Migration{Namespace: "core", Version: 12, Name: "opencode_runtime_identity", Up: func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
ALTER TABLE runs ADD COLUMN runtime_session_id TEXT NOT NULL DEFAULT '';
ALTER TABLE runs ADD COLUMN runtime_container_id TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN runtime_harness TEXT NOT NULL DEFAULT 'opencode';
UPDATE runs SET runtime_session_id=ao_session_id WHERE runtime_session_id='';
UPDATE tasks SET runtime_harness='opencode' WHERE runtime_harness='' OR runtime_harness='codex';
UPDATE runs SET harness='opencode' WHERE harness='' OR harness='codex';
UPDATE runs SET isolation_policy='opencode-workspace-write-v1' WHERE isolation_policy='ao-opencode-workspace-write-v1';
`)
		return err
	}})
}
