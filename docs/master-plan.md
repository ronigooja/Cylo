# Cylo master plan

**Status:** documentation Stage 0 is complete and product implementation is underway. The table below records the evidence-backed implementation boundary. Milestone 1 is not accepted: AO/runtime integration, complete runner delivery, production deployment, device acceptance, and full scenario evidence remain outstanding. This document is the current decision and delivery plan. Source requirements and research are preserved under `docs/archive/`; archived proposals are evidence, not current decisions.

## 1. Status and decisions

Cylo is a private, self-hosted system that keeps authorized work alive beyond a conversation or client session. The selected baseline is a private Linux Go control server, an independent runner, SQLite in WAL mode with `synchronous=FULL`, and native SwiftUI clients for macOS, iPhone, and iPad.

AO (OrchestratorInc/agent-orchestrator) owns creation and lifecycle of every runtime, including worker and curator runtimes. Cylo routes runtime creation through its authorized gateway and remains the authority for business tasks, permissions, events, and memory records. The existing remote CLIProxyAPI deployment is reused as-is and is not modified. The local `~/.codex/config.toml` is configured with provider `cliproxyapi` using the Responses API. An authenticated, read-only `/models` check returned HTTP 200 and 13 model entries, including `gpt-6-luna`; this confirms catalog access only, not successful model execution. Never print or track credential values. Space3044 `chatgpt-web-voice` supplies the selected ChatGPT web real-time gateway and account pool; its credentials will be entered manually by the user during setup. Mem0 is a controlled worker; Qdrant is a rebuildable retrieval index.

Decisions and limits:

- The first server and execution host target is Linux. Native clients are macOS, iPhone, and iPad; there is no PWA.
- There is no CAO implementation, macOS server, or Codex-specific recovery feature in the current baseline.
- Runtime creation is delegated to AO through an explicit Cylo gateway. Agent output cannot directly mutate authoritative Cylo state.
- The global concurrency ceiling is four active runs, including Super, workers, and curator.
- A one-use identity bootstrap precedes device access. Device approval and revocation are explicit; device credentials reside in Keychain.
- Commands are idempotent and create an atomic receipt, work record, and outbox entry. Optimistic versions protect updates. Unknown external effects are reconciled or escalated; they are never blindly retried.
- Voice turns have stable identities, revisions, and deduplication. Device, account, and media leases must end before reuse. Uncertain accounts are quarantined.
- Raw audio is not retained by default.
- Memory authority uses versions and tombstones. Changes become visible synchronously in the authority; asynchronous cleanup updates derived stores. Backups are included in deletion and recovery rules.
- At most one active server instance may operate on a database during migration or restore.
- The first release is single-instance and multi-project. It reserves user/workspace data boundaries but excludes a team collaboration UI, official Codex App integration, and mandatory seamless terminal takeover.
- Role, runtime, and model route are configured separately. Registration of an AO adapter does not establish Cylo support or acceptance for that runtime.

Current implementation status:

| Area | Implemented evidence | Remaining acceptance |
|---|---|---|
| Control plane | Linux Go server source, SQLite migrations, authenticated device and runner APIs, durable command receipts/events, task/run/result handling, owner and typed project membership checks, and inbox notifications; subprocess restart and recovery tests run locally. | AO lifecycle integration, real runtime termination proof, production deployment/restore rehearsal, and complete task scenarios. |
| Runner | Authenticated claim/report APIs are present. Claims preserve their admitted isolation policy and grants across restart. | Independent runner executable, real AO execution adapter, registered isolation verifier/backend, end-to-end execution, and production recovery evidence. |
| Memory | Source-linked, versioned Go authority with typed scopes, ownership checks, tombstones, and synchronous command/search paths; controlled Python Mem0 worker, pinned shared multilingual embedder, Qdrant adapter, claim-ID/history-ownership ledger, and safe logging are integrated. Root ran a local Go→Python→Mem0→ephemeral-Qdrant E2E with a Responses stub in 17.929 seconds; GPT-6.1 independently passed three source/logging/deleted-history regressions. The owner previously reported a selected authenticated CLIProxyAPI E2E in 15.176 seconds; that run was not repeated in the latest check and is separate from AO runtime execution. | Full model-call/cost ledger, pre-ledger history mapping, broader entity/export/cache/backup erasure guarantees, encrypted backup and restore rehearsal, live provider repetition, and AO-mediated runtime acceptance. |
| Voice | Space3044 signaling and stable turn bridge source is wired into server startup; confirmed transcript task admission writes canonical task data and event payloads atomically. A missing gateway returns an unavailable response. | Live gateway/account-pool behavior, media lifecycle, interruption and device tests, and end-to-end task completion. |
| Native Apple clients | Shared Swift client, native macOS/iOS app source, server-backed task and inbox flows, WebRTC client source, nine Swift tests, and an unsigned macOS app build pass on the reviewed Apple source. The iOS 18.2 SDK files exist on the available Mac, but Xcode exposes no eligible iOS destination; the current iOS app source compile is unverified. APNs registration/revocation method regressions pass with faked API/defaults. | iOS compile in a supported Xcode destination, signing, device installation, live notification delivery, and real-device voice acceptance. |
| Capabilities | Versioned release, grant, and deployment control APIs are source-wired with fenced claim/report routes. Release manifests are checked against the explicit `CYLO_AVAILABLE_CAPABILITY_IDS` registry; an unset runtime backend returns an unavailable response. | A real capability runtime adapter, external action execution, trigger evaluation/admission, and production deployment acceptance. |
- Daily backups retained for 30 days, RPO of 24 hours, and RTO of two hours are planning targets, not measured guarantees.

## 2. Requirements and scope

The product baseline is the complete 22-chapter product requirements document, including scenarios A–E. It is not reduced to a minimum product subset. The current delivery sequence changes when a capability arrives, not whether an in-scope requirement remains required. The official Codex desktop app and Codex-specific recovery subsystem are out of scope; AO owns available runtime adapters. Human handoff into an existing CLI session is optional. The complete source PRD is [archived product-requirements.md](archive/research/product-requirements.md), and [report 01](archive/research/reports/01-requirements.md) provides stable IDs and detailed acceptance mapping.

The product follows ten design principles: (1) AI handles judgment and ordinary programs handle deterministic work; (2) Super handles complex coordination rather than every operation; (3) Workers can execute specialist work independently; (4) authorized capabilities can be created, deployed, and managed as long-running services; (5) tasks, memory, and execution state persist beyond a conversation; (6) users can continue work across devices; (7) users are not locked into Cylo clients; (8) models, agents, voice, and integrations remain replaceable; (9) accuracy, reliability, and efficient model use take priority over unnecessary calls; and (10) data ownership retains room for future team and organization use. These principles do not imply that organization collaboration UI is in the first release.

The following are explicit non-goals for this baseline: PWA, CAO implementation, macOS-hosted server, and a Codex-specific recovery subsystem. The independent CLIProxyAPI deployment is an existing dependency and must remain untouched.

## 3. Architecture and ownership

| Area | Owner | Boundary |
|---|---|---|
| Client experience | Native SwiftUI apps | Voice/text, task and service views, confirmations, notifications; no authoritative state |
| Control plane | Private Go server | Identity, policy, projects, tasks, commands, events, receipts, memory authority, service definitions and audit |
| Runtime lifecycle | AO behind Cylo gateway | Creates and manages all runtimes; receives only scoped, authorized work |
| Execution | Independent Linux runner | Executes admitted work and reports observations; cannot grant itself authority |
| Existing model proxy | Remote CLIProxyAPI | Reused unchanged; Cylo/AO use deployment-provided endpoint and secret reference |
| Voice | Space3044 gateway and account pool | Real-time upstream voice transport; Cylo owns turn-to-command receipt and task relationship |
| Memory worker | Mem0 under Cylo policy | Proposes and performs permitted memory operations; cannot override authoritative records |
| Retrieval | Qdrant | Derived and rebuildable index, never source of truth |
| Durable store | SQLite WAL, `synchronous=FULL` | Authoritative transaction boundary for the initial private deployment |

The runner and control server are separate processes. Only the control plane commits authoritative business state. The four-run global cap applies across all AO-created active runtimes, including Super and memory curator work.

The durable relationship is `Project → Task → Run → AO Session → Runtime Native Session`. Each run records its runtime, model route, execution host, workspace, authorization, artifacts, and result evidence. User and Super requests, including child Worker requests, enter one controlled admission path: Cylo records intent and checks authority, AO creates the runtime, observations return to Cylo, and Cylo judges the result against evidence. No second automatic dispatcher may bypass this path. Prefer AO configuration and adapters; extend AO only where a measured gap requires it.

## 4. Native application flows

The supported clients are native SwiftUI applications for macOS, iPhone, and iPad. They authenticate a device after one-use bootstrap and explicit approval. Device credentials are stored in Keychain and can be revoked centrally. A device shows the server's authoritative version and exposes stale/offline status instead of presenting cached state as current.

The primary navigation entrances are **Conversation**, **Tasks**, and **Projects**. Memory, Capabilities, and Settings are secondary entrances. Users can submit commands by text or voice, inspect task evidence and service health, answer approval requests, stop work, and receive linked notifications. Conversation and voice-session lifetimes do not control task lifetimes. Raw audio retention is off by default. Cross-device continuation means access to the same durable task context; it does not claim live audio migration between devices.

Voice uses WebRTC with a fixed libwebrtc dependency. Platform audio capture/playback remains behind platform-specific adapters while all devices use the same session, turn, and command pipeline. Acceptance requires a 30-minute mixed Mandarin-English voice session on both a real iPhone and a real iPad while each device is locked, plus interruption, reconnect, route change, and task-continuity checks. This is a target test, not a completed result.

One universal iOS app adapts to iPhone and iPad; a separate native macOS target shares Swift packages for API, authentication, domain state, synchronization, and voice protocols. Task views show the goal, plan, actual progress, waiting reason, artifacts, and verification. Worker/run details belong in an advanced view. Muting capture, stopping playback, hanging up, and stopping a task are distinct operations; closing a client does not cancel a durably accepted task.

WebRTC owns the media pipeline. The platform adapter coordinates AVAudioSession, permissions, audio routing, interruptions, background audio, and application lifecycle without competing microphone pipelines. `CyloKit` consumes the community `stasel/WebRTC` M155 binary at source revision `44f36788497f701bc8c0306de3a1c2511d56019d`, artifact checksum `cb9adacf2d6bbf598d1d9e3e6c16d8fb97ca987b5b697746891828f9dc068ffe`. The repository's candidate source-build script pins WebRTC `3b7ef6826ea6cb4c89c3972b1f14fcb3ed0fa469` and `depot_tools` `c84f5f5081e89eda268785a04a9e8a9a1987724f`; it has not been run or shown binary-equivalent. Keep its generated outputs under the approved Mac build root. iOS/iPadOS distribution targets TestFlight; macOS targets a signed, notarized installer. APNs supplies a hint to reload the durable inbox, never the authoritative notification record.

Opening a microphone or starting an upstream voice session requires an explicit user action and granted platform permission. Cylo does not initiate a voice call autonomously. A user-confirmed voice turn enters the same authorized command path as typed input; neither partial captions nor spoken model output authorize a business action. Lock-screen acceptance records device model/OS, session duration, network changes, audio route and interruption events, reconnects, task receipts, and whether capture/playback stopped when requested. Raw audio is not retained.

## 5. Execution and model routing

Cylo validates a request, authorization, budget, and concurrency slot before asking AO to create or select a runtime. AO owns runtime creation and lifecycle. Cylo controls task state and accepted effects. Workers return structured outcomes and evidence; deterministic transitions, health checks, and ordinary routing do not require model calls. Super is reserved for complex coordination and escalation.

Provider/model calls must follow an explicit user request or a previously authorized service action and an admitted task/run budget. The deterministic idle, health, scheduling, and event path must make no model calls. Acceptance requires attributing each actual call to its task, runtime, and route, recording its cost, and rejecting missing authority or exhausted budgets before dispatch; the full model-call and cost ledger is still outstanding.

The shared maximum is four active runs across Super, workers, and curator. Every dispatched run has a Cylo task/run identity, scoped context, permissions, and budget accounting. AO adapters determine which native create, resume, and stop operations are available; Cylo does not add a Codex-specific recovery layer. Existing CLIProxyAPI routing remains external and unchanged. AO must use its configured endpoint and secret reference; credentials are never copied into Cylo documents or logs.

CLIProxyAPI owns account selection, quota handling, and proxying for routes it supplies. Neither AO nor Cylo repeats account rotation for that route. Other providers have independent configuration. Validate the currently available route first, then accept at least two runtime types through actual execution, tools, artifacts, and confirmed stop; a direct model probe is distinct from an AO/Cylo execution chain.

## 6. Voice

Space3044 `chatgpt-web-voice` is the selected real-time ChatGPT web gateway and account pool. Cylo must bridge stable user turns to durable commands and return business receipts that distinguish voice transport success from task acceptance and task completion. A stable turn identity and revision scheme prevents duplicate dispatch after retransmission or interruption.

Device, account, and media resources use scoped leases with holder, generation, start/expiry, and last termination evidence. A lease is released only after termination is confirmed; lease expiry is not proof of process or media termination. An uncertain session or account is quarantined rather than reused. Account exhaustion changes the media session without re-dispatching the task. No raw audio is retained by default. Real-device latency, interruption, mobile lifecycle, and account-pool behavior remain acceptance work, not established capabilities.

Natural conversation has priority. Do not close upstream speech by default while a task executes. Display a natural answer, durable acceptance, execution in progress, and verified completion separately. Only task-ledger events produce formal completion notices; upstream speech is never a tool-result receipt. Business submission uses the same command interface as other clients and handles speaker role, turn identity, revisions, duplicate events, and reconnect retransmission. Confirmation submission is a fallback when the gateway lacks a stable turn-completion signal; that fallback does not establish hands-free acceptance.

Every active voice session binds a device, account lease, and media generation. Account changes create a new media session and preserve Cylo task associations; an upstream session ID is never reused across accounts. The old client media session must end before its account slot is reusable. Lease expiry is not proof of media termination.

## 7. Memory and context

The Go control plane owns authoritative memory versions, source references, and tombstones. Mem0 operates as a controlled worker with scoped capabilities and cannot alter task, permission, configuration, or confirmed decision authority without the corresponding policy path. Qdrant contains rebuildable derived retrieval data.

An accepted memory change is synchronously visible through the authority. Mem0 history remains available for provenance and correction. Deletion records a tombstone and then propagates through the full chain: authoritative records, Mem0 working/history data, Qdrant vectors and payloads, caches, exports, replicas, and backups according to the retention/erasure policy. Asynchronous cleanup is version-checked so stale copies cannot resurrect deleted memories. Context is assembled per task, runtime, project, and permission scope. Facts, execution outcomes, and model inferences remain distinguishable and traceable. Retrieval omission or conflict is surfaced as uncertainty rather than filled by invention.

The processing chain is `source event → authority → transactional outbox → extraction/indexing → scoped retrieval → permission/version recheck → context assembly`. Explicit user decisions register directly; model inferences remain candidates. The memory curator runs on demand for cross-record summaries, conflicts, expiry, or relationship judgments, rather than participating in every conversation turn. Formal decisions and execution state reference their authoritative ledger records.

Use a local multilingual embedding model with a pinned revision, dimension, and encoding. Structured lookup and Chinese full-text retrieval remain available when the model or vector service fails. Deletion immediately excludes content from retrieval and context, then performs idempotent versioned cleanup and reconciliation across stores; no cross-database transaction is assumed. Conversations, formal memory, and task evidence remain until user deletion by default, with configurable retention. Raw audio is not saved by default.

The worker uses Mem0 2.2.1 and the shared FastEmbed 0.9.0 CPU model `sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2`, pinned to ONNX snapshot `Qdrant/paraphrase-multilingual-MiniLM-L12-v2-onnx-Q@faf4aa4225822f3bc6376869cb1164e8e3feedd0`, with 384-dimensional cosine vectors. The default Compose setup gives Qdrant disposable `tmpfs` storage and keeps Mem0 history, ownership metadata, and model cache in the `memory-worker-state` volume at `/var/lib/cylo/memory`; back up or delete that volume under the configured retention policy. Startup verifies model artifact hashes. A changed model codec gets a new Qdrant collection and rebuild from Go authority.

The opt-in cross-language harness is `go test ./tests/integration -run '^TestMemoryWorkerGoPythonMem0QdrantEndToEnd$' -count=1` with `CYLO_MEMORY_E2E=1`, `CYLO_MEMORY_E2E_LLM_BASE_URL`, `CYLO_MEMORY_E2E_LLM_MODEL`, and `CYLO_MEMORY_E2E_LLM_API_KEY` supplied from a deployment secret reference. It uses synthetic source and a temporary authority database and starts disposable Qdrant. A local Responses stub validates composition but does not establish live model behavior or AO runtime acceptance.

## 8. Long-running services

A task is finite work; a service is a durable definition with its own identity, configuration, trigger, permissions, health, and execution history. Services are created or changed only after the required user authorization. Deterministic event handling runs as ordinary code; a worker or Super is called only when judgment is needed. Service execution remains independent of a Super conversation and client availability.

Each service lifecycle action must report the actual process or deployment result. Stop, pause, revoke, update, and uninstall cannot be represented by a UI state change alone. Service executions share the same global four-run limit when they invoke runtimes. Deployment artifacts, workspaces, and persistent Cylo data use separate roots and permissions. A constrained manifest names the artifact/version, entrypoint, deployment-provided environment references, allowed mounts, and capabilities. The runner enforces those limits; services receive neither the Docker socket nor privileged host access.

Generated code does not obtain deployment permission. An Agent submits source, tests, and a release manifest; the capability manager validates them before an authorized runner deploys. Keep temporary workspaces, build staging, immutable release artifacts, deployment configuration/secret references, and long-lived data volumes separate. AO workspace cleanup must not remove deployed service data or its only artifact copy.

Every persistent service volume has an explicit owner, purpose, mount path, backup/restore policy, retention rule, and deletion action in its deployment manifest. Temporary workspaces and caches are disposable; release artifacts remain immutable and addressable; long-lived service data is excluded from workspace cleanup. Mount only the named volume and only after the user-authorized capability grants it.

Constrained containers limit user identity, mounts, network, ports, CPU, memory, process count, and logs. Deny arbitrary host mounts, privileged mode, host devices, and Agent access to Docker's socket. systemd supervises the server and memory Compose stack. A trusted runner runs as an independently managed systemd service/process on a dedicated Linux execution host and manages authorized deployments. The runner necessarily holds Docker-daemon or equivalent root-level authority; this is trusted infrastructure, not an isolation boundary. Keep the daemon socket out of the server, memory, AO workload containers, and capability manifests; no workload container runs privileged. This configuration/template design is not production installation evidence. Schedules and event listeners are durable programs independent of Super availability. Preauthorization names resources, actions, external connectors, deployment scope, expiry, and limits: execution is automatic within that scope and requires confirmation to expand it. Ordinary long-running services have resource limits separate from the four-Agent run ceiling.

## 9. APIs, events, and ownership

The public client API uses REST under `/v1` and SSE for ordered event delivery. An authenticated client reads `GET /v1/snapshot` for the authoritative project/task/device state, server version, and current event cursor; it resumes `GET /v1/events` after that durable cursor using `Last-Event-ID`. Reconnect repairs from a snapshot when the cursor is invalid or too old. Device setup begins with one-use bootstrap and pairing; already-approved devices can approve or revoke devices under policy.

All business mutations go through `POST /v1/commands` with an `Idempotency-Key` and optional `expected_version`. Initial command kinds are `project.create`, `task.create`, `task.append`, `task.stop`, and `task.approve`. Workspace references are opaque server-owned IDs; clients cannot submit arbitrary host paths. `GET /v1/commands/{id}` and `GET /v1/commands?key=...` recover the durable receipt after a timeout or reconnect; each receipt preserves its original idempotency key. The transaction commits the receipt, durable work record, and outbox event atomically before returning acceptance. Event consumers deduplicate by stable event identity. The outbox is the source for delivery retries; external side effects keep their own idempotency or reconciliation state.

Optimistic versions reject stale writes with a recoverable conflict. AO callbacks and runner reports are observations or proposals until the control plane validates and commits them. Memory changes use authority versions and tombstones. Voice gateway callbacks correlate to stable voice turn IDs and Cylo command IDs. Runner routes use separate credentials. Durable work claims are atomic and obey the global four-run cap; a dispatched operation with an unknown outcome is reconciled or escalated, never resent blindly. Implemented schemas live with the contracts and code; new interfaces are coordinated before implementation. REST covers commands/receipts, projects/tasks/runs/artifacts, snapshots/events, voice, memory/context, capability releases/deployments, authorization/devices, and notifications. Large files, build outputs, and service data use file storage rather than database blobs.

## 10. Security, reliability, and lifecycle

Identity starts with a one-use bootstrap; subsequent device access requires approval. Revocation blocks future operations and defines how in-flight work is stopped or reconciled. Secrets are references to deployment-managed credentials. Every command, approval, runtime dispatch, external effect, and authority change is auditable.

Unknown side-effect results never trigger blind retry. If the effect can be queried, reconcile it; otherwise mark it unknown and request human resolution. Recovery preserves receipts and work identity to avoid duplicate effects. Device/account/media leases must terminate before reuse, with uncertain accounts quarantined. A database restore or migration must fence the old instance so only one server can write.

Daily backup, 30-day retention, RPO 24 hours, and RTO two hours are unverified targets. The first implementation must define backup contents, encryption/key recovery, restore procedure, and deletion treatment for backups. The targets become claims only after repeatable restore evidence.

Data inventory and deletion acceptance covers authoritative rows, worker history and model cache, vector payloads, local client caches, exports, replicas, service volumes, and backup snapshots. Deletion immediately blocks retrieval and new use, records a versioned tombstone, and queues idempotent cleanup for each named store. Report stores that are pending or outside the configured retention window; do not claim physical erasure until each applicable store and backup lifecycle has evidence. Service-volume deletion requires explicit authorization and a recoverable backup policy where retention requires one.

Backups cover a consistent authoritative database snapshot, artifacts and releases, service configuration, required persistent volumes, and deletion manifests. Recover keys separately or reauthorize their external integrations. On migration, pause dispatch and isolate the old instance before restoring. Reconcile services and uncertain external effects before reopening execution; never activate two copied databases. Report full vector-index rebuild duration separately from core-service RTO. Device identity is independent of the voice account pool: the first device uses a one-time bootstrap, subsequent devices require approval, credentials remain in Keychain, and revocation/admin recovery remain available.

### Deployment and recovery operations

The Linux control server reads deployment settings from its process environment. Keep bootstrap and runner credentials in deployment-managed secret configuration; the server does not print them. Bind to loopback or a private trusted interface and put external TLS termination behind the deployment's authenticated network boundary.

Required settings:

- `CYLO_DATABASE_PATH`: path to the SQLite database. The server acquires a single-writer process lock before opening it.
- `CYLO_BOOTSTRAP_CODE`: one-use bootstrap credential. Generate 32 random bytes and encode them with unpadded base64url.

Optional settings:

- `CYLO_LISTEN_ADDR`: defaults to `127.0.0.1:8787`.
- `CYLO_DEFAULT_PROFILE_ID`: the server-selected profile for new tasks.
- `CYLO_RUNTIME_PROFILES`: JSON object keyed by profile ID. Each profile sets `id`, `ao_project_id`, `runtime_route_id`, `model_route_id`, `role`, `ao_agent_kind` (`worker`), `isolation_policy`, `capability_ids`, and `scopes`. Client requests cannot override these execution routes or grants.
- `CYLO_RUNNER_TOKEN`: deployment-provisioned 32-byte base64url runner credential. When set, `CYLO_RUNNER_ID`, `CYLO_RUNNER_WORKSPACES`, and `CYLO_RUNNER_SCOPES` default to `configured-runner`, `default`, and `runner:claim,runner:observe,runner:control:read`.
- `CYLO_MEMORY_RUNNER_TOKEN_FILE`: optional path to a deployment-managed file containing a distinct 32-byte base64url memory worker credential. When enabled, `CYLO_MEMORY_RUNNER_WORKSPACES` must list exact workspace IDs; `CYLO_MEMORY_RUNNER_ID` defaults to `memory-worker`; and `CYLO_MEMORY_RUNNER_SCOPES` may only contain exactly `memory:jobs:claim,memory:jobs:complete,memory:propose`. The server stores only the credential hash and never logs the token. This credential exposes the durable derived-job API; it does not enable a Mem0 or Qdrant runtime.
- Memory-worker deployment also requires `CYLO_CONTROL_URL`, the scoped worker credential injected as `CYLO_RUNNER_TOKEN` (provisioned for the server through `CYLO_MEMORY_RUNNER_TOKEN_FILE`), `CYLO_MEM0_LLM_MODEL`, `CYLO_MEM0_LLM_BASE_URL`, and `CYLO_MEM0_LLM_API_KEY` from deployment-managed secret references. `CYLO_MEM0_LLM_PROVIDER` selects `openai` or `ollama`; `CYLO_MEM0_HISTORY_DB` and the model cache belong on the private persistent memory volume. Qdrant URL, optional API-key reference, collection/codec, and worker batch/poll settings are deployment configuration. The pinned multilingual embedding revision and dimensions are recorded in §7; changing them requires a separate collection and rebuild.
- For local execution, start the worker from the repository root with `python -m services.memory.worker`, or use `docker compose -f services/memory/docker-compose.yml up --build` with the settings above. The Compose Qdrant service is loopback-bound and disposable; it is not a production persistence configuration. These commands describe source setup and do not establish production installation or recovery.
- `CYLO_OWNER_SCOPES`: comma-separated owner/device grants. When omitted, the bootstrap owner receives the client, device-management, and personal/workspace memory read/write/delete scopes. Setting this value replaces the defaults exactly; it never adds a wildcard or restores omitted grants. Project/task memory scopes must name their exact scope IDs.

For example, a runtime profile environment value can be supplied as JSON:

```json
{"standard":{"id":"standard","ao_project_id":"cylo-workers","runtime_route_id":"default-worker","model_route_id":"cliproxyapi","role":"worker","ao_agent_kind":"worker","isolation_policy":"workspace-sandbox-v1","capability_ids":[],"scopes":["task:read","task:write"]}}
```

The first device uses `POST /v1/bootstrap`; later devices use pairing and approval. Pairing grants devices the same owner scope set; configure `CYLO_OWNER_SCOPES` when a restricted device policy is needed. Clients read the durable inbox from `GET /v1/notifications` (`?unread=true` filters it), and mark an item read through the idempotent `notification.mark_read` command. Inbox entries link to their source event and task. If every approved device is lost, stop the service and run the local recovery command from the server host:

```sh
cylo-server recover-bootstrap --database /path/to/cylo.db --confirm-revoke-all-devices
```

The command requires the database lock, revokes all device credentials, requests stop for active runs, records an audit event, and prints one new bootstrap code after its transaction commits. Update the deployment-managed `CYLO_BOOTSTRAP_CODE` reference with that value before restarting the server, then bootstrap a replacement device. Active run capacity remains fenced until the runner supplies termination proof. Existing database state is preserved.

The process handles `SIGINT` and `SIGTERM` with a 20-second graceful shutdown window. SQLite is opened with WAL, `synchronous=FULL`, foreign keys, a busy timeout, and mode `0600` for the database and sidecars. The server and memory worker are delivered as a systemd-supervised Compose application stack; the runner is a separately managed systemd process on its dedicated host. These are planned and source-configured delivery assets; production installation, migration, and physical restore are not yet accepted.

## 11. Repository and workflow

Repository documents, new filenames, and routine progress updates are in English. Use Chinese for final reports and important findings, decisions, or questions. Archived Markdown narrative is translated into English while source quotations and raw evidence remain verbatim. Archived proposals are historical evidence, not the current decision. Keep secrets out of tracked files. Report verification commands and results honestly; static checks, fixtures, and prototypes must be labeled by what they actually prove.

Concurrent work uses separate branches and worktrees under `$HOME/subagents` with explicit file ownership. The main agent coordinates planning, assignments, dependencies, review, and integration. Implementation uses GPT-6 Luna subagents at high reasoning effort; substantive independent verification uses GPT-6.1 at medium reasoning effort. Use at most four concurrent implementation subagents and reserve one independent review slot. Coordinate cross-module API/data changes with affected owners before editing. Review commits before integration. Before a task or fresh-context handoff, record the branch, ownership, commit, completed and remaining work, verification, and limitations.

Implementation begins after the archive stage is complete and the main agent freezes the API contract. Do not create placeholder directories. Once implementation begins, add only directories that contain tracked files. The planned core Go module is `github.com/ronigooja/Cylo` (`go.mod` and `go.sum` at repository root), targeting Go 1.26 and pure-Go SQLite through `modernc.org/sqlite`. The initial ownership tree is:

```text
contracts/                  shared HTTP, event, runner, and extension contracts
cmd/cylo-server/            Go control-server entry point
internal/storage/           SQLite schema, migrations, transaction and repository layer
internal/auth/              one-use bootstrap, device approval, and revocation
internal/control/            authenticated route registration and server composition
internal/tasks/             command admission, task state, dispatch and receipts
internal/events/            durable outbox, snapshot version, SSE cursor and replay
internal/notifications/     linked delivery records and deduplication
```

Create only directories that contain tracked files. Other contributors will own native clients, AO/runner/deployment, memory, voice, and capability implementations. Core interfaces must let those modules register authenticated routes and command handlers without replacing server composition. On the Mac, all generated, cloned, and build files must stay beneath `/Users/zhuwei/terminal/<dedicated-folder>`; do not write Mac-side files elsewhere.

Available integration machines/tools: macOS host `zhuwei@macair`, Xcode 16.2, Swift 6.0.3, Go 1.26.4 at `/root/environment/go1.26.4/bin/go`, and the planned Go module uses `modernc.org/sqlite` for pure-Go SQLite. All Mac-side files must be beneath `/Users/zhuwei/terminal/<dedicated-folder>`. Availability does not mean a device build, voice test, or deployment has passed.

## 12. Milestones and acceptance

| Milestone | Scope | Exit evidence |
|---|---|---|
| 0. Documentation baseline | English README, repository guidance, master plan, preserved research archive | Decisions, scope, source links, stable IDs, risks, and status are reviewable; no implementation claimed |
| 1. Contracts and trusted task foundation | Go service and independent runner; SQLite WAL/FULL; API and ownership contracts; device bootstrap/approval/revocation; projects, commands, tasks, events, durable receipts/outbox; optimistic versions; native SwiftUI app shells; task evidence and status | Atomic receipt/work/outbox under crash injection; duplicate command/event suppression; stale-version rejection; device revocation behavior; real runner report flow; app shells on macOS/iPhone/iPad; Linux deployment and one-active-instance fencing |
| 2. Runtime, voice, and memory integration | AO gateway owns runtime creation; voice turn/receipt bridge; Mem0 controlled worker; Qdrant rebuild path; source/version/tombstone semantics | AO dispatch and callback authorization; four-run cap; stable voice turn dedupe, leases and uncertain-account quarantine; authority-first memory visibility, rebuildable Qdrant, tombstone and backup behavior |
| 3. Complete task loop with two runtimes | User request through planning/worker execution, durable progress, evidence, approval, stop/recovery, and result; at least two runtime types | Two-runtime execution; task lifecycle and outcomes grounded in runner evidence; restart and unknown-effect recovery; scenario A and D evidence; no duplicate irreversible effects |
| 4. Long-running services | Authorized service creation, lifecycle, deterministic events, worker escalation, notifications and service history | Service outlives client and Super session; all lifecycle operations reflect actual process state; scenario B and E evidence; bounded retries and notification dedupe |
| 5. Production reliability and Apple delivery | Backup/restore, migration fencing, data control, hardening, complete native client flows and delivery, performance and operations | Restore rehearsal with measured RPO/RTO; fault injection and security negatives; real-device voice and notification checks; complete scenarios A–E; every in-scope ID has evidence or an explicitly unresolved acceptance gap |

Evidence labels remain distinct: source wiring and unit tests establish code-level behavior; fake-provider method harnesses establish only the extracted paths they exercise; a Responses stub validates local component composition without a live model; a direct model/provider probe does not prove AO or Cylo dispatch; a live worker/provider test does not prove an AO task chain; and a device test does not prove production delivery. Promote a claim only with evidence at the boundary stated in its acceptance row.


### System-wide quality acceptance (CY-20-01)

The product requirement defines these fifteen dimensions; each requires evidence during final acceptance. Numeric thresholds not stated in the PRD remain open targets.

| Dimension | Acceptance intent |
|---|---|
| Accuracy | Do not guess execution results or turn unconfirmed inferences into facts. |
| Continuity | Projects, tasks, and long-running services survive conversation end. |
| Persistence | Important data and memory survive process termination. |
| Reliability | Detect execution exceptions and recover under safe conditions. |
| Extensibility | Add models, agents, services, and system capabilities. |
| Independence | Keep core capabilities replaceable across models and services. |
| Interoperability | Support two-way continuation with native tools as far as their interfaces allow. |
| Cross-device consistency | Clients share authoritative task and memory state. |
| Cross-platform support | macOS, iPhone, and iPad clients share a unified backend; first-release server and execution hosts use Linux. |
| Controllability | Users can understand, authorize, pause, and stop related work. |
| Security | Use appropriate permissions and protect data and external services. |
| Observability | Trace runtime state, exceptions, results, and their sources. |
| Cost efficiency | Reduce unnecessary model calls and repeated context. |
| Ease of use | Keep natural interaction primary without requiring knowledge of internal technology. |
| Long-term evolution | Add capabilities without repeatedly rebuilding the system from scratch. |

Source: archived product requirements, §20. The matrix row for CY-20-01 requires evidence against every dimension above; no dimension is accepted solely from a design claim.
Milestones are sequencing, not product scope reduction. The global four-run cap applies at every milestone that dispatches runtimes.

Apple push uses APNs as a silent wake-up channel only. The control server owns the environment (`sandbox` or `production`), team/key IDs, and one static bundle topic; device requests can register or revoke only a token bound to their authenticated device. Payloads may carry the inbox cursor and must not carry notification titles, bodies, or task content. Durable notification and delivery rows remain authoritative, with capped exponential backoff and token disablement on APNs `410`. Backend fixtures and HTTP mocks do not establish real APNs delivery; that remains open until native token registration, signed client builds, and real-device delivery checks pass.

## 13. Dependencies and limits

The implementation depends on AO and its supported runtime adapters, the existing remote CLIProxyAPI deployment, Space3044 voice gateway behavior, Apple platform delivery and Keychain, and Linux process/service facilities. External services can change independently. Their availability, supported interfaces, account eligibility, quota behavior, and release compatibility must be checked at integration time.

No production reliability, voice quality, migration, backup recovery, isolation boundary, or numeric service target is established yet. SQLite WAL/FULL is the selected initial persistence mode, not a claim of multi-host database availability. The one-active-instance rule is mandatory during migration and restore. The backup policy's RPO/RTO values remain targets pending measurements. Existing CLIProxyAPI remains an external dependency and is outside this repository's deployment lifecycle.

## 14. History and references

The complete product PRD and research reports are retained under `docs/archive/research/`, including `product-requirements.md`, superseded plans, project comparisons, Codex Super material, reports 01–19, and original evidence. Report 01 is the requirements traceability source; it retains the stable IDs below. Report 02 is the orchestration and Codex history; reports 03, 06, 08, 11, 12, 13, 14, 16, 17, and 18 cover voice, AO boundaries, proxy integration, and memory research. Archived documents are not the current implementation baseline where they conflict with the decisions in section 1.

### Stable requirement traceability

The requirement descriptions and detailed acceptance scenarios live in [archived report 01](archive/research/reports/01-requirements.md). This index preserves every stable ID and maps it to the responsible area, target milestone, and evidence required. Evidence is a future acceptance artifact, not a claim that it exists or has passed. “N/A” marks an explicitly excluded requirement; “Optional” marks an enhancement that does not block the baseline.

| Stable ID | Requirement | Primary component | Target | Acceptance evidence |
|---|---|---|---|---|
| `CY-01-01` | Long-running cross-device system with durable memory, autonomous execution, coordination, and extensible capabilities. | Control plane; runner; clients | 1 and 5 | Architecture review against the ten principles and seven exclusions; complete A–E acceptance |
| `CY-01-02` | Apply the ten principles: deterministic work outside models, independent workers, replaceable modules, efficiency, reliability, and future ownership boundaries. | Control plane; runner; clients | 1 and 5 | Architecture review against the ten principles and seven exclusions; complete A–E acceptance |
| `CY-01-03` | Use the host OS and existing specialist tools; Cylo does not recreate an OS, IDE, or terminal desktop. | Control plane; runner; clients | 1 and 5 | Architecture review against the ten principles and seven exclusions; complete A–E acceptance |
| `CY-02-01` | Keep six system responsibilities explicit and run deterministic state and event handling without model calls. | Go control plane and AO gateway | 1 | Ownership map, API contracts, and trace proving deterministic transitions do not call models |
| `CY-03-01` | macOS, iPhone, and iPad clients share one authoritative backend and consistent project/task/memory state. | SwiftUI clients; voice gateway; control plane | 1, 2, and 5 | macOS/iPhone/iPad device runs; voice and notice evidence; scenario A |
| `CY-03-02` | Provide a simple voice/text interface for tasks, progress, results, projects, services, approvals, notices, and optional technical detail. | SwiftUI clients; voice gateway; control plane | 1, 2, and 5 | macOS/iPhone/iPad device runs; voice and notice evidence; scenario A |
| `CY-03-03` | Support contextual real-time voice, interruption, cross-device continuation, visible results, and task lifetimes independent from voice sessions. | SwiftUI clients; voice gateway; control plane | 1, 2, and 5 | macOS/iPhone/iPad device runs; voice and notice evidence; scenario A |
| `CY-03-04` | Proactively notify users about completion, failure, mail, approvals, scheduled outcomes, and unrecoverable conditions with source links. | SwiftUI clients; voice gateway; control plane | 1, 2, and 5 | macOS/iPhone/iPad device runs; voice and notice evidence; scenario A |
| `CY-04-01` | Run the backend independently on supported host OSes; report sleep, shutdown, and network loss accurately and recover safely. | Go server; runner; operations | 1 and 5 | Linux unattended run, client disconnect, fenced restore/migration rehearsal; scenario E |
| `CY-04-02` | Keep agent execution, services, data, memory, permissions, events, status, and health on the server. | Go server; runner; operations | 1 and 5 | Linux unattended run, client disconnect, fenced restore/migration rehearsal; scenario E |
| `CY-04-03` | Migrate linked projects, history, agents, tasks, memory, permissions, services, state, and execution files between environments. | Go server; runner; operations | 1 and 5 | Linux unattended run, client disconnect, fenced restore/migration rehearsal; scenario E |
| `CY-05-01` | Create and manage agent work with a real workspace, instructions, state/results, task links, supported continuation, and stop. | AO gateway; adapters; runner | 1 and 3 | AO adapter execution and supported recovery evidence; no custom Codex recovery is implied |
| `CY-05-02` | Official Codex desktop-app integration is excluded; retain the stable ID for traceability. | AO gateway; adapters; runner | N/A | No desktop-app discovery, deep link, or host handoff acceptance; scope exclusion retained |
| `CY-05-03` | Optional: hand Cylo-started work to the supported native CLI session. | AO gateway; adapters; runner | Optional | If implemented, prove original session/project/workdir and controlled handoff per report 01 |
| `CY-05-04` | Optional: discover and continue authorized work created in the native CLI. | AO gateway; adapters; runner | Optional | If implemented, prove original session/project/workdir and controlled handoff per report 01 |
| `CY-05-05` | Preserve project, workspace, code, history, tools, goals, outcomes, open items, and context; prevent uncoordinated concurrent writes. | AO gateway; adapters; runner | 1 and 3 | Prove base task recovery and conflict protection; any human session handoff is optional |
| `CY-05-06` | Keep project outputs usable without Cylo and optionally synchronize external native-tool changes. | AO gateway; adapters; runner | 1 and 3 | AO adapter execution and supported recovery evidence; no custom Codex recovery is implied |
| `CY-06-01` | Super interprets complex goals, plans dependencies, chooses workers, adjusts execution, and escalates when needed. | AO; control plane; runner | 2 and 3 | Two runtime types, routing traces, dependency/failure/stop evidence; scenario D |
| `CY-06-02` | Workers execute specialist work with scoped context/tools and return independent results, records, and errors. | AO; control plane; runner | 2 and 3 | Two runtime types, routing traces, dependency/failure/stop evidence; scenario D |
| `CY-06-03` | Route ordinary events to deterministic programs, semantic judgment to workers, and complex cases to Super as needed. | AO; control plane; runner | 2 and 3 | Two runtime types, routing traces, dependency/failure/stop evidence; scenario D |
| `CY-06-04` | Support parallel agents, dependencies, result passing, unified status, and controlled failure, cancel, and recovery. | AO; control plane; runner | 2 and 3 | Two runtime types, routing traces, dependency/failure/stop evidence; scenario D |
| `CY-07-01` | Identify durable capability needs and create or configure the program only for authorized deployment. | Control plane; AO; runner | 4 | Authorization-negative test and actual install/update/stop/uninstall lifecycle evidence; scenario B |
| `CY-07-02` | Manage capability install, configuration, authorization, run, pause, resume, update, stop, removal, health, and errors. | Control plane; AO; runner | 4 | Authorization-negative test and actual install/update/stop/uninstall lifecycle evidence; scenario B |
| `CY-07-03` | Keep long-running ordinary programs alive independently; call agents only where judgment is needed. | Control plane; AO; runner | 4 | Authorization-negative test and actual install/update/stop/uninstall lifecycle evidence; scenario B |
| `CY-07-04` | Allow pluggable capabilities across mail, calendar, files, development, web, apps, system, schedules, and monitoring. | Control plane; AO; runner | 4 | Authorization-negative test and actual install/update/stop/uninstall lifecycle evidence; scenario B |
| `CY-08-01` | Represent every execution subject with a unified task identity, goal, source, status, times, result, errors, recovery, and records. | Go control plane; runner | 1 and 3 | State transition, durable receipt, stale version, crash recovery, and unknown-effect reconciliation |
| `CY-08-02` | Define real semantics for queued, active, waiting, awaiting user, paused, complete, failed, and canceled states. | Go control plane; runner | 1 and 3 | State transition, durable receipt, stale version, crash recovery, and unknown-effect reconciliation |
| `CY-08-03` | Allow long tasks to outlive sessions and expose current evidence-backed progress later. | Go control plane; runner | 1 and 3 | State transition, durable receipt, stale version, crash recovery, and unknown-effect reconciliation |
| `CY-08-04` | Persist outcomes, external status, pending work, and recovery data; avoid repeating irreversible effects after uncertainty. | Go control plane; runner | 1 and 3 | State transition, durable receipt, stale version, crash recovery, and unknown-effect reconciliation |
| `CY-08-05` | Link project tasks, dependencies, parallel and child work, shared resources, and ambiguous references. | Go control plane; runner | 1 and 3 | State transition, durable receipt, stale version, crash recovery, and unknown-effect reconciliation |
| `CY-09-01` | Separate finite task lifecycles from durable service lifecycles and give each service trigger a run record. | Control plane; runner; clients | 4 | Service/execution separation, event dedupe, delivery and lifecycle evidence; scenarios B and E |
| `CY-09-02` | Store service identity, purpose, scope, environment, trigger, permission, configuration, status, health, errors, and recovery details. | Control plane; runner; clients | 4 | Service/execution separation, event dedupe, delivery and lifecycle evidence; scenarios B and E |
| `CY-09-03` | Trigger programs or agents from mail, files, schedules, APIs, task outcomes, failures, and user events. | Control plane; runner; clients | 4 | Service/execution separation, event dedupe, delivery and lifecycle evidence; scenarios B and E |
| `CY-09-04` | Classify and deduplicate routine, completion, failure, and decision notices; prevent noisy repetition. | Control plane; runner; clients | 4 | Service/execution separation, event dedupe, delivery and lifecycle evidence; scenarios B and E |
| `CY-10-01` | Retain source-linked memory across conversations, tasks, projects, devices, and restarts. | Go memory authority; Mem0; Qdrant | 2 and 5 | Version/source/tombstone tests, stale-index rebuild, permissions, uncertainty and deletion evidence; scenario C |
| `CY-10-02` | Separate global, project, task, and raw-history memory while preserving project access boundaries. | Go memory authority; Mem0; Qdrant | 2 and 5 | Version/source/tombstone tests, stale-index rebuild, permissions, uncertainty and deletion evidence; scenario C |
| `CY-10-03` | Optional event-driven Memory Agent may identify facts, links, duplicates, staleness, conflicts, and update suggestions. | Go memory authority; Mem0; Qdrant | 2 and 5 | Version/source/tombstone tests, stale-index rebuild, permissions, uncertainty and deletion evidence; scenario C |
| `CY-10-04` | Distinguish confirmed facts, execution evidence, and AI inference; track decision timing and validity. | Go memory authority; Mem0; Qdrant | 2 and 5 | Version/source/tombstone tests, stale-index rebuild, permissions, uncertainty and deletion evidence; scenario C |
| `CY-10-05` | Keep task, permission, configuration, and confirmed-decision authority outside unrestricted memory writes. | Go memory authority; Mem0; Qdrant | 2 and 5 | Version/source/tombstone tests, stale-index rebuild, permissions, uncertainty and deletion evidence; scenario C |
| `CY-10-06` | Retrieve only relevant authorized history, cite sources, save context, and surface missing/conflicting evidence as uncertainty. | Go memory authority; Mem0; Qdrant | 2 and 5 | Version/source/tombstone tests, stale-index rebuild, permissions, uncertainty and deletion evidence; scenario C |
| `CY-11-01` | Assemble current context separately from long-term memory according to task, agent, and permissions. | Control plane; memory authority; AO gateway | 2 and 3 | Per-runtime context source/scope/token record and cross-project negative access tests |
| `CY-12-01` | Use one versioned state authority across devices and expose stale cache/conflicts clearly. | Go control plane; SwiftUI clients | 1 and 5 | Multi-device version convergence and stale/offline presentation; scenario A |
| `CY-12-02` | Continue one linked project/task across Mac and iPhone and return to the same work context. | Go control plane; SwiftUI clients | 1 and 5 | Multi-device version convergence and stale/offline presentation; scenario A |
| `CY-12-03` | Keep task existence and status independent of any conversation window. | Go control plane; SwiftUI clients | 1 and 5 | Multi-device version convergence and stale/offline presentation; scenario A |
| `CY-13-01` | Replace models without rewriting task, memory, and service records. | AO gateway; adapters; voice gateway | 2 and 5 | Provider/runtime/voice replacement and dependency-failure isolation evidence |
| `CY-13-02` | Integrate heterogeneous agents through shared capability, execution, result, and permission contracts. | AO gateway; adapters; voice gateway | 2 and 5 | Provider/runtime/voice replacement and dependency-failure isolation evidence |
| `CY-13-03` | Replace speech recognition, synthesis, or live voice without changing core task and project data. | AO gateway; adapters; voice gateway | 2 and 5 | Provider/runtime/voice replacement and dependency-failure isolation evidence |
| `CY-13-04` | Isolate external service integration failures from unrelated modules. | AO gateway; adapters; voice gateway | 2 and 5 | Provider/runtime/voice replacement and dependency-failure isolation evidence |
| `CY-14-01` | Apply least privilege per agent and service; Super authority does not implicitly transfer to workers. | Go control plane; gateway; runner; clients | 1, 2, and 5 | Bootstrap, approval/revocation, least-privilege negative tests and audit trail |
| `CY-14-02` | Authorize read, write, send, delete, generate, and deploy actions separately, with risk-based confirmation. | Go control plane; gateway; runner; clients | 1, 2, and 5 | Bootstrap, approval/revocation, least-privilege negative tests and audit trail |
| `CY-14-03` | Let users inspect, change, revoke grants, stop tasks/services, audit actions, and confirm high-risk work. | Go control plane; gateway; runner; clients | 1, 2, and 5 | Bootstrap, approval/revocation, least-privilege negative tests and audit trail |
| `CY-14-04` | Isolate project, worker, service data and execution; protect sensitive context and audit important actions. | Go control plane; gateway; runner; clients | 1, 2, and 5 | Bootstrap, approval/revocation, least-privilege negative tests and audit trail |
| `CY-15-01` | Detect service exit, agent failure, unavailable APIs, network loss, timeout, denial, resource shortage, and conflicts. | Control plane; runner; operations | 1, 3, and 5 | Fault injection at effect boundaries; no blind retry; measured recovery and health evidence; scenario E |
| `CY-15-02` | Retry safely; reconcile external effects when possible and require resolution for unknown irreversible outcomes. | Control plane; runner; operations | 1, 3, and 5 | Fault injection at effect boundaries; no blind retry; measured recovery and health evidence; scenario E |
| `CY-15-03` | Distinguish request receipt, scheduling, execution, agent output, successful operation, and user-goal completion. | Control plane; runner; operations | 1, 3, and 5 | Fault injection at effect boundaries; no blind retry; measured recovery and health evidence; scenario E |
| `CY-15-04` | Monitor service health, recover within policy, notify users, and retain context when recovery fails. | Control plane; runner; operations | 1, 3, and 5 | Fault injection at effect boundaries; no blind retry; measured recovery and health evidence; scenario E |
| `CY-16-01` | Persist important records and report acceptance only after durable commit. | Go control plane; operations | 1 and 5 | Crash durability, export/deletion, single-instance fencing and restore rehearsal |
| `CY-16-02` | Migrate projects, history, tasks, memory, and services without binding records to one model provider. | Go control plane; operations | 1 and 5 | Crash durability, export/deletion, single-instance fencing and restore rehearsal |
| `CY-16-03` | Back up and restore important database, files, and configuration with rehearsed evidence. | Go control plane; operations | 1 and 5 | Crash durability, export/deletion, single-instance fencing and restore rehearsal |
| `CY-16-04` | Retain decision/task/memory source, version, and effective-time history. | Go control plane; operations | 1 and 5 | Crash durability, export/deletion, single-instance fencing and restore rehearsal |
| `CY-16-05` | Protect content, files, conversations, secrets, external grants, and configuration. | Go control plane; operations | 1 and 5 | Crash durability, export/deletion, single-instance fencing and restore rehearsal |
| `CY-16-06` | Support permission-aware inspection, export, and deletion across derived stores and defined backup policy. | Go control plane; operations | 1 and 5 | Crash durability, export/deletion, single-instance fencing and restore rehearsal |
| `CY-17-01` | Use programs for time, file, rule, state-transition, and health checks instead of model polling. | Control plane; AO gateway; runner | 1, 2, and 4 | Usage ledger, four-run cap and budget enforcement under failure and idle operation |
| `CY-17-02` | Build minimal relevant context without sacrificing traceability or correctness. | Control plane; AO gateway; runner | 1, 2, and 4 | Usage ledger, four-run cap and budget enforcement under failure and idle operation |
| `CY-17-03` | Record and bound model usage; stop further dispatch when budgets or loop limits are reached. | Control plane; AO gateway; runner | 1, 2, and 4 | Usage ledger, four-run cap and budget enforcement under failure and idle operation |
| `CY-17-04` | Keep routine monitoring, scheduling, and event processing efficient without high-frequency model calls. | Control plane; AO gateway; runner | 1, 2, and 4 | Usage ledger, four-run cap and budget enforcement under failure and idle operation |
| `CY-18-01` | Retain extensible ownership and access boundaries for multiple users, projects, agents, devices, and services. | Go control plane; authorization | 1 and 5 | Ownership model and two-principal isolation tests; no team UI is implied |
| `CY-19-A` | Scenario A: voice/text software task, native agent execution, client disconnect, cross-device progress, continuation and safe stop. | All components named by each scenario | 3, 4, and 5 | Device timeline, project/task/run links, agent result and failure evidence, client disconnect/reconnect, continuation and stop; optional native handoff separate. |
| `CY-19-B` | Scenario B: authorized long-running mail capability with deterministic, worker, and Super routing plus notices. | All components named by each scenario | 3, 4, and 5 | Authorization denial before deployment; durable service/run IDs; ordinary/worker/Super routing; service survives client/Super exit; real lifecycle outcomes. |
| `CY-19-C` | Scenario C: recover dated project decisions and reasons across devices without inventing missing evidence. | All components named by each scenario | 3, 4, and 5 | Source-linked old/new decision versions and dates, restart/device query, adversarial stale and unsupported claims. |
| `CY-19-D` | Scenario D: parallel specialist work, conflict coordination, and truthful aggregate outcomes. | All components named by each scenario | 3, 4, and 5 | Two runtime timelines, independent evidence, injected shared-write conflict, failed/canceled branch and truthful final status. |
| `CY-19-E` | Scenario E: unattended network/API/process failure detection, safe recovery, unknown-effect handling, and records. | All components named by each scenario | 3, 4, and 5 | Network loss, lost external receipt, process crash, recovery timeline, zero blind duplicate effects, user escalation when unqueryable. |
| `CY-20-01` | Meet and measure all fifteen system-quality dimensions; unspecified thresholds remain open targets. | All components | 5 | Evidence for each dimension in “System-wide quality acceptance” above; thresholds remain to be set |
| `CY-21-01` | Keep Cylo outside the boundaries of chat-only, Super-only, orchestration-only, editor, OS, memory-only, and personal-only products. | Control plane and architecture | 1 and 5 | Scope review and A–E evidence demonstrate the product remains within the stated boundaries |
| `CY-22-01` | Deliver the complete product behavior across interaction, decisions, execution, interoperability, services, memory, state, security, modularity, and evolution. | All components | 5 | A–E complete and every in-scope stable ID has evidence or a recorded open gap |
