# ADR 0005: Separate voice turns, durable tasks, and lease termination

Status: accepted architecture baseline; implementation and acceptance status remain in [the master plan](../../master-plan.md). Recorded during the 2026-10-27 documentation consolidation from existing decisions; this record does not introduce a new delivery claim.

## Context

A real-time voice session can be interrupted, retransmitted, or moved to a different upstream account while accepted work continues. Upstream speech does not prove a business command was accepted or a task completed. An expired lease does not prove microphone, media, account, or runtime resources have terminated.

## Decision

For real-time voice, use Space3044 `chatgpt-web-voice` as the selected ChatGPT web gateway/account pool and native WebRTC media with platform audio/lifecycle adapters. Starting microphone capture or an upstream session requires explicit user action and granted platform permission. Retain no raw audio by default.

Bridge user turns through stable identity, revisions, and deduplication to the same durable authorized command path as typed input. Partial captions and upstream spoken output do not authorize work. If the gateway lacks a stable completion signal, require transcript confirmation as the fallback. Formal completion notices come only from the task ledger.

Bind device, account, and media resources to holder/generation leases and termination evidence. A new account creates a new media session while keeping task associations. End the old client media session before account reuse. Quarantine uncertain sessions/accounts; expiry alone does not release them. Keep mute, stop playback, End voice, and Stop task distinct.

## Consequences

Task life survives voice loss and account changes without duplicate dispatch. Conversation may continue naturally during work. Safe account reuse depends on trusted account-scoped identity and termination evidence, which the selected gateway response has not established. The adapter remains fail-closed. Stable local fixtures do not establish real pool behavior, background/locked-device audio, interruptions, latency, or end-to-end voice acceptance. Cross-device continuation promises durable context, not live-audio migration.

## Sources

[Product chapters 3, 8, and 12](../../product.md); [voice architecture](../../architecture.md#voice-and-business-work); [selected upstream](https://github.com/Space3044/chatgpt-web-voice); historical evidence: [voice/Super boundary](https://github.com/ronigooja/Zoe/blob/95ee55a5bbd8dccfb7f14af9f9703bb83727da6c/docs/archive/research/reports/12-voice-super-boundary.md).
