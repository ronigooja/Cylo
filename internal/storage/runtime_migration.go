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
UPDATE runs SET runtime_session_id=ao_session_id WHERE runtime_session_id='';
UPDATE tasks SET harness='opencode' WHERE harness='' OR harness='codex';
UPDATE runs SET harness='opencode' WHERE harness='' OR harness='codex';
UPDATE runs SET isolation_policy='opencode-workspace-write-v1' WHERE isolation_policy='ao-opencode-workspace-write-v1';
CREATE TABLE runtime_harness_quarantine (
  entity_type TEXT NOT NULL,
  entity_id TEXT NOT NULL,
  harness TEXT NOT NULL,
  quarantined_at TEXT NOT NULL,
  PRIMARY KEY(entity_type, entity_id)
);
INSERT INTO runtime_harness_quarantine(entity_type,entity_id,harness,quarantined_at)
SELECT 'task',id,harness,strftime('%Y-%m-%dT%H:%M:%fZ','now') FROM tasks WHERE harness NOT IN ('opencode','')
ON CONFLICT(entity_type,entity_id) DO UPDATE SET harness=excluded.harness;
INSERT INTO runtime_harness_quarantine(entity_type,entity_id,harness,quarantined_at)
SELECT 'run',id,harness,strftime('%Y-%m-%dT%H:%M:%fZ','now') FROM runs WHERE harness NOT IN ('opencode','')
ON CONFLICT(entity_type,entity_id) DO UPDATE SET harness=excluded.harness;
`)
		return err
	}})
}
