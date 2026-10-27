# Cylo product requirements

This is the current product requirements home. Requirements express required behavior, not implementation or acceptance claims. [The master plan](master-plan.md) owns delivery status and acceptance boundaries; [architecture](architecture.md) owns component responsibilities and links to recorded decisions.

Provenance: the [archived complete PRD](archive/research/product-requirements.md) supplies the 22-chapter structure, scenarios, and quality dimensions; [requirements report 01](archive/research/reports/01-requirements.md) supplies stable identities and historical acceptance analysis. Historical platform, UI, recovery, and authentication proposals apply only where retained by current decisions. IDs remain stable when a requirement is excluded or optional.

## 1. Product Positioning

Cylo is a private, self-hosted system that keeps authorized work, projects, memory, and services alive beyond a conversation or client session. The full product scope comprises all 22 chapters; sequencing delivery does not remove an in-scope requirement.

The ten principles apply when designing or changing Cylo:

1. Use AI for judgment and ordinary programs for deterministic, continuous work.
2. Use Super for complex coordination when needed.
3. Let Workers execute specialist tasks independently.
4. Create, deploy, and manage durable capabilities only under authorization.
5. Preserve tasks, memory, and execution state beyond conversations.
6. Continue the same work across devices.
7. Preserve usable work products and supported native-tool access outside Cylo.
8. Keep models, agents, voice, and integrations replaceable.
9. Prioritize accuracy, reliability, and efficient model use.
10. Reserve ownership and access boundaries for future team and organization use.

| Stable ID | Requirement |
|---|---|
| `CY-01-01` | Long-running cross-device system with durable memory, autonomous execution, coordination, and extensible capabilities. |
| `CY-01-02` | Apply the ten principles: deterministic work outside models, independent workers, replaceable modules, efficiency, reliability, and future ownership boundaries. |
| `CY-01-03` | Use the host OS and existing specialist tools; Cylo does not recreate an OS, IDE, or terminal desktop. |

## 2. Overall System Composition

The six functional responsibilities are client interaction; core request/project/task/state/permission/event management; Super coordination; specialist Workers; ordinary background services; and durable data/memory/retrieval. Their technical ownership is defined in [architecture](architecture.md).

| Stable ID | Requirement |
|---|---|
| `CY-02-01` | Keep six system responsibilities explicit and run deterministic state and event handling without model calls. |

## 3. Client Requirements

macOS, iPhone, and iPad share one backend. The native assistant centers conversation, task progress, results, and decisions. Use clear names, layout, and interaction; omit redundant purpose text and operating tutorials by default. Provide help on demand and disclose material consequences, risks, and limits at the action.

When presenting programming work, show goals, progress, outcome summaries, and required decisions. Do not add code browsers, editors, diff viewers, terminals, or test consoles, including hidden variants. Voice uses a prominent compact composer microphone, accessible labels, icons, color, and subtle motion. Routine state does not need persistent explanations, banners, large waveforms, or a separate control strip.

Conversation, Tasks, and Projects are primary entrances; Memory, Capabilities, and Settings are secondary. Every task entrance opens the same workspace. Preserve drafts until durable acceptance; unknown submissions retain the original receipt identity for reconciliation. End voice and Stop task are distinct actions. Voice acceptance includes a 30-minute mixed Mandarin–English session on each of a real iPhone and iPad while locked, plus interruption, reconnect, audio-route changes, and durable-task continuity; this is a target, not a completed result. APNs only prompts a durable-inbox refresh; a push is not the notice authority.

| Stable ID | Requirement |
|---|---|
| `CY-03-01` | macOS, iPhone, and iPad clients share one authoritative backend and consistent project/task/memory state. |
| `CY-03-02` | Provide a simple voice/text interface for tasks, progress, results, projects, services, approvals, notices, and optional technical detail. |
| `CY-03-03` | Support contextual real-time voice, interruption, cross-device continuation, visible results, and task lifetimes independent from voice sessions. |
| `CY-03-04` | Proactively notify users about completion, failure, mail, approvals, scheduled outcomes, and unrecoverable conditions with source links. |

## 4. Server Requirements

The current server and execution host target is Linux. Native Apple clients do not host the control server. Background execution must survive client closure; host sleep, shutdown, and network loss must be reported truthfully. Migration and restore must preserve linked identities and prevent two active copies of the authority.

| Stable ID | Requirement |
|---|---|
| `CY-04-01` | Run the backend independently on supported host OSes; report sleep, shutdown, and network loss accurately and recover safely. |
| `CY-04-02` | Keep agent execution, services, data, memory, permissions, events, status, and health on the server. |
| `CY-04-03` | Migrate linked projects, history, agents, tasks, memory, permissions, services, state, and execution files between environments. |

## 5. Agent and native-tool integration requirements

Codex is one development runtime, not the product authority or the only execution engine. AO owns runtime creation and lifecycle through the Cylo gateway. Basic execution, evidence, artifacts, supported recovery, conflict protection, and real stop remain required. Official Codex desktop-app integration is excluded. Seamless manual takeover and two-way native CLI handoff are optional, must use supported native facilities, and cannot be simulated by exporting a summary. Cylo has no Codex-specific recovery subsystem.

| Stable ID | Requirement |
|---|---|
| `CY-05-01` | Create and manage agent work with a real workspace, instructions, state/results, task links, supported continuation, and stop. |
| `CY-05-02` | Official Codex desktop-app integration is excluded; retain the stable ID for traceability. |
| `CY-05-03` | Optional: hand Cylo-started work to the supported native CLI session. |
| `CY-05-04` | Optional: discover and continue authorized work created in the native CLI. |
| `CY-05-05` | Preserve project, workspace, code, history, tools, goals, outcomes, open items, and context; prevent uncoordinated concurrent writes. |
| `CY-05-06` | Keep project outputs usable without Cylo and optionally synchronize external native-tool changes. |

## 6. Agent System Requirements

Separate the coordination role, runtime implementation, and model/provider route. Deterministic events use code; semantic judgment can invoke a Worker directly; Super handles complex dependencies or escalation. The shared ceiling is four active runs, including Super, Workers, and memory curator. Adapter registration alone does not establish support or acceptance.

| Stable ID | Requirement |
|---|---|
| `CY-06-01` | Super interprets complex goals, plans dependencies, chooses workers, adjusts execution, and escalates when needed. |
| `CY-06-02` | Workers execute specialist work with scoped context/tools and return independent results, records, and errors. |
| `CY-06-03` | Route ordinary events to deterministic programs, semantic judgment to workers, and complex cases to Super as needed. |
| `CY-06-04` | Support parallel agents, dependencies, result passing, unified status, and controlled failure, cancel, and recovery. |

## 7. Autonomous Capability Expansion

A generated program is not permission to deploy it. Authorized capability work must produce a validated release and constrained deployment manifest before a runner applies it. Installed capabilities retain their own lifecycle, health, configuration, permissions, immutable release artifact, and persistent-data policy. Pluggable domain examples describe extensibility, not a claim that every connector exists.

| Stable ID | Requirement |
|---|---|
| `CY-07-01` | Identify durable capability needs and create or configure the program only for authorized deployment. |
| `CY-07-02` | Manage capability install, configuration, authorization, run, pause, resume, update, stop, removal, health, and errors. |
| `CY-07-03` | Keep long-running ordinary programs alive independently; call agents only where judgment is needed. |
| `CY-07-04` | Allow pluggable capabilities across mail, calendar, files, development, web, apps, system, schedules, and monitoring. |

## 8. Task Management Requirements

Task state represents observed reality rather than a conversation or UI animation. Preserve goal, source, identity, timestamps, result, error, recovery information, dependencies, and project/workspace associations. State semantics cover queued, active, waiting, awaiting user, paused, complete, failed, and canceled work. A stop request or agent answer alone does not prove termination or goal completion.

| Stable ID | Requirement |
|---|---|
| `CY-08-01` | Represent every execution subject with a unified task identity, goal, source, status, times, result, errors, recovery, and records. |
| `CY-08-02` | Define real semantics for queued, active, waiting, awaiting user, paused, complete, failed, and canceled states. |
| `CY-08-03` | Allow long tasks to outlive sessions and expose current evidence-backed progress later. |
| `CY-08-04` | Persist outcomes, external status, pending work, and recovery data; avoid repeating irreversible effects after uncertainty. |
| `CY-08-05` | Link project tasks, dependencies, parallel and child work, shared resources, and ambiguous references. |

## 9. Long-Running Background Services

A finite task and a durable service have different lifetimes. Each service trigger creates an execution record. Ordinary schedules, listeners, and rule evaluation continue independently of Super and clients; intelligent work enters the same authorized admission path as other runtime work. Notices distinguish routine information, completion, failure, and decisions and link back to source records.

| Stable ID | Requirement |
|---|---|
| `CY-09-01` | Separate finite task lifecycles from durable service lifecycles and give each service trigger a run record. |
| `CY-09-02` | Store service identity, purpose, scope, environment, trigger, permission, configuration, status, health, errors, and recovery details. |
| `CY-09-03` | Trigger programs or agents from mail, files, schedules, APIs, task outcomes, failures, and user events. |
| `CY-09-04` | Classify and deduplicate routine, completion, failure, and decision notices; prevent noisy repetition. |

## 10. Long-Term Memory Requirements

Memory has global, project, task, and raw-history layers, each respecting its access scope. Confirmed user decisions, execution evidence, and model inference remain distinguishable. Record sources, versions, dates, validity, correction history, and tombstones. An automatic summary cannot overwrite a confirmed decision. Curator judgment is on demand, not required on every turn; missing or conflicting history must remain visible as uncertainty.

| Stable ID | Requirement |
|---|---|
| `CY-10-01` | Retain source-linked memory across conversations, tasks, projects, devices, and restarts. |
| `CY-10-02` | Separate global, project, task, and raw-history memory while preserving project access boundaries. |
| `CY-10-03` | Optional event-driven Memory Agent may identify facts, links, duplicates, staleness, conflicts, and update suggestions. |
| `CY-10-04` | Distinguish confirmed facts, execution evidence, and AI inference; track decision timing and validity. |
| `CY-10-05` | Keep task, permission, configuration, and confirmed-decision authority outside unrestricted memory writes. |
| `CY-10-06` | Retrieve only relevant authorized history, cite sources, save context, and surface missing/conflicting evidence as uncertainty. |

## 11. Context Management

Current execution context and long-term memory are separate. Assemble the context relevant to each task, agent/runtime, project, and permission scope. Balance completeness, relevance, token cost, and accuracy without loading all history or crossing project boundaries. Super's coordination role does not imply unconditional access to all data.

| Stable ID | Requirement |
|---|---|
| `CY-11-01` | Assemble current context separately from long-term memory according to task, agent, and permissions. |

## 12. Unified State and Cross-Device Continuity

Clients show the same durable project/task identities and authoritative versions. Mark stale or offline views and reconcile after reconnect. A follow-up on another device attaches to the original work when unambiguous; ask for disambiguation when records do not identify one task. Cross-device continuation preserves durable context; it does not promise live-audio transfer.

| Stable ID | Requirement |
|---|---|
| `CY-12-01` | Use one versioned state authority across devices and expose stale cache/conflicts clearly. |
| `CY-12-02` | Continue one linked project/task across Mac and iPhone and return to the same work context. |
| `CY-12-03` | Keep task existence and status independent of any conversation window. |

## 13. Modular AI and Services

Model, agent, voice, and external-service implementations remain replaceable through explicit contracts. Dependency failure should isolate affected work and preserve unrelated state and functionality. The existing remote CLIProxyAPI deployment is reused unchanged; it is an external dependency, not a Cylo deployment target.

| Stable ID | Requirement |
|---|---|
| `CY-13-01` | Replace models without rewriting task, memory, and service records. |
| `CY-13-02` | Integrate heterogeneous agents through shared capability, execution, result, and permission contracts. |
| `CY-13-03` | Replace speech recognition, synthesis, or live voice without changing core task and project data. |
| `CY-13-04` | Isolate external service integration failures from unrelated modules. |

## 14. Permission and Security Requirements

Authorization applies to the actual action and resource: reading differs from sending, deleting, or deploying. Super authority does not implicitly transfer to a Worker or service. Users can inspect and revoke grants, stop work, and review audited actions. Preauthorization names resources, actions, connectors, deployment scope, expiry, and limits; expanding it requires the applicable confirmation.

The current fresh-deployment identity is the fixed owner/workspace using a shared instance key. Runner and memory-worker credentials remain separate. Local client clearing removes local connection data; it does not revoke the shared key or stop server tasks. See the [authentication decision](architecture/decisions/0004-instance-key-authentication.md).

| Stable ID | Requirement |
|---|---|
| `CY-14-01` | Apply least privilege per agent and service; Super authority does not implicitly transfer to workers. |
| `CY-14-02` | Authorize read, write, send, delete, generate, and deploy actions separately, with risk-based confirmation. |
| `CY-14-03` | Let users inspect, change, revoke grants, stop tasks/services, audit actions, and confirm high-risk work. |
| `CY-14-04` | Isolate project, worker, service data and execution; protect sensitive context and audit important actions. |

## 15. Reliability and Exception Handling

Detect process failure, API/network loss, timeout, denial, resource shortage, and conflict. Retry only when safe. Query and reconcile unknown external effects where possible; otherwise retain uncertainty and escalate. Distinguish receipt, scheduling, execution, agent output, actual operation success, and achieved user goal. Preserve the record while no client is connected.

| Stable ID | Requirement |
|---|---|
| `CY-15-01` | Detect service exit, agent failure, unavailable APIs, network loss, timeout, denial, resource shortage, and conflicts. |
| `CY-15-02` | Retry safely; reconcile external effects when possible and require resolution for unknown irreversible outcomes. |
| `CY-15-03` | Distinguish request receipt, scheduling, execution, agent output, successful operation, and user-goal completion. |
| `CY-15-04` | Monitor service health, recover within policy, notify users, and retain context when recovery fails. |

## 16. Data Management Requirements

Persist important records beyond model context and process lifetime. Acceptance follows durable commit. Data control includes permission-aware inspection, export, and deletion of authoritative and derived copies, client caches, exports, replicas, service volumes, and backups under a stated retention policy. Preserve sources, versions, effective times, configuration, artifacts, and required volumes for migration/recovery. Protect secrets through deployment references rather than values in source or evidence.

| Stable ID | Requirement |
|---|---|
| `CY-16-01` | Persist important records and report acceptance only after durable commit. |
| `CY-16-02` | Migrate projects, history, tasks, memory, and services without binding records to one model provider. |
| `CY-16-03` | Back up and restore important database, files, and configuration with rehearsed evidence. |
| `CY-16-04` | Retain decision/task/memory source, version, and effective-time history. |
| `CY-16-05` | Protect content, files, conversations, secrets, external grants, and configuration. |
| `CY-16-06` | Support permission-aware inspection, export, and deletion across derived stores and defined backup policy. |

## 17. Resource and Cost Management

Ordinary programs handle timers, file checks, explicit rules, conversion, state changes, and health. Idle scheduling and event processing make no model calls. Each admitted model action needs task/run authority, a route, and a budget; record usage and prevent further dispatch when limits are exhausted. Unknown consumption stays reserved pending reconciliation. Numeric limits represented in a DTO do not establish actual provider/tool/artifact enforcement.

| Stable ID | Requirement |
|---|---|
| `CY-17-01` | Use programs for time, file, rule, state-transition, and health checks instead of model polling. |
| `CY-17-02` | Build minimal relevant context without sacrificing traceability or correctness. |
| `CY-17-03` | Record and bound model usage; stop further dispatch when budgets or loop limits are reached. |
| `CY-17-04` | Keep routine monitoring, scheduling, and event processing efficient without high-frequency model calls. |

## 18. Openness and Scope for Expansion

The initial release is private, single-owner, single-instance, and multi-project. Preserve user/workspace and permission-principal boundaries for future evolution. This does not add a team collaboration interface to the current release.

| Stable ID | Requirement |
|---|---|
| `CY-18-01` | Retain extensible ownership and access boundaries for multiple users, projects, agents, devices, and services. |

## 19. Complete Usage Scenarios

These five scenarios combine requirements across components; they are acceptance journeys, not development phases. Required evidence and current gaps belong in [the master plan](master-plan.md). Each scenario remains unaccepted until its complete chain is demonstrated.

### Scenario A: Software Development

**CY-19-A.** Scenario A: voice/text software task, native agent execution, client disconnect, cross-device progress, continuation and safe stop.

A user says through Cylo on a Mac:

“Develop a new login system for me.”

Cylo understands the request and creates a project task.

Super plans the work when needed and assigns specific development work to a Codex Worker.

Codex performs the development.

The task system continuously records the actual state.

Closing the Cylo client should not interrupt background execution.

The user picks up an iPhone and asks about development progress.

Cylo answers from the actual task records.

The user then asks to add tests.

The system continues the existing work instead of creating a completely unrelated project.

The first release needs to verify persistent task recovery, real stopping, and artifact viewing. The following manual-continuation steps are optional enhancements: the user returns to the Mac, enters the same background execution environment, and opens the official Codex CLI.

If the official CLI supports it, the user finds the original development session and continues work.

Cylo can then recognize the updated work state.

Throughout the process, the project, task, session, and code workspace must remain correctly associated.

### Scenario B: Creating a Long-Term Automation Capability

**CY-19-B.** Scenario B: authorized long-running mail capability with deterministic, worker, and Super routing plus notices.

A user says:

“Monitor important email for me from now on.”

Cylo understands this as a long-term service request.

Super can coordinate a plan as needed.

A Worker implements or configures the email-monitoring capability.

The background service starts after the user authorizes it.

Thereafter:

An ordinary program continuously checks email.

Unimportant email is handled according to explicit rules.

Email that requires intelligent judgment is sent to a Worker.

Events involving complex decisions are escalated to Super.

Important results are proactively reported to the user.

**After this capability has been deployed, Super does not need to repeat the entire email-checking process each time.**

### Scenario C: Long-Term Project Memory

**CY-19-C.** Scenario C: recover dated project decisions and reasons across devices without inventing missing evidence.

A user discusses a project with Cylo.

At first, they decide to use a particular technical approach.

After development, they change that decision.

Several weeks later, the user asks:

“Why did we move away from that approach?”

Cylo should find the relevant discussion and formal decision using long-term memory and historical records.

Its answer should include not only the approach currently in use, but also the change process, timing, and rationale.

If a rationale was not recorded, Cylo should say that it cannot confirm it rather than make one up.

### Scenario D: Parallel Work by Multiple Workers

**CY-19-D.** Scenario D: parallel specialist work, conflict coordination, and truthful aggregate outcomes.

A user says:

“Check this project's security and organize its API documentation at the same time.”

Cylo can create the corresponding tasks.

It assigns them to different Workers.

The two Workers do not have to wait for Super to direct each individual step in sequence.

The system preserves their execution status and results.

Super coordinates if there is a resource conflict, a major dependency, or a complex decision.

The user receives one consolidated result.

### Scenario E: Failure and Recovery

**CY-19-E.** Scenario E: unattended network/API/process failure detection, safe recovery, unknown-effect handling, and records.

A long-running background service fails because of a network interruption.

Cylo should detect the exception.

Once the service recovers, it should continue or retry according to the safety requirements of the specific task.

If automatic recovery would not be safe, it should notify the user to take action.

Even if the client was closed during the failure, the related event and failure record should be retained.

## 20. Overall Quality Requirements

All fifteen dimensions below apply to final system acceptance under CY-20-01. Numeric thresholds absent from the requirements remain open targets; a design or static check cannot establish measured quality.

| Stable ID | Requirement |
|---|---|
| `CY-20-01` | Meet and measure all fifteen system-quality dimensions; unspecified thresholds remain open targets. |

| Dimension | Requirement |
|---|---|
| Accuracy | Do not guess execution results or treat unconfirmed inferences as facts |
| Continuity | Projects, tasks, and long-running services remain after a conversation ends |
| Persistence | Important data and memory are not lost when a process ends |
| Reliability | Detect execution exceptions and recover under safe conditions |
| Extensibility | Add models, Agents, services, and system capabilities |
| Independence | Do not bind core capabilities tightly to one model or service |
| Interoperability | Support two-way continuation with native tools such as official Codex as far as possible |
| Cross-device consistency | Multiple clients access the same authoritative task and memory state |
| Cross-platform support | macOS, iPhone, and iPad clients access a unified backend; server and execution hosts use Linux |
| Controllability | Users can understand, authorize, pause, and stop related work |
| Security | Run with appropriate permissions and protect data and external services |
| Observability | Runtime state, exceptions, results, and their sources are traceable |
| Cost efficiency | Reduce unnecessary model calls and repeated context |
| Ease of use | Make natural interaction primary; do not require users to understand internal technology |
| Long-term evolution | Integrate new capabilities without repeatedly rebuilding the system from scratch |

Interoperability applies within supported runtime interfaces. Optional human native-session handoff is recorded separately from required execution/recovery acceptance.

## 21. Key Boundaries of Cylo

Cylo manages persistent projects, tasks, services, and memory through conversation. Super coordinates complex work; Workers and ordinary programs retain independent roles. Cylo extends execution through authorized capabilities on existing operating systems and specialist tools. The interface remains an assistant, and the architecture retains future ownership boundaries.

| Stable ID | Requirement |
|---|---|
| `CY-21-01` | Keep Cylo outside the boundaries of chat-only, Super-only, orchestration-only, editor, OS, memory-only, and personal-only products. |

## 22. Final Complete Product Definition

The complete product combines natural interaction; complex planning and specialist execution; supported native interoperability; independent ordinary services; authorized capability expansion; source-linked memory; unified state; scoped security; replaceable modules; and durable evolution. The user sets a goal, Super coordinates when needed, Workers execute specialist work, and ordinary programs perform deterministic continuous work. All results enter the unified task/state system and important knowledge enters controlled memory.

| Stable ID | Requirement |
|---|---|
| `CY-22-01` | Deliver the complete product behavior across interaction, decisions, execution, interoperability, services, memory, state, security, modularity, and evolution. |
