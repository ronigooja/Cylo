# ADR 0001: Linux/native Apple platform and durable persistence

Status: accepted architecture baseline; implementation and acceptance status remain in [the master plan](../../master-plan.md). Recorded during the 2026-10-27 documentation consolidation from existing decisions; this record does not introduce a new delivery claim.

## Context

Zoe needs durable background execution independent of conversation and client availability, plus consistent access from macOS, iPhone, and iPad. Archived proposals considered web/PWA and macOS background hosts, but those are not the selected baseline. The six product responsibilities must remain explicit without forcing every responsibility into a separate process.


Commands, runner reports, voice retransmissions, and reconnects may duplicate messages or lose responses. External work can succeed even when Zoe never receives a receipt. Conversation state, an agent answer, or an optimistic client update cannot establish actual business success.

## Decision

For the current private self-hosted baseline, use a Linux Go control server, a separate Linux runner, and native SwiftUI macOS/iPhone/iPad clients. Use SQLite WAL with `synchronous=FULL` as the initial authoritative store. The control plane owns business state; native clients render records and submit commands.

The first release is single-instance and multi-project with reserved user/workspace access boundaries. Exclude PWA, CAO implementation, and macOS-hosted server work from this baseline. Preserve future evolution without adding a team collaboration UI now.


For every business command, the Go authority validates authorization and applicable optimistic versions, then atomically commits the receipt, work record, and transactional outbox entry in SQLite before returning acceptance. Use stable command keys and event identities for replay/deduplication; clients reconcile the original receipt after an unknown response.

Treat AO callbacks and runner reports as observations until the control plane validates ownership, fence, version, and evidence. Keep acceptance, admission, running work, agent output, actual effect success, achieved goal, and confirmed stop distinct.

When an external effect is unknown, query/reconcile it if possible or escalate for human resolution. Never blindly retry an uncertain effect or free its execution resources on lease expiry alone. During migration or restore, fence the old server so no two active instances operate on copied authority.

## Consequences

Server work can continue when clients close, and client replacement does not move authority. Separate runner supervision avoids making a client or control process the execution lifetime. Linux operational recovery and Apple client delivery require distinct evidence. This choice requires native platform builds/device checks and does not establish that deployment, UI, or end-to-end behavior is accepted.


Delivery can retry safely without creating a second business command, while external effects require their own identity/reconciliation state. Unknown work can retain capacity until evidence resolves it; that is a truthful safety boundary rather than proof of a stopped process. Tests must exercise crashes/lost receipts at actual effect boundaries. WAL/FULL and local fixtures do not establish host durability, production migration fencing, or complete recovery acceptance.

## Sources

[Product chapters 2–4, 12, and 18](../../product.md); [architecture ownership map](../../architecture.md#system-and-authority); [command/event architecture](../../architecture.md#commands-receipts-and-events); [implemented contracts](../../../contracts/http.go).
