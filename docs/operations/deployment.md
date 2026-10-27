# Deployment operations

This runbook describes the local Linux installation and the macOS client on
`zhuwei@macair`. Deployment evidence and acceptance limits belong in the
[evidence register](../acceptance/evidence.md). Starting services is not end-to-end product
acceptance.

## Linux services and storage

- `/opt/cylo/releases/current` selects the immutable server/runner release.
- `cylo-compose.service` supervises the server, memory worker, and Qdrant.
- `cylo-ao.service` runs the separately managed, Cylo-patched AO daemon.
- `cylo-runner.service` runs the trusted execution runner.
- `cylo-macair-tunnel.service` maintains private SSH forwarding to the Mac.
- `/etc/cylo` contains deployment configuration and secret references.
- `/var/lib/cylo` contains authority databases and persistent service data.

The external Docker network `cylo` is prepared by
`deploy/linux/prepare-host-layout.sh`. The existing remote CLIProxyAPI
deployment remains outside Cylo's lifecycle. Preserve the existing runner
deployment-authority database and capability containers during maintenance.

Check service status without printing environment files or secret values:

```bash
systemctl is-active cylo-compose cylo-ao cylo-runner cylo-macair-tunnel
curl --fail http://127.0.0.1:8787/healthz
curl --fail http://127.0.0.1:18001/readyz
```

The previously recorded installation binds port 8787 on both host loopback and the authorized
LAN interface. The LAN endpoint is `http://debian.local:8787`.
The deployment now resolves `debian.local` at each `cylo-compose` systemd start using
`/etc/cylo/resolve-server-address.py` (source: `deploy/linux/resolve-server-address.py`).
The resolver requires one local IPv4 match and updates `CYLO_SERVER_LAN_ADDRESS`
before Compose reads the environment. Docker receives the resolved numeric IP.
An address change while the service is already running requires restarting the
Cylo service to resolve and republish it.
The deployment override `/etc/cylo/control.lan.compose.yaml` adds this binding;
`/etc/systemd/system/cylo-compose.service.d/lan.conf` includes the override in
the service's Compose commands. The source template is
`deploy/linux/docker-compose.lan.yml`; set `CYLO_SERVER_LAN_ADDRESS` to the
host LAN address. Update that setting if the host address changes.
AO binds host loopback port
18001; its listener uses the trusted local-process boundary and does not
enforce bearer authentication. Health/readiness responses do not prove model
execution, memory processing, or task completion.

## Installation and configuration

Use the [Linux deployment templates](../../deploy/linux/) and [release workflow](../../.github/workflows/server-release.yml) for the server/runner release. Prepare the host layout with [prepare-host-layout.sh](../../deploy/linux/prepare-host-layout.sh) before selecting a reviewed immutable release. Use GitHub-hosted runners for builds and prepare build tools under `$HOME/environment`; do not compile on the control host.

The operator release installation procedure below installs only the maintenance CLI. It does not install server images or configure a complete deployment. Run `cylo status` before `cylo setup`, `cylo provider configure`, or `cylo provider test`; these operations are also available from the interactive menu. Follow [the CLI implementation](../../cmd/cylo/) for current flags and [deployment configuration](../../internal/ops/deployment_config.go) for detection and rollback behavior.

## Server and worker settings

The Linux control server reads deployment settings from its process environment. Keep instance and runner credentials in deployment-managed secret configuration; the server does not print them. Bind to loopback or a private trusted interface and put external TLS termination behind the deployment's authenticated network boundary.

The Cylo-pinned AO source patch is applied to a separately managed AO checkout; this repository does not provide an AO daemon unit or environment template. The AO daemon requires `AO_CODEX_EXTERNAL_BASE_URL`, `AO_CODEX_EXTERNAL_API_KEY_FILE`, and `AO_CODEX_EXTERNAL_MODEL` together to configure the external Codex Responses route. The selected model is required and requests must match it. Both the AO daemon and Cylo runner require the same `AO_CODEX_CONTAINER_IMAGE` value for the supervised image. The URL, key-file path, and model belong only to the separately managed AO daemon; the image setting belongs to both deployments, not the Cylo server. The [patch helper](../../deploy/linux/apply-ao-patch.sh) and its locked source/tree fingerprints define the supported AO revision; do not replace those pins from historical notes.

Required settings:

- `CYLO_DATABASE_PATH`: path to the SQLite database. The server acquires a single-writer process lock before opening it.
- Initialize the database with `cylo key init --database PATH --server https://... [--output PRIVATE_JSON]`. The command displays the key and installation link once (and renders a terminal QR using its built-in encoder). `CYLO_BOOTSTRAP_CODE` is removed; use a fresh database for this deployment.

Optional settings:

- `CYLO_LISTEN_ADDR`: defaults to `127.0.0.1:8787`.
- `CYLO_DEFAULT_PROFILE_ID`: the server-selected profile for new tasks.
- `CYLO_RUNTIME_PROFILES`: JSON object keyed by profile ID. Each profile sets `id`, `ao_project_id`, `runtime_route_id`, `model_route_id`, `role`, `ao_agent_kind` (`worker`), `isolation_policy`, `capability_ids`, and `scopes`. Client requests cannot override these execution routes or grants.
- `CYLO_RUNNER_TOKEN`: deployment-provisioned 32-byte base64url runner credential. When set, `CYLO_RUNNER_ID`, `CYLO_RUNNER_WORKSPACES`, and `CYLO_RUNNER_SCOPES` default to `configured-runner`, `default`, and `runner:claim,runner:observe,runner:control:read`.
- `CYLO_MEMORY_RUNNER_TOKEN_FILE`: optional path to a deployment-managed file containing a distinct 32-byte base64url memory worker credential. When enabled, `CYLO_MEMORY_RUNNER_WORKSPACES` must list exact workspace IDs; `CYLO_MEMORY_RUNNER_ID` defaults to `memory-worker`; and `CYLO_MEMORY_RUNNER_SCOPES` may only contain exactly `memory:jobs:claim,memory:jobs:complete,memory:propose`. The server stores only the credential hash and never logs the token. This credential exposes the durable derived-job API; it does not enable a Mem0 or Qdrant runtime.
- Memory-worker deployment also requires `CYLO_CONTROL_URL`, the scoped worker credential injected as `CYLO_RUNNER_TOKEN` (provisioned for the server through `CYLO_MEMORY_RUNNER_TOKEN_FILE`), `CYLO_MEM0_LLM_MODEL`, `CYLO_MEM0_LLM_BASE_URL`, and `CYLO_MEM0_LLM_API_KEY` from deployment-managed secret references. `CYLO_MEM0_LLM_PROVIDER` must use the accounted `openai` path; `ollama` is rejected until it supports the same accounting boundary; `CYLO_MEM0_HISTORY_DB` and the model cache belong on the private persistent memory volume. Qdrant URL, optional API-key reference, collection/codec, and worker batch/poll settings are deployment configuration. The pinned multilingual embedding revision and dimensions are defined in [worker configuration](../../services/memory/config.py); changing them requires a separate collection and rebuild.
- For local execution, start the worker from the repository root with `python -m services.memory.worker`, or deploy reviewed CI-built worker images with the settings above. The Compose Qdrant service is loopback-bound and disposable; it is not a production persistence configuration. These commands describe source setup and do not establish production installation or recovery.
- `CYLO_INSTANCE_SCOPES`: comma-separated fixed-owner grants. An explicit value replaces the defaults exactly; it never adds a wildcard or restores omitted grants. Device approval/revocation scopes are not part of the client model. Project/task memory scopes must name their exact scope IDs.

## Fresh-instance operator commands

Stop the server before `cylo key init --database PATH --output PRIVATE_JSON --server URL` or `cylo key rotate --database PATH --output NEW_PRIVATE_JSON --server URL`. Output files must be new private destinations. Update or remove any stale deployment instance-key secret after rotation. Keep generated keys and installation links out of logs and chat.

Data export and destruction procedures are in [data lifecycle operations](data-lifecycle.md).

## Unknown AO creation reconciliation

Unknown create outcomes keep their execution slots and must never be retried as
new AO creates. The operator command changes authority only through the normal
runner observation API; it does not delete journal rows, task/run history, or
container records.

Use a runner built from the same reviewed source as the control server and the
patched AO gateway. Stop `cylo-runner` first so the maintenance command can take
the deployment-host and journal locks. Restart AO after stopping the runner to
drain the original create connections. Reconciliation requires a direct AO
daemon connection without a proxy that buffers or retries create requests. The
complete inventory must report a server start time later than the journal's unknown-create update. Keep the runner
stopped until reconciliation finishes. Run the command under the existing
runner service account and deployment environment/secret-entrypoint boundary:

```sh
cylo-runner --reconcile-runs RUN_ID --actor OPERATOR --confirm-no-effect
```

Supply exact run IDs and a non-secret operator identity. Run each ID separately
if every refusal must be inspected: a multi-ID invocation stops at the first
failure. The command checks the original journal claim, current server claim,
instance fence, complete AO inventory and `clientRequestId` absence, and a
complete Docker inventory. Missing identities, unavailable evidence, running or
previously started AO containers, and ambiguous managed containers cause refusal.
Do not remove a container or edit a database to force a check to pass.

An operator may explicitly select an existing AO record for preservation with
repeatable `--retain-never-started-container FULL_CONTAINER_ID`. Every selected
record must have complete AO ownership, `created` state, PID zero, no process
start/finish, no restart policy or restart history, and no exit/OOM/error facts.
Two complete Docker inspections must have identical identities and contents.
The record is retained as a created artifact, and its effect is recorded as
`runtime_not_started`, rather than claiming that no artifact was created.

Known capability containers with matching journal/host authority are excluded
from AO inventory. Historical capability containers lacking those records need
explicit repeatable `--unrelated-preexisting-capability-container FULL_CONTAINER_ID`.
Their full capability ownership/name, pinned image, and immutable creation time
must prove they predate the original dispatch; the control plane independently
compares them against the run's authoritative start time. Both inspections must
remain identical. This exception does not change those services or attest to
their current deployment authority. Ordinary complete, untagged non-AO service
records are inventoried without being mistaken for an AO worker.

The journal's `journal_reconciliations` table audits both successful and refused
checks. Successful checks persist an immutable `no_effect_terminal` receipt with
the operator and absence evidence before delivery. Repeating the same operation
replays that receipt; it does not emit another terminal transition. The control
plane validates ownership, attempt/fence, instance, and prior effect history,
marks the run/task failed, and publishes the normal event used by client refresh.
Restart `cylo-runner` after maintenance. A refusal keeps the run uncertain and its
slot occupied; it requires further lifecycle evidence or a separately authorized
AO recovery operation.

The snapshot's `execution_capacity` reports the global four-slot gate, active and
uncertain counts, available slots, and a stable blocking reason for operator
diagnosis. Queued tasks report `execution_reconciliation_required` when uncertain
runs contribute to saturation. Apple clients show ordinary task status and
refresh through existing events; internal capacity/reconciliation diagnostics
are not rendered in the product UI. Clients must not resubmit unknown runs.

## Voice gateway

The separately deployed gateway is
[Space3044/chatgpt-web-voice](https://github.com/Space3044/chatgpt-web-voice),
pinned to `6338ac478070aaa34bfe7a3c0469caf9e9174c02`.
See [the deployment instructions](../../deploy/voice/README.md).

- Linux management URL: `http://127.0.0.1:18090/login`.
- Authorized LAN management URL: `http://debian.local:18090/login`.
- Source: `/opt/cylo/voice/upstream`.
- Container: `cylo-voice-gateway`.
- Persistent data: `/var/lib/cylo/voice`.
- Compose configuration: `/etc/cylo/voice.compose.yaml`.
- Private environment: `/etc/cylo/voice.env`.
- Management login reference: `/etc/cylo/voice-admin.txt`, on Linux only.

The local installation explicitly publishes the gateway on `192.168.1.4`
as well as loopback, following the user's request for `debian.local` access.
The opt-in address is recorded in `/etc/cylo/voice.lan.env`; the repository
default remains loopback only. The SSH tunnel also forwards Mac loopback
port 18090. A browser worker in a
container has its own loopback interface; it needs an explicitly configured
network route before it can use the gateway. Do not assume the host's
`127.0.0.1` URL works inside a container.

The user must manually import a ChatGPT Web account in the gateway management
UI. Deploying the gateway does not establish voice functionality in Cylo:
trusted HTTPS and account/media lifecycle integration remain required. Keep
Cylo account-media disabled until those acceptance requirements are met.

## Operator CLI deployment metadata

The `cylo` operator CLI reads deployment metadata without opening secret files.
Its status view reports the detected systemd or Compose mode, service states,
provider endpoint and model, the source of provider configuration, the last
provider check category, and the secret file mode. Token contents, instance
keys, and complete installation links are never included in status output.

The canonical AO provider variables are `AO_CODEX_EXTERNAL_BASE_URL`,
`AO_CODEX_EXTERNAL_API_KEY_FILE`, and `AO_CODEX_EXTERNAL_MODEL`. The default
key file is `/etc/cylo/runner-secrets/cliproxyapi-api-key` with mode `0600`.
These variables configure the patched AO Codex route only. The preserved
OpenCode example may name the same endpoint and model, but the current AO patch
does not implement external OpenCode configuration. `AO_OPENCODE_EXTERNAL_*` is
not a supported AO namespace. An OpenCode container image must remain an
OpenCode image setting; it must not be supplied as `AO_CODEX_CONTAINER_IMAGE`.

Service reloads are performed by the CLI through the detected deployment
manager. For systemd it runs `daemon-reload` followed by
`reload-or-restart` for the affected units. For Compose it runs the configured
project with `docker compose up -d --no-build`. A failed command is returned to
the caller so setup can retain the previous configuration and report a
rollback-safe failure.

Run `cylo` without arguments for the interactive operator menu. The key
initialization and rotation entries ask for the server URL, then display the
key and `cylo://connect` installation link once. The built-in QR encoder
renders the same link in the terminal for App scanning; no `qrencode` host
dependency is required. Keep this terminal private because the QR grants access. Status never
prints keys, tokens, or installation links.

## Operator release installation

The Linux workflow's manual release option builds the `cylo` operator CLI for
Linux `amd64` and `arm64` on GitHub-hosted runners after its complete checks pass;
see [CI verification](../development/verification.md#completion-gate-and-manual-checks).
Each `operator-linux-<arch>-<revision>` artifact contains
a tarball with `bin/cylo`, `deploy-linux/install-cli.sh`, `SOURCE_REVISION`, and
`SHA256SUMS`. Select the host architecture, extract into a new immutable release
directory, and verify `sha256sum -c SHA256SUMS` from that directory. This operator
archive does not contain the server images or a complete service deployment.

For a complete source-built release, `deploy/linux/build-release.sh RELEASE_TAG`
also installs `bin/cylo` beside the runner in `/opt/cylo/releases/RELEASE_TAG`.
Builds must run in CI or an authorized worker, not on the controller. After
reviewing the release, install its CLI on the standard host PATH with:

```sh
sudo bash /opt/cylo/releases/RELEASE_TAG/deploy-linux/install-cli.sh /opt/cylo/releases/RELEASE_TAG
cylo
```

The installer atomically selects the immutable CLI through `/usr/local/bin/cylo`
and refuses to overwrite an existing regular file. It does not start services
or alter external CLIProxyAPI. Run `cylo status` to inspect deployment metadata
before setup or provider changes. The release staging and PATH tests use fake
build tools and temporary paths; Go unit/integration/race checks, actual binary
builds, Apple builds, and physical-device scanning have separate evidence.
No new deployment or physical-device acceptance is implied by source changes.

## Native client installation and connection

Install reviewed CI artifacts with the applicable platform signing/provisioning. Keep Mac inputs, signing/install scripts, artifacts, and logs under `~/Developer/Cylo/runs/<task-id>/`; the installed application belongs at `/Applications/Cylo.app`. A launch/signature check establishes installation only. See [Apple module instructions](../../clients/apple/README.md) and [current evidence](../acceptance/evidence.md) for tested revisions and remaining device/distribution gates.

The current client accepts an operator-selected server URL and shared instance key stored only in Keychain. The exact QR/link interaction is defined in [product](../product.md#3-client-requirements). Use HTTPS across an untrusted network; HTTP exposes the key in transit. Key initialization/rotation requires the server's exclusive database lock; stop it first. Rotation preserves the fixed owner/workspace, tasks, and separate worker credentials. Remove or update stale `CYLO_INSTANCE_KEY` or Compose `CYLO_INSTANCE_KEY_FILE` configuration before restart; a mismatched key fails startup. Restart closes old streams. Clearing a client removes its local data, not the shared key or durable tasks.

For export, backup, migration, restore, and destruction, use [data lifecycle operations](data-lifecycle.md).
