# Cylo architecture

This document records the current architecture and its boundaries. It describes responsibility and required behavior; it does not certify implementation or production readiness. [Product requirements](product.md) define the full scope. [The master plan](master-plan.md) owns delivery status, evidence, open gaps, and acceptance boundaries. Archived research is historical provenance rather than a competing current design.

## System and authority

Cylo is a private, self-hosted Linux Go control server with an independent Linux runner, SQLite WAL with `synchronous=FULL`, and native SwiftUI clients for macOS, iPhone, and iPad. The first release is single-instance and multi-project with one fixed owner principal. User/workspace boundaries remain explicit for future evolution.

| Component | Responsibility | Authority boundary |
|---|---|---|
| Native Apple clients | Conversation, voice/text input, task progress/results, decisions, projects, notices, and local drafts | Render server records and stale/offline state; never establish business completion locally |
| Go control plane | Projects, commands, permissions, tasks, runs, receipts, events, service definitions, memory versions, and audit | Sole authoritative business-state writer |
| Independent runner | Claim admitted work, invoke authorized runtime/service operations, observe results, and report evidence | Separate credentials and scoped claims; cannot grant its own authority |
| AO gateway and AO | Authorized runtime creation and lifecycle, including Super, Workers, and curator runtimes | Cylo gateway admits work; AO owns runtime/session lifecycle |
| Existing remote CLIProxyAPI | Account selection, quota handling, and proxying for its configured model routes | External deployment reused unchanged; no duplicate account rotation in Cylo/AO |
| Space3044 `chatgpt-web-voice` | ChatGPT web real-time transport and voice account pool | Voice transport and spoken output do not authorize or complete business work |
| Mem0 | Scoped extraction, memory proposals, and permitted worker operations | Controlled worker, never unrestricted task/permission/configuration/decision authority |
| Qdrant | Derived retrieval vectors and payloads | Rebuildable index; never the source of truth |
| SQLite and file storage | Durable authority transactions; artifacts/releases and service data in file storage | Database commit establishes acceptance; large outputs do not become database blobs |

The durable association is `Project → Task → Run → AO Session → Runtime Native Session`. Each run records its runtime, model route, execution host, workspace, authorization, artifacts, and result evidence. Role, runtime implementation, and model route are separate choices. The six product responsibilities map onto these components; they do not require six separately deployed services.

There is no PWA, CAO implementation, macOS server, official Codex desktop-app integration, or Codex-specific recovery subsystem. Native CLI handoff is optional and bounded by supported native runtime facilities. See [ADR 0001](architecture/decisions/0001-linux-and-native-apple-clients.md) and [ADR 0002](architecture/decisions/0002-ao-runtime-ownership.md).

## Commands, receipts, and events

The public API uses REST under `/v1` and SSE for ordered events. Implemented data contracts live in [contracts](../contracts/), particularly [HTTP contracts](../contracts/http.go); documents do not establish a second schema.

1. A native client submits a business command with an `Idempotency-Key` and, when applicable, `expected_version`.
2. The control plane checks the current principal, workspace/resource permissions, command validity, and optimistic version.
3. One transaction commits the durable command receipt, work record, and outbox event before returning acceptance.
4. The runner claims admitted work atomically. Cylo checks authority, budget, and the shared run cap before the gateway requests AO work.
5. AO and runner reports return observations/proposals. The control plane validates the current claim, fence, version, and evidence before committing business-state changes.
6. Event delivery retries from the outbox. Consumers deduplicate stable identities and repair missing history from the authority.

Initial command kinds include `project.create`, `task.create`, `task.append`, `task.stop`, and `task.approve`. Workspace references are opaque server-owned IDs, not client-supplied host paths. A timed-out client recovers the original receipt through command ID or original key; it does not create a second command merely because the first response was lost. Drafts clear only after durable acceptance.

An authenticated snapshot gives the current workspace/subject, version, and durable event cursor. Clients resume events using `Last-Event-ID`; an invalid or expired cursor requires snapshot repair. Paged task history reads authoritative messages and deduplicates by message ID. Snapshot caches and push payloads do not create alternative state authorities.

Unknown external effects are reconciled if queryable and escalated otherwise. They are never blindly retried. Receipt, admission, running work, model output, actual operation success, goal completion, and confirmed stop remain distinct. See [ADR 0003](architecture/decisions/0003-durable-authority-and-effects.md).

## Runtime admission and model routing

Every request, including a child Worker request from Super, uses the same controlled admission path. No automatic dispatcher may bypass it. AO owns creation and lifecycle of every runtime; Cylo owns task authority and accepted effects. Prefer AO configuration/adapters and extend AO only for a measured gap.

The global ceiling is **four active runs**, counting Super, Workers, and curator work. A waiting or uncertain runtime does not free a slot merely because no output is visible. Ownership/fence checks and termination evidence govern release. Unknown AO creation is not automatic replay authority; explicit reconciliation requires complete, auditable evidence. The [operator runbook](local-deployment.md#unknown-ao-creation-reconciliation) owns the operational procedure.

Deterministic scheduling, health, state transitions, and routine event routing make no model calls. A semantic event may call a Worker; complex coordination may call Super. Every actual model call must be attributable to an authorized task/run, route, and budget. Usage reservations and settlements must be idempotent; unknown usage remains reserved. Provider calls, tool loops, artifact bytes, and termination limits require enforcement at the actual effect boundary, not merely budget fields in a contract. Full per-provider-call enforcement and cost evidence remain acceptance work; see the master plan.

For routes supplied by CLIProxyAPI, account selection and quota handling remain in that existing deployment. Cylo/AO use deployment-provided endpoint and secret references without changing that service. Adapter registration or a model catalog response does not prove runtime execution, artifact creation, provider usage, or successful stop.

## Identity, clients, and notices

A fresh deployment uses one canonical shared instance key for `owner` in workspace `default`. Runner and memory-worker credentials are separate. There is no password, pairing/bootstrap, approved-device session, saved-server list, or fixed client endpoint in this design. [ADR 0004](architecture/decisions/0004-instance-key-authentication.md) records the decision and its consequences.

Apple clients ask for server URL and key, allow installation-link/QR import for review, and require Connect before use. Keychain is the only credential store. An authenticated snapshot validates the connection; rejected credentials close workspace access, while ordinary transport failure preserves validated state with stale feedback. Replacing/clearing a connection discards its local queue, snapshot, exports, and drafts. Local clearing does not revoke the shared key or stop durable tasks. Key rotation blocks future use of the old key and requires redistribution.

A stable random installation UUID identifies a push registration independently of the shared owner key. One installation cannot overwrite or revoke another installation's token. APNs is only a wake-up hint to reload durable inbox state. Payloads contain no task content or notice body. Delivery retry and invalid-token handling remain server responsibilities; real APNs delivery requires separate acceptance evidence.

The interface follows [product chapter 3](product.md#3-client-requirements). Infrastructure capacity, AO faults, runtime IDs, and reconciliation procedures belong in operator diagnostics/audit, not conversation/task surfaces. Actual task state, actionable submission failure, and pending confirmation remain visible. Runtime diagnostics do not change receipt or retry semantics.

## Voice and business work

Space3044 `chatgpt-web-voice` is the selected gateway/account pool. Native clients use WebRTC with platform-specific audio/lifecycle adapters; WebRTC owns the media pipeline. Dependency pins and platform setup live with [the Apple clients](../clients/apple/README.md) and [voice deployment](../deploy/voice/README.md).

Opening a microphone or upstream session requires explicit user action and granted platform permission. Stable turn IDs, revisions, and deduplication bind voice turns to durable commands. Partial captions and upstream spoken answers never authorize a business action. When a stable turn-completion signal is unavailable, explicit transcript confirmation is the fallback; it does not establish hands-free acceptance.

Device, account, and media leases carry holder, generation, expiry, and termination evidence. A new account gets a new media session while preserving task associations. The old media session must end before account reuse; expiry alone is not evidence of termination. Unknown sessions/accounts remain quarantined. Muting capture, stopping playback, ending voice, and stopping a task are distinct operations. Natural conversation may continue while business work runs; only authoritative task events announce formal completion. Raw audio is not retained by default. See [ADR 0005](architecture/decisions/0005-voice-task-and-lease-boundaries.md).

The selected gateway does not yet establish trusted account-scoped lease identity and termination evidence. Safe pool reuse and real-device voice-to-task behavior remain open acceptance work; the fail-closed boundary is part of the design, not proof of a working end-to-end voice path.

## Memory, retrieval, and context

The Go authority owns memory sources, versions, and tombstones. Accepted changes are synchronously visible there; Mem0/Qdrant maintenance is asynchronous. Explicit user decisions register through policy; model-derived claims remain candidates. Source events, execution facts, and inferences remain distinguishable. Formal decisions and task state reference authoritative ledger records rather than summaries.

The pipeline is `source event → authority → transactional outbox → extraction/indexing → scoped retrieval → permission/version recheck → context assembly`. Context is built per task/runtime/project/permission scope. Retrieval omission or conflict is uncertainty, not permission to invent history. A curator runs on demand for summaries, conflicts, expiry, and relationship judgment, within the same global runtime cap.

Embeddings use a local multilingual model with pinned revision, dimension, and encoding. Structured lookup and Chinese full-text retrieval remain available if vectors/models fail. A codec change creates a new collection and rebuilds it from authority. Exact versions, model hashes, worker configuration, and storage layout belong in [the memory worker documentation](../services/memory/README.md), not duplicated here.

Deletion immediately excludes content from retrieval and future context through a versioned tombstone, then propagates idempotent cleanup across authority, worker history, vectors/payloads, caches, exports, replicas, and backups under retention policy. Stale asynchronous work cannot resurrect deleted data. No cross-store transaction is assumed. Unsupported cleanup targets stay `adapter_missing`; physical erasure cannot be claimed without adapter/checkpoint evidence. Conversations, formal memory, and task evidence are retained until user deletion by default, with configurable retention. See [ADR 0006](architecture/decisions/0006-memory-authority-and-derived-stores.md).

## Capabilities and deployment isolation

A service definition has its own identity, purpose, environment, triggers, permissions, configuration, health, and execution history. Each trigger produces a finite run. Ordinary services continue without a client or Super session and have resource limits separate from the agent-run ceiling.

When deploying generated capabilities, source, tests, and a release manifest first pass capability-manager validation and the applicable user authorization. A constrained manifest names immutable artifact/version, entrypoint, deployment-managed environment references, allowed mounts, and capabilities. The runner enforces user, mount, network, port, CPU, memory, process, and log limits. Workload manifests cannot grant privileged mode, arbitrary host mounts/devices, or Docker-socket access.

Temporary workspaces/caches, immutable releases, deployment configuration, and long-lived service volumes use separate roots. Each persistent volume has an owner, purpose, mount, backup/restore policy, retention rule, and deletion action. AO workspace cleanup cannot remove deployed data or its only release artifact.

The trusted runner is independently supervised on a dedicated Linux execution host and necessarily has Docker-daemon or equivalent host authority. That host authority is trusted infrastructure, not a workload isolation boundary. Keep its socket out of server, memory, AO workload containers, and capability manifests. systemd supervises the control/memory stack and independent runner. [Linux deployment configuration](../deploy/linux/) and [the operator runbook](local-deployment.md) own exact commands and layout.

## Backup, deletion, and recovery

At most one active control server may operate on a database. Migration/restore pauses dispatch, isolates the old instance, restores a consistent snapshot plus artifacts/configuration/required volumes and deletion manifests, and reconciles services and uncertain effects before execution reopens. Secrets are recovered separately or external integrations are reauthorized.

Restore must preserve the operator-maintained current deletion generation/head/hash as its independent trust root. A backup's embedded journal head cannot prove freshness or later deletion coverage. Restore-only gating blocks activation and business reads/writes until reconciliation succeeds. Missing real adapters/checkpoints keep the gate closed; operators cannot replace evidence with a manually asserted success.

Backup inventory and retention include applicable service data and deletion manifests. Physical deletion, retention, host isolation, migration, and measured production restore remain acceptance boundaries. Daily backup scheduling, RPO 24 hours, and RTO two hours are unverified targets, not measured guarantees. Vector-index rebuild duration is reported separately from core recovery time. Operational evidence and procedures belong in the master plan and runbook.

## Decision records

These records capture selected decisions, not new scope or acceptance claims:

- [0001 — Linux server and native Apple clients](architecture/decisions/0001-linux-and-native-apple-clients.md)
- [0002 — AO owns runtime lifecycle](architecture/decisions/0002-ao-runtime-ownership.md)
- [0003 — Durable authority and uncertain effects](architecture/decisions/0003-durable-authority-and-effects.md)
- [0004 — Shared instance-key authentication](architecture/decisions/0004-instance-key-authentication.md)
- [0005 — Voice turns, tasks, and lease termination](architecture/decisions/0005-voice-task-and-lease-boundaries.md)
- [0006 — Memory authority and derived stores](architecture/decisions/0006-memory-authority-and-derived-stores.md)
