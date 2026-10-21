package capabilities

import (
	"context"
	"database/sql"

	"github.com/ronigooja/Cylo/internal/storage"
)

func RegisterMigrations(store *storage.Store) error {
	return store.RegisterMigration(storage.Migration{Version: 300, Name: "capability_releases_and_deployments_v1", Up: func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
CREATE TABLE capability_releases (
  release_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  digest TEXT NOT NULL,
  manifest BLOB NOT NULL,
  submitted_by TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(workspace_id,digest)
);
CREATE TABLE capability_deployments (
  deployment_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  service_id TEXT NOT NULL,
  release_digest TEXT NOT NULL,
  requested_operation TEXT NOT NULL CHECK(requested_operation IN ('install','start','stop','pause','resume','update','uninstall')),
  desired_state TEXT NOT NULL CHECK(desired_state IN ('awaiting_authorization','installing','running','stopped','paused','updating','removing')),
  observed_state TEXT NOT NULL CHECK(observed_state IN ('unknown','pending','installing','running','stopped','paused','failed','uninstalling','removed','uncertain')),
  grant_id TEXT NOT NULL DEFAULT '',
  config_ref TEXT NOT NULL DEFAULT '',
  config_sha256 TEXT NOT NULL DEFAULT '',
  version INTEGER NOT NULL DEFAULT 1,
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  latest_attempt_id TEXT NOT NULL DEFAULT '',
  latest_fence INTEGER NOT NULL DEFAULT 0,
  last_error_code TEXT NOT NULL DEFAULT '',
  UNIQUE(workspace_id,service_id)
);
CREATE TABLE capability_grants (
  grant_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  deployment_id TEXT NOT NULL REFERENCES capability_deployments(deployment_id),
  release_digest TEXT NOT NULL,
  scope_kind TEXT NOT NULL,
  scope_id TEXT NOT NULL DEFAULT '',
  principal_id TEXT NOT NULL DEFAULT '',
  actions BLOB NOT NULL,
  budget BLOB NOT NULL,
  expires_at TEXT NOT NULL,
  authorized_by TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE capability_deployment_jobs (
  attempt_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  deployment_id TEXT NOT NULL REFERENCES capability_deployments(deployment_id),
  fence INTEGER NOT NULL,
  operation TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('pending','claimed','observed','uncertain','failed')),
  runner_id TEXT NOT NULL DEFAULT '',
  claimed_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(deployment_id,fence)
);
CREATE TABLE capability_observations (
  observation_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  deployment_id TEXT NOT NULL REFERENCES capability_deployments(deployment_id),
  attempt_id TEXT NOT NULL REFERENCES capability_deployment_jobs(attempt_id),
  fence INTEGER NOT NULL,
  body BLOB NOT NULL,
  observed_at TEXT NOT NULL,
  UNIQUE(deployment_id,attempt_id,fence,observation_id)
);
CREATE INDEX capability_jobs_ready ON capability_deployment_jobs(status,created_at);
CREATE INDEX capability_deployments_workspace ON capability_deployments(workspace_id,updated_at);
`)
		return err
	}})
}
