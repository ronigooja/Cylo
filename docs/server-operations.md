# Control server operations

The Linux control server reads deployment settings from its process environment. Keep `CYLO_BOOTSTRAP_CODE` and `CYLO_RUNNER_TOKEN` in the deployment secret manager. The server never prints them. Bind to loopback or a private trusted interface and put external TLS termination behind the deployment's authenticated network boundary.

Required settings:

- `CYLO_DATABASE_PATH`: path to the SQLite database. The server acquires a single-writer process lock before opening it.
- `CYLO_BOOTSTRAP_CODE`: one-use bootstrap credential. Generate 32 random bytes and encode them with unpadded base64url.

Optional settings:

- `CYLO_LISTEN_ADDR`: defaults to `127.0.0.1:8787`.
- `CYLO_DEFAULT_PROFILE_ID`: the server-selected profile for new tasks.
- `CYLO_RUNTIME_PROFILES`: JSON object keyed by profile ID. Each profile sets `id`, `ao_project_id`, `runtime_route_id`, `model_route_id`, `role`, `ao_agent_kind` (`worker`), `isolation_policy`, `capability_ids`, and `scopes`. Client requests cannot override these execution routes or grants.
- `CYLO_RUNNER_TOKEN`: deployment-provisioned 32-byte base64url runner credential. When set, `CYLO_RUNNER_ID`, `CYLO_RUNNER_WORKSPACES`, and `CYLO_RUNNER_SCOPES` default to `configured-runner`, `default`, and `runner:claim,runner:observe,runner:control:read`.
- `CYLO_MEMORY_RUNNER_TOKEN_FILE`: optional path to a deployment-managed file containing a distinct 32-byte base64url memory worker credential. When enabled, `CYLO_MEMORY_RUNNER_WORKSPACES` must list exact workspace IDs; `CYLO_MEMORY_RUNNER_ID` defaults to `memory-worker`; and `CYLO_MEMORY_RUNNER_SCOPES` may only contain exactly `memory:jobs:claim,memory:jobs:complete,memory:propose`. The server stores only the credential hash and never logs the token. This credential exposes the durable derived-job API; it does not enable a Mem0 or Qdrant runtime.
- `CYLO_OWNER_SCOPES`: comma-separated owner/device grants. When omitted, the bootstrap owner receives the client, device-management, and personal/workspace memory read/write/delete scopes. Setting this value replaces the defaults exactly; it never adds a wildcard or restores omitted grants. Project/task memory scopes must name their exact scope IDs.

For example, a profile environment value can be supplied as JSON:

```json
{"standard":{"id":"standard","ao_project_id":"cylo-workers","runtime_route_id":"default-worker","model_route_id":"cliproxyapi","role":"worker","ao_agent_kind":"worker","isolation_policy":"workspace-sandbox-v1","capability_ids":[],"scopes":["task:read","task:write"]}}
```

The first device uses `POST /v1/bootstrap`; later devices use pairing and approval. Pairing grants devices the same owner scope set; configure `CYLO_OWNER_SCOPES` when a restricted device policy is needed. Clients read the durable inbox from `GET /v1/notifications` (`?unread=true` filters it), and mark an item read through the idempotent `notification.mark_read` command. Inbox entries link to their source event and task. If every approved device is lost, stop the service and run the local recovery command from the server host:

```sh
cylo-server recover-bootstrap --database /path/to/cylo.db --confirm-revoke-all-devices
```

The command requires the database lock, revokes all device credentials, requests stop for active runs, records an audit event, and prints one new bootstrap code after its transaction commits. Update the deployment-managed `CYLO_BOOTSTRAP_CODE` reference with that value before restarting the server, then bootstrap a replacement device. Active run capacity remains fenced until the runner supplies termination proof. Existing database state is preserved.

The process handles `SIGINT` and `SIGTERM` with a 20-second graceful shutdown window. SQLite is opened with WAL, `synchronous=FULL`, foreign keys, a busy timeout, and mode `0600` for the database and sidecars.
