package storage

import (
	"context"
	"database/sql"
)

// Owner credentials are separate from historical device approval. Migrating an
// installation never promotes a legacy bearer into password setup authority.
func RegisterOwnerAuthMigration(s *Store) error {
	return s.RegisterMigration(Migration{Namespace: "owner_auth", Version: 8, Name: "single_owner_password", Up: func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
 CREATE TABLE owner_auth (
 id INTEGER PRIMARY KEY CHECK(id=1),
 subject_id TEXT NOT NULL DEFAULT '',
 password_salt BLOB,
 password_hash BLOB,
 password_iterations INTEGER NOT NULL DEFAULT 600000,
 generation INTEGER NOT NULL DEFAULT 1,
 failed_attempts INTEGER NOT NULL DEFAULT 0,
 blocked_until INTEGER NOT NULL DEFAULT 0
 );
 INSERT INTO owner_auth(id,subject_id) VALUES(1,coalesce(
 (SELECT d.subject_id FROM bootstrap_state b JOIN devices d ON d.id=b.consumed_by_device_id WHERE b.id=1),
 (SELECT subject_id FROM devices WHERE workspace_id='default' ORDER BY created_at,id LIMIT 1),''));
 UPDATE tasks SET status=CASE WHEN status='queued' THEN 'canceled' ELSE status END,version=version+1,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now'),finished_at=CASE WHEN status='queued' THEN strftime('%Y-%m-%dT%H:%M:%fZ','now') ELSE finished_at END WHERE status IN ('queued','active','waiting','awaiting_user','paused');
 UPDATE runs SET desired_control='stop',control_version=control_version+1 WHERE status IN ('dispatching','active','uncertain') AND desired_control!='stop';
 UPDATE devices SET status='revoked',version=version+1,revoked_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE status!='revoked';
 ALTER TABLE devices ADD COLUMN owner_generation INTEGER;
 CREATE TABLE owner_auth_requests (
 request_id TEXT PRIMARY KEY,
 kind TEXT NOT NULL CHECK(kind IN ('setup','login')),
 fingerprint BLOB NOT NULL,
 device_id TEXT NOT NULL REFERENCES devices(id),
 generation INTEGER NOT NULL,
 created_at TEXT NOT NULL
 );`)
		return err
	}})
}
