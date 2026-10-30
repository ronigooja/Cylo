# ADR 0008: Ambient macOS screen client

Status: accepted macOS architecture baseline; implementation and acceptance remain open. The previous conversation-centered direction and its ADR 0003 were removed; iPhone and iPad have no approved interface design until a later task. The [master plan](../../master-plan.md) owns delivery status and the [evidence register](../../acceptance/evidence.md) owns evidence boundaries.

## Context

The macOS client should reduce required operation rather than reproduce a chat app. A generic server-authored display must support changing work without embedding business concepts or granting the client outcome authority.

## Decision

Make macOS an ambient screen with no sidebar, tabs, or persistent composer. Continuous voice is on by default and ends after the configurable silence timeout (default 60 seconds when no voice interaction is in progress); server context supports later continuation. Text is a transient fallback. Render a versioned generic screen document supplied by the server/AI, with client-owned visuals and text degradation for unknown types. Interactive nodes submit opaque intents and arguments; the server decides outcomes. Expose only bounded screen, settings, voice, text-bar, and silence-timeout tools. Settings metadata, AI changes, notices, and undo records follow [the draft contract](../../design/screen-document.md).

## Consequences

The client can remain small, accessible, and adaptable while the server controls authoritative content and business policy. The renderer and component library become versioned compatibility surfaces. High-impact actions still require server-validated scoped confirmation; voice and UI never silently authorize work. The draft contract must be mirrored into `/contracts` before server implementation.

## Sources

[Product §3](../../product.md#3-client-requirements); [ambient screen contract draft](../../design/screen-document.md); [client architecture](../../architecture.md#ambient-macos-screen-and-client-tools-boundary).
