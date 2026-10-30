# ADR 0006: Memory authority with controlled workers and rebuildable retrieval

Status: accepted architecture baseline; implementation and acceptance evidence are tracked in [the acceptance evidence register](../../acceptance/evidence.md), while delivery status remains in [the master plan](../../master-plan.md).

## Context

Zoe needs accurate historical decisions and scoped context beyond conversation lifetime. Extraction and retrieval are asynchronous and can be stale. A memory model must not overwrite confirmed decisions or revive deleted content from an old index or backup.

## Decision

For memory changes, the Go control plane owns sources, versions, and tombstones; accepted changes become synchronously visible through that authority. Mem0 is a scoped controlled worker. Qdrant is a derived, rebuildable index. Task, permission, configuration, and confirmed-decision authority cannot be changed through unrestricted memory writes.

Use the pipeline source event → authority → transactional outbox → extraction/indexing → scoped retrieval → permission/version recheck → context assembly. Keep facts, outcomes, and model inference distinct. Explicit decisions register through policy; inferred claims remain candidates. Build context by task/runtime/project/permission scope and surface missing/conflicting evidence as uncertainty. Run curator judgment on demand within the runtime cap.

Implementation parameters for embeddings, collections, and worker storage are maintained in the [memory worker documentation](../../../services/memory/README.md). The authority boundary and deletion safety model are the decision: deletion immediately blocks retrieval/new context through a versioned tombstone, then requires idempotent, version-checked cleanup across applicable derived stores and backups under the configured retention/erasure policy. Do not assume a cross-store transaction; unsupported targets remain `adapter_missing`, and restore remains gated until it reconciles against the current deletion generation/head/hash.

## Consequences

Mem0 and Qdrant can evolve without replacing Zoe's task/memory authority. Rebuild and cleanup lag require explicit reconciliation and stale-version rejection. A backup's embedded journal head cannot establish freshness or prove later deletions were applied. Physical erasure and production retention require each applicable store/backup checkpoint; the applicable checks are recorded in the [acceptance evidence register](../../acceptance/evidence.md). Model versions, artifact hashes, and storage configuration remain in the worker's primary documentation rather than duplicated in the ADR.

## Sources

[Product chapters 10–11 and 16](../../product.md); [memory architecture](../../architecture.md#memory-retrieval-and-context); [memory worker](../../../services/memory/README.md); historical evidence: [memory requirements](https://github.com/ronigooja/Zoe/blob/95ee55a5bbd8dccfb7f14af9f9703bb83727da6c/docs/archive/research/reports/17-memory-requirements-and-design.md).
