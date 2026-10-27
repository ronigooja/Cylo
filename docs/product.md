# Zoe product requirements

This is the current product requirements home. Requirements express required behavior, not implementation or acceptance claims. [The master plan](master-plan.md) owns delivery status and acceptance boundaries; [architecture](architecture.md) owns responsibilities and [decision records](architecture.md#decision-records).

The complete scope retains all 22 chapters, all 75 stable IDs, scenarios A–E, and all fifteen quality dimensions. This document carries forward the valid details of the [original complete PRD](https://github.com/ronigooja/Zoe/blob/95ee55a5bbd8dccfb7f14af9f9703bb83727da6c/docs/archive/research/product-requirements.md) and its [stable-ID mapping](https://github.com/ronigooja/Zoe/blob/95ee55a5bbd8dccfb7f14af9f9703bb83727da6c/docs/archive/research/reports/01-requirements.md). Those immutable sources are provenance, not competing current plans. Current Linux/native-client, AO, optional CLI-handoff, instance-key, and assistant-UI decisions take precedence over superseded proposals.

## 1. Product Positioning

Zoe is a private, self-hosted system that keeps authorized work, projects, memory, and services alive beyond a conversation or client session. The full product scope comprises all 22 chapters; sequencing delivery does not remove an in-scope requirement.

### Name and role

Zoe takes its name from Zoë Washburne, Captain Malcolm Reynolds's second-in-command in *Firefly*. The product expresses the role of a dependable first mate: the user sets direction, and Zoe helps carry the work forward with continuity, judgment, and reliable execution.

When describing Zoe's role in product copy or interaction design, use this first-mate relationship: understand the user's intent, act within granted authority, coordinate ongoing work, report actual outcomes, and bring decisions back to the user when their judgment or authorization is needed.

<a id="cy-01-01"></a>

**Disposition: Required.**

### 1.1 What Zoe Is

**CY-01-01.** Long-running cross-device system with durable memory, autonomous execution, coordination, and extensible capabilities.

**Zoe is a cross-device, long-running AI system with persistent memory, autonomous execution, task coordination, and mechanisms for expanding its capabilities.**

It interacts with users through natural language and speech, understands requests, performs tasks, manages projects, coordinates different AI agents, and, when necessary, creates and deploys ordinary programs so the system gains new capabilities that can run over time.

Zoe is not a chat tool that works only when the user issues a command.

It has a background runtime independent of its clients and can continuously maintain tasks, memory, services, and runtime state.

Authorized background work can continue even when the user does not keep Zoe open.

<a id="cy-01-02"></a>

**Disposition: Required.**

### 1.2 Core Design Principles

**CY-01-02.** Apply the ten principles: deterministic work outside models, independent workers, replaceable modules, efficiency, reliability, and future ownership boundaries.

Zoe is not intended to make AI responsible for everything. Instead, different kinds of execution units should take on the work best suited to them.

The system follows these principles:

1. **AI provides intelligence; ordinary programs handle deterministic, continuous execution.**
2. **Super handles complex coordination and does not intervene in every simple operation.**
3. **Workers can complete specialist tasks independently; not everything must pass through Super.**
4. **The system can create, deploy, and manage new long-running capabilities.**
5. **Tasks, memories, and execution state must be stored long term and must not depend on a single conversation.**
6. **Users can continue the same work from different devices.**
7. **Zoe should not lock users into its own client.**
8. **AI models, agents, voice services, and other components should be independently replaceable.**
9. **Accuracy, reliability, and efficient token use take priority over unnecessary AI calls.**
10. **The product is not limited to personal use; its design leaves room to expand to teams and other scenarios.**

<a id="cy-01-03"></a>

**Disposition: Required.**

### 1.3 Product Boundaries

**CY-01-03.** Use the host OS and existing specialist tools; Zoe does not recreate an OS, IDE, or terminal desktop.

Zoe does not need to replace macOS, Linux, or Windows.

It runs on an existing operating system and uses the operating system's computing, file, network, and process-management capabilities.

Zoe also does not need to recreate a full code editor, terminal, or operating-system desktop.

For existing specialist software such as Codex, Zoe should make direct use of native capabilities whenever possible and remain interoperable with the official tools.

## 2. Overall System Composition

The six functional responsibilities are client interaction; core request/project/task/state/permission/event management; Super coordination; specialist Workers; ordinary background services; and durable data/memory/retrieval. Their technical ownership is defined in [architecture](architecture.md).

<a id="cy-02-01"></a>

**Disposition: Required.**

**CY-02-01.** Keep six system responsibilities explicit and run deterministic state and event handling without model calls.

Zoe separates its clients from its server.

It has six functional parts:

| System part | Core responsibility |
|---|---|
| Client | Voice and text interaction, task progress, result display, and notifications |
| Core management system | User requests, tasks, projects, state, permissions, and event management |
| Super Agent | Understanding complex requirements, planning, decision-making, and coordination across tasks |
| Worker Agents | Specialist task execution and local intelligent decisions |
| Background services | Scheduled tasks, event listeners, long-running services, and automation programs |
| Data and memory system | Persistent data, history, project knowledge, long-term memory, and retrieval |

Each part must have clearly defined responsibilities.

The system should not require every event, state update, or background operation to pass through a large language model.

## 3. Client Requirements

macOS, iPhone, and iPad share one backend. The native assistant centers conversation, task progress, results, and decisions. Use clear names, layout, and interaction; omit redundant purpose text and operating tutorials by default. Provide help on demand and disclose material consequences, risks, and limits at the action.

When presenting programming work, show goals, progress, outcome summaries, and required decisions. Do not add code browsers, editors, diff viewers, terminals, or test consoles, including hidden variants. Voice uses a prominent compact composer microphone, accessible labels, icons, color, and subtle motion. Routine state does not need persistent explanations, banners, large waveforms, or a separate control strip.

Conversation, Tasks, and Projects are primary entrances; Memory, Capabilities, and Settings are secondary. Every task entrance opens the same workspace. Preserve drafts until durable acceptance; unknown submissions retain the original receipt identity for reconciliation. End voice and Stop task are distinct actions. Voice acceptance includes a 30-minute mixed Mandarin–English session on each of a real iPhone and iPad while locked, plus interruption, reconnect, audio-route changes, and durable-task continuity; this is a target, not a completed result. APNs only prompts a durable-inbox refresh; a push is not the notice authority.

<a id="cy-03-01"></a>

**Disposition: Required.**

### 3.1 Cross-Device Access

**CY-03-01.** macOS, iPhone, and iPad clients share one authoritative backend and consistent project/task/memory state.

Zoe should support access to the same backend system from different devices.

Target clients include:

- macOS
- iPhone
- iPad

The architecture should leave room to add other devices and platforms in the future.

Different devices accessing the same server should see consistent projects, tasks, memories, and runtime state.

Clients should not maintain separate, conflicting task states.

<a id="cy-03-02"></a>

**Disposition: Required.**

### 3.2 Minimal Interaction Interface

**CY-03-02.** Provide a simple voice/text interface for tasks, progress, results, projects, services, approvals, notices, and optional technical detail.

The Zoe client is primarily for communicating with the system, not for managing large amounts of technical detail.

Core features include:

- Natural voice conversation
- Text input and output
- Viewing current tasks
- Viewing task progress
- Displaying results
- Viewing project status
- Viewing background-service status
- Proactive notifications
- User authorization and confirmation
- Alerts for errors, exceptions, and issues requiring human attention

The interface should be as simple as possible.

Ordinary users should be able to use Zoe without understanding how Super, Workers, databases, or background processes work.

Advanced status and operation details should still be available when needed.

<a id="cy-03-03"></a>

**Disposition: Required.**

### 3.3 Natural Voice Interaction

**CY-03-03.** Support contextual real-time voice, interruption, cross-device continuation, visible results, and task lifetimes independent from voice sessions.

Natural speech is one of Zoe's primary interaction methods.

Requirements:

**Natural conversation**

Users should be able to make requests as they would in an ordinary conversation, without memorizing fixed commands.

**Continuous context**

The system should understand how the current conversation relates to earlier content.

**Real-time interaction**

Support fluent voice input and output while minimizing perceived latency.

**Allow interruption**

Users can interrupt the system while it is speaking, correct it, or change their request.

**Separate voice from tasks**

Ending a voice session does not end a background task.

**Continue across devices**

A user can begin a conversation on one device and continue related work on another.

**Provide text or visual results when appropriate**

Complex reports, task lists, programming outcomes, and validation summaries must be readable as text or visual results rather than expressed entirely through speech. Source code, raw execution logs, terminals, diff viewers, and test consoles are not native assistant surfaces; work products remain available through authorized external tools and artifact access.

<a id="cy-03-04"></a>

**Disposition: Required.**

### 3.4 Proactive Interaction

**CY-03-04.** Proactively notify users about completion, failure, mail, approvals, scheduled outcomes, and unrecoverable conditions with source links.

Zoe should not merely wait for users to ask questions.

It should be able to proactively notify users when important events occur in the background.

Examples include:

- A development task has completed
- A service has encountered an error
- An important email needs attention
- A task requires user authorization
- Scheduled work has produced an important result
- A background operation failed and could not recover automatically

A notification should identify its event source and link to the relevant project or task.

### Native assistant direction

The user approved the [native assistant reference](design/native-assistant-reference.png). Zoe is an automation assistant whose frequent workloads include programming. Its interface centers on natural conversation, durable task progress, results, and decisions. Present programming execution and validation through concise, understandable summaries. The client does not provide source-code browsing or editing, diff viewers, terminals, or test consoles, including optional or hidden variants.

- **Simple appearance, useful density.** Use native typography, restrained semantic colors, compact rows, and clear grouping. Keep more useful conversation and task information visible by reducing oversized headings, excessive whitespace, decorative cards, and repeated metadata. Preserve readability, keyboard access, Dynamic Type, and platform touch targets; density does not mean uniformly smaller text or controls.
- **Conversation leads.** Keep projects and recent conversations/tasks in a compact sidebar. Show task receipts and concise progress/results in context. A compact, collapsible task rail may surface ongoing work, pending decisions, and recent completion on wide windows; collapse secondary surfaces before constraining the conversation in narrow windows. Avoid statistics dashboards and compulsory workflow panels.
- **Voice is prominent and compact.** Integrate a readily reachable microphone button into the composer with stronger emphasis than secondary input tools. Routine capture/session state uses distinct icon states, color, and optional subtle motion. Do not add persistent labels such as "Microphone on", "Microphone off", "Listening", or "Voice in progress", nor large waveforms, timers, voice cards, or separate persistent control strips. Give icon controls accessible names and state values, keyboard access, and help on demand; color alone must not carry state. Surface a meaningful permission failure, interruption, or connection problem with a recovery action at the affected control.
- **Automation reduces required interaction.** After the user expresses and authorizes the intent, work proceeds within its granted scope. Keep ordinary progress concise; emphasize decisions that require the user, actionable failures, and recorded results. Do not add repeated confirmations to work that is already authorized. Existing command, voice-submission, and permission boundaries still apply; this visual direction does not grant new authority or establish unattended voice submission.
- **Focus on outcomes.** Summarize programming work using its goal, current stage, outcome, validation summary, and decisions that require the user. Users should be able to understand what was accomplished and what remains unresolved through these summaries, without reviewing source code. The same conversation and task surfaces must support research and other authorized work.

### Native presentation and acceptance targets

AO startup failures, unresolved runtime diagnostics, execution-slot accounting, and reconciliation instructions belong to operator interfaces and audit records. Conversation and task views show the actual task status without infrastructure queue reasons or capacity banners. Preserve actionable submission errors and pending-confirmation feedback; this presentation rule does not authorize client retries or change task authority.

The Apple redesign uses system typography, SF Symbols, semantic colors, system appearance, and English/Simplified Chinese localization. Spacing follows 4/8/12/16/24/32 pt steps; conversation text is limited to 760 pt. Blue emphasizes primary actions. Task outcomes and actionable exceptional states have concise text labels; routine voice state follows the compact icon treatment above. Normal connected state has no banner. Offline, stale, and failed operations remain visible with an action at the affected surface.

macOS uses a resizable sidebar (240 pt default) and workspace. iPad uses the same split structure in regular width and collapses in narrow windows; iPhone has Conversation, Tasks, and Projects tabs, hidden in the task workspace. Notifications are a toolbar entrance with an unread count. Account menus expose Settings, Memory, and Capabilities. Home contains a composer and recent work, without statistics or tutorial cards. Coverage includes home, empty, loading, send failure, and task states with language, appearance, and text-size variants; design examples are not application acceptance evidence.

Every task entrance leads to the same workspace. The workspace shows persisted messages, actual task status, approvals, and recorded results/evidence; runtime identifiers and cursors belong in Technical details. No simulated assistant answers, historical progress, or inactive download actions fill missing server capabilities. Tasks support search, status filters, and update ordering. Projects offer a contextual new conversation. Settings group current connection/key management, notifications, data, and advanced options, following [the instance-key decision](architecture/decisions/0004-instance-key-authentication.md).

Submission has accepted, failed, and unknown outcomes. Drafts clear only after durable acceptance. Unknown submissions reconcile the original journaled receipt rather than issuing a new command; the interface must retain the draft and show confirmation pending. Task drafts and selection survive navigation within the connected context and are cleared when the server or identity changes. Voice has one compact composer entrance; its transcript-draft review opens when needed rather than permanently occupying the conversation. Submission requires confirmation, and End voice remains distinct from Stop task. macOS keyboard targets are Command-Return to send, Command-N for a new conversation, and Command-F for task search.

Authoritative task-history paging and reconnect contracts are defined in [the HTTP contract](../contracts/http.go) and [task-entry handlers](../internal/control/task_entries.go); clients must not fabricate messages or maintain a competing conversation authority. Dates and counts use the system region; server-authored content remains in its original language.

Native application acceptance must cover Mac 840×620 and desktop sizes, iPad portrait/landscape/narrow windows, and small/large iPhones; both languages and appearances; long titles and Dynamic Type; keyboard avoidance; VoiceOver, keyboard focus, reduced motion, and 44×44 pt mobile targets. Core flows must verify one-tap continuation, contextual approvals/results, linked notifications, retained failed drafts, unknown receipts, duplicate events, reconnect history, terminal tasks, rejected/rotated instance keys, connection clearing, and identity changes. Record Go tests, Swift state/contract tests, Xcode app builds, simulator UI runs, real-device runs, and real-service checks separately. Linux source review and design references cannot establish Apple compilation, screenshots, interaction, or end-to-end acceptance.

## 4. Server Requirements

The current server and execution host target is Linux. Native Apple clients do not host the control server. Background execution must survive client closure; host sleep, shutdown, and network loss must be reported truthfully. Migration and restore must preserve linked identities and prevent two active copies of the authority.

<a id="cy-04-01"></a>

**Disposition: Required.**

### 4.1 Independent Background Operation

**CY-04-01.** Run the backend independently on supported host OSes; report sleep, shutdown, and network loss accurately and recover safely.

The Zoe server should be able to run as an independent background system.

For the first release, the server and execution host use **Linux**. They can be deployed on a Linux server or another supported Linux environment to execute agent tasks and run ordinary background services.

Running the Zoe server, AO, or Worker background services on macOS is outside the current baseline. Native macOS clients remain in scope; there is no browser/PWA client.

The server should not depend on a graphical client remaining open.

Closing the Zoe client should not stop background services that are already running independently.

If the execution environment becomes unavailable because a device is powered off, asleep, or disconnected, the system must be able to identify that condition and continue processing according to task rules when conditions recover.

<a id="cy-04-02"></a>

**Disposition: Required.**

### 4.2 Separation of Client and Server

**CY-04-02.** Keep agent execution, services, data, memory, permissions, events, status, and health on the server.

The client is primarily responsible for interaction and presentation.

The server is responsible for:

- Agent management
- Task execution
- Background-service operation
- Data storage
- Long-term memory
- Permission control
- Event handling
- State management
- Service health monitoring

Users can access the same backend through different clients.

<a id="cy-04-03"></a>

**Disposition: Required.**

### 4.3 Environment Migration

**CY-04-03.** Migrate linked projects, history, agents, tasks, memory, permissions, services, state, and execution files between environments.

Zoe data and work products must remain migratable across supported Linux environments.

When moving to a different Linux backend server or supported execution environment, it should be possible to migrate through backup, export, and restore:

- Project data
- Conversation history
- Agent configuration
- Task records
- Long-term memories
- Permission configuration
- Background-service configuration
- System state
- Necessary execution files and working-directory associations

Native operating-system processes may not migrate directly, but the system should retain enough information to restart or resume the corresponding task or service in the target environment.

## 5. Agent and native-tool integration requirements

Codex is one development runtime, not the product authority or the only execution engine. AO owns runtime creation and lifecycle through the Zoe gateway. Basic execution, evidence, artifacts, supported recovery, conflict protection, and real stop remain required. Official Codex desktop-app integration is excluded. Seamless manual takeover and two-way native CLI handoff are optional, must use supported native facilities, and cannot be simulated by exporting a summary. Zoe has no Codex-specific recovery subsystem.

**First-release Codex integration must support CLI execution, viewing real status and artifacts, stopping, and persistent recovery. Seamless human takeover within the same thread, live switching of input control, and a complete bidirectional handoff entry point are optional future enhancements; they are not hard requirements for first-release selection or delivery.**

The first release prioritizes automatic Super planning, independent Workers, multiple worktrees, long-running tasks, and ordinary background services while preserving task recovery, artifacts, and cost records.

<a id="cy-05-01"></a>

**Disposition: Required.**

### 5.1 Zoe Invoking Codex

**CY-05-01.** Create and manage agent work with a real workspace, instructions, state/results, task links, supported continuation, and stop.

Zoe should be able to assign software-development tasks to Codex.

This includes:

- Creating a development task
- Specifying the project and execution environment
- Sending instructions to Codex
- Obtaining the real execution status
- Obtaining output and results
- Recording session associations
- Appending requirements to a saved session
- Resuming sessions that support recovery
- Interrupting or canceling execution where supported
- Including Codex tasks in Zoe's unified task system

Zoe should not guess whether a task is complete based only on Codex chat content.

<a id="cy-05-02"></a>

**Disposition: Excluded.**

### 5.2 Official Codex App (Out of Current Scope)

**CY-05-02.** Official Codex desktop-app integration is excluded; retain the stable ID for traceability.

Under the user's latest scope decision, support for discovering, resuming, deep-linking, or handing off sessions from the official macOS Codex desktop app across hosts is not required. This section number is retained for historical traceability.

The native macOS Zoe client and users' independent use of Codex CLI remain in scope; there is no browser/PWA client. First-release server and execution-host support is defined in §4.1; cross-environment data migration remains in scope.

<a id="cy-05-03"></a>

**Disposition: Optional.**

### 5.3 Codex CLI Continuation

**CY-05-03.** Optional: hand Zoe-started work to the supported native CLI session.

The first-release Linux execution environment must preserve basic CLI invocation and a recoverable session association. Users can access that Linux environment over SSH. A macOS-hosted control server or runtime service is outside the current baseline. The following experience of manually continuing the original session through the official CLI is an optional future enhancement:

A user can:

1. Create a Codex development task through Zoe.
2. Have Codex perform development work in the background.
3. Later enter the same background execution environment directly; the remote environment can be accessed over SSH.
4. Use the official Codex CLI's supported session-recovery mechanism.
5. Find the relevant session and continue working.

The project, code, and execution results should remain accessible. Human continuation of the same native session is subject to separate verification within the official support boundary and does not block the first-release automatic execution loop.

<a id="cy-05-04"></a>

**Disposition: Optional.**

### 5.4 Bidirectional Continuation

**CY-05-04.** Optional: discover and continue authorized work created in the native CLI.

As an optional enhancement, in addition to continuing sessions created by Zoe through the official Codex CLI, the system may support the reverse direction.

For example:

A user first develops a project in the official Codex CLI.

Later, the user opens Zoe and says:

“Continue that project from earlier and finish the tests.”

Zoe should identify the relevant project, existing session, and work state as far as possible, then continue the work when the required support and permissions are available.

**Bidirectional native continuation is optional; basic CLI invocation and persistent task recovery remain required.**

<a id="cy-05-05"></a>

**Disposition: Required.**

### 5.5 Integrity of Native Continuation

**CY-05-05.** Preserve project, workspace, code, history, tools, goals, outcomes, open items, and context; prevent uncoordinated concurrent writes.

If the optional native-continuation capability above is implemented, it should preserve as much as possible of the following:

- Project identity
- Working directory
- Code state
- Conversation history
- Record of tool operations already performed
- Task objective
- Work already completed
- Unresolved issues
- Necessary development context

The system must also distinguish between two concepts:

**Resuming a saved session** and **taking over a session that another client is currently running**.

These are different capabilities.

If a session is still running in another client, Zoe must not modify the same work state simultaneously without coordination.

It must prevent duplicate execution, session conflicts, and code overwrites.

<a id="cy-05-06"></a>

**Disposition: Required.**

### 5.6 Codex Independence

**CY-05-06.** Keep project outputs usable without Zoe and optionally synchronize external native-tool changes.

Zoe coordinates development tasks; it is not the only way to access development tools.

Even when Zoe is temporarily unavailable, users should be able to access existing projects and work products directly through the official Codex CLI as far as possible.

Likewise, Zoe should be able to recognize and synchronize changes made by users in the official Codex CLI.

**Clarification: CLI execution, viewing, stopping, and task recovery under §5.1, along with preservation of work products, remain required. Manual same-session continuation under §§5.3–5.5 and automatic synchronization of external changes under §5.6 are optional enhancements; their absence does not block first-release or full required-scope acceptance. Basic access to code and artifacts must not depend on the human-takeover feature. Session discovery and control are limited by what official interfaces support and what has been tested; recovery must never create uncoordinated concurrent writes.**

## 6. Agent System Requirements

Separate the coordination role, runtime implementation, and model/provider route. Deterministic events use code; semantic judgment can invoke a Worker directly; Super handles complex dependencies or escalation. The shared ceiling is four active runs, including Super, Workers, and memory curator. Adapter registration alone does not establish support or acceptance.

<a id="cy-06-01"></a>

**Disposition: Required.**

### 6.1 Super Agent

**CY-06-01.** Super interprets complex goals, plans dependencies, chooses workers, adjusts execution, and escalates when needed.

Super is the system's high-level intelligent coordinator.

Its core responsibilities include:

- Understanding complex requests
- Breaking complex goals into tasks
- Preparing an execution plan
- Selecting suitable Workers
- Coordinating multiple Workers
- Handling dependencies between tasks
- Adjusting the plan based on task results
- Addressing problems that require reasoning across domains
- Deciding which issues require user involvement
- Maintaining continuity across complex work

Super should not perform every low-level operation.

In particular, the system should not have to invoke Super just to update a status, check the time once, or evaluate an explicit rule.

<a id="cy-06-02"></a>

**Disposition: Required.**

### 6.2 Worker Agents

**CY-06-02.** Workers execute specialist work with scoped context/tools and return independent results, records, and errors.

A Worker is an execution unit responsible for specific intelligent work.

Different Workers can have different specialties, such as:

- Software development
- Code review
- Document analysis
- Email understanding
- Information organization
- Data analysis
- Application operation

A Worker must be able to:

- Receive a clear task
- Obtain necessary context
- Use authorized tools
- Execute a task independently
- Report its results
- Save necessary execution records
- Report problems or request coordination when exceptions occur

A Worker does not have to be created or invoked by Super.

<a id="cy-06-03"></a>

**Disposition: Required.**

### 6.3 Direct Invocation of Workers

**CY-06-03.** Route ordinary events to deterministic programs, semantic judgment to workers, and complex cases to Super as needed.

An ordinary background service can invoke a designated Worker directly according to rules.

For example:

An email-monitoring program receives an email.

It handles a simple system notification itself.

If the importance of the email needs to be assessed, it can invoke an email-analysis Worker.

If the email concerns multiple projects, a complex plan, or an important decision, it can escalate the issue to Super.

The call path should be:

**Background service → Worker → Super when needed**

rather than requiring every action to follow:

Background service → Super → Worker.

<a id="cy-06-04"></a>

**Disposition: Required.**

### 6.4 Multi-Agent Collaboration

**CY-06-04.** Support parallel agents, dependencies, result passing, unified status, and controlled failure, cancel, and recovery.

The system must support multiple Agents handling different tasks at the same time.

This includes:

- Different Workers executing in parallel
- Workers passing results to one another
- Managing dependencies between tasks
- Unified execution status
- Failure handling
- Task cancellation
- Task recovery
- Consolidating results

Agent execution must follow unified permission, task, and state rules rather than maintaining conflicting information independently.

## 7. Autonomous Capability Expansion

A generated program is not permission to deploy it. Authorized capability work must produce a validated release and constrained deployment manifest before a runner applies it. Installed capabilities retain their own lifecycle, health, configuration, permissions, immutable release artifact, and persistent-data policy. Pluggable domain examples describe extensibility, not a claim that every connector exists.

This is one of Zoe's most distinctive requirements.

<a id="cy-07-01"></a>

**Disposition: Required.**

### 7.1 Creating New Capabilities

**CY-07-01.** Identify durable capability needs and create or configure the program only for authorized deployment.

Zoe should not only use existing features; it should also be able to create new system capabilities in response to user needs.

For example, a user says:

“Check my important email every day from now on, and tell me when something important arrives.”

Zoe should understand that this is a request for a long-term capability, not a one-time email lookup.

The system can arrange for a Worker to write a program or configure existing tools and services.

After obtaining the necessary authorization, it can deploy the corresponding background service.

The service can then run independently.

<a id="cy-07-02"></a>

**Disposition: Required.**

### 7.2 Lifecycle of a New Capability

**CY-07-02.** Manage capability install, configuration, authorization, run, pause, resume, update, stop, removal, health, and errors.

Zoe must be able to manage the complete lifecycle of an added capability, including:

- Creation
- Installation
- Configuration
- Authorization
- Startup
- Pause
- Resume
- Update
- Stop
- Uninstall
- Runtime status checks
- Exception handling

Users should be able to see which capabilities are installed and whether they are currently running.

<a id="cy-07-03"></a>

**Disposition: Required.**

### 7.3 Independent Operation of New Capabilities

**CY-07-03.** Keep long-running ordinary programs alive independently; call agents only where judgment is needed.

An ordinary program that has been created should not need Super to participate continuously in order to work.

For example, an email-checking service can continuously monitor a mailbox.

Most of the time it does not need to call any large language model.

It calls the relevant Agent only when it encounters content that needs semantic understanding or complex decisions.

This means:

**AI can create a capability without necessarily being responsible for its ongoing execution.**

<a id="cy-07-04"></a>

**Disposition: Required.**

### 7.4 Scope of Capability Expansion

**CY-07-04.** Allow pluggable capabilities across mail, calendar, files, development, web, apps, system, schedules, and monitoring.

Zoe should be able to gain a range of capabilities through modules, external services, or programs it creates autonomously.

Examples include email, calendars, file management, development tools, GitHub, browser automation, third-party apps, system operations, scheduled tasks, information monitoring, and other software services that can be authorized.

Adding a capability should not require changing the entire system's core architecture.

## 8. Task Management Requirements

Task state represents observed reality rather than a conversation or UI animation. Preserve goal, source, identity, timestamps, result, error, recovery information, dependencies, and project/workspace associations. State semantics cover queued, active, waiting, awaiting user, paused, complete, failed, and canceled work. A stop request or agent answer alone does not prove termination or goal completion.

<a id="cy-08-01"></a>

**Disposition: Required.**

### 8.1 Unified Task System

**CY-08-01.** Represent every execution subject with a unified task identity, goal, source, status, times, result, errors, recovery, and records.

Zoe needs a unified system for managing all tasks.

Every task, whether executed by Super, a Worker, Codex, an ordinary program, or another service, should have a clear identity and status.

Task information must include at least:

- Task identifier
- Associated project
- Task objective
- Creation source
- Executing entity
- Current status
- Start time
- Most recent update time
- Execution result
- Error information
- Necessary recovery information
- Related execution records

<a id="cy-08-02"></a>

**Disposition: Required.**

### 8.2 Task Lifecycle

**CY-08-02.** Define real semantics for queued, active, waiting, awaiting user, paused, complete, failed, and canceled states.

The system must support the following states or equivalent semantics:

| Status | Meaning |
|---|---|
| Pending | The task has been created but has not started |
| In progress | The task is executing |
| Waiting | Waiting for a dependency, external event, or other condition |
| Waiting for user | User authorization, confirmation, or additional information is needed |
| Paused | Execution is temporarily stopped |
| Completed | Completion criteria have been met |
| Failed | Execution failed |
| Canceled | The task was canceled |

The system must update status based on actual execution.

It must not mark work as complete without verification merely because an Agent said “done.”

<a id="cy-08-03"></a>

**Disposition: Required.**

### 8.3 Long-Running Tasks

**CY-08-03.** Allow long tasks to outlive sessions and expose current evidence-backed progress later.

Zoe should support tasks that take a long time, such as software development, complex research, or multi-step automation.

Users do not need to keep a conversation open.

They can ask at any time:

“How far along is that task from earlier?”

The system should retrieve its actual current status and explain it using context when needed.

<a id="cy-08-04"></a>

**Disposition: Required.**

### 8.4 Task Recovery

**CY-08-04.** Persist outcomes, external status, pending work, and recovery data; avoid repeating irreversible effects after uncertainty.

When task execution is interrupted, the system should preserve as much as possible of:

- Completed steps
- Execution results
- External resource state
- Uncompleted steps
- Information needed for recovery

Recovery should avoid unnecessary repetition.

For steps that may have irreversible effects, the system must take special care to prevent duplicate execution.

<a id="cy-08-05"></a>

**Disposition: Required.**

### 8.5 Association Between Tasks and Projects

**CY-08-05.** Link project tasks, dependencies, parallel and child work, shared resources, and ambiguous references.

A project can have multiple tasks.

Tasks can have:

- Sequential dependencies
- Parallel relationships
- Parent-child relationships
- Shared-resource relationships

Zoe should understand what the user means by “that earlier project,” “the task from a moment ago,” or “yesterday's development work.”

If several objects are possible, it must not choose one arbitrarily.

## 9. Long-Running Background Services

A finite task and a durable service have different lifetimes. Each service trigger creates an execution record. Ordinary schedules, listeners, and rule evaluation continue independently of Super and clients; intelligent work enters the same authorized admission path as other runtime work. Notices distinguish routine information, completion, failure, and decisions and link back to source records.

<a id="cy-09-01"></a>

**Disposition: Required.**

### 9.1 A Service Is Not a Task

**CY-09-01.** Separate finite task lifecycles from durable service lifecycles and give each service trigger a run record.

Zoe must clearly distinguish between a **one-time task** and a **long-running service**.

For example:

“Check my email once” is a task.

“Monitor my email from now on” is a service.

They need different lifecycle management.

<a id="cy-09-02"></a>

**Disposition: Required.**

### 9.2 Background-Service Management

**CY-09-02.** Store service identity, purpose, scope, environment, trigger, permission, configuration, status, health, errors, and recovery details.

A service needs its own record, including:

- Service identity
- Service purpose
- Project or scope
- Runtime environment
- Trigger conditions
- Permissions
- Configuration
- Current runtime status
- Most recent execution record
- Health status
- Error record
- Stop and recovery methods

<a id="cy-09-03"></a>

**Disposition: Required.**

### 9.3 Event Triggers

**CY-09-03.** Trigger programs or agents from mail, files, schedules, APIs, task outcomes, failures, and user events.

Background services can perform actions in response to different events, such as:

- A new email arrives
- A file changes
- A timer fires
- An external API returns an event
- A task completes
- The system encounters an abnormal state
- A user issues a command

An event can trigger an ordinary program, a Worker, or Super.

It should not default to triggering Super for every event.

<a id="cy-09-04"></a>

**Disposition: Required.**

### 9.4 Proactive Notifications

**CY-09-04.** Classify and deduplicate routine, completion, failure, and decision notices; prevent noisy repetition.

When a background service produces an event that deserves the user's attention, it should be able to send a notification.

Notifications should distinguish ordinary information, task completion, runtime errors, and events requiring a user decision.

The system should avoid duplicate notifications and frequent, meaningless reminders as far as possible.

## 10. Long-Term Memory Requirements

Memory has global, project, task, and raw-history layers, each respecting its access scope. Confirmed user decisions, execution evidence, and model inference remain distinguishable. Record sources, versions, dates, validity, correction history, and tombstones. An automatic summary cannot overwrite a confirmed decision. Curator judgment is on demand, not required on every turn; missing or conflicting history must remain visible as uncertainty.

<a id="cy-10-01"></a>

**Disposition: Required.**

### 10.1 Persistent Memory

**CY-10-01.** Retain source-linked memory across conversations, tasks, projects, devices, and restarts.

Zoe needs long-term memory that works across conversations, tasks, projects, and devices.

Memory must not depend on the context window of a single model call.

Important information must remain recoverable after a client closes, a session ends, or the service restarts.

<a id="cy-10-02"></a>

**Disposition: Required.**

### 10.2 Memory Layers

**CY-10-02.** Separate global, project, task, and raw-history memory while preserving project access boundaries.

At minimum, memory includes the following categories.

#### Global Memory

Store generally applicable information that remains valid over time, such as:

- User or organization preferences
- Communication habits
- General working rules
- Long-term goals
- Confirmed ongoing constraints

#### Project Memory

Store project-specific information such as:

- Project goals
- Product requirements
- Architecture decisions
- Historical changes
- Design rationale
- Project background
- Important discussions

Memories for different projects need clear boundaries.

#### Task Memory

Store the following for a specific task:

- Objective
- Status
- Execution history
- Results
- Unresolved issues
- Recovery context

#### Raw History

Store necessary source material, including:

- Conversation records
- Operation records
- Agent output
- Important events
- Sources of decisions
- Execution logs

Raw history should be available as evidence for other memory content.

<a id="cy-10-03"></a>

**Disposition: Optional.**

### 10.3 Memory Agent

**CY-10-03.** Optional event-driven Memory Agent may identify facts, links, duplicates, staleness, conflicts, and update suggestions.

Zoe can use a separate Memory Agent to organize memory.

Its responsibilities include:

- Identifying information worth retaining long term
- Extracting key facts
- Organizing project history
- Linking related information
- Finding duplicate information
- Finding outdated information
- Identifying contradictions
- Proposing memory updates

The Memory Agent does not need to participate in every user conversation.

The system should allow memory organization to run on background events or suitable triggers.

<a id="cy-10-04"></a>

**Disposition: Required.**

### 10.4 Memory Accuracy

**CY-10-04.** Distinguish confirmed facts, execution evidence, and AI inference; track decision timing and validity.

This is a core requirement.

Zoe must distinguish between:

**Facts explicitly confirmed by the user, results actually produced by the system, and content inferred by AI.**

An inference must not be treated directly as an authoritative fact.

For example:

The user first decides to use SQLite.

Later, the user formally decides to switch to PostgreSQL.

The system should understand:

- What the earlier decision was
- What the new decision is
- When the change happened
- Which decision is currently effective
- What evidence supports it

It must not simply combine old and new information.

<a id="cy-10-05"></a>

**Disposition: Required.**

### 10.5 Separate Authoritative Data from AI Memory

**CY-10-05.** Keep task, permission, configuration, and confirmed-decision authority outside unrestricted memory writes.

Formal task status, permissions, system configuration, and confirmed decisions must be saved in reliable, persistent records.

A Memory Agent can organize this information but must not arbitrarily modify authoritative records without permission.

In particular, an automatic summary must not overwrite a confirmed user decision.

<a id="cy-10-06"></a>

**Disposition: Required.**

### 10.6 Retrieval on Demand

**CY-10-06.** Retrieve only relevant authorized history, cite sources, save context, and surface missing/conflicting evidence as uncertainty.

Zoe should not send the entire history to the model on every call.

The system needs to retrieve only information relevant to the current task.

Goals include:

- Reducing token use
- Avoiding irrelevant context
- Preventing information from different projects from being confused
- Improving traceability of historical facts
- Improving accuracy when continuing work

When retrieval is insufficient or results conflict, the system should state the uncertainty instead of inventing missing history.

## 11. Context Management

Current execution context and long-term memory are separate. Assemble the context relevant to each task, agent/runtime, project, and permission scope. Balance completeness, relevance, token cost, and accuracy without loading all history or crossing project boundaries. Super's coordination role does not imply unconditional access to all data.

<a id="cy-11-01"></a>

**Disposition: Required.**

**CY-11-01.** Assemble current context separately from long-term memory according to task, agent, and permissions.

Long-term memory and the current execution context need to be managed separately.

Zoe should be able to provide each Agent with context relevant to its task.

For example, a development Worker needs the project structure, technical requirements, and current task.

An email Worker needs the email content, relevant handling rules, and necessary context.

They do not need every past conversation.

Super can access cross-task information when coordinating complex work, but it should not load all system data unconditionally either.

Context management must balance:

- Information completeness
- Relevance
- Token cost
- Permission boundaries
- Memory accuracy

## 12. Unified State and Cross-Device Continuity

Clients show the same durable project/task identities and authoritative versions. Mark stale or offline views and reconcile after reconnect. A follow-up on another device attaches to the original work when unambiguous; ask for disambiguation when records do not identify one task. Cross-device continuation preserves durable context; it does not promise live-audio transfer.

<a id="cy-12-01"></a>

**Disposition: Required.**

### 12.1 Unified Source of Data

**CY-12-01.** Use one versioned state authority across devices and expose stale cache/conflicts clearly.

Zoe should have one authoritative source of state.

Different clients viewing the same task must not show unexplained conflicting states.

For example, if a Mac shows a task as completed, an iPhone should not incorrectly continue to show that task as running.

<a id="cy-12-02"></a>

**Disposition: Required.**

### 12.2 Continuing Work Across Devices

**CY-12-02.** Continue one linked project/task across Mac and iPhone and return to the same work context.

A typical scenario:

A user tells Zoe on a Mac:

“Develop a new user authentication module for me.”

The user then leaves the computer.

On an iPhone, the user asks:

“How is the authentication module from earlier coming along?”

Zoe should identify the original task and report its actual progress.

The user then says:

“When the code is done, check it for security issues too.”

The system should associate this request correctly with the original task or a follow-up task.

When the user returns to the Mac, they can continue interacting through Zoe and, where supported, continue development directly in the official Codex tool.

<a id="cy-12-03"></a>

**Disposition: Required.**

### 12.3 Separate State from Conversation

**CY-12-03.** Keep task existence and status independent of any conversation window.

A task's existence should not depend on a particular conversation window.

Existing tasks remain present even when the user starts a new conversation.

A conversation is only one way to access and control tasks.

## 13. Modular AI and Services

Model, agent, voice, and external-service implementations remain replaceable through explicit contracts. Dependency failure should isolate affected work and preserve unrelated state and functionality. The existing remote CLIProxyAPI deployment is reused unchanged; it is an external dependency, not a Zoe deployment target.

<a id="cy-13-01"></a>

**Disposition: Required.**

### 13.1 Independence from AI Models

**CY-13-01.** Replace models without rewriting task, memory, and service records.

Zoe must not bind its core capabilities to a single model.

The system should support different kinds of AI models and services.

Replacing a model in the future should not require rewriting the entire task-management, long-term-memory, and background-service system.

<a id="cy-13-02"></a>

**Disposition: Required.**

### 13.2 Agent Independence

**CY-13-02.** Integrate heterogeneous agents through shared capability, execution, result, and permission contracts.

Codex can serve as a development Worker.

Other Agents can handle other work.

Different Agents do not need to share the same internal implementation.

Zoe needs to know what capabilities they provide, how they execute, how they return results, and how permissions constrain them.

<a id="cy-13-03"></a>

**Disposition: Required.**

### 13.3 Independence of Voice Services

**CY-13-03.** Replace speech recognition, synthesis, or live voice without changing core task and project data.

The voice system should be independently replaceable.

Changing speech recognition, speech synthesis, or a real-time voice service should not affect:

- Long-term memory
- Task state
- Agent coordination
- Background services
- Project data

<a id="cy-13-04"></a>

**Disposition: Required.**

### 13.4 Independence of External Services

**CY-13-04.** Isolate external service integration failures from unrelated modules.

Email, calendars, GitHub, file systems, and other third-party services should be independently integrated, updated, or removed.

When an external service is unavailable, it should not cause unrelated modules to fail as a whole.

## 14. Permission and Security Requirements

Authorization applies to the actual action and resource: reading differs from sending, deleting, or deploying. Super authority does not implicitly transfer to a Worker or service. Users can inspect and revoke grants, stop work, and review audited actions. Preauthorization names resources, actions, connectors, deployment scope, expiry, and limits; expanding it requires the applicable confirmation.

The current fresh-deployment identity is the fixed owner/workspace using a shared instance key. Runner and memory-worker credentials remain separate. Local client clearing removes local connection data; it does not revoke the shared key or stop server tasks. See the [authentication decision](architecture/decisions/0004-instance-key-authentication.md).

Zoe can run programs, modify files, and operate external applications, so it must have clear permission controls.

<a id="cy-14-01"></a>

**Disposition: Required.**

### 14.1 Separate Permissions

**CY-14-01.** Apply least privilege per agent and service; Super authority does not implicitly transfer to workers.

Different Agents and background services should receive permissions relevant to their work.

A permission held by Super must not automatically give every Worker the same permission.

<a id="cy-14-02"></a>

**Disposition: Required.**

### 14.2 Operation Authorization

**CY-14-02.** Authorize read, write, send, delete, generate, and deploy actions separately, with risk-based confirmation.

Different operations have different levels of risk.

For example, reading email and sending email are different permissions.

Reading a file and deleting a file are different permissions.

Writing a program and deploying it directly are also different permissions.

Installing software, running high-risk system commands, changing sensitive configuration, and performing irreversible external operations must follow the user's authorization rules.

<a id="cy-14-03"></a>

**Disposition: Required.**

### 14.3 User Control

**CY-14-03.** Let users inspect, change, revoke grants, stop tasks/services, audit actions, and confirm high-risk work.

Users should be able to:

- View existing permissions
- Adjust the scope of permissions
- Revoke permissions
- Stop tasks
- Stop background services
- View important operation records
- Confirm high-risk operations

A single vague authorization must not give a long-running service unlimited operating permissions.

<a id="cy-14-04"></a>

**Disposition: Required.**

### 14.4 Isolation and Audit

**CY-14-04.** Isolate project, worker, service data and execution; protect sensitive context and audit important actions.

Different projects, Workers, and background services should have appropriate data and execution boundaries.

Sensitive information must not be exposed casually to unrelated Agents through shared context.

Important operations need traceable execution records.

## 15. Reliability and Exception Handling

Detect process failure, API/network loss, timeout, denial, resource shortage, and conflict. Retry only when safe. Query and reconcile unknown external effects where possible; otherwise retain uncertainty and escalate. Distinguish receipt, scheduling, execution, agent output, actual operation success, and achieved user goal. Preserve the record while no client is connected.

Zoe is a system that needs to run over long periods.

It must therefore handle more than successful tasks.

<a id="cy-15-01"></a>

**Disposition: Required.**

### 15.1 Exception Detection

**CY-15-01.** Detect service exit, agent failure, unavailable APIs, network loss, timeout, denial, resource shortage, and conflicts.

The system needs to detect:

- A background service exiting
- A Worker execution failing
- An unavailable external API
- A network interruption
- An execution timeout
- Insufficient permissions
- Insufficient resources
- Conflicting data or state

<a id="cy-15-02"></a>

**Disposition: Required.**

### 15.2 Fault Recovery

**CY-15-02.** Retry safely; reconcile external effects when possible and require resolution for unknown irreversible outcomes.

For operations that can be retried safely, the system should support retry or recovery.

For operations that may produce duplicate side effects, it must prevent blind retries.

For example, recovery after an email-send failure must avoid sending the same email again if it was actually sent successfully.

<a id="cy-15-03"></a>

**Disposition: Required.**

### 15.3 Observable Actual State

**CY-15-03.** Distinguish request receipt, scheduling, execution, agent output, successful operation, and user-goal completion.

Zoe should distinguish between:

- A task has been received
- Execution has been scheduled
- Execution is in progress
- Agent output has been received
- The actual operation succeeded
- The result requested by the user has been achieved

These states must not be conflated.

When users ask about progress, Zoe should answer from actual records.

<a id="cy-15-04"></a>

**Disposition: Required.**

### 15.4 Background Health Management

**CY-15-04.** Monitor service health, recover within policy, notify users, and retain context when recovery fails.

Long-running services need to report their runtime status.

Where necessary, they can recover automatically or notify the user to take action.

For problems that cannot be recovered, the system should clearly save the reason for failure and relevant context.

## 16. Data Management Requirements

Persist important records beyond model context and process lifetime. Acceptance follows durable commit. Data control includes permission-aware inspection, export, and deletion of authoritative and derived copies, client caches, exports, replicas, service volumes, and backups under a stated retention policy. Preserve sources, versions, effective times, configuration, artifacts, and required volumes for migration/recovery. Protect secrets through deployment references rather than values in source or evidence.

<a id="cy-16-01"></a>

**Disposition: Required.**

### 16.1 Persistence

**CY-16-01.** Persist important records and report acceptance only after durable commit.

Important Zoe data must be saved long term and must not exist only in model context or process memory.

<a id="cy-16-02"></a>

**Disposition: Required.**

### 16.2 Data Migration

**CY-16-02.** Migrate projects, history, tasks, memory, and services without binding records to one model provider.

The system should support migration of projects, history, tasks, memories, and service configuration.

Data should not be lost when the AI model changes.

<a id="cy-16-03"></a>

**Disposition: Required.**

### 16.3 Data Backup and Recovery

**CY-16-03.** Back up and restore important database, files, and configuration with rehearsed evidence.

The system needs to save necessary backups and be able to recover important data.

<a id="cy-16-04"></a>

**Disposition: Required.**

### 16.4 Data Sources and Versions

**CY-16-04.** Retain decision/task/memory source, version, and effective-time history.

Important decisions, task records, and memories should retain appropriate timestamps, sources, and version information.

<a id="cy-16-05"></a>

**Disposition: Required.**

### 16.5 Data Security

**CY-16-05.** Protect content, files, conversations, secrets, external grants, and configuration.

The system needs to protect:

- User content
- Project files
- Conversation history
- Keys and credentials
- External-service authorizations
- System configuration

<a id="cy-16-06"></a>

**Disposition: Required.**

### 16.6 Data Control

**CY-16-06.** Support permission-aware inspection, export, and deletion across derived stores and defined backup policy.

Users or authorized administrators should be able to view, export, and delete data as appropriate to its type and permission requirements.

## 17. Resource and Cost Management

Ordinary programs handle timers, file checks, explicit rules, conversion, state changes, and health. Idle scheduling and event processing make no model calls. Each admitted model action needs task/run authority, a route, and a budget; record usage and prevent further dispatch when limits are exhausted. Unknown consumption stays reserved pending reconciliation. Numeric limits represented in a DTO do not establish actual provider/tool/artifact enforcement.

Zoe should pay attention to long-term operating costs, especially token use.

<a id="cy-17-01"></a>

**Disposition: Required.**

### 17.1 Invoke AI When Needed

**CY-17-01.** Use programs for time, file, rule, state-transition, and health checks instead of model polling.

Prefer ordinary programs for work they can perform.

Examples include:

- Scheduled triggers
- Checking whether a file exists
- Explicit rule evaluation
- State updates
- Simple data conversion
- Service health checks

Invoke an Agent only when intelligent understanding or reasoning is needed.

<a id="cy-17-02"></a>

**Disposition: Required.**

### 17.2 Context Efficiency

**CY-17-02.** Build minimal relevant context without sacrificing traceability or correctness.

Agent calls should receive only the context they need.

Avoid repeatedly sending large amounts of the same history.

<a id="cy-17-03"></a>

**Disposition: Required.**

### 17.3 Call Management

**CY-17-03.** Record and bound model usage; stop further dispatch when budgets or loop limits are reached.

The system should record and manage resource consumption caused by Agent calls.

Where necessary, it should be possible to set call limits to prevent abnormal tasks from consuming resources indefinitely.

<a id="cy-17-04"></a>

**Disposition: Required.**

### 17.4 Efficiency During Continuous Operation

**CY-17-04.** Keep routine monitoring, scheduling, and event processing efficient without high-frequency model calls.

Background services should not depend on frequent large-language-model calls to stay running.

Long-term monitoring, scheduling, and event handling need mechanisms that can run continuously with stability and low cost.

## 18. Openness and Scope for Expansion

The initial release is private, single-owner, single-instance, and multi-project. Preserve user/workspace and permission-principal boundaries for future evolution. This does not add a team collaboration interface to the current release.

<a id="cy-18-01"></a>

**Disposition: Required.**

**CY-18-01.** Retain extensible ownership and access boundaries for multiple users, projects, agents, devices, and services.

Zoe is not defined as a product that can serve only one person.

Although an individual can deploy and use it independently, the product structure should not assume that there will forever be only one user, one project, or one Agent.

The system should leave room for:

- One user with multiple projects
- Collaboration among multiple Agents
- Access from multiple devices
- Multiple running services
- Different permission principals
- Team or organizational use

Future multi-user support will need corresponding identity, data isolation, permission management, and collaboration capabilities.

**This means the product is not limited to a single-user positioning; it does not mean every deployment must enable multi-user collaboration.**

## 19. Complete Usage Scenarios

These five scenarios combine requirements across components; they are acceptance journeys. Required evidence and gaps belong in [the acceptance matrix](acceptance/requirements.md).

The scenarios below show how the requirements work together in Zoe; they are not development steps.

<a id="cy-19-a"></a>

**Disposition: Required.**

### Scenario A: Software Development

**CY-19-A.** Scenario A: voice/text software task, native agent execution, client disconnect, cross-device progress, continuation and safe stop.

A user says through Zoe on a Mac:

“Develop a new login system for me.”

Zoe understands the request and creates a project task.

Super plans the work when needed and assigns specific development work to a Codex Worker.

Codex performs the development.

The task system continuously records the actual state.

Closing the Zoe client should not interrupt background execution.

The user picks up an iPhone and asks about development progress.

Zoe answers from the actual task records.

The user then asks to add tests.

The system continues the existing work instead of creating a completely unrelated project.

The first release needs to verify persistent task recovery, real stopping, and artifact viewing. The following manual-continuation steps are optional enhancements: the user returns to the Mac, enters the same background execution environment, and opens the official Codex CLI.

If the official CLI supports it, the user finds the original development session and continues work.

Zoe can then recognize the updated work state.

Throughout the process, the project, task, session, and code workspace must remain correctly associated.

<a id="cy-19-b"></a>

**Disposition: Required.**

### Scenario B: Creating a Long-Term Automation Capability

**CY-19-B.** Scenario B: authorized long-running mail capability with deterministic, worker, and Super routing plus notices.

A user says:

“Monitor important email for me from now on.”

Zoe understands this as a long-term service request.

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

<a id="cy-19-c"></a>

**Disposition: Required.**

### Scenario C: Long-Term Project Memory

**CY-19-C.** Scenario C: recover dated project decisions and reasons across devices without inventing missing evidence.

A user discusses a project with Zoe.

At first, they decide to use a particular technical approach.

After development, they change that decision.

Several weeks later, the user asks:

“Why did we move away from that approach?”

Zoe should find the relevant discussion and formal decision using long-term memory and historical records.

Its answer should include not only the approach currently in use, but also the change process, timing, and rationale.

If a rationale was not recorded, Zoe should say that it cannot confirm it rather than make one up.

<a id="cy-19-d"></a>

**Disposition: Required.**

### Scenario D: Parallel Work by Multiple Workers

**CY-19-D.** Scenario D: parallel specialist work, conflict coordination, and truthful aggregate outcomes.

A user says:

“Check this project's security and organize its API documentation at the same time.”

Zoe can create the corresponding tasks.

It assigns them to different Workers.

The two Workers do not have to wait for Super to direct each individual step in sequence.

The system preserves their execution status and results.

Super coordinates if there is a resource conflict, a major dependency, or a complex decision.

The user receives one consolidated result.

<a id="cy-19-e"></a>

**Disposition: Required.**

### Scenario E: Failure and Recovery

**CY-19-E.** Scenario E: unattended network/API/process failure detection, safe recovery, unknown-effect handling, and records.

A long-running background service fails because of a network interruption.

Zoe should detect the exception.

Once the service recovers, it should continue or retry according to the safety requirements of the specific task.

If automatic recovery would not be safe, it should notify the user to take action.

Even if the client was closed during the failure, the related event and failure record should be retained.

## 20. Overall Quality Requirements

All fifteen dimensions below apply to final system acceptance under CY-20-01. Numeric thresholds absent from the requirements remain open targets; a design or static check cannot establish measured quality.

<a id="cy-20-01"></a>

**Disposition: Required.**

Human native-session handoff remains optional under chapter 5; required interoperability preserves native work products and supported execution/recovery.

**CY-20-01.** Meet and measure all fifteen system-quality dimensions; unspecified thresholds remain open targets.

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

## 21. Key Boundaries of Zoe

Zoe manages persistent projects, tasks, services, and memory through conversation. Super coordinates complex work; Workers and ordinary programs retain independent roles. Zoe extends execution through authorized capabilities on existing operating systems and specialist tools. The interface remains an assistant, and the architecture retains future ownership boundaries.

<a id="cy-21-01"></a>

**Disposition: Required.**

**CY-21-01.** Keep Zoe outside the boundaries of chat-only, Super-only, orchestration-only, editor, OS, memory-only, and personal-only products.

Several distinctions need to remain clear to avoid future misunderstandings.

**First, Zoe is not an ordinary chat assistant.**

Conversation is its interaction method. What Zoe actually manages are projects, tasks, memories, and execution capabilities that persist over time.

**Second, Zoe is not a centralized Agent that requires Super to direct everything.**

Super handles complex coordination, Workers can execute independently, and ordinary background services can keep running.

**Third, Zoe is more than an Agent orchestration tool.**

In addition to invoking and coordinating Agents, it can create, deploy, and manage ordinary programs under authorization, giving the system new capabilities that keep running.

**Fourth, Zoe is not a new code editor.**

It should use native development tools such as Codex and preserve CLI execution, stopping, viewing, task recovery, and access to code artifacts. Seamless human takeover and bidirectional native-session handoff are optional future enhancements.

**Fifth, Zoe is not an operating system in the traditional sense.**

It does not replace macOS or Linux; it builds an AI system with long-term execution, memory, coordination, and extensibility on top of existing operating systems.

**Sixth, Zoe is more than a memory system.**

Long-term memory is one foundational capability, but Zoe must ultimately be able to act on memory, real-world state, and user instructions.

**Seventh, Zoe is not limited to a personal product.**

The system can serve individuals, but its product definition should not prevent future development for teams or other scenarios.

## 22. Final Complete Product Definition

The complete product combines natural interaction; complex planning and specialist execution; supported native interoperability; independent ordinary services; authorized capability expansion; source-linked memory; unified state; scoped security; replaceable modules; and durable evolution. The user sets a goal, Super coordinates when needed, Workers execute specialist work, and ordinary programs perform deterministic continuous work. All results enter the unified task/state system and important knowledge enters controlled memory.

<a id="cy-22-01"></a>

**Disposition: Required.**

**CY-22-01.** Deliver the complete product behavior across interaction, decisions, execution, interoperability, services, memory, state, security, modularity, and evolution.

**Zoe is an open AI system that can run long term, continuously accumulate knowledge, coordinate intelligent execution units, and gain new capabilities over time under authorization.**

It has the following complete capabilities:

**Interaction:** Communicate with users through natural speech and text and support continuity across devices.

**Understanding and decision-making:** Super handles complex requirements, planning, and coordination; Workers handle specific specialist tasks and local intelligent decisions.

**Execution:** Invoke tools such as Codex to perform development and other real work, and manage actual task status.

**Native interoperability:** Preserve usable projects/work products and supported runtime session associations. Human continuation in the same official Codex CLI session, reverse native-session discovery, and automatic synchronization of external changes are optional enhancements under chapter 5; they do not gate required-scope acceptance.

**Continuous operation:** Ordinary background programs handle scheduled work, monitoring, event response, and long-running services without requiring AI to repeatedly perform deterministic operations.

**Autonomous expansion:** Under user authorization, create, deploy, register, and manage new software services so the system gradually gains new functions that remain available.

**Long-term memory:** Store history, project context, formal decisions, task state, and experience, then retrieve and use them reliably and traceably.

**State management:** All clients, Agents, tasks, and background services share one consistent and trustworthy state system.

**Security controls:** Different execution principals have clear permissions, and important operations are controlled and traceable.

**Modularity:** AI models, Agents, voice services, and third-party applications can be added, replaced, and upgraded independently.

**Long-term evolution:** Expand to new scenarios while preserving the core architecture and continuity of existing data.

### Zoe's Core Operating Logic

**The user sets a goal; Super performs complex planning and coordination when needed; Workers perform specialist execution; ordinary programs handle work that can run reliably over time.**

All execution results enter a unified task and state system, and important information enters reliable long-term memory.

Users can view, intervene in, or continue work at any time from different devices, and can also continue tasks directly in native tools such as Codex where supported.

As it is used over time, Zoe should gain more than a larger conversation history: it should accumulate knowledge, preserve work products, manage more continuously running services, and gain more reusable practical capabilities.

**Ultimate goal: to make Zoe an AI system that understands and executes instructions into a system that can run long term, accumulate knowledge continuously, expand its capabilities autonomously under control, and coordinate intelligent and non-intelligent programs to do real work.**

---

*This complete product requirements definition was organized from requirements expressed earlier. It does not assume a specific technology framework or describe unverified Codex interoperability as implemented. It does not divide the product into development phases or an MVP; all sections together describe the capabilities the complete Zoe product should have.*
