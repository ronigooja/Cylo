# Cylo master plan

**Status:** planning baseline; no product implementation has started. This document is the current decision and delivery plan. Source requirements and research are preserved under `docs/archive/`; archived proposals are evidence, not current decisions.

## 1. Status and decisions

Cylo is a private, self-hosted system that keeps authorized work alive beyond a conversation or client session. The selected baseline is a private Linux Go control server, an independent runner, SQLite in WAL mode with `synchronous=FULL`, and native SwiftUI clients for macOS, iPhone, and iPad.

AO (OrchestratorInc/agent-orchestrator) owns creation and lifecycle of every runtime, including worker and curator runtimes. Cylo routes runtime creation through its authorized gateway and remains the authority for business tasks, permissions, events, and memory records. The existing remote CLIProxyAPI deployment is reused as-is and is not modified. Space3044 `chatgpt-web-voice` supplies the selected ChatGPT web real-time gateway and account pool. Mem0 is a controlled worker; Qdrant is a rebuildable retrieval index.

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
- Daily backups retained for 30 days, RPO of 24 hours, and RTO of two hours are planning targets, not measured guarantees.

## 2. Requirements and scope

The product baseline is the complete 22-chapter product requirements document, including scenarios A–E. It is not reduced to a minimum product subset. The current delivery sequence changes when a capability arrives, not whether an in-scope requirement remains required. The official Codex desktop app is out of scope; native Codex CLI execution, viewing, stopping, and durable recovery remain requirements. Human handoff into an existing CLI session is optional. See [the requirements source and full acceptance detail](archive/research/reports/01-requirements.md).

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

## 4. Native application flows

The supported clients are native SwiftUI applications for macOS, iPhone, and iPad. They authenticate a device after one-use bootstrap and explicit approval. Device credentials are stored in Keychain and can be revoked centrally. A device shows the server's authoritative version and exposes stale/offline status instead of presenting cached state as current.

Users can create and follow projects, submit commands by text or voice, inspect task evidence and service health, answer approval requests, stop work, and receive linked notifications. Conversation and voice-session lifetimes do not control task lifetimes. Raw audio retention is off by default. Cross-device continuation means access to the same durable task context; it does not claim live audio migration between devices.

## 5. Execution and model routing

Cylo validates a request, authorization, budget, and concurrency slot before asking AO to create or select a runtime. AO owns runtime creation and lifecycle. Cylo controls task state and accepted effects. Workers return structured outcomes and evidence; deterministic transitions, health checks, and ordinary routing do not require model calls. Super is reserved for complex coordination and escalation.

The shared maximum is four active runs across Super, workers, and curator. Every dispatched run has a Cylo task/run identity, scoped context, permissions, and budget accounting. Existing CLIProxyAPI routing remains external and unchanged. AO must use its configured endpoint and secret reference; credentials are never copied into Cylo documents or logs.

## 6. Voice

Space3044 `chatgpt-web-voice` is the selected real-time ChatGPT web gateway and account pool. Cylo must bridge stable user turns to durable commands and return business receipts that distinguish voice transport success from task acceptance and task completion. A stable turn identity and revision scheme prevents duplicate dispatch after retransmission or interruption.

Device, account, and media resources use leases. A lease is released only after termination is confirmed; an uncertain session or account is quarantined rather than reused. Account exhaustion changes the media session without re-dispatching the task. No raw audio is retained by default. Real-device latency, interruption, mobile lifecycle, and account-pool behavior remain acceptance work, not established capabilities.

## 7. Memory and context

The Go control plane owns authoritative memory versions, source references, and tombstones. Mem0 operates as a controlled worker with scoped capabilities and cannot alter task, permission, configuration, or confirmed decision authority without the corresponding policy path. Qdrant contains rebuildable derived retrieval data.

An accepted memory change is synchronously visible through the authority. Index, cache, replica, and backup cleanup may complete asynchronously, with version/tombstone checks preventing stale resurrection. Context is assembled per task, runtime, project, and permission scope. Facts, execution outcomes, and model inferences remain distinguishable and traceable. Retrieval omission or conflict is surfaced as uncertainty rather than filled by invention.

## 8. Long-running services

A task is finite work; a service is a durable definition with its own identity, configuration, trigger, permissions, health, and execution history. Services are created or changed only after the required user authorization. Deterministic event handling runs as ordinary code; a worker or Super is called only when judgment is needed. Service execution remains independent of a Super conversation and client availability.

Each service lifecycle action must report the actual process or deployment result. Stop, pause, revoke, update, and uninstall cannot be represented by a UI state change alone. Service executions share the same global four-run limit when they invoke runtimes.

## 9. APIs, events, and ownership

The public client API authenticates a device and accepts versioned, idempotent commands. A committed command transaction writes its receipt, durable work record, and outbox event atomically. Event consumers deduplicate by stable event identity. The outbox is the source for delivery retries; external side effects keep their own idempotency or reconciliation state.

Optimistic versions reject stale writes with a recoverable conflict. AO callbacks and runner reports are observations or proposals until the control plane validates and commits them. Memory changes use authority versions and tombstones. Voice gateway callbacks correlate to stable voice turn IDs and Cylo command IDs. Exact wire schemas are implementation contracts to be defined in milestone 1.

## 10. Security, reliability, and lifecycle

Identity starts with a one-use bootstrap; subsequent device access requires approval. Revocation blocks future operations and defines how in-flight work is stopped or reconciled. Secrets are references to deployment-managed credentials. Every command, approval, runtime dispatch, external effect, and authority change is auditable.

Unknown side-effect results never trigger blind retry. If the effect can be queried, reconcile it; otherwise mark it unknown and request human resolution. Recovery preserves receipts and work identity to avoid duplicate effects. Device/account/media leases must terminate before reuse, with uncertain accounts quarantined. A database restore or migration must fence the old instance so only one server can write.

Daily backup, 30-day retention, RPO 24 hours, and RTO two hours are unverified targets. The first implementation must define backup contents, encryption/key recovery, restore procedure, and deletion treatment for backups. The targets become claims only after repeatable restore evidence.

## 11. Repository and workflow

The repository is documentation-only at this baseline. English is used for repository documents; Chinese is used for routine user-facing progress, decisions, findings, questions, and final reports. Archived documents remain historical evidence and are preserved verbatim. Do not treat an archived proposal as a current decision. Keep secrets out of tracked files. Report verification commands and results honestly; static checks, fixtures, and prototypes must be labeled by what they actually prove.

The main agent coordinates implementation. When delegation is requested, implementation and independent verification go to GPT-6 Luna at high reasoning effort in separate worktrees under `$HOME/subagents`, with narrow ownership and commit reporting.

## 12. Milestones and acceptance

| Milestone | Scope | Exit evidence |
|---|---|---|
| 0. Documentation baseline | English README, repository guidance, master plan, preserved research archive | Decisions, scope, source links, stable IDs, risks, and status are reviewable; no implementation claimed |
| 1. Contracts and trusted task foundation | Go service and independent runner; SQLite WAL/FULL; API and ownership contracts; device bootstrap/approval/revocation; projects, commands, tasks, events, durable receipts/outbox; optimistic versions; native SwiftUI app shells; task evidence and status | Atomic receipt/work/outbox under crash injection; duplicate command/event suppression; stale-version rejection; device revocation behavior; real runner report flow; app shells on macOS/iPhone/iPad; Linux deployment and one-active-instance fencing |
| 2. Runtime, voice, and memory integration | AO gateway owns runtime creation; voice turn/receipt bridge; Mem0 controlled worker; Qdrant rebuild path; source/version/tombstone semantics | AO dispatch and callback authorization; four-run cap; stable voice turn dedupe, leases and uncertain-account quarantine; authority-first memory visibility, rebuildable Qdrant, tombstone and backup behavior |
| 3. Complete task loop with two runtimes | User request through planning/worker execution, durable progress, evidence, approval, stop/recovery, and result; at least two runtime types | Two-runtime execution; task lifecycle and outcomes grounded in runner evidence; restart and unknown-effect recovery; scenario A and D evidence; no duplicate irreversible effects |
| 4. Long-running services | Authorized service creation, lifecycle, deterministic events, worker escalation, notifications and service history | Service outlives client and Super session; all lifecycle operations reflect actual process state; scenario B and E evidence; bounded retries and notification dedupe |
| 5. Production reliability and Apple delivery | Backup/restore, migration fencing, data control, hardening, complete native client flows and delivery, performance and operations | Restore rehearsal with measured RPO/RTO; fault injection and security negatives; real-device voice and notification checks; complete scenarios A–E; every in-scope ID has evidence or an explicitly unresolved acceptance gap |

Milestones are sequencing, not product scope reduction. The global four-run cap applies at every milestone that dispatches runtimes.

## 13. Dependencies and limits

The implementation depends on AO and its supported runtime adapters, the existing remote CLIProxyAPI deployment, Space3044 voice gateway behavior, Apple platform delivery and Keychain, and Linux process/service facilities. External services can change independently. Their availability, supported interfaces, account eligibility, quota behavior, and release compatibility must be checked at integration time.

No production reliability, voice quality, migration, backup recovery, isolation boundary, or numeric service target is established yet. SQLite WAL/FULL is the selected initial persistence mode, not a claim of multi-host database availability. The one-active-instance rule is mandatory during migration and restore. The backup policy's RPO/RTO values remain targets pending measurements. Existing CLIProxyAPI remains an external dependency and is outside this repository's deployment lifecycle.

## 14. History and references

The complete product PRD and research reports are retained under `docs/archive/research/`, including the requirements source, superseded plans, project comparisons, Codex Super material, reports 01–19, and original evidence. Report 01 is the requirements traceability source; it retains the stable IDs below. Report 02 is the orchestration and Codex history; reports 03, 06, 08, 11, 12, 13, 14, 16, 17, and 18 cover voice, AO boundaries, proxy integration, and memory research. Archived documents are not the current implementation baseline where they conflict with the decisions in section 1.

### Stable requirement traceability

The requirement descriptions and detailed acceptance scenarios live in [archived report 01](archive/research/reports/01-requirements.md). This index preserves every stable ID and maps it to the responsible area, target milestone, and evidence required. Evidence is a future acceptance artifact, not a claim that it exists or has passed. “N/A” marks an explicitly excluded requirement; “Optional” marks an enhancement that does not block the baseline.

| Stable ID | Requirement family | Primary component | Target | Acceptance evidence |
|---|---|---|---|---|
| `CY-01-01` | Product purpose and design boundaries | Control plane; runner; clients | 1 and 5 | Architecture review against the ten principles and seven exclusions; complete A–E acceptance |
| `CY-01-02` | Product purpose and design boundaries | Control plane; runner; clients | 1 and 5 | Architecture review against the ten principles and seven exclusions; complete A–E acceptance |
| `CY-01-03` | Product purpose and design boundaries | Control plane; runner; clients | 1 and 5 | Architecture review against the ten principles and seven exclusions; complete A–E acceptance |
| `CY-02-01` | System responsibilities and deterministic control | Go control plane and AO gateway | 1 | Ownership map, API contracts, and trace proving deterministic transitions do not call models |
| `CY-03-01` | Native device access, interaction, voice, and notices | SwiftUI clients; voice gateway; control plane | 1, 2, and 5 | macOS/iPhone/iPad device runs; voice and notice evidence; scenario A |
| `CY-03-02` | Native device access, interaction, voice, and notices | SwiftUI clients; voice gateway; control plane | 1, 2, and 5 | macOS/iPhone/iPad device runs; voice and notice evidence; scenario A |
| `CY-03-03` | Native device access, interaction, voice, and notices | SwiftUI clients; voice gateway; control plane | 1, 2, and 5 | macOS/iPhone/iPad device runs; voice and notice evidence; scenario A |
| `CY-03-04` | Native device access, interaction, voice, and notices | SwiftUI clients; voice gateway; control plane | 1, 2, and 5 | macOS/iPhone/iPad device runs; voice and notice evidence; scenario A |
| `CY-04-01` | Linux service operation, server responsibility, and migration | Go server; runner; operations | 1 and 5 | Linux unattended run, client disconnect, fenced restore/migration rehearsal; scenario E |
| `CY-04-02` | Linux service operation, server responsibility, and migration | Go server; runner; operations | 1 and 5 | Linux unattended run, client disconnect, fenced restore/migration rehearsal; scenario E |
| `CY-04-03` | Linux service operation, server responsibility, and migration | Go server; runner; operations | 1 and 5 | Linux unattended run, client disconnect, fenced restore/migration rehearsal; scenario E |
| `CY-05-01` | Agent/runtime interoperability and work continuity | AO gateway; adapters; runner | 1 and 3 | AO adapter execution and supported recovery evidence; no custom Codex recovery is implied |
| `CY-05-02` | Agent/runtime interoperability and work continuity | AO gateway; adapters; runner | N/A | No desktop-app discovery, deep link, or host handoff acceptance; scope exclusion retained |
| `CY-05-03` | Agent/runtime interoperability and work continuity | AO gateway; adapters; runner | Optional | If implemented, prove original session/project/workdir and controlled handoff per report 01 |
| `CY-05-04` | Agent/runtime interoperability and work continuity | AO gateway; adapters; runner | Optional | If implemented, prove original session/project/workdir and controlled handoff per report 01 |
| `CY-05-05` | Agent/runtime interoperability and work continuity | AO gateway; adapters; runner | 1 and 3 | Prove base task recovery and conflict protection; any human session handoff is optional |
| `CY-05-06` | Agent/runtime interoperability and work continuity | AO gateway; adapters; runner | 1 and 3 | AO adapter execution and supported recovery evidence; no custom Codex recovery is implied |
| `CY-06-01` | Super, worker, routing, and multi-agent coordination | AO; control plane; runner | 2 and 3 | Two runtime types, routing traces, dependency/failure/stop evidence; scenario D |
| `CY-06-02` | Super, worker, routing, and multi-agent coordination | AO; control plane; runner | 2 and 3 | Two runtime types, routing traces, dependency/failure/stop evidence; scenario D |
| `CY-06-03` | Super, worker, routing, and multi-agent coordination | AO; control plane; runner | 2 and 3 | Two runtime types, routing traces, dependency/failure/stop evidence; scenario D |
| `CY-06-04` | Super, worker, routing, and multi-agent coordination | AO; control plane; runner | 2 and 3 | Two runtime types, routing traces, dependency/failure/stop evidence; scenario D |
| `CY-07-01` | Authorized capability creation and lifecycle | Control plane; AO; runner | 4 | Authorization-negative test and actual install/update/stop/uninstall lifecycle evidence; scenario B |
| `CY-07-02` | Authorized capability creation and lifecycle | Control plane; AO; runner | 4 | Authorization-negative test and actual install/update/stop/uninstall lifecycle evidence; scenario B |
| `CY-07-03` | Authorized capability creation and lifecycle | Control plane; AO; runner | 4 | Authorization-negative test and actual install/update/stop/uninstall lifecycle evidence; scenario B |
| `CY-07-04` | Authorized capability creation and lifecycle | Control plane; AO; runner | 4 | Authorization-negative test and actual install/update/stop/uninstall lifecycle evidence; scenario B |
| `CY-08-01` | Authoritative task model, lifecycle, and recovery | Go control plane; runner | 1 and 3 | State transition, durable receipt, stale version, crash recovery, and unknown-effect reconciliation |
| `CY-08-02` | Authoritative task model, lifecycle, and recovery | Go control plane; runner | 1 and 3 | State transition, durable receipt, stale version, crash recovery, and unknown-effect reconciliation |
| `CY-08-03` | Authoritative task model, lifecycle, and recovery | Go control plane; runner | 1 and 3 | State transition, durable receipt, stale version, crash recovery, and unknown-effect reconciliation |
| `CY-08-04` | Authoritative task model, lifecycle, and recovery | Go control plane; runner | 1 and 3 | State transition, durable receipt, stale version, crash recovery, and unknown-effect reconciliation |
| `CY-08-05` | Authoritative task model, lifecycle, and recovery | Go control plane; runner | 1 and 3 | State transition, durable receipt, stale version, crash recovery, and unknown-effect reconciliation |
| `CY-09-01` | Services, event routing, and notifications | Control plane; runner; clients | 4 | Service/execution separation, event dedupe, delivery and lifecycle evidence; scenarios B and E |
| `CY-09-02` | Services, event routing, and notifications | Control plane; runner; clients | 4 | Service/execution separation, event dedupe, delivery and lifecycle evidence; scenarios B and E |
| `CY-09-03` | Services, event routing, and notifications | Control plane; runner; clients | 4 | Service/execution separation, event dedupe, delivery and lifecycle evidence; scenarios B and E |
| `CY-09-04` | Services, event routing, and notifications | Control plane; runner; clients | 4 | Service/execution separation, event dedupe, delivery and lifecycle evidence; scenarios B and E |
| `CY-10-01` | Durable memory, provenance, and retrieval | Go memory authority; Mem0; Qdrant | 2 and 5 | Version/source/tombstone tests, stale-index rebuild, permissions, uncertainty and deletion evidence; scenario C |
| `CY-10-02` | Durable memory, provenance, and retrieval | Go memory authority; Mem0; Qdrant | 2 and 5 | Version/source/tombstone tests, stale-index rebuild, permissions, uncertainty and deletion evidence; scenario C |
| `CY-10-03` | Durable memory, provenance, and retrieval | Go memory authority; Mem0; Qdrant | 2 and 5 | Version/source/tombstone tests, stale-index rebuild, permissions, uncertainty and deletion evidence; scenario C |
| `CY-10-04` | Durable memory, provenance, and retrieval | Go memory authority; Mem0; Qdrant | 2 and 5 | Version/source/tombstone tests, stale-index rebuild, permissions, uncertainty and deletion evidence; scenario C |
| `CY-10-05` | Durable memory, provenance, and retrieval | Go memory authority; Mem0; Qdrant | 2 and 5 | Version/source/tombstone tests, stale-index rebuild, permissions, uncertainty and deletion evidence; scenario C |
| `CY-10-06` | Durable memory, provenance, and retrieval | Go memory authority; Mem0; Qdrant | 2 and 5 | Version/source/tombstone tests, stale-index rebuild, permissions, uncertainty and deletion evidence; scenario C |
| `CY-11-01` | Permission-scoped context assembly | Control plane; memory authority; AO gateway | 2 and 3 | Per-runtime context source/scope/token record and cross-project negative access tests |
| `CY-12-01` | Versioned shared state and device continuity | Go control plane; SwiftUI clients | 1 and 5 | Multi-device version convergence and stale/offline presentation; scenario A |
| `CY-12-02` | Versioned shared state and device continuity | Go control plane; SwiftUI clients | 1 and 5 | Multi-device version convergence and stale/offline presentation; scenario A |
| `CY-12-03` | Versioned shared state and device continuity | Go control plane; SwiftUI clients | 1 and 5 | Multi-device version convergence and stale/offline presentation; scenario A |
| `CY-13-01` | Replaceable models, runtimes, voice, and integrations | AO gateway; adapters; voice gateway | 2 and 5 | Provider/runtime/voice replacement and dependency-failure isolation evidence |
| `CY-13-02` | Replaceable models, runtimes, voice, and integrations | AO gateway; adapters; voice gateway | 2 and 5 | Provider/runtime/voice replacement and dependency-failure isolation evidence |
| `CY-13-03` | Replaceable models, runtimes, voice, and integrations | AO gateway; adapters; voice gateway | 2 and 5 | Provider/runtime/voice replacement and dependency-failure isolation evidence |
| `CY-13-04` | Replaceable models, runtimes, voice, and integrations | AO gateway; adapters; voice gateway | 2 and 5 | Provider/runtime/voice replacement and dependency-failure isolation evidence |
| `CY-14-01` | Identity, authorization, revocation, isolation, and audit | Go control plane; gateway; runner; clients | 1, 2, and 5 | Bootstrap, approval/revocation, least-privilege negative tests and audit trail |
| `CY-14-02` | Identity, authorization, revocation, isolation, and audit | Go control plane; gateway; runner; clients | 1, 2, and 5 | Bootstrap, approval/revocation, least-privilege negative tests and audit trail |
| `CY-14-03` | Identity, authorization, revocation, isolation, and audit | Go control plane; gateway; runner; clients | 1, 2, and 5 | Bootstrap, approval/revocation, least-privilege negative tests and audit trail |
| `CY-14-04` | Identity, authorization, revocation, isolation, and audit | Go control plane; gateway; runner; clients | 1, 2, and 5 | Bootstrap, approval/revocation, least-privilege negative tests and audit trail |
| `CY-15-01` | Fault detection, side-effect recovery, and health | Control plane; runner; operations | 1, 3, and 5 | Fault injection at effect boundaries; no blind retry; measured recovery and health evidence; scenario E |
| `CY-15-02` | Fault detection, side-effect recovery, and health | Control plane; runner; operations | 1, 3, and 5 | Fault injection at effect boundaries; no blind retry; measured recovery and health evidence; scenario E |
| `CY-15-03` | Fault detection, side-effect recovery, and health | Control plane; runner; operations | 1, 3, and 5 | Fault injection at effect boundaries; no blind retry; measured recovery and health evidence; scenario E |
| `CY-15-04` | Fault detection, side-effect recovery, and health | Control plane; runner; operations | 1, 3, and 5 | Fault injection at effect boundaries; no blind retry; measured recovery and health evidence; scenario E |
| `CY-16-01` | Durability, migration, backup, provenance, and data control | Go control plane; operations | 1 and 5 | Crash durability, export/deletion, single-instance fencing and restore rehearsal |
| `CY-16-02` | Durability, migration, backup, provenance, and data control | Go control plane; operations | 1 and 5 | Crash durability, export/deletion, single-instance fencing and restore rehearsal |
| `CY-16-03` | Durability, migration, backup, provenance, and data control | Go control plane; operations | 1 and 5 | Crash durability, export/deletion, single-instance fencing and restore rehearsal |
| `CY-16-04` | Durability, migration, backup, provenance, and data control | Go control plane; operations | 1 and 5 | Crash durability, export/deletion, single-instance fencing and restore rehearsal |
| `CY-16-05` | Durability, migration, backup, provenance, and data control | Go control plane; operations | 1 and 5 | Crash durability, export/deletion, single-instance fencing and restore rehearsal |
| `CY-16-06` | Durability, migration, backup, provenance, and data control | Go control plane; operations | 1 and 5 | Crash durability, export/deletion, single-instance fencing and restore rehearsal |
| `CY-17-01` | Model use, context efficiency, and budget limits | Control plane; AO gateway; runner | 1, 2, and 4 | Usage ledger, four-run cap and budget enforcement under failure and idle operation |
| `CY-17-02` | Model use, context efficiency, and budget limits | Control plane; AO gateway; runner | 1, 2, and 4 | Usage ledger, four-run cap and budget enforcement under failure and idle operation |
| `CY-17-03` | Model use, context efficiency, and budget limits | Control plane; AO gateway; runner | 1, 2, and 4 | Usage ledger, four-run cap and budget enforcement under failure and idle operation |
| `CY-17-04` | Model use, context efficiency, and budget limits | Control plane; AO gateway; runner | 1, 2, and 4 | Usage ledger, four-run cap and budget enforcement under failure and idle operation |
| `CY-18-01` | Extensible ownership and future organization boundaries | Go control plane; authorization | 1 and 5 | Ownership model and two-principal isolation tests; no team UI is implied |
| `CY-19-A` | End-to-end product scenarios A–E | All components named by each scenario | 3, 4, and 5 | Scenario A: native task flow; B: service; C: memory; D: parallel runtimes; E: fault recovery |
| `CY-19-B` | End-to-end product scenarios A–E | All components named by each scenario | 3, 4, and 5 | Scenario A: native task flow; B: service; C: memory; D: parallel runtimes; E: fault recovery |
| `CY-19-C` | End-to-end product scenarios A–E | All components named by each scenario | 3, 4, and 5 | Scenario A: native task flow; B: service; C: memory; D: parallel runtimes; E: fault recovery |
| `CY-19-D` | End-to-end product scenarios A–E | All components named by each scenario | 3, 4, and 5 | Scenario A: native task flow; B: service; C: memory; D: parallel runtimes; E: fault recovery |
| `CY-19-E` | End-to-end product scenarios A–E | All components named by each scenario | 3, 4, and 5 | Scenario A: native task flow; B: service; C: memory; D: parallel runtimes; E: fault recovery |
| `CY-20-01` | System-wide quality dimensions | All components | 5 | Measured acceptance evidence across all 15 quality dimensions; thresholds remain to be set |
| `CY-21-01` | Product boundaries | Control plane and architecture | 1 and 5 | Scope review and A–E evidence demonstrate the product remains within the stated boundaries |
| `CY-22-01` | Complete product definition | All components | 5 | A–E complete and every in-scope stable ID has evidence or a recorded open gap |
