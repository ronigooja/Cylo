# ADR 0001: Linux server and native Apple clients

Status: accepted architecture baseline; implementation and acceptance status remain in [the master plan](../../master-plan.md). Recorded during the 2026-10-27 documentation consolidation from existing decisions; this record does not introduce a new delivery claim.

## Context

Cylo needs durable background execution independent of conversation and client availability, plus consistent access from macOS, iPhone, and iPad. Archived proposals considered web/PWA and macOS background hosts, but those are not the selected baseline. The six product responsibilities must remain explicit without forcing every responsibility into a separate process.

## Decision

For the current private self-hosted baseline, use a Linux Go control server, a separate Linux runner, and native SwiftUI macOS/iPhone/iPad clients. Use SQLite WAL with `synchronous=FULL` as the initial authoritative store. The control plane owns business state; native clients render records and submit commands.

The first release is single-instance and multi-project with reserved user/workspace access boundaries. Exclude PWA, CAO implementation, and macOS-hosted server work from this baseline. Preserve future evolution without adding a team collaboration UI now.

## Consequences

Server work can continue when clients close, and client replacement does not move authority. Separate runner supervision avoids making a client or control process the execution lifetime. Linux operational recovery and Apple client delivery require distinct evidence. This choice requires native platform builds/device checks and does not establish that deployment, UI, or end-to-end behavior is accepted.

## Sources

[Product chapters 2–4, 12, and 18](../../product.md); [architecture ownership map](../../architecture.md#system-and-authority).
