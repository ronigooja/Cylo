package storage

import (
	"context"
	"database/sql"
)

// RegisterInstanceAuthMigration creates the single-instance credential record.
// The secret is provisioned by the server process and only its SHA-256 digest
// is stored in SQLite.
func RegisterInstanceAuthMigration(s *Store) error {
	return s.RegisterMigration(Migration{Namespace: "instance_auth", Version: 9, Name: "single_instance_bearer", Up: func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
CREATE TABLE instance_auth (
  singleton INTEGER PRIMARY KEY CHECK(singleton=1),
  key_hash BLOB NOT NULL CHECK(length(key_hash)=32),
  generation INTEGER NOT NULL DEFAULT 1 CHECK(generation>0),
  created_at TEXT NOT NULL,
  rotated_at TEXT NOT NULL
);`)
		return err
	}})
}
