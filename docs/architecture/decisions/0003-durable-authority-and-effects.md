# ADR 0003: Durable authority and uncertain external effects

Status: accepted architecture baseline; implementation and acceptance status remain in [the master plan](../../master-plan.md). Recorded during the 2026-10-27 documentation consolidation from existing decisions; this record does not introduce a new delivery claim.

## Context

Commands, runner reports, voice retransmissions, and reconnects may duplicate messages or lose responses. External work can succeed even when Cylo never receives a receipt. Conversation state, an agent answer, or an optimistic client update cannot establish actual business success.

## Decision

For every business command, the Go authority validates authorization and applicable optimistic versions, then atomically commits the receipt, work record, and transactional outbox entry in SQLite before returning acceptance. Use stable command keys and event identities for replay/deduplication; clients reconcile the original receipt after an unknown response.

Treat AO callbacks and runner reports as observations until the control plane validates ownership, fence, version, and evidence. Keep acceptance, admission, running work, agent output, actual effect success, achieved goal, and confirmed stop distinct.

When an external effect is unknown, query/reconcile it if possible or escalate for human resolution. Never blindly retry an uncertain effect or free its execution resources on lease expiry alone. During migration or restore, fence the old server so no two active instances operate on copied authority.

## Consequences

Delivery can retry safely without creating a second business command, while external effects require their own identity/reconciliation state. Unknown work can retain capacity until evidence resolves it; that is a truthful safety boundary rather than proof of a stopped process. Tests must exercise crashes/lost receipts at actual effect boundaries. WAL/FULL and local fixtures do not establish host durability, production migration fencing, or complete recovery acceptance.

## Sources

[Product chapters 8, 12, and 15–16](../../product.md); [command/event architecture](../../architecture.md#commands-receipts-and-events); [implemented contracts](../../../contracts/http.go).
