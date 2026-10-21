# Cylo memory worker

The controlled Python worker extracts candidates and maintains derived Mem0/Qdrant state; the Go control plane remains authoritative for scope, source, versions, and tombstones. The pinned embedding, persistent versus rebuildable data, deployment settings, secret handling, and deletion boundaries are documented in the [master plan](../../docs/master-plan.md#7-memory-and-context) and [deployment and recovery operations](../../docs/master-plan.md#deployment-and-recovery-operations).

The current implementation is source integrated and has local stub-backed cross-language evidence; this does not establish a live provider-to-AO task chain or production restore. The master plan records exact evidence labels, remaining acceptance, and the opt-in integration command.
