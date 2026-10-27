# ADR 0003: Native assistant UI direction

Status: accepted architecture baseline; implementation and acceptance status remain in [the master plan](../../master-plan.md). Recorded during the 2026-10-27 documentation consolidation from the approved 2026-10-25 direction; this record does not establish native visual or interaction acceptance.

## Context

Cylo coordinates programming and other authorized work through natural interaction. Users need to understand actual progress, results, and decisions without learning runtime infrastructure or reviewing source code. The approved native assistant reference establishes the visual direction; earlier HTML examples are coverage artifacts, not native screenshots or acceptance evidence.

## Decision

When designing any native client screen or flow, communicate function through naming, layout, and interaction. Omit redundant purpose descriptions and operating tutorials by default; provide necessary help on demand and disclose material consequences, risks, and limits at the action.

Use compact, readable native layouts centered on conversation, durable task progress, results, and decisions. Reduce oversized headings, decoration, repeated metadata, and excessive whitespace while preserving keyboard access, Dynamic Type, and platform targets. Keep projects/recent work in compact navigation and secondary task activity collapsible on wide windows. Collapse secondary surfaces before constraining conversation in narrow windows. Avoid statistics dashboards and compulsory workflow panels.

For programming work, present goals, current stage, outcome/evidence and validation summaries, and required decisions. Do not provide source-code browsers, editors, diff viewers, terminals, or test consoles, including optional or hidden variants. The same task/conversation surfaces must support non-programming work.

Keep a prominent compact microphone in the composer. Routine voice state uses icons, color, and subtle optional motion with accessible names/state, keyboard support, and on-demand help; color alone cannot carry state. Omit persistent voice explanations/status labels, large waveforms, timers, voice cards, and separate control strips. Show a meaningful permission, interruption, or connection failure with a recovery action at the affected control.

Authorized automation proceeds within its granted scope without repeated confirmations. Preserve command receipts, draft safety, explicit voice-submission confirmation, and actual permission boundaries. Keep infrastructure faults, queue capacity, and reconciliation instructions in operator diagnostics/audit; show actual task state, actionable submission errors, and pending confirmation in conversation/task views.

## Consequences

Users assess work through evidence-backed outcomes and decisions rather than embedded developer tools. Density cannot justify unreadable content or smaller interaction targets. Accessible voice state and truthful offline/error/unknown-submission behavior require native checks. The visual reference and HTML coverage cannot prove compiled layout, device interaction, VoiceOver, Dynamic Type, or end-to-end behavior. The complete presentation and acceptance matrix lives in product requirements; this decision grants no new execution authority.

## Sources

[Product client requirements and native presentation targets](../../product.md#3-client-requirements); [approved native reference](../../design/native-assistant-reference.png); [client architecture](../../architecture.md#identity-clients-and-notices).
