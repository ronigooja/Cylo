> Historical design proposal dated 2026-10-21; it was not a delivered runtime. For the current implementation plan, see the [master plan](../../../master-plan.md).

# Cylo Memory Requirements and Minimal Implementation Design

Date: 2026-10-21. This is a design proposal, not a delivered runtime. It is based on current product requirements and the adopted AO/Space/CLIProxyAPI boundaries and existing architecture; no credentials were read, no models were called, and no third-party memory engine was validated. This document adds only the memory-specific design and does not change the overall architecture.

## 1. Recommendation: Own the Memory Control Layer and Reuse Open-Source Components

**For the first release, use a Go memory API to own permissions, scope, versions, and persistence; use rules for deterministic writes; reuse Mem0's general-purpose extraction/retrieval as needed; and enable a Memory Agent for complex cross-record organization when useful. A dedicated organization responsibility does not require a separate Agent, nor a resident process. Start with SQLite + FTS; connect Mem0 in the first release if automatic extraction or semantic needs enter that release.**

Cylo's main challenge is “who confirmed what, which project it applies to, how it changed later, which Agents may read it, and how to prevent old summaries from retrieving it after deletion.” These rules must agree with the existing task, authorization, and evidence systems. Even when an open-source memory engine is used, Cylo remains responsible for this layer. With no product runtime in the first release, there is no need to start by maintaining a Python memory service, graph database, and vector database. There is also no need to build a database, full-text search, embedding algorithm, or general Agent framework from scratch.

| Path | Use in Cylo | Current recommendation |
|---|---|---|
| Go control module + SQLite/FTS | Unify scope, sources, decision versions, candidate review, retrieval, and deletion; reuse existing control-plane contracts | First-release main path |
| A memory extraction/retrieval component such as [Mem0](https://github.com/mem0ai/mem0) | Could later provide replaceable candidate extraction or semantic recall | Pluggable candidate; do not grant authoritative write access by default |
| A temporal relationship graph such as [Zep/Graphiti](https://github.com/getzep/graphiti) | Evaluate if real gaps emerge in cross-entity, cross-period relationship retrieval | There is currently no evidence that a graph database is required |
| A stateful Agent platform such as [Letta](https://github.com/letta-ai/letta) | Evaluate when its Agent runtime and memory-management system are needed | AO already orchestrates multiple runtimes; adding another platform in the first release risks duplicating session and runtime lifecycles |

The table is an architecture-fit judgment, not a hands-on comparison of third-party functionality, licenses, operational cost, or current versions. If adopted in the future, validate the pinned version's interfaces, data export/deletion, permission filtering, and license. A project name containing “memory” does not prove it meets the PRD. Engine selection is not a prerequisite for implementing the current design.

## 2. Requirements Basis and Scope

Authoritative requirements: the [Complete Product Requirements](../product-requirements.md); traceable acceptance: [01 Requirements Mapping](01-requirements.md). Section numbers below refer to the PRD.

| Actual requirement | Source | Design implication and boundary |
|---|---|---|
| Persistent memory across conversations, tasks, projects, and devices; recover after client close or service restart | §§10.1, 4.2, 12.1 | Data resides server-side; cross-project retrieval continuity does not mean unconditional cross-project disclosure |
| Four layers: global, project, task, and raw history | §10.2 | Organize with scopes and references; four databases or four Agents are unnecessary |
| Organize facts, relationships, duplicates, stale/conflicting items, and update suggestions; may be triggered by background events | §10.3 | Implement through a combination of rules, human review, and on-demand Agents; a separate Agent is an optional implementation, not a resident process or per-turn model call |
| Distinguish confirmed facts, execution results, and AI inferences; support old/new decisions, time, and rationale | §10.4, Scenario C | Store source type and effective version independently; high model confidence is not a substitute for confirmation |
| Formal tasks, permissions, configuration, and confirmed decisions are reliable authoritative records and cannot be overwritten by summaries | §10.5 | The memory index references the ledger instead of reconstructing state from natural-language summaries |
| Relevant retrieval, low token use, project isolation, and expressing uncertainty when information is missing/conflicting | §10.6, §11, §§17.1–2 | Retrieval and context assembly are program capabilities; simple queries need not invoke Super |
| Replaceable Agents, models, and voice without losing tasks and memory | §§13.1–3, 16.2 | Memory keys/formats are independent of provider, AO session, and voice account |
| View, export, and delete; inspect source/time/version | §§16.1–6 | Raw material, revisions, indexes, caches, and backups need a consistent deletion path |
| Restore projects/history/permissions/memory across macOS, Linux, or servers | §§4.3, 16.2–3 | Migrate data and objects; native processes are not portable memory |
| First release may be single-user, but structure cannot lock into one person/project/Agent; isolate future users | §18 | Include workspace/principal/scope from the start; do not require a full team-collaboration UI in the first release |
| Ending voice does not affect background tasks; different Agents follow unified state | §§3.3, 6, 12 | Voice and Workers read the same control plane and do not maintain competing facts |

“Modify memory” is designed here as revision, correction, and candidate handling based on the source/version and accuracy requirements. §16.6 explicitly lists viewing, exporting, and deleting; it does not separately require a complete memory editor. A simple management page/API is recommended, but a complex knowledge-base editing product is not an added delivery gate.

### Responsibilities of the Memory Agent, Super, API, and Engine

The original text of PRD §10.3 says, “Cylo can use a separate Memory Agent to organize memory,” and explicitly says it “does not need to participate in every user conversation.” A dedicated organization responsibility, an independent Agent role, and a resident process are three separate things. In the first release, the API and rules handle explicit memory/decision registration, authorization versions, rule-based deduplication, and ordinary retrieval. Mem0 can perform general fact extraction and semantic retrieval, without requiring another Agent runtime. When cross-record historical organization, semantic conflict/staleness judgment, linking, or complex project summaries provide real value, AO can dispatch an organization role and background task on demand. A separate Agent is not a first-release prerequisite. Its runtime/model uses AO's configurable selection and is not locked to Codex; deterministic indexing, deduplication, and fast queries need not invoke a model.

```text
Cylo / Super → Memory Agent (on demand) → Cylo Memory API → SQLite / FTS
                                                  └→ Mem0 extraction/retrieval engine (connect as needed)
Cylo / Super / authorized Worker ───────────────→ Cylo Memory API (fast read/write)
```

- **Super** understands requests, coordinates tasks, and uses relevant context. It dispatches a Memory Agent when complex historical organization, relationship analysis, or retrieval assistance is needed; it does not personally perform all background organization.
- **Memory Agent (enabled on demand)** identifies information worth retaining, extracts facts, organizes project history and summaries, links items, merges duplicate candidates, finds stale/conflicting information, and proposes updates; it can help locate evidence for complex retrieval. If sources and versions cannot resolve a conflict, preserve both sides rather than adjudicating a user's decision. Event-triggered or periodic background jobs may invoke this role; periodic execution is an implementation option, not a frequency specified by the PRD.
- **Cylo Memory API / Go service** checks identity, scope, source, and version; commits authorized writes; and maintains persistence, deletion, and context budgets. **Mem0** performs reusable extraction or retrieval steps and does not receive independent authoritative write access. Mem0 can cover general extraction/retrieval; decide whether complex organization needs an Agent based on actual value.

Avoid duplicate extraction: do not extract the same source with an Agent and then let Mem0 extract it again by default. Delegate general extraction directly to Mem0; use a controlled adapter to skip re-extraction for already organized candidates. In a pinned Mem0 version, `infer=False` can skip the fact-extraction LLM but still calls embeddings; it is not a zero-model path. Deterministic events go through rules/API.

Authorized entry points can call the API directly for fast retrieval, explicit user memories, and formal decision registration; they need not wait for a Memory Agent. Register a decision the user has clearly made from the original message and do not ask them to confirm it again. The Memory Agent and Super can only read within the task's authorization scope; sharing relevant project memory does not mean exposing every Worker's private history or native session.

Distinguish “offline” local retrieval from offline AI. When a local deployment's host is available, viewing/retrieving/revising SQLite data does not require a model. If the client is disconnected from the backend, it may display authorized cached data marked stale; after reconnecting, use the server version. The PRD does not require mobile offline multi-user writes, running a cloud model offline, or automatic bidirectional merging across deployments. The host-offline requirement in §4 means accurately displaying state and recovering, not continuing execution after power is lost.

## 3. Ownership of the Three Data Categories

| Data | Storage and authority | Use by the memory module |
|---|---|---|
| Business facts: task/run status, permissions, configuration, formal decisions, execution receipts | Cylo business ledger, authorized command + transaction; account credentials in their respective secure stores | Query live or reference a version; Memory Agent must not rewrite them |
| Native Agent execution context: message history, native session ID, running process, worktree | AO + corresponding runtime/host, using native resume/fork/fresh capabilities | Save associations and necessary recovery summaries; do not claim lossless history interchange across runtimes |
| Long-term semantic memory: preferences, project context, discussion summaries, experience, unresolved questions, and fact indexes | Cylo memory/context module, with traceable sources and scopes | Retrieve on demand across sessions/models, keeping inferences marked |

For example, check the task/acceptance ledger to answer “Is the task complete?”; check decision versions and discussion evidence to answer “Why did we change databases?”; use AO's native adapter to answer “Continue my last Codex session.” The three can reference one another, but successful resume does not imply cross-project long-term memory, and a long-term summary does not prove that a task is currently running.

Account pools affect request routing only: the existing CLIProxyAPI handles model routing/accounts, and Space handles voice accounts. Account identity is not a basis for Cylo user identity or memory partitioning. Never put OAuth tokens, API keys, or account-pool secrets into memory extraction content.

## 4. Minimal Data Model

Reuse the claim/revision/evidence/context contract in [04 Architecture §8](04-architecture.md#8-long-term-memory-evidence-facts-and-versions-failed) to avoid building a second authoritative system. These are logical objects; they do not need to be separate services.

| Object | Minimum fields/rules |
|---|---|
| `evidence` raw source | `id, workspace_id, scope, kind, source_ref, source_version/hash, recorded_at, producer, retention/deletion_state`; reference a message, file version, execution event, or receipt and locate its paragraph/event |
| `memory_claim` + revision | `id, workspace_id, scope_kind/id, subject/key, content, authority_kind, status, valid_from/to, recorded_at, revision, supersedes, evidence_refs`; personal preferences also have an owner principal |
| `memory_proposal` | Candidate content, source, target scope, extractor version, relationship to existing entries, reason for pending, baseline revision; approval operation uses an idempotency key |
| Derived index/summary | `claim_revision/evidence_hash, index_version, stale`; rebuildable and not the sole canonical content |
| `context_manifest` | task/run/recipient, query scope, actual entries and versions, injection time, budget, and trimming/missing-information notes; protected by the same permissions |

`authority_kind` follows the existing design: `user_confirmed / execution_observed / imported_authoritative / model_inferred`; `confidence` only helps rank candidates. `status` may be proposed/active/conflicted/superseded/retracted/deleted; freeze exact table enums during implementation. Scope may be personal, workspace, project, or task. “Global” means the authorized scope of a person/organization, not all users in a deployment.

The decision ledger continues to own the body/effective revision of a formal decision. A memory claim references it rather than copying it into a second editable “current decision.” SQL constraints and server-side authorization both check workspace and ownership; an Agent cannot gain permission just by supplying a `project_id`.

## 5. Writing, Organization, Conflicts, and Confirmation

1. **Reliably store the source first.** The owning module first commits confirmed user turns, task events, execution results, formal decisions, and user-imported documents. Record identity, time, object, and version. Voice partials and an assistant's spoken promise are not directly stored as facts. Preserve important source text according to retention policy; retaining all audio/video forever is not required.
2. **Trigger an organization job through a transactional event.** Reuse outbox/jobs; deduplicate by source ID + version + extractor version. Task acceptance, decision changes, session end, bulk history, and manual organization can all trigger it; organization can also run periodically. First apply rule-based deduplication, exact references, and FTS updates. Delegate general extraction directly to a replaceable extractor or Mem0; dispatch a Memory Agent only for complex cross-record judgment. Do not run extraction or embeddings on every sentence. Organization failure must not block a task or voice interaction.
3. **Generate a candidate.** State its content, source location, proposed scope, fact type, applicable time, and relationship (new/duplicate/supplement/replacement/conflict). Inherit the source's scope by default. Elevating a project-specific item to an organization-wide rule requires explicit evidence/authorization; a model's judgment that something is “general” cannot broaden scope.
4. **Validate in the control plane.** Verify the source exists and is authorized, the reference resolves, scope is not exceeded, and the baseline version has not changed. Commands inside a source are material, not authorization to override system rules. A model-generated statement that “the user approved this” is not an approval credential.
5. **Commit by category.** Deterministic indexes, labeled summaries, and inferences can be saved automatically without asking the user to confirm every item. `execution_observed` comes from a verified receipt and states only what the evidence supports. A user saying “Remember to use Chinese from now on” can create a confirmed preference through an ordinary authorized command. “Adopt AO,” “keep Space for voice,” or “reuse the existing CLIProxyAPI” should be registered directly as formal decisions with the speaker and original-message source; do not show another confirmation prompt. Ambiguous preferences or a model suggestion that replaces an existing decision remain candidates; ask the user only when judgment is needed. Formal decision changes, permissions, and configuration keep their own command paths and required authorization; the Memory Agent must not promote facts directly.
6. **Handle concurrency and contradictions.** Use expected-revision/transaction checks. Recheck a proposal from a second Worker if it was based on an old revision. Merge identical content with its references; mark conflicting evidence as conflicted and preserve both sides. Do not use “last writer wins” or highest similarity as the deciding rule.

SQLite → PostgreSQL example: T1 formally confirms d1; T2 confirms d2 through a business command and records the reason; in one transaction, set d1's effective end, d2's effective time, and `supersedes`. When asked today, return d2; when asked about T1, return d1; when asked why it changed, return T2's reason and the original discussion. If there is no evidence for the reason, answer “A change is recorded, but I couldn't find the reason”; do not infer the user's motivation from technical common sense. Keep recording time separate from actual effective time to support backdated history.

## 6. Retrieval and On-Demand Context

Retrieval path: **identity/task scope → current authoritative snapshot → effective decisions → relevant memories and evidence → revalidation → assembly for the recipient**.

- Answer simple status questions directly from the ledger. For “that project from earlier,” first disambiguate using the current interaction binding and recent authorized objects; ask if several candidates remain.
- Start with FTS, project/task links, and time/source filters. SQLite FTS5's default unicode61 tokenization is inadequate for Chinese; trigram search also cannot guarantee recall of one- or two-character Chinese terms. In the first release use a validated Chinese keyword/tokenization strategy, exact-term aliases, and structured project/task/time filters. State the coverage demonstrated by Chinese samples; do not claim Chinese semantic retrieval is built in. Later combine semantic recall with FTS, always search within the authorized set, and recheck current ACL, deletion state, claim/source version, and effective time before returning results.
- Historical decisions remain searchable, but do not treat superseded items as current constraints when asked what applies “now”; historical queries specify time/version. Keep inferences separately labeled and carry missing sources/conflicts into the answer.
- Give a Worker only its task goal/acceptance, necessary current state, effective constraints, relevant project material, and a small amount of history. An email Worker does not automatically receive code-project data. Super receives authorized cross-task summaries for coordination and retrieves more as needed.
- Prioritize authorization boundaries, current instructions and goals, current status/recovery point, and effective decisions before allocating a historical-evidence budget. For long logs, provide references and local excerpts. Make the budget configurable; when it is exceeded, compress background first. If a critical constraint still cannot fit, work in stages or report the gap; do not silently truncate it.
- Save a context manifest so the system can explain “which memories were used for this response.” If a native session already has history, provide deltas and current-version notices instead of repeating the full history. Compression does not change underlying facts.

Semantic vectors are only a retrieval channel; they do not confirm facts or decide permissions or effective dates. Changes to embedding provider/model/version rebuild derived indexes without changing memory IDs. Evaluate embeddings or graph relationships only if retrieval quality is insufficient; do not add infrastructure based only on a feature list.

## 7. AO Multi-Agent Sharing and Model Switching

Cylo is the sole memory-control entry point; AO orchestrates sessions. For a new run, the control plane assembles context from the task/run and the AO adapter passes it to the selected runtime through a validated launch-input/prompt interface. Later reads use the same restricted API; MCP is only an adapter for that API. Do not assume AO already has a Cylo memory plugin.

Suggested minimum capabilities: `search_memory`, `read_evidence`, `propose_memory`, and `get_task_context`. An Agent can request only within the principal/scope bound to its run; a scope in the request body cannot override the range granted server-side. The user-management interface separately provides viewing, revision, candidate handling, export, and deletion. Do not give each Agent its own memory store that can independently modify authoritative facts.

When a runtime does not support tools/MCP, the control plane can provide a versioned, source-referenced `CYLO_CONTEXT.md` projection as a fallback. It is an authorized snapshot scoped to that run's directory; prevent it from being committed to the repository or leaking into other workspaces. An Agent editing that file is not writing memory; it must submit a proposal. On revocation/deletion, stop providing the old projection and clean up controlled copies. Deleting a file cannot erase text already read by a running process; when needed, stop the old run and rebuild the session with clean context. Handle copies already sent to a native provider according to its actual capabilities.

When switching models or runtimes, keep task/run, memory, and evidence IDs and rebuild current context. If AO can natively resume, reuse it; otherwise explicitly open/fork a new session with a recovery summary and workspace state. Do not promise lossless conversion of tokens/native message history between models. Before recovery, verify business state, in-flight operations, and workspace; do not repeat side effects based on a summary. See [16 AO Boundaries](16-ao-agent-provider-boundaries.md).

## 8. Voice Context and Account Isolation

Space handles media, voice accounts, and signaling; Cylo owns stable user turns, tasks, and memory. Voice queries use the same retrieval service but typically inject only brief facts relevant to the current question, current task snapshot, necessary preferences, and user-visible results, with source references the interface can display. Switching voice accounts or starting a new session adds only a necessary summary, not the full history. Hanging up neither deletes long-term memory nor terminates background tasks.

Filter injected content according to Cylo user, project, and current voice-session permissions. The voice account pool is not a cross-user knowledge base; do not send keys, all project documents, all memories, or other users' conversations upstream. Content previously sent may remain in upstream copies; local deletion does not mean upstream history was deleted.

The current Space private protocol has no verified system/tool bridge; its existing relay uses the user role. The assembly design above must not be described as “reliably injecting system memory already.” Validate adapter capabilities and readback accuracy according to [12 Voice Boundaries](12-voice-super-boundary.md). Until then, Cylo's on-screen result and sources can stand independently; a voice assistant's own report that it remembered/completed something is not acceptance evidence.

## 9. Viewing, Correction, Forgetting, Deletion, and Migration

The minimal management view shows scope, type, content, confirmed/inferred status, effective time, source, and revision, with search by project/keyword. A user correction creates a new revision and, when necessary, synchronizes through the formal-decision command. Editing only a summary does not change the ledger. Stale items leave current retrieval but remain visible to historical queries. Forgetting an incorrect inference can retract it; that differs from deleting its source entirely.

Deletion path: authorized request → mark it non-retrievable/retract references in one transaction and enqueue cleanup → remove derived summaries/FTS/vector/cache/file projections → delete source text/objects according to policy. Revalidation during retrieval immediately blocks stale indexes; effect does not wait for asynchronous cleanup. Deletion scope includes every derived item that depends on a source; do not delete only the source row and leave a readable summary. Retain only necessary content-free deletion metadata for audit. Show authorized users backup-retention/expiration rules; apply known deletion manifests on restore and do not silently restore deleted content. If an old backup lacks a newer deletion manifest, explicitly state the deletion-state gap for that recovery point and reconcile before enabling queries.

Track copies sent to native Agents/upstreams separately, along with the ability to control/delete them. Do not promise to withdraw information already read into a running model's context. Mark the corresponding old session as not reusable and assemble clean context again. Recovery after deletion must not reinject old summaries.

Export versioned JSON/JSONL plus a source-object manifest containing stable IDs, scope, version, time, evidence associations, and deletion records; indexes need not be exported and can be rebuilt on restore. Reuse the control plane's consistent SQLite backup mechanism; do not copy a running main file and omit the WAL. For Mac → Linux, migrate logical data/objects and remap paths; handle credentials separately and recover native sessions according to actual runtime support. Without an embedding service, users can still view, search with FTS, and export; model availability must not determine data accessibility.

## 10. Minimal Implementation and Validation

1. **Start with sources and boundaries.** On the existing business-entity contract, add evidence/decision references, scope, version, source, and deletion state; connect commands/events and provide basic view/export/delete APIs. With no product runtime yet, first define the Go module/API and data contract; do not claim integration has been completed.
2. **Then provide usable retrieval.** Add FTS + scope/time filtering, source reads, context manifest, task context, and AO input projection; define the Chinese retrieval strategy. Manually confirmed material is enough to test core value; connecting a memory model is not a prerequisite.
3. **Then connect background organization and enable Memory Agent if useful.** Let rules/humans/general extractors handle candidates first; dispatch the role through AO on demand for complex organization. Background events/periodic tasks trigger candidate organization, conflict, and revision suggestions through the API using a replaceable extractor or Mem0. Once voice and multiple Agents are connected, route all access through the same entry point. Record summary usage, job latency, tokens, candidate acceptance, and corrections in existing observability.
4. **Upgrade only to address real gaps.** Semantic search, graph relationships, separate engines, or PostgreSQL are triggered by actual retrieval/concurrency needs. New engines still follow source/permission/deletion contracts; moving to an open-source engine does not complete quality acceptance.

Minimal validation uses fixed local fixtures and a replaceable fake extractor; it does not require real model calls:

| Validation | Expected result |
|---|---|
| SQLite → PostgreSQL decision change; insert an incorrect AI summary and a record with no rationale | Current/history/time/rationale are correct; reject the incorrect summary and do not invent a rationale |
| Project A/B, two principals, personal/organization preferences | Unauthorized queries, tool calls, and source reads are all rejected; scope is not enforced only by frontend filtering |
| Two Workers submit conflicting proposals/duplicate events | Idempotent deduplication; an old revision cannot overwrite a new decision; conflict is visible |
| After deletion/revocation, vector index and projection are stale, then a backup is restored | Old entry is immediately no longer injected; completed cleanup is verifiable; restore does not revive a known deletion |
| End session, restart service, switch device/model/runtime/voice account | Same memory IDs/sources remain readable; new context contains only authorized relevant summaries; fresh start is not described as resume |
| Recover a long-running task after its native session is lost | Verify status and side effects from the ledger; summary supplies background only and does not repeat execution |
| Chinese reference resolution, history across several weeks, insufficient token budget, missing source | Disambiguate/retrieve; preserve important constraints; clearly state gaps and uncertainty |

This delivery is a specialist design only. The validations above are implementation acceptance recommendations; none is claimed as a completed product test or real upstream experiment.
