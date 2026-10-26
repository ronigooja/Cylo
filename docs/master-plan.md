# Cylo master plan

**Status:** product implementation is underway; no product milestone is accepted. The matrix below separates source implementation, repeatable verification, and acceptance still required. Local tests and synthetic integrations do not establish live AO/provider execution, production deployment, Apple device delivery, or complete scenarios. This document is the current decision and delivery plan. Source requirements and research are preserved under `docs/archive/`; archived proposals are evidence, not current decisions.

## 1. Status and decisions

Cylo is a private, self-hosted system that keeps authorized work alive beyond a conversation or client session. The selected baseline is a private Linux Go control server, an independent runner, SQLite in WAL mode with `synchronous=FULL`, and native SwiftUI clients for macOS, iPhone, and iPad.

AO (OrchestratorInc/agent-orchestrator) owns creation and lifecycle of every runtime, including worker and curator runtimes. Cylo routes runtime creation through its authorized gateway and remains the authority for business tasks, permissions, events, and memory records. The existing remote CLIProxyAPI deployment is reused as-is and is not modified. The local `~/.codex/config.toml` is configured with provider `cliproxyapi` using the Responses API. An authenticated, read-only `/models` check returned HTTP 200 and 13 model entries, including `gpt-6-luna`; this confirms catalog access only, not successful model execution. Never print or track credential values. Space3044 `chatgpt-web-voice` supplies the selected ChatGPT web real-time gateway and account pool; its credentials will be entered manually by the user during setup. Mem0 is a controlled worker; Qdrant is a rebuildable retrieval index.

Decisions and limits:

- The first server and execution host target is Linux. Native clients are macOS, iPhone, and iPad; there is no PWA.
- There is no CAO implementation, macOS server, or Codex-specific recovery feature in the current baseline.
- Runtime creation is delegated to AO through an explicit Cylo gateway. Agent output cannot directly mutate authoritative Cylo state.
- The global concurrency ceiling is four active runs, including Super, workers, and curator.
- A fresh deployment uses one shared instance key for its fixed owner principal. Apple clients collect the server URL and key, import installation links or QR images, and store credentials only in Keychain. There is no password, device approval/session, fixed client endpoint, or saved-server list. The instance-key decision in §4 supersedes earlier authentication designs; rollout and acceptance remain separate.
- Commands are idempotent and create an atomic receipt, work record, and outbox entry. Optimistic versions protect updates. Unknown external effects are reconciled or escalated; they are never blindly retried.
- Voice turns have stable identities, revisions, and deduplication. Device, account, and media leases must end before reuse. Uncertain accounts are quarantined.
- Raw audio is not retained by default.
- Memory authority uses versions and tombstones. Changes become visible synchronously in the authority; asynchronous cleanup updates derived stores. Backups are included in deletion and recovery rules.
- At most one active server instance may operate on a database during migration or restore.
- The first release is single-instance and multi-project. It reserves user/workspace data boundaries but excludes a team collaboration UI, official Codex App integration, and mandatory seamless terminal takeover.
- Role, runtime, and model route are configured separately. Registration of an AO adapter does not establish Cylo support or acceptance for that runtime.

Current implementation status:

The matrix distinguishes source implementation from checks that exercised it. A local fake service, test fixture, design, source review, or successful compile is not production end-to-end acceptance.

| Area | Implemented in source | Verified evidence | Pending acceptance |
|---|---|---|---|
| Control plane and runner | Linux Go control server, SQLite-backed command/task/run records, authenticated instance-key/runner APIs, capability-trigger admission, runner claim/report flow, AO create journal and recovery. AO create handling distinguishes definite rejection from uncertain outcomes; uncertain creates are not blindly replayed. Explicit stop uses ownership/fence checks and requires termination evidence before releasing the run. | On integrated source commit `c0380c7`, the owner reported PASS for `PATH=/usr/local/go/bin:$PATH go test ./cmd/cylo-server -run TestComposedServerRegistersDerivedWorkerCredentialAndFailsClosedWithoutVoiceGateway -count=1` and `PATH=/usr/local/go/bin:$PATH go test ./...`; `git diff --check 2d35a86..HEAD` also passed on the later candidate containing this source. Independent review passed `go test ./internal/capabilities -run TestCapabilityTriggerRunnerAOResultChain -count=1`. The AO-chain fixture uses fake AO HTTP. The initial composition failure was a test-fixture omission; review confirmed production startup registers the maintenance migration. No remaining concrete findings were reported for this integration scope. The earlier live AO create attempt returned HTTP 409 without a retained allowlisted rejection code, so the result is treated as uncertain. A later read-only inspection observed zero sessions, but that does not establish that this request had no external effect; no artifact, provider outcome, or termination proof was recorded. | A successful real AO task chain with provider/tool/artifact evidence and confirmed termination; recovery against a real AO deployment; provider-side usage/cost evidence; production deployment and cross-host migration fencing; complete scenario acceptance. |
| Budget enforcement | Per-run immutable budget snapshots and an idempotent reservation/settlement ledger account for concurrent reservations, overflow, stop, grant, and unknown usage. Unknown usage remains reserved. The current runner only reserves one model call for the initial AO prompt. | Owner and independent AO/control/runner review reported passing targeted tests; the integrated Go suite and relevant race packages passed on the owner checkpoint. These checks cover local ledger and control behavior. | AO has no production per-provider-call reservation hook or actual input/output usage reporting across its internal provider/tool calls; only the initial prompt is currently counted as one `ModelCalls` reservation. It also has no production pre-call tool-step enforcement or bounded artifact sink. `MaxToolSteps` and `MaxArtifactBytes` currently exist as DTO, snapshot, or ledger values, not as end-to-end limits on actual tool loops and artifact writes. Hard-deadline termination needs an online fence and is not an offline hard-stop guarantee. Full usage/cost accounting requires provider-backed evidence. |
| Memory and export | Versioned Go memory authority, controlled Mem0 worker, Qdrant index, deletion sidecar journal, replay/maintenance gates, local source/proposal reconciliation, schema-2 ZIP export, cache/export-temp cleanup, and default 30-day backup artifact purge are implemented in source. Cache and export-temp cleanup use a private 0700 root with 0600 files, workspace registry, and active-export try-lock; busy returns 202, unknown resources fail as `adapter_missing`, and path cleanup requires exact scope/job/lease/tombstone authority. Context-cache deletion is synchronous; final context save rechecks sequence, current claim, and sidecar. | Existing stub-backed Go→Python→Mem0→ephemeral-Qdrant tests cover component composition, not live AO/provider behavior. Bounded export checks passed for >64 MiB ZIP, hash/count, oversized rows, cancellation, permission, and concurrent snapshots; cursor/event-ID alignment passed source review. Restore candidate `59c61d93` passed owner Go/Python/restore-drill checks and independent review with no blocking findings. Cache/export-temp commits `fa4930d`/`9041464` passed independent review; owner reported seven Go-package and two race-package checks PASS. Independent commands passed: `/usr/local/go/bin/go test ./internal/memory ./internal/control ./tests/acceptance ./tests/integration -count=1` and `/usr/local/go/bin/go test -race ./internal/memory ./internal/control -count=1`. Backup purge/manifest commit `f3c8ab6`/`4642ff9` passed independent review and 13 tests: `PYTHONDONTWRITEBYTECODE=1 PYTHONPATH=tools /tmp/cylo-memory-export-venv/bin/python -m unittest data_lifecycle.test_retention data_lifecycle.test_backup_manifest -v`. The integrated local backup fixture also exercised encrypted temporary-backup create/delete, retention, latest-head staging, and restore preparation/reconciliation; it confirmed Mem0/Qdrant remain `adapter_missing`, the restore gate stays closed, and one artifact remains. The backup lifecycle/worker checks are local; they do not establish physical deletion from production external stores. | The maintenance user's current authority generation/head/hash remains the trust root; journal continuity does not prove every restore transition independently. Real Mem0/Qdrant cleanup and physical external-store deletion remain pending; unsupported resources stay `adapter_missing` and cannot be manually cleared. Production backup erasure, a measured retention guarantee, and restore rehearsal remain open. |
| Voice | Stable turn/task bridge, client media close before `media_closed`, generation-fenced session and account release, idempotent lease cleanup, stale-media-generation rejection, hash-only account identifiers, and optional release adapter/HTTP path. Unknown gateway DELETE outcomes are quarantined without automatic retry. | `PATH=/usr/local/go/bin:$PATH go test ./...` passed on the integrated source candidate after commit `c0380c7`, including voice, acceptance, and server packages. This verifies local code paths only. | The selected Space3044 response does not expose account-lease identity or trusted account-scoped termination evidence. The adapter remains fail-closed; safe pool account reuse, real gateway behavior, interruption, device, and voice-to-task acceptance remain unverified. |
| Native Apple clients | Swift source includes server-backed task/inbox flows, WebRTC lifecycle controls, and schema-2 ZIP export download with temporary streaming, SHA/CRC validation, strict ZIP handling, a 1 GiB transfer cap, and cleanup leases/locks. The latest source validates the data cursor separately from the prepared-audit cursor, checks the matching event ID/type, removes exports after authorization changes or snapshot failure, and refreshes the snapshot/connection epoch before Save. | Source review confirmed the server/Apple event-ID and cursor contract, Save-authority checks, and invalidation paths (server `114f7b8`, Apple `e009e39`, fixture fix `0af961d`). At that earlier checkpoint, related tests were added but no Swift tests or builds ran. On 2026-10-24 a macOS Xcode build, installation, launch, relocated WebRTC loading, ad-hoc signature verification, and native backend reachability passed; see local installation evidence below. Existing APNs token registration, revocation, and inbox code is source implementation only. | Authenticated macOS app flows after manual registration, Swift package tests on this installation, distribution signing/notarization, iPhone/iPad installation, APNs delivery, and real-device voice/media acceptance. |
| Apple interface redesign | Adaptive navigation, a shared task workspace and composer, English/Chinese resources, draft-safe typed submission outcomes, original-receipt reconciliation, paged authoritative history, task filtering, contextual project creation, transcript drafts, and grouped secondary screens are implemented as a source candidate. The three-platform HTML reference is a design artifact. | On this Linux host, `PATH=/usr/local/go/bin:$PATH go test ./...` and `PATH=/usr/local/go/bin:$PATH go test -race ./internal/control` passed on the integrated API source; history tests cover ACL/scope denial, cursor/limit validation, stable timestamp ordering, and pagination. At the Linux checkpoint, nine new Swift contract/state regression tests were added but had not run. The later Apple installation refresh below records successful execution of the full 67-test Swift suite and fresh macOS/iOS target builds. A tree-sitter 0.26.0 / Swift grammar 0.7.4 syntax pass covered all 25 Swift files; paired locale keys, literal-source coverage, format placeholders, plural plists, `python3 tools/check_docs.py`, and `git diff --check` passed. These checks do not type-check Swift or render the UI. | The installation refresh establishes fresh Swift tests and macOS/iOS app-target builds, macOS local installation/startup, and Launchpad registration only. Screenshots across the section 4 matrix, core UI flow tests, VoiceOver/Dynamic Type/keyboard checks, real iPhone/iPad devices, and real services remain open. No complete redesign UI or product acceptance is claimed. |
| Capabilities | Opt-in bounded runner admission polling (off by default), authorized task-trigger admission, idempotent worker/supervisor/schedule handling, explicit unsupported-program results, revocation/expiry cancellation and active stop, and waiting results that retain their run slot. Docker service stop requires explicit authorization and fence/ownership plus termination proof. | The integrated trigger→control/engine→fake AO HTTP fixture passed through authorized acceptance, terminal report, and slot release. Owner and independent reviewer both passed the lifecycle test; the independent command was `go test ./internal/capabilities -run TestCapabilityTriggerRunnerAOResultChain -count=1`. Isolated Docker recovery and revocation checks passed on synthetic workloads (76.52 s and 26.62 s respectively). These local tests do not exercise real AO or external business actions. | Real AO-backed trigger execution, real external actions, production service lifecycle/deployment and recovery. |
| Backup and recovery | Encrypted backup and staged restore, persistent restore-only gate, deletion journal sidecar, snapshot-bound metadata, restore validation, deletion-only cleanup route, and default 30-day backup artifact purge are implemented in source. The 30-day purge uses an HMAC-authenticated artifact manifest and locks the private artifact directory. Backup deletion uses a separate private 0600 manifest bound to the trusted journal hash chain by its source-hash reference, with `flock`/`fsync` and lease/scope/tombstone validation. A non-empty journal requires a configured matching independent manifest, never a journal-synthesized success. Both normal and maintenance workers accept only HTTP 200 with JSON `completed`; export cleanup then finishes the generic job, while backup deletion writes its own finish/checkpoint exactly once. | On integrated candidate `d9f97d7e21b81bf62b4f7132698761ac3703bf85`, the owner reported PASS for `PATH=/usr/local/go/bin:$PATH CYLO_OPS_PYTHON=/tmp/cylo-memory-export-venv/bin/python go test -race -timeout 120s ./internal/memory ./tests/ops` and `PATH=/usr/local/go/bin:$PATH go test -timeout 120s ./internal/memory -run 'TestExportTempCleanupHTTPThenDeletionCompletionAdvancesCheckpoint|TestBackupDeleteManifestCleanupHTTP' -count=1`. Independent review accepted the candidate with no blocking findings and passed `PATH=/usr/local/go/bin:$PATH CYLO_OPS_PYTHON=/tmp/cylo-memory-export-venv/bin/python PYTHONDONTWRITEBYTECODE=1 go test -race -timeout 120s ./internal/memory ./tests/ops -run 'TestExportTempCleanupHTTPThenDeletionCompletionAdvancesCheckpoint|TestBackupDeleteManifestCleanupHTTP|TestEncryptedBackupPersistsNonemptyDeletionManifest|TestMaintenanceRESTEncryptedRestoreDrill' -count=1 -v`. On that same source HEAD, the owner also reran `PATH=/usr/local/go/bin:$PATH CYLO_OPS_PYTHON=/tmp/cylo-memory-export-venv/bin/python go test ./...` and `PATH=/usr/local/go/bin:$PATH CYLO_OPS_PYTHON=/tmp/cylo-memory-export-venv/bin/python go test -race -timeout 120s ./internal/memory ./internal/control ./internal/storage ./internal/runner ./tests/ops ./tests/integration ./tests/acceptance`; both passed. Python checks passed: `PYTHONPATH=tools PYTHONDONTWRITEBYTECODE=1 /tmp/cylo-memory-export-venv/bin/python -m unittest discover -s tools/data_lifecycle -t tools -p 'test*.py'` (35 tests), `PYTHONDONTWRITEBYTECODE=1 services/memory/.venv/bin/python -m unittest services.memory.test_deletion_only_worker services.memory.test_mem0_backend services.memory.test_model_call_accounting services.memory.test_worker_logging` (21 tests), and `PYTHONPYCACHEPREFIX=/tmp/cylo-integration-pycache /tmp/cylo-memory-export-venv/bin/python -m compileall -q services/memory tools/data_lifecycle`; all passed. These tests cover authenticated export cleanup through generic-job completion/checkpoint and backup-manifest cleanup through real PUT/DELETE transactions, journal/durable-manifest updates, encrypted CLI round-trip, HMAC retention, latest-head staging, and restore preparation/reconciliation. The Go fixture used synthetic export registry/artifact data and did not launch a Python process; its assertions keep Mem0/Qdrant `adapter_missing`, the restore gate closed, and one artifact present. Separately, Python worker tests cover both modes through RPC and completion. These are local fixture and test results. | Preserve the operator-maintained current generation/head/hash as the trust root; journal continuity does not prove every restore transition independently. Complete real external adapter checkpoints and physical deletion; physical host migration; production host/systemd isolation; measured RPO/RTO; production backup/restore and full data-erasure acceptance. |
| Operator CLI and QR connection | Interactive `cylo` maintenance menu and explicit setup/provider/key/status commands, built-in terminal QR encoding, immutable release CLI packaging, and standard PATH installer are source candidates. Apple connection accepts installation links and QR images; iOS live camera scanning is a separate source candidate. Provider setup targets the AO Codex external route. | Dependency version/checksums resolved without compilation. Shell syntax and whitespace checks passed for release/install changes. GitHub workflows cover Go tests, ops/CLI race checks, release staging/PATH fixtures, Linux amd64/arm64 CLI binaries, Swift tests, and unsigned macOS/iOS builds; successful integrated run evidence must be collected before claiming those checks. | Integrated CI results, actual systemd/Compose change and rollback behavior, release installation, physical iPhone/iPad scanning, and real AO/provider execution. No service deployment or external CLIProxyAPI change is established by this candidate. |
| Linux deployment | Compose/systemd stack, independent runner, native ARM64 release builds, shared-network provisioning, writable Mem0/Qdrant paths, and isolated service restart behavior. | On 2026-10-24 the ARM64 images built and the final release started locally; runner authentication, Linux/Mac reachability, Qdrant readiness, and independent checks passed. See local installation evidence below. The pinned memory-model artifacts downloaded and hash verification passed; both Qdrant collections initialized. | Memory-job processing, real AO/model task execution, production security/isolation acceptance, operational recovery, and migration. |

The only live full-task AO attempt recorded in this repository returned HTTP 409 without a retained allowlisted rejection code. Under the current protocol, its create outcome is uncertain. A later read-only inspection observed zero sessions, but this is not proof that the request had no external effect; no artifact, provider outcome, or termination proof was recorded. AO has no read-only lookup for automatically reconciling this uncertain create, so it must not be retried blindly. No credentials or private account values are stored here; real deployment configuration and secret references must be supplied by the operator. The existing remote CLIProxyAPI is an external dependency and remains outside Cylo's deployment or lifecycle changes.
- Default 30-day backup-artifact retention and purge are implemented and locally tested. Daily backup scheduling, RPO of 24 hours, and RTO of two hours remain planning targets, not measured guarantees.

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

UI design must communicate function through clear naming, layout, and interaction so users can understand features and complete actions. Do not display redundant purpose descriptions or operating tutorials by default. Offer necessary help on demand, and state material consequences, risks, and limits at the point of action. If removing explanatory copy makes a feature unclear, improve the design first; add brief contextual help for unfamiliar concepts or complex operations when needed. This rule applies to all client screens and flows.

### Approved interface direction — 2026-10-25

The user approved the [native assistant reference](design/native-assistant-reference.png). Cylo is an automation assistant whose frequent workloads include programming. Its interface centers on natural conversation, durable task progress, results, and decisions. Present programming execution and validation through concise, understandable summaries. The client does not provide source-code browsing or editing, diff viewers, terminals, or test consoles, including optional or hidden variants.

- **Simple appearance, useful density.** Use native typography, restrained semantic colors, compact rows, and clear grouping. Keep more useful conversation and task information visible by reducing oversized headings, excessive whitespace, decorative cards, and repeated metadata. Preserve readability, keyboard access, Dynamic Type, and platform touch targets; density does not mean uniformly smaller text or controls.
- **Conversation leads.** Keep projects and recent conversations/tasks in a compact sidebar. Show task receipts and concise progress/results in context. A compact, collapsible task rail may surface ongoing work, pending decisions, and recent completion on wide windows; collapse secondary surfaces before constraining the conversation in narrow windows. Avoid statistics dashboards and compulsory workflow panels.
- **Voice is prominent and compact.** Integrate a readily reachable microphone button into the composer with stronger emphasis than secondary input tools. Routine capture/session state uses distinct icon states, color, and optional subtle motion. Do not add persistent labels such as "Microphone on", "Microphone off", "Listening", or "Voice in progress", nor large waveforms, timers, voice cards, or separate persistent control strips. Give icon controls accessible names and state values, keyboard access, and help on demand; color alone must not carry state. Surface a meaningful permission failure, interruption, or connection problem with a recovery action at the affected control.
- **Automation reduces required interaction.** After the user expresses and authorizes the intent, work proceeds within its granted scope. Keep ordinary progress concise; emphasize decisions that require the user, actionable failures, and recorded results. Do not add repeated confirmations to work that is already authorized. Existing command, voice-submission, and permission boundaries still apply; this visual direction does not grant new authority or establish unattended voice submission.
- **Focus on outcomes.** Summarize programming work using its goal, current stage, outcome, validation summary, and decisions that require the user. Users should be able to understand what was accomplished and what remains unresolved through these summaries, without reviewing source code. The same conversation and task surfaces must support research and other authorized work.

The reference is a drawn design preview with illustrative Chinese dialogue and task states, not a native screenshot or a claim that the shown capabilities are implemented. It establishes the approved visual direction; the rules above govern behavior and accessibility. Native implementation and acceptance remain separate. The earlier [three-platform reference](design/apple-redesign.html) retains its coverage examples but does not override this approved direction.

### Instance key decision and candidate — 2026-10-25

The current product is private, single-owner, and single-instance with multiple native clients. This task targets a **fresh deployment**; it does not retain owner-password, bootstrap/pairing, approved-device sessions, or their schema/API compatibility. Earlier evidence below remains historical.

- One canonical 32-byte unpadded base64url instance key grants access as `owner` in workspace `default`. The server keeps its hash; deployment-managed runner and memory-worker credentials stay separate. Normal startup does not print the instance key.
- First connection asks for a server URL and key. A user may import an installation link or a QR image on macOS, iPhone, or iPad. The canonical format is `cylo://connect?server=<URL-encoded>#key=<key>`. Local image decoding requires no camera access. Import only stages the normalized address and key for review; the user must select Connect. Treat the entire link/QR as a credential.
- Keychain is the only credential store, scoped by normalized URL (lowercase scheme/host, omitted default port, trailing slashes removed, retained deployment path). Reject URL user information, query/fragment, traversal, encoded separators, and invalid ports. Accept only canonical 43-character keys.
- An authenticated `GET /v1/snapshot` validates access and returns explicit `workspace_id=default` and `subject_id=owner`; it has no device list. There is no authentication/session endpoint or password/device-management UI. Rejected credentials close workspace access. Ordinary transport failures preserve validated state with stale feedback.
- Settings show the current server, reconnect, replace its key, and clear saved credentials. Key replacement keeps the server address fixed. A different server requires explicit clearing followed by a fresh connection; there is no normal server-switching or saved-server list. Candidate validation precedes replacement. Clearing/replacement discards that connection's local durable queue, cached snapshot, exports, and drafts. It does not revoke the shared key or stop server tasks.
- Push registration uses `X-Cylo-Installation-ID`, a stable random UUID independent of the shared key. Token registration/revocation is scoped to that installation; one device must not overwrite or revoke another device's registration. Clearing tries to revoke the current token and unregisters locally; inability to confirm remote removal is reported.

Source changes are a candidate, not an installed release or accepted behavior. Required acceptance covers manual first connection, incoming link review, real QR image decoding, malformed import rejection, rejected/rotated keys, Keychain denial, relaunch, reconnect, replacement and local clearing, distinct concurrent push installations, and macOS/iPhone/iPad interaction. CI results must record the exact source revision, environment, commands, exit statuses, and saved artifacts separately. Protected transport, signing/installation, APNs delivery, live voice, and end-to-end task execution remain separate acceptance boundaries.

#### Candidate verification — GitHub-hosted CI

[Apple run 40252480817](https://github.com/ronigooja/Cylo/actions/runs/40252480817) checked out `ede06a7686809b44e33981d23150466c8bdbe733` without uncommitted input changes. Its GitHub-hosted runner used macOS 15.7.9 (24G830), arm64, Xcode 16.4 (16F6), and Swift 6.1.2. Collected logs establish 76 Swift package tests with zero failures, an unsigned macOS archive, and an unsigned generic iOS build; all three saved exit files contain `0`, and the build logs contain `ARCHIVE SUCCEEDED` / `BUILD SUCCEEDED`. The later candidate `5679168d74ca1f8e271c2708088e7ff620b4f195` changes only backend/tool files relative to this Apple source; Apple sources and workflow are identical.

[Linux run 40252752759](https://github.com/ronigooja/Cylo/actions/runs/40252752759) checked out `5679168d74ca1f8e271c2708088e7ff620b4f195` with a clean working tree on GitHub-hosted `ubuntu-latest`, Go 1.26.9 linux/amd64, Linux 6.17.0-1022-azure. `go test ./...` passed, including the operations package's synthetic lifecycle CLI checks; `go test -race ./internal/control ./cmd/cylo-server` passed both packages. Collected `go-tests.exit` and `auth-race.exit` both contain `0`. Environment and command logs are saved at `/tmp/cylo-single-key-verification/linux-40252752759/` as `environment.txt`, `go-tests.log` / `.exit`, and `auth-race.log` / `.exit`; the run's `linux-logs-5679168d74ca1f8e271c2708088e7ff620b4f195` artifact retains them for seven days. Exact commands and log capture are defined in `.github/workflows/linux.yml`. This run did not separately rerun the complete Python memory-worker suite, launch live AO/provider or Docker scenarios, or establish acceptance for environment-gated scenarios. Lifecycle tests use synthetic executors; no real external cleanup or destruction was performed.

The workflow set `CI_OUTPUT="$RUNNER_TEMP/cylo-apple"` and `CYLO_BUILD_ROOT="$CI_OUTPUT/test-data"`, then ran these commands (each piped through `tee` to its named log, with the original command status saved from `PIPESTATUS[0]`):

```sh
swift test --package-path clients/apple/Packages/CyloKit \
  --scratch-path "$CI_OUTPUT/swift-build" --cache-path "$CI_OUTPUT/package-cache" \
  --config-path "$CI_OUTPUT/swift-config" --security-path "$CI_OUTPUT/swift-security"
xcodebuild -project clients/apple/Cylo.xcodeproj -scheme CyloMac -configuration Release \
  -destination 'generic/platform=macOS' -derivedDataPath "$CI_OUTPUT/mac-derived" \
  -clonedSourcePackagesDirPath "$CI_OUTPUT/xcode-packages" -packageCachePath "$CI_OUTPUT/package-cache" \
  -archivePath "$CI_OUTPUT/CyloMac.xcarchive" CODE_SIGNING_ALLOWED=NO archive
xcodebuild -project clients/apple/Cylo.xcodeproj -scheme CyloMobile -configuration Release \
  -destination 'generic/platform=iOS' -derivedDataPath "$CI_OUTPUT/ios-derived" \
  -clonedSourcePackagesDirPath "$CI_OUTPUT/xcode-packages" -packageCachePath "$CI_OUTPUT/package-cache" \
  CODE_SIGNING_ALLOWED=NO build
```

Retrieved evidence is under `/tmp/cylo-single-key-verification/apple-40252480817/`: `environment.txt`, `swift-tests.log` / `.exit`, `macos-archive.log` / `.exit`, and `ios-build.log` / `.exit`. The run's `apple-logs-ede06a7686809b44e33981d23150466c8bdbe733` artifact retains logs for seven days; `apple-unsigned-ede06a7686809b44e33981d23150466c8bdbe733` retains the unsigned archive/app packages and SHA256 sums for three days. No local executable tests or compilation were used for this candidate. These checks do not establish installation, signing, interactive native acceptance, APNs delivery, live voice, or end-to-end task execution. The earlier Apple run 40252055619 failed in a command-line test host's native notification initialization; the accepted run includes the application-bundle guard that fixes that crash.

### Historical owner login decision — 2026-10-25

The following decision and verification/rollout records describe a superseded password-based implementation. They are retained as evidence, not current behavior or migration instructions.


The approved product remains single-owner with multiple devices. Clients use the fixed server address `http://debian.local:8787`; login and settings provide no server-address input or server-switching flow. This carries forward the installed endpoint, not a new deployment change.

- First setup uses the deployment-provided one-use setup code to establish the owner account and password. The setup code is not the routine login credential.
- A blank device logs in with the owner password, without a username selector or approval from another device. Successful authentication automatically registers the device and issues a revocable session, stored in Keychain by default for automatic subsequent login.
- Settings identify the current device and other signed-in devices, with local sign-out and central revocation. Sign-out invalidates the current session. Device credentials maintain login; they do not substitute for initial owner authentication.
- Password recovery uses an administrator command on the server and revokes all sessions. Existing-device QR authorization may be added as a convenience; it is not a required login path or an implemented capability.

This decision supersedes the bootstrap/pairing product requirements elsewhere in this document. Earlier installation evidence describes the legacy installation, not the replacement login experience. The replacement source now implements the owner APIs, owner-preserving schema migration, and shared Apple login gate; rollout and live interaction acceptance remain separate.

Migration must preserve existing tasks, projects, and memory and bind them to the owner. Existing device credentials alone must not authorize establishing or replacing the owner password. Passwords require salted adaptive hashing, login abuse controls, and protected password/session transport before live acceptance. The current plain-HTTP LAN endpoint does not establish transport acceptance.

Acceptance requires first setup, password login on a blank second device without another device online, relaunch with a valid session, incorrect-password handling, sign-out, remote revocation, server-side password recovery, and existing-data migration. Verify macOS, iPhone, and iPad separately. Source and CI evidence below establish only their exercised scope; installed-app interaction and physical-device acceptance remain open.

### Historical owner login replacement verification — 2026-10-25

The replacement source removes the pairing UI and adds shared first-setup/password-login views for macOS, iPhone, and iPad, including the macOS Settings scene. The main workspace opens only after server session validation. Staged attempt tokens survive uncertain setup/login responses without persisting the password or setup code; successful login promotes the credential into the selected store. Routine network failures preserve an already validated session; revocation closes the workspace and preserves unsent drafts. Snapshot permission denial clears cached memory and prepared exports, while a normal permission error does not invalidate a valid session. Device settings mark this device and provide sign-out and revocation with task consequences. Advanced credential-store selection and explicit copying remain available.

[Linux CI run 40230735501](https://github.com/ronigooja/Cylo/actions/runs/40230735501), revision `bbf6d681282b6a47134fbc6e86a680036617b53c`, passed `go test ./...` and `go test -race ./internal/control ./cmd/cylo-server`, both exit 0, on a GitHub-hosted Ubuntu runner with Go 1.26.9. The revision had no uncommitted inputs. No Go source changed in the subsequent Apple-only fixes. Retrieved logs and exit files are in `/tmp/cylo-owner-login/evidence/linux-final/`; the artifact ZIP is retained alongside them. The tests cover owner setup/login/replay, password rejection and persistent throttling, session/logout/revocation, consumed-code rejection, owner-preserving legacy migration, host-locked recovery, and stale-principal mutation rejection. Opt-in live integration/container scenarios were not enabled.

[Apple CI run 40231144967](https://github.com/ronigooja/Cylo/actions/runs/40231144967), revision `2d9d779ae13e36f1c868e90cc2a0586f995b4f81` with no uncommitted inputs, passed all 78 Swift package tests, the unsigned macOS archive, and the unsigned iOS build, each exit 0. The GitHub-hosted runner used macOS 15.7.9 (arm64), Xcode 16.4, and Swift 6.1.2. Exact commands and destinations are in `.github/workflows/apple.yml`: `swift test --package-path clients/apple/Packages/CyloKit` with CI-only scratch/cache paths; `xcodebuild` scheme `CyloMac`, Release, `generic/platform=macOS`, `CODE_SIGNING_ALLOWED=NO archive`; and scheme `CyloMobile`, Release, `generic/platform=iOS`, `CODE_SIGNING_ALLOWED=NO build`. Full command logs, environment, and exit files were retrieved to `/tmp/cylo-owner-login/evidence/apple-final/`; the logs ZIP and artifact inventory are alongside them. Unsigned app artifacts were initially uploaded under three-day retention; the combined app artifact was deleted during the user-requested rollout cleanup below. No build caches were copied locally. Initial runs found obsolete Go fixture assumptions, a streamed-request test helper crash, and a cached-memory clearing regression; these were corrected before the successful runs.

Independent source review is recorded in `/tmp/cylo-owner-login-review.md`. Controller checks were limited to whitespace and locale-key/placeholder parity; no local compilation or automated suite ran. At the source-verification boundary, the live database and installed applications had not yet been changed; the subsequent rollout is recorded below. External services were not altered. The fixed HTTP endpoint still requires transport protection for live password-login acceptance. No native screenshot, physical-device interaction, or live second-device login is claimed.

### Historical owner login installation and cleanup — 2026-10-25

The user authorized updating the server and installed clients without retaining backups, then explicitly deferred iOS installation/testing and requested its artifacts be removed. [Server artifact run 40231799160](https://github.com/ronigooja/Cylo/actions/runs/40231799160) built the ARM64 server binary and Docker image from `f62b55f3a9643296941354d95f54bfd77a6aaf85` on a GitHub-hosted ARM64 Linux runner; both build exits were 0. The workflow records the exact commands and environment, and SHA-256 verification passed before loading the image. The deployed image is `cylo-server:owner-login-f62b55f3a964`, ID `sha256:0ad7f4ee87651172084e98fb4d0f8d66636ff42897ace434a1bbaf25503a0481`. Build logs and artifact records are saved under `/tmp/cylo-owner-rollout/`.

The stack and runner were stopped, and the new `recover-owner --database /var/lib/cylo/server/data/cylo.sqlite --confirm-revoke-all-devices` migrated the database and prepared owner setup. The first attempt exited 1 before migration because the data UID could not traverse the host parent directory; the successful attempt used its existing parent-directory group and exited 0 without changing permissions. No database backup was made. Owner identity and all business records were retained (projects 0, tasks 0, memory claims 0); three old device credentials were revoked. SQLite quick-check returned `ok`. The stack, runner, AO, and SSH tunnel were subsequently active; health returned 200, owner status returned `setup_required:true`, and the old bootstrap route returned 410. The setup code was delivered to the Mac clipboard and its temporary plaintext file deleted, without printing or logging the value. Choosing the new password remains the owner's action.

The verified macOS artifact from `2d9d779` was signed ad hoc on the local Mac, verified with `codesign --verify --deep --strict` (exit 0), and moved into `/Applications/Cylo.app` after deleting the old app, without a rollback copy. Launch, continued process existence, a `Cylo` window, and Mac-to-server owner-status access were observed. Screenshot capture failed (`could not create image from rect`), so visual and interactive setup acceptance are not claimed. The Mac task root is `~/Developer/Cylo/runs/owner-login-20261025/`; installation logs were retrieved to `/tmp/cylo-owner-rollout/evidence/`.

Four old server image tags/images and seven old Mac app/archive products were removed. Local iOS downloads/staging/signing entitlements and the original combined CI app artifact were deleted after the user's iOS deferral; the installed phone app was left unchanged. The earlier iOS signing attempt failed at Keychain access (`errSecInternalComponent`); no signing access controls were changed. No remote CLIProxyAPI or voice deployment change was made. No Vultr worker was provisioned. The current HTTP transport and the still-pending owner setup/login remain acceptance limits. The sanitized deployment record is `/tmp/cylo-owner-rollout/evidence/deployment.json`.

### Interface decisions and acceptance boundary

AO startup failures, unresolved runtime diagnostics, execution-slot accounting, and reconciliation instructions belong to operator interfaces and audit records. Conversation and task views show the actual task status without infrastructure queue reasons or capacity banners. Preserve actionable submission errors and pending-confirmation feedback; this presentation rule does not authorize client retries or change task authority.

The Apple redesign uses system typography, SF Symbols, semantic colors, system appearance, and English/Simplified Chinese localization. Spacing follows 4/8/12/16/24/32 pt steps; conversation text is limited to 760 pt. Blue emphasizes primary actions. Task outcomes and actionable exceptional states have concise text labels; routine voice state follows the compact icon treatment above. Normal connected state has no banner. Offline, stale, and failed operations remain visible with an action at the affected surface.

macOS uses a resizable sidebar (240 pt default) and workspace. iPad uses the same split structure in regular width and collapses in narrow windows; iPhone has Conversation, Tasks, and Projects tabs, hidden in the task workspace. Notifications are a toolbar entrance with an unread count. Account menus expose Settings, Memory, and Capabilities. Home contains a composer and recent work, without statistics or tutorial cards. The [three-platform visual reference](design/apple-redesign.html) covers home, empty, loading, send failure, and task states with language, appearance, and text-size controls. It is a design artifact, not an application screenshot or acceptance evidence.

Every task entrance leads to the same workspace. The workspace shows persisted messages, actual task status, approvals, and recorded results/evidence; runtime identifiers and cursors belong in Technical details. No simulated assistant answers, historical progress, or inactive download actions fill missing server capabilities. Tasks support search, status filters, and update ordering. Projects offer a contextual new conversation. Settings group connection, device identity/authorization, notifications, data, and advanced options. First-time owner setup and routine password login are separate; the server address is fixed. Credential storage remains an explicit choice without silent identity migration.

Submission has accepted, failed, and unknown outcomes. Drafts clear only after durable acceptance. Unknown submissions reconcile the original journaled receipt rather than issuing a new command; the interface must retain the draft and show confirmation pending. Task drafts and selection survive navigation within the connected context and are cleared when the server or identity changes. Voice has one compact composer entrance; its transcript-draft review opens when needed rather than permanently occupying the conversation. Submission requires confirmation, and End voice remains distinct from Stop task. macOS keyboard targets are Command-Return to send, Command-N for a new conversation, and Command-F for task search.

`GET /v1/tasks/{task_id}/entries` reads the existing authoritative message table using task-read permissions. The response contains `messages` (`id`, `task_id`, `kind`, `content`, `created_at`) and nullable `next_cursor`; `cursor` and bounded `limit` page forward. Clients load history on entry/reconnect and deduplicate by message ID. This does not establish a second conversation authority or provide assistant messages that the server has not recorded. Dates and counts use the system region; server-authored content remains in its original language.

Acceptance remains open until native application checks cover Mac 840×620 and desktop sizes, iPad portrait/landscape/narrow windows, and small/large iPhones; both languages and appearances; long titles and Dynamic Type; keyboard avoidance; VoiceOver, keyboard focus, reduced motion, and 44×44 pt mobile targets. Core flows must verify one-tap continuation, contextual approvals/results, linked notifications, retained failed drafts, unknown receipts, duplicate events, reconnect history, terminal tasks, device revocation, and identity changes. Record Go tests, Swift state/contract tests, Xcode app builds, simulator UI runs, real-device runs, and real-service checks separately. Linux source review and design references cannot establish Apple compilation, screenshots, interaction, or end-to-end acceptance.

### Assistant layout implementation check — 2026-10-25

Source revision `52eeac9` implements the compact macOS assistant layout: searchable project/task navigation, recent work and a bottom composer, clearer message authorship, visible evidence summaries, and a collapsible activity rail. The rail uses authoritative task/approval/result state, gives pending decisions and failures priority, and automatically hides when the detail column is narrower than 960 pt. The minimum Mac window remains 840×620. The microphone is a compact primary composer control; its popover provides explicit transcript review and ending voice, including during connection. Source-code viewing controls and dedicated fenced-code viewports are absent. Existing command receipts, drafts, approvals, and server authority remain in use.

On the authorized Mac with macOS 27.0.1, Xcode 27.1, and Swift 6.4, the final revision passed 70 Swift tests with zero failures and both unsigned Xcode app-scheme builds (`CyloMac`, generic macOS; `CyloMobile`, generic iOS), all with exit status 0. The three new activity tests cover attention precedence, failures and authoritative completion, and settled approvals. Independent source review identified the missing stop action during voice connection; the final revision fixes it on both platforms, and review confirmed the fix. Documentation, whitespace, and paired locale-key/placeholder checks also passed. Compiled source has no uncommitted changes; the transferred README's pre-existing documentation edit is not executable input.

Inputs, caches, logs, and build outputs remain under the dedicated Mac task root `/Users/zhuwei/terminal/cylo-assistant-ui-20261025`. Its `evidence/verification.md` records the exact final commands, environment, exit statuses, and scope; `evidence/source-sha256.txt` records inputs, verified against the remote source. Final logs, statuses, and the verification record were retrieved to `/tmp/cylo-ui-validation/` without copying dependency/build caches. A fixture-based native rendering probe produced incomplete transparent view-cache images, which were rejected as visual evidence; window capture then failed with `could not create image from window`. No successful screenshot, native visual/accessibility acceptance, installed-app replacement, physical-device run, or live task/voice acceptance is claimed. The approved design reference remains the visual target, not a screenshot of this implementation.

#### Installed Mac app replacement — 2026-10-25

The user authorized replacing the installed app. The verified `52eeac9` build was staged, its embedded WebRTC framework and application were ad-hoc signed, and `/Applications/Cylo.app` was replaced and registered with LaunchServices. Installation and launch commands returned exit status 0; `codesign --verify --deep --strict` passed. The installed executable retains both arm64 and x86_64 slices. A running process at the installed path, an on-screen 900×672 window, and WebRTC loaded from the installed bundle were observed. These are installation/startup checks, not screenshot or functional acceptance. Local data and device identity were not reset. At the user's request, the old-app rollback copy was deleted and its absence confirmed.

Replacement commands and results are retained in `evidence/replace-installed-app.sh`, `evidence/app-replacement.log`, and `evidence/installed-app-probe.log` under the same Mac task root, with copies in `/tmp/cylo-ui-validation/`. The replacement script's original log records the temporary backup before the user's later deletion request; no rollback app remains.

#### Settings window repair — 2026-10-25

The macOS app now registers a native Settings scene and shares its form with the account navigation destination. The account sheet has explicit macOS dimensions, and the form uses grouped styling. [GitHub Actions run 40204390633](https://github.com/ronigooja/Cylo/actions/runs/40204390633), on source `042f289`, passed all 70 Swift tests, archived the unsigned universal macOS app, and built the unsigned iOS app. The GitHub-hosted arm64 runner used macOS 15.7.9, Xcode 16.4, and Swift 6.1.2. Local integrated source `da29a8b` has the same committed tree as the tested source. The [Apple workflow](../.github/workflows/apple.yml) records the commands; logs, exit statuses, unsigned artifacts, and verified SHA-256 manifests were retrieved to `/tmp/cylo-settings-ci/run-40204390633/`.

The CI Mac archive was transferred to the existing Mac, ad-hoc signed, installed at `/Applications/Cylo.app`, and launched without local compilation. Strict signature verification and a running process at the installed path were confirmed. The user confirmed that both Command-comma and Account & Settings → Settings display the settings correctly. This accepts the reported blank-settings presentation fix only; connection, authorization, notifications, and export actions were not newly exercised. Device identity and app data were preserved. Installation evidence and the previous-app rollback copy were initially retained under `/Users/zhuwei/terminal/cylo-settings-fix-ci-20261025/` and later moved to `/Users/zhuwei/Developer/Cylo/runs/cylo-settings-fix-ci-20261025/` with the user's approval. The old directory is absent; `migration.json` records the move, and original command scripts and logs preserve their historical paths. Installation logs were also retrieved to `/tmp/cylo-settings-ci/`.

### Existing platform and media boundaries

The supported clients are native SwiftUI applications for macOS, iPhone, and iPad. They authenticate the single owner with a password and register the device on successful login, following the owner login decision in §4. Device credentials are stored in Keychain and can be revoked centrally. A device shows the server's authoritative version and exposes stale/offline status instead of presenting cached state as current.

The primary navigation entrances are **Conversation**, **Tasks**, and **Projects**. Memory, Capabilities, and Settings are secondary entrances. Users can submit commands by text or voice, inspect task evidence and service health, answer approval requests, stop work, and receive linked notifications. Conversation and voice-session lifetimes do not control task lifetimes. Raw audio retention is off by default. Cross-device continuation means access to the same durable task context; it does not claim live audio migration between devices.

Voice uses WebRTC with a fixed libwebrtc dependency. Platform audio capture/playback remains behind platform-specific adapters while all devices use the same session, turn, and command pipeline. Acceptance requires a 30-minute mixed Mandarin-English voice session on both a real iPhone and a real iPad while each device is locked, plus interruption, reconnect, route change, and task-continuity checks. This is a target test, not a completed result.

One universal iOS app adapts to iPhone and iPad; a separate native macOS target shares Swift packages for API, authentication, domain state, synchronization, and voice protocols. Task views show the goal and available server-recorded progress, waiting reason, evidence, and verification; missing records do not imply completed work. Worker/run details belong in an advanced view. Muting capture, stopping playback, hanging up, and stopping a task are distinct operations; closing a client does not cancel a durably accepted task.

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

The public client API uses REST under `/v1` and SSE for ordered event delivery. An authenticated client reads `GET /v1/snapshot` for the authoritative project/task/run state with explicit workspace and subject, server version, and current event cursor; it resumes `GET /v1/events` after that durable cursor using `Last-Event-ID`. Reconnect repairs from a snapshot when the cursor is invalid or too old. The shared instance bearer authorizes the fixed owner/workspace. A snapshot validates the connection; authentication/session endpoints and device approval/revocation are absent.

All business mutations go through `POST /v1/commands` with an `Idempotency-Key` and optional `expected_version`. Initial command kinds are `project.create`, `task.create`, `task.append`, `task.stop`, and `task.approve`. Workspace references are opaque server-owned IDs; clients cannot submit arbitrary host paths. `GET /v1/commands/{id}` and `GET /v1/commands?key=...` recover the durable receipt after a timeout or reconnect; each receipt preserves its original idempotency key. The transaction commits the receipt, durable work record, and outbox event atomically before returning acceptance. Event consumers deduplicate by stable event identity. The outbox is the source for delivery retries; external side effects keep their own idempotency or reconciliation state.

Optimistic versions reject stale writes with a recoverable conflict. AO callbacks and runner reports are observations or proposals until the control plane validates and commits them. Memory changes use authority versions and tombstones. Voice gateway callbacks correlate to stable voice turn IDs and Cylo command IDs. Runner routes use separate credentials. Durable work claims are atomic and obey the global four-run cap; a dispatched operation with an unknown outcome is reconciled or escalated, never resent blindly. Implemented schemas live with the contracts and code; new interfaces are coordinated before implementation. REST covers commands/receipts, projects/tasks/runs/artifacts, snapshots/events, voice, memory/context, capability releases/deployments, authorization, and notifications. Large files, build outputs, and service data use file storage rather than database blobs.

## 10. Security, reliability, and lifecycle

The shared instance key authorizes the fixed owner principal; deployment-managed runner credentials are separate. Rotating the key blocks future use of the old key. Local client clearing does not revoke shared authorization or stop server tasks. Secrets are references to deployment-managed credentials. Every command, approval, runtime dispatch, external effect, and authority change is auditable.

Unknown side-effect results never trigger blind retry. If the effect can be queried, reconcile it; otherwise mark it unknown and request human resolution. Recovery preserves receipts and work identity to avoid duplicate effects. Device/account/media leases must terminate before reuse, with uncertain accounts quarantined. A database restore or migration must fence the old instance so only one server can write. The restore-only deletion gate now blocks activation and business API reads and writes, including authority memory reads. Backup metadata now carries the claim deletion ledger ID, generation, baseline, initialization time, event count, and sequence watermark from the consistent database snapshot, alongside deletion record counts. This metadata cannot prove the trusted latest ledger head or freshness, replay events created after the snapshot, or cover source deletions; the gate remains closed until complete reconciliation is implemented and verified.

The default 30-day backup-artifact purge uses a locally tested HMAC-authenticated retention manifest; the separate private deletion manifest is bound to the trusted journal hash chain. Daily backup scheduling, RPO 24 hours, RTO two hours, physical external-store erasure, production retention, and restore remain unverified; local purge/manifest tests are not production recovery guarantees.

Data inventory and deletion acceptance covers authoritative rows, worker history and model cache, vector payloads, local client caches, exports, replicas, service volumes, and backup snapshots. Deletion immediately blocks retrieval and new use, records a versioned tombstone, and queues idempotent cleanup for each named store. External targets without real adapters/checkpoints stay `adapter_missing`; a backup's embedded journal head alone is not proof that it includes later deletions. Report stores that are pending or outside the configured retention window; do not claim physical erasure until each applicable store and backup lifecycle has evidence. Service-volume deletion requires explicit authorization and a recoverable backup policy where retention requires one.

Backups cover a consistent authoritative database snapshot, artifacts and releases, service configuration, required persistent volumes, and deletion manifests. Recover keys separately or reauthorize their external integrations. On migration, pause dispatch and isolate the old instance before restoring. Reconcile services and uncertain external effects before reopening execution; never activate two copied databases. Report full vector-index rebuild duration separately from core-service RTO. The instance key is independent of the voice account pool. All native clients share the instance credential in Keychain; physical installation UUIDs identify notification targets only. Losing the key requires an operator-controlled replacement and redistribution, not password recovery.

### Deployment and recovery operations

The Linux control server reads deployment settings from its process environment. Keep instance and runner credentials in deployment-managed secret configuration; the server does not print them. Bind to loopback or a private trusted interface and put external TLS termination behind the deployment's authenticated network boundary.

The Cylo-pinned AO source patch is applied to a separately managed AO checkout; this repository does not provide an AO daemon unit or environment template. The AO daemon requires `AO_CODEX_EXTERNAL_BASE_URL`, `AO_CODEX_EXTERNAL_API_KEY_FILE`, and `AO_CODEX_EXTERNAL_MODEL` together to configure the external Codex Responses route. The selected model is required and requests must match it. Both the AO daemon and Cylo runner require the same `AO_CODEX_CONTAINER_IMAGE` value for the supervised image. The URL, key-file path, and model belong only to the separately managed AO daemon; the image setting belongs to both deployments, not the Cylo server. The patch helper pins AO source commit `7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff`, rejects tracked staged/unstaged and untracked changes, and checks the resulting source tree against `15884d33d6b48ce58d865faf699279e0c5b2be95`.

Required settings:

- `CYLO_DATABASE_PATH`: path to the SQLite database. The server acquires a single-writer process lock before opening it.
- Initialize the database with `cylo key init --database PATH --server https://... [--output PRIVATE_JSON]`. The command displays the key and installation link once (and renders a terminal QR using its built-in encoder). `CYLO_BOOTSTRAP_CODE` is removed; use a fresh database for this deployment.

Optional settings:

- `CYLO_LISTEN_ADDR`: defaults to `127.0.0.1:8787`.
- `CYLO_DEFAULT_PROFILE_ID`: the server-selected profile for new tasks.
- `CYLO_RUNTIME_PROFILES`: JSON object keyed by profile ID. Each profile sets `id`, `ao_project_id`, `runtime_route_id`, `model_route_id`, `role`, `ao_agent_kind` (`worker`), `isolation_policy`, `capability_ids`, and `scopes`. Client requests cannot override these execution routes or grants.
- `CYLO_RUNNER_TOKEN`: deployment-provisioned 32-byte base64url runner credential. When set, `CYLO_RUNNER_ID`, `CYLO_RUNNER_WORKSPACES`, and `CYLO_RUNNER_SCOPES` default to `configured-runner`, `default`, and `runner:claim,runner:observe,runner:control:read`.
- `CYLO_MEMORY_RUNNER_TOKEN_FILE`: optional path to a deployment-managed file containing a distinct 32-byte base64url memory worker credential. When enabled, `CYLO_MEMORY_RUNNER_WORKSPACES` must list exact workspace IDs; `CYLO_MEMORY_RUNNER_ID` defaults to `memory-worker`; and `CYLO_MEMORY_RUNNER_SCOPES` may only contain exactly `memory:jobs:claim,memory:jobs:complete,memory:propose`. The server stores only the credential hash and never logs the token. This credential exposes the durable derived-job API; it does not enable a Mem0 or Qdrant runtime.
- Memory-worker deployment also requires `CYLO_CONTROL_URL`, the scoped worker credential injected as `CYLO_RUNNER_TOKEN` (provisioned for the server through `CYLO_MEMORY_RUNNER_TOKEN_FILE`), `CYLO_MEM0_LLM_MODEL`, `CYLO_MEM0_LLM_BASE_URL`, and `CYLO_MEM0_LLM_API_KEY` from deployment-managed secret references. `CYLO_MEM0_LLM_PROVIDER` selects `openai` or `ollama`; `CYLO_MEM0_HISTORY_DB` and the model cache belong on the private persistent memory volume. Qdrant URL, optional API-key reference, collection/codec, and worker batch/poll settings are deployment configuration. The pinned multilingual embedding revision and dimensions are recorded in §7; changing them requires a separate collection and rebuild.
- For local execution, start the worker from the repository root with `python -m services.memory.worker`, or use `docker compose -f services/memory/docker-compose.yml up --build` with the settings above. The Compose Qdrant service is loopback-bound and disposable; it is not a production persistence configuration. These commands describe source setup and do not establish production installation or recovery.
- `CYLO_INSTANCE_SCOPES`: comma-separated fixed-owner grants. An explicit value replaces the defaults exactly; it never adds a wildcard or restores omitted grants. Device approval/revocation scopes are not part of the client model. Project/task memory scopes must name their exact scope IDs.

For example, a runtime profile environment value can be supplied as JSON:

```json
{"standard":{"id":"standard","ao_project_id":"cylo-workers","runtime_route_id":"default-worker","model_route_id":"cliproxyapi","role":"worker","ao_agent_kind":"worker","isolation_policy":"workspace-sandbox-v1","capability_ids":[],"scopes":["task:read","task:write"]}}
```

The explicit operator commands are `cylo key init --database PATH --server URL [--output PRIVATE_JSON]` and `cylo key rotate --database PATH --server URL [--output PRIVATE_JSON]`. The server must be stopped because both operations require its exclusive database lock. Output JSON contains `key` and `installation_url` when `--output` is supplied; the interactive command prints the same values once and never repeats them in status. Optional `CYLO_INSTANCE_KEY` provisions or checks a canonical 32-byte base64url key at startup; a value that disagrees with the stored hash fails startup, so remove or replace stale environment configuration after rotation, including the deployment secret file mapped through `CYLO_INSTANCE_KEY_FILE` when Compose is used. Rotation preserves the fixed owner/workspace, tasks, and separate worker credentials; restarting closes old streams.

The operator export command is `python3 tools/cylo_export.py --server URL --key-file PRIVATE_JSON --output NEW_ZIP [--scope workspace|personal]`. The default key-file format is the private JSON produced by `cylo key init`/`cylo key rotate`; use `--key-format plain` for a private raw-key file. The tool requires an operator-owned regular key file with no group/other access, refuses redirects and existing output paths, creates a mode-0600 ZIP, caps transfer at 1 GiB, and removes partial output on failure. It exports the existing logical authority archive; external runtime volumes, deployment secrets, and backups remain outside its scope. This command does not perform the native client's ZIP/hash validation.

The destruction command is `python3 tools/cylo_destroy.py --manifest PRIVATE_MANIFEST --receipt NEW_RECEIPT` for a read-only plan; explicit execution additionally requires `--execute --confirm-instance INSTANCE_ID`. Start from `deploy/linux/destroy-inventory.example.json`; the supported topology is a dedicated operator-owned directory whose only children are the declared data, artifacts, backups, and config roots, plus the enumerated systemd units and exact external resource inventory. This does not match the historical installation's split `/etc/cylo` and `/var/lib/cylo` layout automatically. The manifest and receipt must be private, and the receipt and executable adapters must survive outside the deletion roots. Mounted resources, including same-filesystem bind mounts, must be detached before planning or execution; Linux mountinfo is checked before any action and again before deletion.

Execution masks/stops the supported services, verifies inactivity, fences dispatch through `cylo-maintenance`, and requires a deployment-owned cleanup adapter. The adapter receives a fresh challenge and must prove complete inventory, no unaccounted resources or active runtimes, and removal of exactly the named external resources. Only after that proof and a repeated path/identity check does the tool delete owned roots. Missing adapters or incomplete proof fail closed and retain owned files, although services may already be stopped and dispatch fenced; inspect the private receipt before recovery or retry. No real cleanup adapter is shipped for AO, capability containers, volumes, Mem0, or Qdrant. This is bounded operator tooling with fixture coverage, not accepted all-store destruction, a production cleanup adapter, or evidence of actual erasure. The existing remote CLIProxyAPI is outside this inventory and lifecycle.

Clients use the instance key as an Authorization bearer credential and validate it through the snapshot API. Old auth/bootstrap/pairing routes and device approval/revocation are absent. Initialization and rotation are explicit operator operations; credential output must be requested explicitly into a protected destination or standard output. Never place actual keys or generated installation links in logs, documentation, repository files, or chat. Routine startup does not print them. The source is intended for a fresh database; earlier password-session migration and `recover-owner` instructions below describe historical releases.

Clients read the durable inbox from `GET /v1/notifications` (`?unread=true` filters it), and mark an item read through the idempotent `notification.mark_read` command. Inbox entries link to their source event and task. Push uses an installation UUID and may target multiple installations sharing the same owner key. An installation ID is notification metadata, not authentication.

The process handles `SIGINT` and `SIGTERM` with a 20-second graceful shutdown window. SQLite is opened with WAL, `synchronous=FULL`, foreign keys, a busy timeout, and mode `0600` for the database and sidecars. Compose and systemd templates configure a systemd-supervised server/memory-worker stack and a separately managed runner process on its dedicated host. The deployment assets are source-integrated. The local installation evidence below establishes image builds and local startup only; production acceptance, migration, and physical restore remain open.

#### Historical local installation evidence — 2026-10-24

The user authorized installing on the current Linux host and installing the
macOS app on `zhuwei@macair`. For this local installation the control server,
trusted runner, AO, memory worker, and Qdrant share one Linux host; the runner
and AO remain independent systemd processes. Co-location is an explicit local
deployment choice and does not establish isolation between those components.
The existing remote CLIProxyAPI configuration and lifecycle were not changed.
The current deployment selects its configured `gpt-6.1-sol` route; no inference
probe or live AO task was performed during installation.

- Native ARM64 server, memory-worker, and runner images were built. The active
  immutable release is `/opt/cylo/releases/release-20261024-final`, selected by
  `/opt/cylo/releases/current`. The Compose and runner units are active; the
  final runner executable is under the selected release.
- Linux and Mac tunnel `/healthz` return HTTP 200; anonymous business and runner
  requests return 401. Authenticated runner reads return 200, dispatch is
  enabled, and current claims are empty. SQLite read-only integrity checks
  passed and WAL was observed. `synchronous=FULL` is configured by the server;
  a separate SQLite connection is not evidence of the live connection's PRAGMA.
- AO binds only loopback port 18001 and returns ready. Its listener trusts local
  processes and does not enforce bearer authentication. Qdrant returns ready.
  The 235,052,644-byte pinned ONNX model and all four companion artifacts
  passed their fixed SHA-256 checks. Both Cylo and Mem0 Qdrant collections
  initialized. An established worker connection to the control server followed
  by sleep, with no polling errors or tracebacks, is consistent with the empty
  polling loop. No memory job or remote model call was exercised.
- The macOS app was built with Xcode 16.2, installed below the dedicated
  `/Users/zhuwei/terminal/cylo-native-install-20261024` directory, and launched.
  App-relative WebRTC loading and strict ad-hoc signature verification passed.
  A native URLSession probe without ATS exceptions reached the backend. First
  device registration was initially left manual because Accessibility
  automation and an external helper's Keychain read were denied. The user
  subsequently registered one approved device; deployment agents did not
  consume a bootstrap credential.
  APNs, distribution signing, iOS installation, and authenticated app task flows
  remain unverified.
- Space3044 `chatgpt-web-voice` was deployed without upstream source changes at
  `6338ac478070aaa34bfe7a3c0469caf9e9174c02`. Its management page returns 200
  from Linux and the Mac; Docker health is healthy. The user explicitly
  requested LAN access at `http://debian.local:18090`, so it binds loopback and
  `192.168.1.4` only. Management credentials remain on Linux. Anonymous
  downstream config requests return 401 and a restricted deployment key returns
  200. No ChatGPT Web account was imported. Cylo voice stays disabled pending
  trusted HTTPS and the required account/media lifecycle evidence.

#### Historical fixed local server and connection settings (2026-10-25)

The user selected `http://debian.local:8787` as the fixed native client endpoint.
The client ignores previously saved server addresses and omits the address editor.
New installations default to Keychain; an existing explicit storage choice for
this origin is preserved. Storage selection and identity copying remain available
under Advanced settings. This supersedes the mandatory initial storage selection
in the earlier installation notes. Identity records are not automatically copied
between origins or stores, and device approval remains server-authoritative.

The settings form places connection state, inline errors, and one Connect action
together. Reconnect shows progress and ignores overlapping connection attempts.
Missing identities direct the user to device approval or first-device setup.
Both Apple targets declare local network access for the fixed `.local` endpoint.
[GitHub Actions run 40226323307](https://github.com/ronigooja/Cylo/actions/runs/40226323307)
built source `36ffb6f1dc28515cdb398c66c2cab5a46b470c65` on a GitHub-hosted
arm64 macOS 15.7.9 runner with Xcode 16.4 and Swift 6.1.2. All 71 Swift tests,
the unsigned universal macOS archive, and the unsigned iOS build passed, each
with recorded exit status 0. Exact commands are in `.github/workflows/apple.yml`.
Logs, exit-status files, unsigned artifacts, and verified SHA-256 manifests were
retrieved to `/tmp/cylo-fixed-server-ci/run-40226323307/`. Existing uncommitted
controller documentation and operator-skill changes were excluded from CI inputs.

The CI Mac archive was ad-hoc signed, installed at `/Applications/Cylo.app`, and
launched without Mac-side compilation. Strict signature verification passed and
both arm64 and x86_64 slices were retained. At the user's explicit request,
Cylo's local preferences, caches, command/snapshot data, and private-file identity
data were deleted before launch. No Cylo Keychain identities were present. The
fresh app saved only the fixed endpoint and default Keychain choice; no saved
pairing or private-file identity remained. The Mac's LAN health request returned
HTTP 200. Installation/reset scripts, archive, signature checks, and logs are
under `~/Developer/Cylo/runs/fixed-server-ui-20261025/`; the install log was also
retrieved alongside the CI logs. Installation and reset exited 0. Device approval,
authenticated task flows, native visual acceptance, and iOS installation were not
exercised by this update.

#### Historical Apple redesign installation refresh

The user authorized installing the redesigned Mac app in Applications/Launchpad, deleting old application versions and all local Cylo data/device identities, and copying the universal iPhone/iPad build artifacts to the Mac Desktop. The source candidate `8282bd6` was built with Xcode 16.2 on `zhuwei@macair`. Build inputs, logs, result bundles, and staging live under `/Users/zhuwei/terminal/cylo-redesign-1466808-20261024`.

- A fresh `xcodebuild -scheme CyloMac -destination generic/platform=macOS ... CODE_SIGNING_ALLOWED=NO build` passed. The installed `/Applications/Cylo.app` is version `0.1.0 (1)`, display name **Cylo**, with `arm64` and `x86_64` slices and English/Chinese resources. The embedded WebRTC framework and application were ad-hoc signed; `codesign --verify --deep --strict` passed. LaunchServices registration, a live process at the installed path, and the Launchpad database entry `Cylo|com.cylo.mac` were verified. This is local ad-hoc installation, not Developer ID distribution/notarization.
- `swift test` with isolated scratch/cache/config/security directories passed **67 tests, 0 failures** in the Mac's Chinese language environment. Initial real compilation exposed an iOS-unavailable home-directory API, a test fixture type mismatch, and a missing test module import. The follow-up run exposed English-only status assertions; the fixes retain behavioral assertions and compare localized messages. Independent review covered these fixes and the final test log.
- The iOS scheme's generic destination remains unavailable because Xcode reports the iOS 18.2 platform/runtime missing despite an installed SDK. A direct Xcode **app-target** build, `xcodebuild -target CyloMobile -configuration Debug -sdk iphoneos ... CODE_SIGNING_ALLOWED=NO build`, passed and produced the complete arm64 device application. This is an Xcode target build, not just a manual SDK source compile. Version metadata, launch-screen configuration, supported orientations, and device families `[1, 2]` were verified. No simulator or physical-device run occurred.
- `/Users/zhuwei/Desktop/Cylo-iPhone-iPad` contains the universal `CyloMobile.app`, `Cylo-iPhone-iPad-0.1.0-unsigned.ipa`, build notes, and a SHA-256 file. The IPA ZIP integrity check passed. These artifacts are **unsigned** and require Apple signing/provisioning before device installation; TestFlight/APNs and device delivery are unverified. Independent review verified the corresponding staged device app; Desktop verification was performed by the installer because the independent SSH session could not list Desktop under TCC.
- After the user's explicit identity-reset choice, two matching Cylo macOS Keychain identities were deleted using attribute-only matching; no credential values were read or printed. The subsequent matching count was zero. Local preferences, private-file identities, cached snapshots/commands, caches, and saved window state were cleared before launch. Thirty-eight old Cylo application bundles in prior dedicated build/install roots were removed and unregistered. New setup requires a new credential-storage choice and pairing/activation. This is a local client reset; server-side device/task/memory data was not deleted, and no external service or existing CLIProxyAPI deployment was changed.

This refresh does not establish authenticated server task/history interaction, the full visual/accessibility matrix, live voice, APNs, or iPhone/iPad acceptance.

A later local device-registration attempt exposed a duplicate-token defect:
requesting pairing after successful bootstrap attempted to insert the same
device token again and returned HTTP 500. The server hotfix from `3b80cbd`
returns the existing approved/pending device status without extending pairing
TTL or adding rows; revoked and expired credentials remain rejected, and
request-ID conflicts remain enforced. Control/acceptance tests and the full Go
suite passed on the backend candidate. Independent review approved the change.
The installed server image is now `cylo-server:release-20261024-pairingfix`;
only that container was replaced. Health and authentication checks passed, the
existing approved device record was preserved, and runner/memory/Qdrant were
left running. The Mac app's approved-pairing branch was also fixed and installed.
No production identity was used to synthesize a pairing request during checks.

The user subsequently requested a credential-storage choice. The installed
macOS app now offers per-server Keychain or private local-file storage. With no
selection it does not read credentials or auto-connect; normal bearer/cache
reads do not create a device identity. Creation occurs only through explicit
device setup. Migration explicitly copies and verifies the identity without
deleting the source or overwriting conflicts. Private files use 0700 directories,
0600 files, and atomic publication. Source review passed; the first app build
found an initializer-order error, which was corrected before the successful
build and installation. Following the user's instruction to install and let
them test, the new storage tests were not run and no functional credential,
bootstrap, or pairing operations were performed during installation.

Installation found and corrected deployment defects: hard-coded image
architecture, unreadable release roots, missing shared-network provisioning,
Mem0's unwritable home, Qdrant's unwritable snapshot directory, whole-stack
shutdown on worker failure, unreadable voice static files, and a non-relocatable
macOS WebRTC runpath. Release-script fixture checks, Compose validation,
systemd unit validation, and independent live checks cover these corrections.
They do not establish memory-job processing, real model execution, voice
calls, recovery, or end-to-end product acceptance. Operational paths and manual
activation steps are in [local deployment operations](local-deployment.md).


## 11. Repository and workflow

Archived proposals are historical evidence, not the current decision. Keep secrets out of tracked files. Report verification commands and results honestly; static checks, fixtures, and prototypes must be labeled by what they actually prove.

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

Create only directories that contain tracked files. Other contributors will own native clients, AO/runner/deployment, memory, voice, and capability implementations. Core interfaces must let those modules register authenticated routes and command handlers without replacing server composition.

Available integration machines/tools: macOS host `zhuwei@macair` (earlier installation evidence used Xcode 16.2 and Swift 6.0.3; the later MCP session observed Xcode 27.1), Go 1.26.4 at `/root/environment/go1.26.4/bin/go`, and the planned Go module uses `modernc.org/sqlite` for pure-Go SQLite. Recheck installed versions before use. Availability does not mean a device build, voice test, or deployment has passed.

### Operator Xcode MCP access

The repository [Xcode MCP over SSH skill](../.agents/skills/xcode-mcp-ssh/SKILL.md) preserves the operator workflow: Linux connects over existing SSH authentication to Apple's `xcrun mcpbridge` on the Mac. This is development tooling, not a Cylo runtime, a new Mac control server, or an exception to AO's runtime ownership. The helper uses client name `Codex`; persistent MCP registration is optional and is not performed by the skill or helper itself. General MCP enablement, agent/project permission, and iPhone developer trust remain distinct user-controlled permissions. Existing restrictions on Mac system changes and external services still apply.

In the motivating session, Xcode 27.1 exposed its MCP tools over SSH and a physical iPhone 13 mini was selected. The remote checkout's iOS runpath was corrected after a dyld crash showed that embedded WebRTC could not be found. A stopped debugger session was ended, the corrected app was installed and launched using Xcode's `devicectl`, a new process was observed, and the user reported visible UI. The scheme/runpath edits were made in that remote checkout and are not integrated into this repository by adding this skill. This is limited installation/startup evidence, not complete functional, voice, APNs, or cross-device acceptance. The original session used an ad hoc probe. The reusable helper subsequently passed read-only discovery/workspace calls on the Mac and local protocol fixtures for pagination, fragmented frames, notifications, workspace substitution, stop-on-error, and timeout cleanup; device deployment through this new helper was not repeated.

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
| `CY-16-06` | Support permission-aware inspection, export, and deletion across derived stores and defined backup policy. | Go control plane; operations | 1 and 5 | Schema 2 ZIP export source includes authority data, hashes, inventory, omissions, and bounded artifact streaming; the native client has a temporary download and explicit Save flow. Verify final Go and Swift paths; all-store deletion and backup-erasure policy remain unaccepted; prove crash durability, single-instance fencing, and production restore rehearsal |
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

## AO startup deployment — 2026-10-26

Source `63cedecaedc26ae73acd8d3ab85cc31c158c2a48` passed the AO deployment workflow on a GitHub-hosted ARM64 runner: [run 40361948749](https://github.com/ronigooja/Cylo/actions/runs/40361948749). The workflow checked the pinned AO patch output tree, passed the Codex app-server and agent-account adapter tests, compiled AO, and built the ARM64 runtime image. Earlier CI attempts exposed socket-root permissions and Unix socket path-length failures; those candidates were not deployed.

The deployed change places the per-session provider socket outside the daemon's private temporary namespace, shortens the socket path, prepares the runtime mount directory, and bounds startup stderr capture with credential redaction. The previous claim that the missing mount directory alone proved the root cause was not established by the retained Docker error.

The CI artifact checksums passed locally. `/opt/cylo/ao/bin/ao` was replaced with SHA-256 `5a5bd490e9a6ac5392fa217cfefc4585769f69fde478fb6a86711e2d2b77f7a2`; AO and runner image references now select `cylo-ao-codex:63cedecaedc2`. AO restarted and its readiness endpoint returned `ready`. At the user's request, no backup copies were created and the replaced `cylo-ao-codex:0.146.0` image was removed. No local compilation, image build, or executable test suite was run for this deployment.

End-to-end chat acceptance remains blocked: the host currently uses `10.211.55.8`, while the server's published port configuration still names `192.168.1.7`; port 8787 is unreachable even on host loopback. The runner consequently restarts after control connection refusal. Updating the Cylo port binding was requested separately and remains pending user approval. SSH, host networking, DNS, routing, and external CLIProxyAPI configuration were not changed. Three existing uncertain runs were preserved and were not replayed or manually rewritten.

### Hostname-selected Cylo binding — 2026-10-26

The user authorized binding the service through `debian.local`. Docker publish addresses are numeric, so `/etc/cylo/resolve-server-address.py` now resolves that name at each `cylo-compose` systemd start, requires exactly one matching local non-loopback IPv4 address, and updates only `CYLO_SERVER_LAN_ADDRESS` in the deployment environment file before Compose reads it. Source: `deploy/linux/resolve-server-address.py`. The systemd LAN override runs the resolver before its existing configuration check. No host DNS, SSH, routing, interface settings, or external services were changed; no backup was created.

After restarting the Cylo stack, `debian.local` resolved to `10.211.55.8`; Docker published port 8787 on that address and `127.0.0.1`. Both `http://debian.local:8787/healthz` and the loopback health endpoint returned `{"status":"ok"}`. Cylo, runner, and AO were active. The previously recorded control connection blocker is resolved. Source syntax and whitespace checks passed locally; this operational verification did not compile code, build images, or run an automated test suite. Existing uncertain runs remain unchanged. No owner key was available on the server for submitting a fresh client command, so a new chat reply and macOS interaction remain unverified.

### Unknown-create reconciliation and queue capacity — 2026-10-26

A timed-out or otherwise ambiguous AO create remains `effect_unknown`; it is
never replayed automatically. Explicit runner maintenance may reconcile only an
unbound unknown create after the same-fence journal/server checks, a complete AO
inventory with request identities, and complete Docker absence evidence. AO must
have restarted after the unknown journal update to exclude an original request
still awaiting handling. The inventory also waits for entered create calls and
includes durable preparation/uncertain rows. Any incomplete evidence or existing
runtime/container record retains the uncertain state. Successful reconciliation
uses the existing audited, idempotent observation protocol to fail the run/task
and release its slot. Refused attempts are retained in the runner audit journal.

The patched AO protocol uses `worker_boot_failed` only after supervised startup
failure, verified runtime cleanup, successful workspace rollback, and verified
absence of the session row. Other failures remain uncertain. Snapshot capacity
and per-task queue reasons expose the global admission gate for operator diagnosis.
Apple conversation and task views show ordinary task status without internal
capacity or reconciliation diagnostics; existing drafts, command keys, and event
refresh remain authoritative. See [the operator runbook](local-deployment.md#unknown-ao-creation-reconciliation).

Hosted verification passed:

- [Linux run 40371027074](https://github.com/ronigooja/Cylo/actions/runs/40371027074), source `c31d8d3d3b7d6643768af946521f3be04a198b50`, passed `go test ./...`, the expanded race check across control/runner/AO/server/operator packages, and release/CLI staging checks. The initial race run found an unsynchronized counter in the existing AO timeout test; the fixture was corrected before this passing run. The final tests cover `409 worker_boot_failed` failure/capacity release and exact receipt replay; the same code with HTTP 503 remains uncertain.
- [AO run 40370123374](https://github.com/ronigooja/Cylo/actions/runs/40370123374), implementation source `5f99878b1ef684ea56c65f0342393fc67af0f703`, passed pinned patch application/tree fixtures, Codex/provider, session-manager, session-service, and HTTP-controller tests and built the ARM64 daemon. Runtime image sources were unchanged, so this run selected the binary-only artifact; the deployed `cylo-ao-codex:63cedecaedc2` image was retained.
- [Apple run 40370598915](https://github.com/ronigooja/Cylo/actions/runs/40370598915), the same implementation source, passed 82 Swift package tests, the unsigned macOS archive, and the unsigned iOS build. [Server run 40370133628](https://github.com/ronigooja/Cylo/actions/runs/40370133628) built the ARM64 server image and runner binary. Later source commits changed comments and tests only; deployed implementation matches this artifact source.

Artifact SHA-256 checks passed before deployment. The server now runs
`cylo-server:reconciliation-5f99878b1ef6`; systemd selects immutable runner
`/opt/cylo/runner/releases/reconciliation-5f99878/cylo-runner` (SHA-256
`944a9ac813f482880d503c93c3bd64e3e24375431b649362c3bd8814b531158a`)
and AO `/opt/cylo/ao/bin/ao-reconciliation-5f99878` (SHA-256
`127255213b445b2f1ac832d338f59e0bcecaa2fe05ce5582b29afc6f3fcc5bd6`).
The loopback and `debian.local` control health endpoints returned `ok`; AO was
ready and its inventory reported complete empty sessions with a fresh process
start time. All three services were active, and the runner had zero restarts
at the final service check. Existing external CLIProxyAPI configuration was not
changed.

With the runner stopped and AO restarted, all four explicit per-run
reconciliation attempts on 2026-10-26 at 17:42:58 UTC correctly refused at Docker
inventory. An existing AO-labeled container remained in `created` state, PID 0,
with no recorded process start; the conservative check also refuses any
uncorrelated managed runtime. Each refusal durably records the actor, run,
attempt/fence/instance, check time, AO incarnation and complete empty inventory,
and Docker refusal in `journal_reconciliations`. All four runs remain
`uncertain`, all four slots remain occupied, and the runner was restarted.
No journal, SQLite authority row, or container record was deleted. Recovery of
the old queue is **not accepted**; the retained container needs a separately
scoped lifecycle recovery decision before absence can be proved.

The CI macOS archive was copied to
`~/Developer/Cylo/runs/ao-reconciliation-20261026/`, signed ad hoc on the authorized
Mac with the installed application's existing no-entitlement signing boundary,
and installed at `/Applications/Cylo.app`. Strict signature verification and
installed/staged executable comparison passed, and a new `CyloMac` process was
observed. The archive and signing logs are preserved in that task directory.
No local compilation or automated test suite ran. Native UI interaction and a
fresh minimum chat remain unverified; the user chose to perform chat verification.
No new AO container/app-server/provider execution is claimed, and the retained
uncertain slots currently prevent a fresh queued task from dispatching.

Controller CI logs and operational refusal outputs are retained under
`/tmp/cylo-reconciliation-20261026/`; persistent refusal evidence is in the runner
journal. See the runbook for repeatable commands and acceptance boundaries.

### Chat recovery correction — 2026-10-26

The earlier implementation incorrectly treated every managed service container
and every container record as an unresolved AO runtime. Operator reconciliation
now supports exact, explicitly selected records with two stable full Docker
inspections: strictly never-started AO records are preserved as created artifacts;
explicit historical capability records must predate the original dispatch and
the authoritative run start, with complete capability identity and pinned image.
Previously started, running, unknown, changed, or incomplete AO state still fails
closed. No database shortcut or blind create retry is introduced.

AO background tasks previously replaced their session config ID but inherited
the parent's `CYLO_CODEX_SESSION_ID`. Supervised container names were derived
only from that identity, causing background and main-session launch collisions.
The observed retained container's working directory belonged to background title
generation. Background task environments now own their own identity and runtime
role, including prepared environments. Every Docker launch also has a random
launch label/name; cleanup without a cidfile can select only that exact launch.
An unacknowledged create with no verified container identity stays ambiguous.
Runner supervision preserves complete never-started historical records while
finding/stopping the actual current runtime; ambiguous live duplicates still
fail closed.

Internal runtime faults, slot counts, and reconciliation instructions have been
removed from the Apple conversation/task surfaces. The actual task status,
submission errors, drafts, and original command receipt behavior are retained.
Operator diagnostics and audit evidence remain available. Source review is
complete; remote verification and live recovery evidence follow below.
