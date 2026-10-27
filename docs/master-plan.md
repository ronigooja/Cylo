# Cylo delivery plan

**Status:** implementation is underway; no product milestone is accepted. Source, fixtures, hosted CI, installation, and complete scenario acceptance have separate meanings. Detailed requirements live in [product](product.md), technical ownership in [architecture](architecture.md), and recorded choices in [ADRs](architecture/decisions/).

## Milestones and acceptance

Milestones sequence delivery without reducing product scope. The [acceptance matrix](acceptance/requirements.md) maps every stable requirement to milestones, evidence, and gaps; [the evidence register](acceptance/evidence.md) records source versions, environments, results, and limits.

| Milestone | Scope | Exit gate | Status |
|---|---|---|---|
| 0. Documentation baseline | Current requirements, architecture, decisions, procedures, and traceability | Complete unique requirements and working source links; honest evidence boundaries | Current requirements and provenance migrated; independent review complete; hosted documentation checks gate changes |
| 1. Contracts and trusted task foundation | Control plane, runner, command/task/event authority, shared instance-key authentication, native shells | Atomic durable receipt/work/outbox under faults; dedupe and stale-version rejection; key rotation/denial; authenticated runner flow; three native clients; single-instance fencing | Source and hosted checks exist; product acceptance open |
| 2. Runtime, voice, and memory integration | AO gateway, turn/receipt bridge, controlled memory and derived index | AO dispatch/report authorization; four-run cap; safe voice lease release; authority-first memory and tombstone/rebuild behavior | Source/component checks exist; real integration gaps remain |
| 3. Complete task loop with two runtimes | Request, coordination, evidence, decisions, stop, recovery, result | Two real runtime types; scenarios A and D; provider/tool/artifact evidence and confirmed termination; no duplicated irreversible effects | Open; Codex route alone is insufficient |
| 4. Long-running services | Authorized lifecycle, deterministic triggers, escalation, notices | Actual process outcomes; service survives client/Super exit; scenarios B and E; bounded retry/dedupe | Source and synthetic checks exist; real external actions open |
| 5. Reliability and Apple delivery | Data lifecycle, security, performance, native delivery | Measured restore/RPO/RTO, migration fencing, security negatives, real-device voice/APNs, A–E and all required IDs/quality dimensions | Open |

## Current status and blockers

| Area | Available evidence | Required next evidence |
|---|---|---|
| Control and AO | Hosted Go/race and patched AO checks; deployed health/readiness observations | Real authorized AO/provider/tool/artifact chain and stop; deployed chat recovery is source-reviewed, remote-unverified; existing uncertain runs must not be assumed recovered |
| Budget enforcement | Reservation/settlement and unknown-usage ledger source checks | Actual AO per-provider-call and cost reporting; production pre-call tool-step and bounded artifact enforcement; online deadline fencing |
| Memory and data lifecycle | Go/Python/component fixtures, export validation, deletion manifests and gated restore checks | Real Mem0/Qdrant and external-store cleanup; measured production backup erasure and restore; unsupported adapters remain closed |
| Voice | Turn identity/dedupe and generation-fenced cleanup source checks | Trusted gateway account identity and account-scoped termination evidence; safe reuse, interruption, and real-device voice-to-task acceptance |
| Apple clients | Hosted Swift tests/macOS archive/iOS build and limited Mac installation evidence | Current-key authenticated flows, screenshots/accessibility/core interactions, real iPhone/iPad/APNs/voice, distribution signing and delivery |
| Capabilities | Admission/revocation and synthetic lifecycle fixtures | Real AO-trigger execution and external service lifecycle/recovery |

No fixture, health response, model catalog, or installation result accepts a complete milestone. The [matrix](acceptance/requirements.md) and [evidence register](acceptance/evidence.md) provide the detailed boundary for each claim. Operations belong in [deployment](operations/deployment.md) and [data lifecycle](operations/data-lifecycle.md).

CI scope optimization is implemented and its full hosted selection passed:
independent Apple/Go/race/script checks, routine builds without release packaging,
and a gate requiring every selected group. See [verification procedures](development/verification.md)
and [retained evidence](acceptance/evidence.md#scoped-ci-verification).
Manual release packaging and remote branch-protection configuration remain outside
this verification; product milestone acceptance is unchanged.

## Delivery dependencies

Runtime work depends on AO and supported adapters; model routes depend on the existing external CLIProxyAPI; voice depends on Space3044 gateway lifecycle evidence and trusted media transport. Apple delivery depends on signing, platform permissions, Keychain, APNs, and real devices. Integration must recheck each dependency's actual compatibility without changing the external CLIProxyAPI deployment.

SQLite WAL/FULL is a single-instance persistence choice, not multi-host database availability. Production reliability, isolation, numeric service-quality thresholds, and measured recovery targets remain unaccepted. Quality requirements and their acceptance boundaries remain in [product chapter 20](product.md#20-overall-quality-requirements) and [acceptance](acceptance/requirements.md).
