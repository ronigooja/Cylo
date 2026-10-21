package storage

import (
	"context"
	"database/sql"
)

func RegisterCoreMigrations(s *Store) error {
	return s.RegisterMigration(Migration{Version: 1, Name: "core_authority", Up: func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
CREATE TABLE workspaces (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_at TEXT NOT NULL
);
INSERT INTO workspaces(id,name,created_at) VALUES('default','Default',strftime('%Y-%m-%dT%H:%M:%fZ','now'));
CREATE TABLE bootstrap_state (
  id INTEGER PRIMARY KEY CHECK(id=1),
  code_hash BLOB NOT NULL,
  consumed_by_device_id TEXT,
  consumed_at TEXT
);
CREATE TABLE devices (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  subject_id TEXT NOT NULL,
  name TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('pending','approved','revoked')),
  token_hash BLOB NOT NULL UNIQUE,
  version INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  approved_at TEXT,
  last_seen_at TEXT,
  pairing_expires_at TEXT,
  revoked_at TEXT
);
CREATE INDEX devices_subject ON devices(workspace_id,subject_id,status);
CREATE TABLE auth_requests (
  request_id TEXT PRIMARY KEY,
  kind TEXT NOT NULL CHECK(kind IN ('bootstrap','pairing')),
  fingerprint BLOB NOT NULL,
  token_hash BLOB NOT NULL,
  device_id TEXT NOT NULL REFERENCES devices(id),
  created_at TEXT NOT NULL
);
CREATE TABLE projects (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL CHECK(status IN ('active','archived')),
  version INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(workspace_id,id)
);
CREATE INDEX projects_workspace ON projects(workspace_id,created_at,id);
CREATE TABLE tasks (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  project_id TEXT,
  title TEXT NOT NULL,
  goal TEXT NOT NULL,
  source TEXT NOT NULL,
  profile_id TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('queued','active','waiting','awaiting_user','paused','complete','failed','canceled')),
  version INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  accepted_at TEXT,
  started_at TEXT,
  finished_at TEXT,
  result TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  created_by_device_id TEXT NOT NULL REFERENCES devices(id),
  command_id TEXT NOT NULL,
  FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id)
);
CREATE INDEX tasks_workspace ON tasks(workspace_id,created_at,id);
CREATE TABLE task_entries (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL REFERENCES tasks(id),
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  kind TEXT NOT NULL,
  content TEXT NOT NULL,
  author_device_id TEXT NOT NULL REFERENCES devices(id),
  created_at TEXT NOT NULL
);
CREATE TABLE approvals (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  task_id TEXT NOT NULL REFERENCES tasks(id),
  status TEXT NOT NULL CHECK(status IN ('pending','approved','rejected','expired','canceled')),
  prompt TEXT NOT NULL,
  requested_action BLOB,
  requested_action_digest TEXT NOT NULL DEFAULT '',
  version INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  expires_at TEXT,
  decided_at TEXT,
  decided_by_device_id TEXT REFERENCES devices(id)
);
CREATE INDEX approvals_workspace_status ON approvals(workspace_id,status,created_at);
CREATE TABLE command_receipts (
  command_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  subject_id TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  request_hash BLOB NOT NULL,
  receipt_json BLOB NOT NULL,
  accepted_at TEXT NOT NULL,
  UNIQUE(workspace_id,subject_id,idempotency_key)
);
CREATE TABLE command_work (
  command_id TEXT PRIMARY KEY REFERENCES command_receipts(command_id),
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  kind TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  completed_at TEXT
);
CREATE TABLE events (
  event_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  cursor INTEGER NOT NULL,
  schema_version INTEGER NOT NULL,
  event_type TEXT NOT NULL,
  occurred_at TEXT NOT NULL,
  subject_id TEXT,
  aggregate_type TEXT NOT NULL,
  aggregate_id TEXT NOT NULL,
  aggregate_version INTEGER NOT NULL,
  payload BLOB NOT NULL,
  delivered_at TEXT,
  UNIQUE(workspace_id,cursor)
);
CREATE TABLE event_counters (
  workspace_id TEXT PRIMARY KEY REFERENCES workspaces(id),
  current_cursor INTEGER NOT NULL DEFAULT 0
);
INSERT INTO event_counters(workspace_id,current_cursor) VALUES('default',0);
CREATE TABLE runs (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  task_id TEXT NOT NULL REFERENCES tasks(id),
  attempt_id TEXT NOT NULL UNIQUE,
  status TEXT NOT NULL CHECK(status IN ('queued','dispatching','active','uncertain','stopped','verification_pending','verified','failed')),
  desired_control TEXT NOT NULL DEFAULT 'run' CHECK(desired_control IN ('run','stop')),
  control_version INTEGER NOT NULL DEFAULT 1,
  fence INTEGER NOT NULL UNIQUE,
  runner_id TEXT NOT NULL DEFAULT '',
  authorized_device_id TEXT NOT NULL REFERENCES devices(id),
  command_id TEXT NOT NULL,
  goal TEXT NOT NULL,
  profile_id TEXT NOT NULL,
  runtime_route_id TEXT NOT NULL,
  model_route_id TEXT NOT NULL,
  role TEXT NOT NULL,
  ao_project_id TEXT NOT NULL DEFAULT '',
  ao_session_id TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL,
  stopped_at TEXT,
  last_observation_at TEXT
);
CREATE INDEX runs_capacity ON runs(status);
CREATE TABLE evidence (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  task_id TEXT NOT NULL REFERENCES tasks(id),
  run_id TEXT REFERENCES runs(id),
  kind TEXT NOT NULL,
  summary TEXT NOT NULL,
  source TEXT NOT NULL,
  provenance BLOB NOT NULL,
  created_at TEXT NOT NULL,
  verified INTEGER NOT NULL DEFAULT 0,
  verifier_id TEXT NOT NULL DEFAULT ''
);
CREATE TABLE runner_observations (
  observation_id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES runs(id),
  attempt_id TEXT NOT NULL,
  fence INTEGER NOT NULL,
  runner_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  occurred_at TEXT NOT NULL,
  body_hash BLOB NOT NULL,
  body BLOB NOT NULL
);
CREATE TABLE runner_credentials (
  token_hash BLOB PRIMARY KEY,
  runner_id TEXT NOT NULL,
  workspace_ids BLOB NOT NULL,
  scopes BLOB NOT NULL,
  revoked_at TEXT
);
`)
		return err
	}})
}
