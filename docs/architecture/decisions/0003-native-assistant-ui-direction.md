# ADR 0003: Native assistant UI direction

Status: accepted architecture baseline; implementation and acceptance evidence are tracked in [the acceptance evidence register](../../acceptance/evidence.md), while delivery status remains in [the master plan](../../master-plan.md).

## Context

Zoe coordinates programming and other authorized work through natural interaction. Users need to understand actual progress, results, and decisions without learning runtime infrastructure or reviewing source code. The approved native assistant reference establishes the visual direction; the detailed behavior and interface requirements are maintained in [Product §3, Client Requirements](../../product.md#3-client-requirements).

## Decision

Adopt the native assistant direction because it keeps user-facing work centered on conversation, durable progress, results, and decisions while avoiding embedded developer tooling and runtime infrastructure. Detailed screen behavior, voice, task, accessibility, and presentation constraints remain in [Product §3, Client Requirements](../../product.md#3-client-requirements); this ADR records the rationale and architectural consequences only.

## Consequences

Users assess work through evidence-backed outcomes and decisions rather than embedded developer tools. The native direction requires platform-specific checks and grants no new execution authority; implementation and acceptance evidence belongs in the [acceptance evidence register](../../acceptance/evidence.md), with detailed presentation requirements in [Product §3](../../product.md#3-client-requirements).

## Sources

[Product client requirements and native presentation targets](../../product.md#3-client-requirements); [approved native reference](../../design/native-assistant-reference.png); [client architecture](../../architecture.md#identity-clients-and-notices).
