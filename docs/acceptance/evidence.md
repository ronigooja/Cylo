# Acceptance evidence register

These retained observations still support current implementation conclusions. They do not accept a complete product milestone. Each record retains its source version, environment, result, and limitations; earlier author-reported checks are not newly rerun by this documentation change. The [pre-restructure source record](https://github.com/ronigooja/Zoe/blob/95ee55a5bbd8dccfb7f14af9f9703bb83727da6c/docs/master-plan.md) supplies provenance for local logs and review reports that have no public CI artifact.

## Component verification

| Area | Source/environment and observed checks | Acceptance limits |
|---|---|---|
| Control plane and runner | On integrated source commit `c0380c7`, the owner reported PASS for `PATH=/usr/local/go/bin:$PATH go test ./cmd/zoe-server -run TestComposedServerRegistersDerivedWorkerCredentialAndFailsClosedWithoutVoiceGateway -count=1` and `PATH=/usr/local/go/bin:$PATH go test ./...`; `git diff --check 2d35a86..HEAD` also passed on the later candidate containing this source. Independent review passed `go test ./internal/capabilities -run TestCapabilityTriggerRunnerAOResultChain -count=1`. The AO-chain fixture uses fake AO HTTP. The initial composition failure was a test-fixture omission; review confirmed production startup registers the maintenance migration. No remaining concrete findings were reported for this integration scope. The earlier live AO create attempt returned HTTP 409 without a retained allowlisted rejection code, so the result is treated as uncertain. A later read-only inspection observed zero sessions, but that does not establish that this request had no external effect; no artifact, provider outcome, or termination proof was recorded. | A successful real AO task chain with provider/tool/artifact evidence and confirmed termination; recovery against a real AO deployment; provider-side usage/cost evidence; production deployment and cross-host migration fencing; complete scenario acceptance. |
| Budget enforcement | Owner and independent AO/control/runner review reported passing targeted tests; the integrated Go suite and relevant race packages passed on the owner checkpoint. These checks cover local ledger and control behavior. | AO has no production per-provider-call reservation hook or actual input/output usage reporting across its internal provider/tool calls; only the initial prompt is currently counted as one `ModelCalls` reservation. It also has no production pre-call tool-step enforcement or bounded artifact sink. `MaxToolSteps` and `MaxArtifactBytes` currently exist as DTO, snapshot, or ledger values, not as end-to-end limits on actual tool loops and artifact writes. Hard-deadline termination needs an online fence and is not an offline hard-stop guarantee. Full usage/cost accounting requires provider-backed evidence. |
| Memory and export | Existing stub-backed Go→Python→Mem0→ephemeral-Qdrant tests cover component composition, not live AO/provider behavior. Bounded export checks passed for >64 MiB ZIP, hash/count, oversized rows, cancellation, permission, and concurrent snapshots; cursor/event-ID alignment passed source review. Restore candidate `59c61d93` passed owner Go/Python/restore-drill checks and independent review with no blocking findings. Cache/export-temp commits `fa4930d`/`9041464` passed independent review; owner reported seven Go-package and two race-package checks PASS. Independent commands passed: `/usr/local/go/bin/go test ./internal/memory ./internal/control ./tests/acceptance ./tests/integration -count=1` and `/usr/local/go/bin/go test -race ./internal/memory ./internal/control -count=1`. Backup purge/manifest commit `f3c8ab6`/`4642ff9` passed independent review and 13 tests: `PYTHONDONTWRITEBYTECODE=1 PYTHONPATH=tools /tmp/zoe-memory-export-venv/bin/python -m unittest data_lifecycle.test_retention data_lifecycle.test_backup_manifest -v`. The integrated local backup fixture also exercised encrypted temporary-backup create/delete, retention, latest-head staging, and restore preparation/reconciliation; it confirmed Mem0/Qdrant remain `adapter_missing`, the restore gate stays closed, and one artifact remains. The backup lifecycle/worker checks are local; they do not establish physical deletion from production external stores. | The maintenance user's current authority generation/head/hash remains the trust root; journal continuity does not prove every restore transition independently. Real Mem0/Qdrant cleanup and physical external-store deletion remain pending; unsupported resources stay `adapter_missing` and cannot be manually cleared. Production backup erasure, a measured retention guarantee, and restore rehearsal remain open. |
| Voice | `PATH=/usr/local/go/bin:$PATH go test ./...` passed on the integrated source candidate after commit `c0380c7`, including voice, acceptance, and server packages. This verifies local code paths only. | The selected Space3044 response does not expose account-lease identity or trusted account-scoped termination evidence. The adapter remains fail-closed; safe pool account reuse, real gateway behavior, interruption, device, and voice-to-task acceptance remain unverified. |
| Native Apple clients | Source review confirmed the server/Apple event-ID and cursor contract, Save-authority checks, and invalidation paths (server `114f7b8`, Apple `e009e39`, fixture fix `0af961d`). At that earlier checkpoint, related tests were added but no Swift tests or builds ran. On 2026-10-24 a macOS Xcode build, installation, launch, relocated WebRTC loading, ad-hoc signature verification, and native backend reachability passed; see the [installation source record](https://github.com/ronigooja/Zoe/blob/95ee55a5bbd8dccfb7f14af9f9703bb83727da6c/docs/master-plan.md#historical-local-installation-evidence--2026-10-24). Existing APNs token registration, revocation, and inbox code is source implementation only. | Current-key authenticated macOS app flows, distribution signing/notarization, iPhone/iPad installation, APNs delivery, and real-device voice/media acceptance. |
| Apple interface redesign | On the earlier Linux validation host, `PATH=/usr/local/go/bin:$PATH go test ./...` and `PATH=/usr/local/go/bin:$PATH go test -race ./internal/control` passed on the integrated API source; history tests cover ACL/scope denial, cursor/limit validation, stable timestamp ordering, and pagination. At the Linux checkpoint, nine new Swift contract/state regression tests were added but had not run. The [earlier installation record](https://github.com/ronigooja/Zoe/blob/95ee55a5bbd8dccfb7f14af9f9703bb83727da6c/docs/master-plan.md#historical-apple-redesign-installation-refresh) records successful execution of the full 67-test Swift suite and fresh macOS/iOS target builds. A tree-sitter 0.26.0 / Swift grammar 0.7.4 syntax pass covered all 25 Swift files; paired locale keys, literal-source coverage, format placeholders, plural plists, `python3 tools/check_docs.py`, and `git diff --check` passed. These checks do not type-check Swift or render the UI. | The installation refresh establishes fresh Swift tests and macOS/iOS app-target builds, macOS local installation/startup, and Launchpad registration only. Screenshots across the [native acceptance matrix](requirements.md#native-interface-coverage), core UI flow tests, VoiceOver/Dynamic Type/keyboard checks, real iPhone/iPad devices, and real services remain open. No complete redesign UI or product acceptance is claimed. |
| Capabilities | The integrated trigger→control/engine→fake AO HTTP fixture passed through authorized acceptance, terminal report, and slot release. Owner and independent reviewer both passed the lifecycle test; the independent command was `go test ./internal/capabilities -run TestCapabilityTriggerRunnerAOResultChain -count=1`. Isolated Docker recovery and revocation checks passed on synthetic workloads (76.52 s and 26.62 s respectively). These local tests do not exercise real AO or external business actions. | Real AO-backed trigger execution, real external actions, production service lifecycle/deployment and recovery. |
| Backup and recovery | On integrated candidate `d9f97d7e21b81bf62b4f7132698761ac3703bf85`, the owner reported PASS for `PATH=/usr/local/go/bin:$PATH ZOE_OPS_PYTHON=/tmp/zoe-memory-export-venv/bin/python go test -race -timeout 120s ./internal/memory ./tests/ops` and `PATH=/usr/local/go/bin:$PATH go test -timeout 120s ./internal/memory -run 'TestExportTempCleanupHTTPThenDeletionCompletionAdvancesCheckpoint|TestBackupDeleteManifestCleanupHTTP' -count=1`. Independent review accepted the candidate with no blocking findings and passed `PATH=/usr/local/go/bin:$PATH ZOE_OPS_PYTHON=/tmp/zoe-memory-export-venv/bin/python PYTHONDONTWRITEBYTECODE=1 go test -race -timeout 120s ./internal/memory ./tests/ops -run 'TestExportTempCleanupHTTPThenDeletionCompletionAdvancesCheckpoint|TestBackupDeleteManifestCleanupHTTP|TestEncryptedBackupPersistsNonemptyDeletionManifest|TestMaintenanceRESTEncryptedRestoreDrill' -count=1 -v`. On that same source HEAD, the owner also reran `PATH=/usr/local/go/bin:$PATH ZOE_OPS_PYTHON=/tmp/zoe-memory-export-venv/bin/python go test ./...` and `PATH=/usr/local/go/bin:$PATH ZOE_OPS_PYTHON=/tmp/zoe-memory-export-venv/bin/python go test -race -timeout 120s ./internal/memory ./internal/control ./internal/storage ./internal/runner ./tests/ops ./tests/integration ./tests/acceptance`; both passed. Python checks passed: `PYTHONPATH=tools PYTHONDONTWRITEBYTECODE=1 /tmp/zoe-memory-export-venv/bin/python -m unittest discover -s tools/data_lifecycle -t tools -p 'test*.py'` (35 tests), `PYTHONDONTWRITEBYTECODE=1 services/memory/.venv/bin/python -m unittest services.memory.test_deletion_only_worker services.memory.test_mem0_backend services.memory.test_model_call_accounting services.memory.test_worker_logging` (21 tests), and `PYTHONPYCACHEPREFIX=/tmp/zoe-integration-pycache /tmp/zoe-memory-export-venv/bin/python -m compileall -q services/memory tools/data_lifecycle`; all passed. These tests cover authenticated export cleanup through generic-job completion/checkpoint and backup-manifest cleanup through real PUT/DELETE transactions, journal/durable-manifest updates, encrypted CLI round-trip, HMAC retention, latest-head staging, and restore preparation/reconciliation. The Go fixture used synthetic export registry/artifact data and did not launch a Python process; its assertions keep Mem0/Qdrant `adapter_missing`, the restore gate closed, and one artifact present. Separately, Python worker tests cover both modes through RPC and completion. These are local fixture and test results. | Preserve the operator-maintained current generation/head/hash as the trust root; journal continuity does not prove every restore transition independently. Complete real external adapter checkpoints and physical deletion; physical host migration; production host/systemd isolation; measured RPO/RTO; production backup/restore and full data-erasure acceptance. |
| Operator CLI and QR connection | Dependency version/checksums resolved without compilation. Shell syntax and whitespace checks passed for release/install changes. The later [hosted records](#hosted-runtime-and-installation-verification) establish integrated Go/race, staging, Swift, and app-build checks on their stated revisions. | Actual systemd/Compose change and rollback behavior, release installation, physical iPhone/iPad scanning, and real AO/provider execution. No service deployment or external CLIProxyAPI change is established by this candidate. |
| Linux deployment | On 2026-10-24 the ARM64 images built and the final release started locally; runner authentication, Linux/Mac reachability, Qdrant readiness, and independent checks passed. See the [installation source record](https://github.com/ronigooja/Zoe/blob/95ee55a5bbd8dccfb7f14af9f9703bb83727da6c/docs/master-plan.md#historical-local-installation-evidence--2026-10-24). The pinned memory-model artifacts downloaded and hash verification passed; both Qdrant collections initialized. | Memory-job processing, real AO/model task execution, production security/isolation acceptance, operational recovery, and migration. |

## Client voice diagnostics — 2026-10-29

Source review and local checks covered the authenticated Go endpoint, migration, redacted-field rejection, bounded event persistence, and Swift source type-checking for the diagnostics queue/API client files. The Go compile-only sweep passed with `GOCACHE=/tmp/zoe-gocache go test ./... -run '^$'`; focused diagnostics endpoint tests passed. Full Go execution remains environment-limited here by restricted listener, cache, and temporary filesystem permissions. Swift package tests and WebRTC app builds were not run; the new queue and API client sources were type-checked with `xcrun swiftc` on the local macOS SDK. Real-device voice, hosted CI, and end-to-end diagnostic upload remain open acceptance work.

## Instance-key and scoped native checks

[Apple run 40252480817](https://github.com/ronigooja/Zoe/actions/runs/40252480817) checked source `ede06a7686809b44e33981d23150466c8bdbe733` on a GitHub-hosted arm64 macOS 15.7.9 runner with Xcode 16.4 and Swift 6.1.2: 76 Swift tests, unsigned universal macOS archive, and unsigned iOS build passed with saved exit statuses 0. [Linux run 40252752759](https://github.com/ronigooja/Zoe/actions/runs/40252752759) checked `5679168d74ca1f8e271c2708088e7ff620b4f195` on GitHub-hosted Ubuntu, Go 1.26.9 linux/amd64, Linux 6.17.0-1022-azure: `go test ./...` and `go test -race ./internal/control ./cmd/zoe-server` passed. Apple source/workflow was identical between these two revisions. Exact commands are in the [Apple workflow at that revision](https://github.com/ronigooja/Zoe/blob/ede06a7686809b44e33981d23150466c8bdbe733/.github/workflows/apple.yml) and [Linux workflow](https://github.com/ronigooja/Zoe/blob/5679168d74ca1f8e271c2708088e7ff620b4f195/.github/workflows/linux.yml). Retrieved logs/exits/artifacts were recorded under `/tmp/zoe-single-key-verification/`. These checks support the current shared-key source baseline only; installation, protected transport, multi-device interaction, signing, real APNs, voice, and complete task flows remain open. Synthetic lifecycle fixtures performed no real external destruction. CI artifacts have limited retention; the immutable run/source links remain the provenance.

The earlier Settings repair at `042f289` passed 70 Swift tests, unsigned universal macOS archive, and unsigned iOS build in [Apple run 40204390633](https://github.com/ronigooja/Zoe/actions/runs/40204390633), on GitHub-hosted arm64 macOS 15.7.9, Xcode 16.4, Swift 6.1.2. Integrated `da29a8b` had the same committed tree. The artifact was ad-hoc signed, installed, and launched with strict signature/process checks. The user confirmed Command-comma and Account & Settings → Settings rendered correctly. This accepts only that reported blank-Settings presentation repair on that revision; it does not accept current connection, authorization, notification, export, or whole-interface flows. Installation evidence was moved to `~/Developer/Zoe/runs/zoe-settings-fix-ci-20261025/`, with `migration.json` preserving its historical paths. The [original scoped record](https://github.com/ronigooja/Zoe/blob/95ee55a5bbd8dccfb7f14af9f9703bb83727da6c/docs/master-plan.md#settings-window-repair--2026-10-25) provides provenance.

## Hosted runtime and installation verification

Hosted verification passed:

- [Linux run 40371027074](https://github.com/ronigooja/Zoe/actions/runs/40371027074), source `c31d8d3d3b7d6643768af946521f3be04a198b50`, passed `go test ./...`, the expanded race check across control/runner/AO/server/operator packages, and release/CLI staging checks. The initial race run found an unsynchronized counter in the existing AO timeout test; the fixture was corrected before this passing run. The final tests cover `409 worker_boot_failed` failure/capacity release and exact receipt replay; the same code with HTTP 503 remains uncertain.
- [AO run 40370123374](https://github.com/ronigooja/Zoe/actions/runs/40370123374), implementation source `5f99878b1ef684ea56c65f0342393fc67af0f703`, passed pinned patch application/tree fixtures, Codex/provider, session-manager, session-service, and HTTP-controller tests and built the ARM64 daemon. Runtime image sources were unchanged, so this run selected the binary-only artifact; the deployed `zoe-ao-codex:63cedecaedc2` image was retained.
- [Apple run 40370598915](https://github.com/ronigooja/Zoe/actions/runs/40370598915), the same implementation source, passed 82 Swift package tests, the unsigned macOS archive, and the unsigned iOS build. [Server run 40370133628](https://github.com/ronigooja/Zoe/actions/runs/40370133628) built the ARM64 server image and runner binary. Later source commits changed comments and tests only; deployed implementation matches this artifact source.

Artifact SHA-256 checks passed before deployment. At this recorded checkpoint, the server ran
`zoe-server:reconciliation-5f99878b1ef6`; systemd selects immutable runner
`/opt/zoe/runner/releases/reconciliation-5f99878/zoe-runner` (SHA-256
`944a9ac813f482880d503c93c3bd64e3e24375431b649362c3bd8814b531158a`)
and AO `/opt/zoe/ao/bin/ao-reconciliation-5f99878` (SHA-256
`127255213b445b2f1ac832d338f59e0bcecaa2fe05ce5582b29afc6f3fcc5bd6`).
The recorded control health endpoints returned `ok`; AO was
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
and Docker refusal in `journal_reconciliations`. At this checkpoint, all four runs remained
`uncertain` and all four slots remained occupied, and the runner was restarted.
No journal, SQLite authority row, or container record was deleted. Recovery of
the old queue is **not accepted**; the retained container needs a separately
scoped lifecycle recovery decision before absence can be proved.

The CI macOS archive was copied to
`~/Developer/Zoe/runs/ao-reconciliation-20261026/`, signed ad hoc on the authorized
Mac with the installed application's existing no-entitlement signing boundary,
and installed at `/Applications/Zoe.app`. Strict signature verification and
installed/staged executable comparison passed, and a new `ZoeMac` process was
observed. The archive and signing logs are preserved in that task directory.
No local compilation or automated test suite ran. Native UI interaction and a
fresh minimum chat remain unverified; the user chose to perform chat verification.
No new AO container/app-server/provider execution is claimed, and the retained
uncertain slots at that checkpoint prevented a fresh queued task from dispatching.

Controller CI logs and operational refusal outputs are retained under
`/tmp/zoe-reconciliation-20261026/`; persistent refusal evidence is in the runner
journal. See [deployment operations](../operations/deployment.md#unknown-ao-creation-reconciliation) for repeatable commands and acceptance boundaries.


## Chat recovery correction

The subsequent correction permits exact operator-selected never-started AO records and historical capability containers only after two stable complete inspections. It also gives AO background tasks their own identity/role and random launch labels so cleanup targets an exact launch. Ambiguous runtime evidence still fails closed. Sources: [runner reconciliation](../../internal/runner/reconciliation.go), [AO patch](../../deploy/linux/patches/ao-supervised-codex.patch), and [original review record](https://github.com/ronigooja/Zoe/blob/95ee55a5bbd8dccfb7f14af9f9703bb83727da6c/docs/master-plan.md#chat-recovery-correction--2026-10-26).

**Source reviewed; remote recovery unverified.** The source review does not show these corrections deployed, the old queue reconciled, slots released, or a new live chat completed. The four previously refused runs must not be inferred recovered.

## Scoped CI verification

On source `d0a786ecac14681f5808ccdef1eb96dfdd02355e`,
[hosted run 40549291218](https://github.com/ronigooja/Zoe/actions/runs/40549291218)
passed scope-selection regressions (including renames, deletions, and unavailable
history), actionlint workflow syntax validation, documentation/checker regressions,
`go test ./...`, the existing authorization race package set, release/CLI-install
script regressions, Swift package tests, and independent unsigned macOS/iOS builds.
The Linux, Apple, and overall required-check gates passed. Checks ran on
GitHub-hosted Linux and macOS runners; no local build or test suite ran.

Independent source review findings were corrected before this run. Artifact
metadata showed only logs, and both release jobs were skipped as expected for
routine verification. This establishes the full selection path and current
component builds/tests; path-specific omissions are protected by selector
regressions. Manual release packaging, installation, external-service integration,
end-to-end acceptance, and remote branch-protection configuration were not
verified or changed. See [CI verification](../development/verification.md) for
scope and manual entry points.

## Voice deployment configuration repair — 2026-10-29

On the user-identified Zoe host `136.0.11.159`, the configured HTTPS gateway's
authenticated `/v1/health` and `/v1/voice/config` returned HTTP 200. The latter
contained the expected public configuration fields. The running server's old
`/etc/zoe/active.compose.yaml` omitted the voice secret mount and secret-file
mapping despite the host environment already configuring both. This left the
server without its gateway credential, triggering the generic unavailable response.

The deployment Compose file was backed up and updated with the voice secret
mount and mapping from the repository template. The first recreation exposed
root-only ownership of the private credential; ownership was corrected to the
server's UID/GID 65532 while retaining mode 0600. The recreated server was running
with zero restarts, `/healthz` returned `ok`, and a value-free inspection confirmed
that the server process loaded the voice credential. No gateway or CLIProxyAPI
configuration changed. No build or test suite ran. An authenticated request through
Zoe's public voice endpoint, macOS retry, and real microphone/WebRTC acceptance
remain unverified.

During this investigation, the operator explicitly ran the CLI key-generation
operation. The resulting private export became the authority database's current
key; the deployment copy was then synchronized from that export and the previous
copy was retained as a mode-0400 backup. Container recreation does not generate or
rotate an instance key: only the explicit CLI `key init`/`key rotate` operations do
so, and every client must reimport the resulting private export after rotation. The
matching key authenticated a local `/v1/voice/config` request and returned the
complete configuration. The key value is not recorded here.

## Evidence boundaries

When adding verification records, state source revision, environment, action/command, result, and limitations. Keep source review, fixtures, hosted CI, installation/startup, and full acceptance distinct. Link immutable CI or source records; omit keys, account data, private message text, and raw audio. The [acceptance matrix](requirements.md) records remaining gates.

## Zoe rename — 2026-10-28

On the local main worktree, exact case-preserving text replacement updated 265
files and renamed 68 files, including their parent directories, across all 299
tracked files. A byte-for-byte comparison against the expected transformation
passed for every tracked file, and a case-insensitive source scan found no old
project-name occurrences. Git whitespace validation passed.

This is source transformation evidence only. Hosted builds, tests, installation,
and end-to-end acceptance were not run for this rename. The existing Git remote
and external deployments were not renamed; rewritten repository URLs and new
application, credential, storage, and service identifiers require corresponding
external migration before deployment. The raster design reference was retained.
