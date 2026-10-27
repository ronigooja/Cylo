# Data lifecycle and recovery

This policy applies to the current single-instance Cylo baseline. The Go control plane is authoritative for tasks, permissions, events, and memory versions. Mem0 is a controlled worker and Qdrant is a rebuildable index. Derived stores must never become the source of truth.

## Storage and durability

SQLite runs in WAL mode with `synchronous=FULL`. Important records are accepted only after a durable commit. Keep one active writer instance during migration or restore; fence and stop the old instance before starting a restored one. A health response does not prove that a commit, worker effect, or external provider operation completed.

Use the SQLite Online Backup API or the reviewed export tooling for consistent logical copies. Do not treat a direct copy of an active database file as a complete backup when WAL frames may be pending. A backup manifest should identify schema/version, event water level, release/configuration references, object checksums, tombstones, and explicit omissions. Secret values and raw credentials are never included in manifests or evidence.

## Export, migration, and restore

`tools/cylo_export.py` produces a bounded logical ZIP containing authority records, hashes, inventory, and omissions. It omits runtime volumes, secrets, and backups. Validate the archive and its checksums before transfer. Migration and restore require a fenced single-instance rehearsal, verification of task/run state, permissions, memory versions, derived-index rebuild, and external-effect reconciliation. Database rollback does not undo messages, orders, or other external effects that already occurred.

The current repository contains source and design checks for these boundaries. A production restore rehearsal, measured RPO/RTO, and all-store migration are still open acceptance work (see [`CY-16-01`–`CY-16-06`](../master-plan.md#requirements-and-acceptance-index)).

## Deletion and retention

Authoritative deletion creates a tombstone/version change first, then propagates to Mem0, Qdrant, caches, exports, and other derived stores according to policy. Rebuildable indexes may be recreated from the authority, but deletion is not complete until the rebuild no longer returns the record. Backups retain deleted data until their defined expiry; a restore must replay tombstones so deleted records do not reappear.

Raw voice audio is not retained by default. Voice account/session leases and external grants must end before reuse; uncertain accounts are quarantined. Logs and evidence must omit keys, authorization headers, complete audio, private message text, and full environment variables.

## Incident and unknown-effect handling

Persist operation identity, request digest, target, policy/grant version, dispatch state, provider receipt or query result, and uncertainty classification. `dispatching` is not proof of delivery, and an HTTP response alone is not proof of a business effect. Query or reconcile an external effect where possible; otherwise keep the task waiting/escalated and do not blindly retry an irreversible operation.

For AO creation uncertainty, use the reconciliation procedure in [deployment operations](deployment.md#unknown-ao-create-outcomes). Preserve journal rows, task/run history, and container records; never edit them to force a check to pass.

## Current limits

No production backup, restore, deletion across every store, or external-world reconciliation has been accepted. The archived reliability report is a design and source reference, not evidence that those procedures ran: [report 05](../archive/research/reports/05-reliability-security-operations.md).
