package storage

import (
	"context"
	"database/sql"
)

// RegisterRunBudgetMigrations registers the isolated run-budget ledger lane.
// Version 700 is globally unique in schema_migrations and leaves feature lanes
// 800+ available to the deletion/export migrations.
func RegisterRunBudgetMigrations(s *Store) error {
	return s.RegisterMigration(Migration{Namespace: "run_budget", Version: 700, Name: "idempotent_run_budget_ledger", Up: func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
CREATE TABLE run_budget_reservations (
  run_id TEXT NOT NULL REFERENCES runs(id),
  reservation_id TEXT NOT NULL,
  attempt_id TEXT NOT NULL,
  fence INTEGER NOT NULL,
  request_hash BLOB NOT NULL,
  reserved_json BLOB NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('reserved','unknown','settled','rejected')),
  actual_json BLOB NOT NULL DEFAULT '{}',
  settlement_id TEXT NOT NULL DEFAULT '',
  settlement_hash BLOB NOT NULL DEFAULT X'',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY(run_id,reservation_id)
);
CREATE INDEX run_budget_reservations_run_status ON run_budget_reservations(run_id,status);
`)
		return err
	}})
}
