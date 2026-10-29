# ADR 0003: Native assistant UI direction

Status: accepted architecture baseline; implementation and acceptance status remain in [the master plan](../../master-plan.md). Recorded during the 2026-10-27 documentation consolidation from the approved 2026-10-25 direction; this record does not establish native visual or interaction acceptance.

## Context

Zoe coordinates programming and other authorized work through natural interaction. Users need to understand actual progress, results, and decisions without learning runtime infrastructure or reviewing source code. The approved native assistant reference establishes the visual direction; earlier HTML examples are coverage artifacts, not native screenshots or acceptance evidence.

## Decision

When designing any native client screen or flow, communicate function through naming, layout, and interaction. Omit redundant purpose descriptions and operating tutorials by default; provide necessary help on demand and disclose material consequences, risks, and limits at the action.

Use compact, readable native layouts centered on conversation, durable task progress, results, and decisions. Reduce oversized headings, decoration, repeated metadata, and excessive whitespace while preserving keyboard access, Dynamic Type, and platform targets. Keep projects/recent work in compact navigation and secondary task activity collapsible on wide windows. Collapse secondary surfaces before constraining conversation in narrow windows. Avoid statistics dashboards and compulsory workflow panels.

For programming work, present goals, current stage, outcome/evidence and validation summaries, and required decisions. Do not provide source-code browsers, editors, diff viewers, terminals, or test consoles, including optional or hidden variants. The same task/conversation surfaces must support non-programming work.

Do not place a microphone button in the composer. Keep voice configuration and the continuous-voice switch in Settings; continuous voice is disabled until explicit Settings opt-in. After connection and identity initialization, the client automatically attempts to establish and maintain voice subject to OS permission, audio policy, lifecycle limits, network, account, and server availability. Show truthful lifecycle/unavailable states and a Settings/system-settings recovery path, while keeping text chat usable. Omit persistent voice explanations/status labels, large waveforms, timers, voice cards, and separate control strips. Continuous transport does not submit or authorize business work.

Authorized automation proceeds within its granted scope without repeated confirmations. Ordinary assistant replies and task results are server-authored content, not Accept/Reject workflows. Preserve command receipts, draft safety, explicit transcript/submission boundaries, and actual permission boundaries. High-impact or irreversible actions require scoped explicit conversational confirmation; this authorizes execution rather than accepting a result. Keep infrastructure faults, queue capacity, and reconciliation instructions in operator diagnostics/audit; show actual task state, actionable submission errors, and pending confirmation in conversation/task views. The server owns interaction state and authoritative `assistant_message` history; clients do not synthesize task entries.

## Consequences

Users assess work through evidence-backed outcomes and decisions rather than embedded developer tools. Density cannot justify unreadable content or smaller interaction targets. Accessible voice state and truthful offline/error/unknown-submission behavior require native checks. The visual reference and HTML coverage cannot prove compiled layout, device interaction, VoiceOver, Dynamic Type, or end-to-end behavior. The complete presentation and acceptance matrix lives in product requirements; this decision grants no new execution authority.

## Sources

[Product client requirements and native presentation targets](../../product.md#3-client-requirements); [approved native reference](../../design/native-assistant-reference.png); [client architecture](../../architecture.md#identity-clients-and-notices).
