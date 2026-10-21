# Cylo Client Information Architecture Proposal

Date: 2026-10-21. This is a design for page structure and information priority, not an implementation or accepted capability. It follows the [complete PRD](../../../product-requirements.md) §§3, 5, 7–10, 12, 14–17, as well as the current [architecture](../../../superseded-implementation-plan.md), [selection](../../../project-selection-comparison.md), and [memory design](17-memory-requirements-and-design.md). No AGENTS.md was found in the repository or applicable ancestors.

Under the latest scope, the first server release runs on **Linux**; Cylo clients and browser access on Mac remain in scope. The current choices are AO multi-agent execution, the Space web real-time voice account pool, the existing CLIProxyAPI model-account/proxy service, the Cylo Go business layer, and Mem0 with an on-demand memory-curation role. These are server-side responsibilities and should not become a set of pages that ordinary users must manage separately.

## 1. What users see first

**The default home page is conversation, with voice/text entry, relevant current tasks, pending confirmations, and recent results; clearly show when long-running tasks continue in the background.** Users should be able to state a goal, understand progress, receive a result, and step in. A duplicate statistics dashboard is unnecessary.

Recommended primary navigation: “Conversation, Tasks, Projects.” Put “Capabilities & Services, Memory, Settings” in secondary navigation. Notifications are a global entry point. A confirmation card may appear on the home page, notifications, and task detail, but all views reference the same confirmation record. Page organization is an implementation proposal; the PRD requires the capabilities, not six fixed standalone pages.

| Page or area | Main content | Key states and actions | Permissions and PRD references |
|---|---|---|---|
| Home / Conversation | Voice and text, conversation history, current project/task associations, cards for running work, items needing attention, and recent results | Start/continue a conversation, send text, enable mic, end voice, open a task; choose the target when a goal is ambiguous | Show only conversations and related objects the user may access; §§3, 8.5, 12.3 |
| Voice session (inside conversation) | Listening/speaking status, captions with distinguishable roles, accepted-task receipts, and necessary text results | Connecting, listening, speaking, reconnecting, unavailable; mute, interrupt, end call, switch to text | Available voice does not mean the user has task or tool authorization; §§3.3, 14 |
| Task list | Goal, project, actual status, recent progress, update time; prioritize tasks waiting for the user or in an exceptional state | Browse by project/status and open details; include development, research, and ordinary service-triggered tasks | Server filters by project/subject; §§8, 12, 15.3 |
| Task detail | Goal, origin, current step/subtasks, dependencies or wait reason, execution result, verification evidence, failure reason; resource use when useful | Add requirements, answer clarification, authorize, stop; pause/resume/retry only when supported and safe; separately show control requests in progress | Enforce task permissions. Expand execution agent, run history, logs, terminal, etc. as needed; §§5.1, 8, 14, 15, 17 |
| Projects and artifacts | Project goal/status/tasks, current decisions and relevant memories, entry points to code, documents, and other outputs | Continue project, view/preview/download accessible artifacts, locate producing task and verification result | Paths do not replace access control. Basic artifact access does not depend on manual CLI takeover; §§5, 8.5, 10, 16 |
| Capabilities and services | Installed capabilities, purpose, trigger conditions, project, runtime/health status, recent runs, existing authorization | View configuration/run history; install, configure, authorize, start, pause, resume, update, stop, or uninstall as permitted | Show separately from one-off tasks. Separate deployment authorization from authorization to generate code; §§7, 9, 14 |
| Notifications / pending items | Completion, exceptions, important events, pending confirmations; source and time | Open source object, read result, handle authorization or exception; processed confirmation updates with the authoritative record | Do not rely on system push alone to retain events. Rejections/revocations are recorded too; §§3.4, 9.4, 14 |
| Memory (secondary page, reachable from projects) | Global/project/task scope; current facts and decisions, source, time, version history; mark inference/conflict | Search, inspect support, correct or handle suggestions according to permission, export/delete; formal decisions use business commands | Read task status from the ledger. Do not reconfirm explicit user decisions, expose Mem0 jobs, or require management of a Memory Agent; §§10, 16 |
| Settings: personal and authorization | Service connection status, voice/notification preferences, current grants and scope, important activity, data controls | Manage notifications, inspect/change/revoke authorization, export/delete data under applicable permissions | Make the target of revocation distinct from stopping a task; §§3, 14, 16 |
| Settings: administrator area | Execution environment health, service configuration, backup/recovery, resource/call limits; management entry for voice pool and model routes | Inspect availability, address connection/credential failures, configure/limit under admin permission | Visible only to subjects with the corresponding admin permission; a full team-role system is not required in the first release; §§4, 14–18 |

## 2. Showing task progress without inventing it

Order the first screen as “Goal → current facts/next step → what we need from you → results.” Show the plan, actual operations, and evidence summary; do not expose model chain-of-thought as a product field. Describe phases in natural language. Do not show an invented percentage when there is no reliable total. Summarize parallel subtasks and let users expand each executor and its record when needed.

| Status / situation | Information and actions to show |
|---|---|
| Queued | Goal received; not started. Explain the queue reason when known. |
| Running | Latest verifiable step and update time; allow adding requirements or requesting a stop. |
| Waiting | Which dependency/resource/event is awaited; show the expected recovery condition when known, without guessing a completion time. |
| Waiting for user | Specific question or authorization, affected object, and scope; allow answer, approve, or reject. |
| Paused | Evidence that work is actually paused and conditions to resume; do not show a pause button if pause is unsupported. |
| Completed | Result and verification; an agent's reply alone cannot move a task into this state. |
| Failed | Failed step, reason, and preserved results; offer only explicitly supported recovery/retry choices. |
| Cancelled | Stop has been confirmed; preserve records of files/external operations that already happened and do not imply automatic rollback. |
| Request submitted, execution side has not confirmed | Separately show “stopping,” “resuming,” or “change received”; retain the last confirmed task status. |
| Client disconnected / host unavailable / result needs reconciliation | Show last confirmed state and observation time, with sync/offline status. Say unknown when it cannot be confirmed; do not continue to claim that work is running or stopped. |

Distinguish “received,” “scheduled,” “executed,” and “result verified.” Show the associated task and receipt for added requirements; use actual records to establish whether they were applied to current execution. Explain insufficient permission, rejection, and expired confirmation. If the task has changed, reread the current pending action; never resubmit an old approval.

## 3. Voice and background work

The voice area has “Mute/End call”; the task area has “Stop task.” Use separate controls with clear targets; do not use one ambiguous stop action.

| User action / situation | Client feedback | Background-work semantics |
|---|---|---|
| End call, close page, switch device | End current voice session; keep related task cards available | Tasks already durably received and authorized continue independently of the media session. |
| Interrupt, correct, or click stop playback | Stop current playback immediately and show new input/captions | Interruption alone does not cancel a task. A correction creates a new requirement for existing work; operations already performed are not automatically undone. |
| Clearly say “Stop this task” or click Stop Task | Show target and “Stopping”; clarify first when target is ambiguous | Send a separate task-control command; show cancelled/stopped only after execution-side confirmation. |
| Say “Stop for a moment” with no clear target | Stop playback and ask whether related work should also stop | Do not interpret ambiguous speech as a background-cancellation command. |
| Media connection lost or upstream quota exhausted | “Voice is reconnecting” or “Voice unavailable; continue in text” | A new media session may use another available account; retain Cylo conversation/task. Do not promise seamless migration of the same upstream session. |
| Voice text is still changing | Show interim captions and, if needed, visible submit/confirm action | Interim captions and an assistant's spoken promise do not mean the task was durably accepted. Use a text receipt until the bridge is validated. |
| Continue on another device | Restore the same project/task/conversation and explicitly start a new voice connection | Continue business context, but do not automatically inherit another device's microphone or current media stream. |

Show the concrete operation, target, authorization scope, and necessary impact for high-risk actions, and request confirmation when existing authorization rules require it. Do not turn every utterance into a repeated modal. Validate the first PWA with the app foregrounded and mic explicitly enabled; do not promise iPhone lock-screen/background calls in advance.

## 4. Desktop, mobile, and delivery scope

| Dimension | Desktop / large screen | Phone / small screen |
|---|---|---|
| Primary information | Conversation alongside current task/results; easy project switching | Voice/text entry, pending confirmations, current progress, and result summary within easy reach |
| Tasks and results | Expand parallel subtasks, document/code diffs, and verification evidence | Start with summary and key evidence; open/download complex results as needed; do not force logs to be read aloud |
| Exceptions and confirmations | Handle within details for the same object | Open the relevant action directly from a notification; retain needed context before approval |
| Advanced information | Expand execution history, terminal, and resource statistics | Keep the entry point, collapsed by default so it does not crowd conversation and controls |

These are responsive layouts over one authoritative state, not two separate task systems. Use the layout appropriate to the available screen space on iPad.

| Scope | Client delivery boundary |
|---|---|
| First-release loop | Cross-device browser access to a Linux server; voice/text, real task status, necessary confirmations, stop and supported persistent recovery, artifact viewing, project associations, notifications, and state sync. Services, memory, and authorization may start as compact secondary pages, but “minimal UI” must not remove their view/control entry points. |
| Full PRD requirements, completed progressively | Full lifecycle for new capabilities, background health/exception handling, memory sources/history, data export/deletion, permission and usage management. The PRD does not define an MVP; sequencing here does not downgrade those requirements to optional. |
| Optional later enhancements | Manual continuation/round-trip handoff in the official CLI, live terminal takeover, and a dedicated team-collaboration UI. Native clients and lock-screen voice depend on measured need and platform capabilities. Mac/iPhone/iPad access remains a goal. |
| Currently out of scope | Discovery, deep links, and continuation in the official Codex macOS desktop app; running the server on macOS in the first release. |

## 5. Admin area and reuse boundaries with AO

AO's existing development board, execution sessions, and terminal UI are candidates for an advanced view of development tasks, subject to permission and state-mapping acceptance before integration. Cylo's main task page must represent Codex, other Workers, ordinary programs, and service-triggered work consistently. AO's session board cannot stand in for every business task. AO-native status is one observation source; it cannot replace Cylo's task, authorization, and acceptance ledger.

| Area | Visible to ordinary users | Visible/actionable to authorized administrators |
|---|---|---|
| Space voice account pool | Voice connection, reconnection, unavailable state, and option to continue with text | Available account count, occupancy/cooldown/errors, quota observation source/time, credential status; never show unknown quota as an exact remaining amount |
| CLIProxyAPI model accounts and proxy | Task waiting for resources or upstream unavailable; task usage when permitted | Reuse existing management entry or controlled summary; configure routes/proxy and handle account failures. Do not create a second account-selection authority in Cylo. |
| AO execution environment | Current step, real outputs, stop/recovery result | Runtime environment, execution mapping, and session diagnostics according to admin/developer permission; terminal access does not automatically grant shell permission |
| Memory backend | What is remembered, supporting source, scope, corrections, data controls | Diagnostics such as index/extraction job health; Mem0 and on-demand curation roles are not prerequisite knowledge for chat |

Space and CLIProxyAPI account pools are independent. Do not infer voice quota from the model proxy account panel or use a provider account identity in place of Cylo user authorization. Do not send credentials, tokens, keys, or proxy URLs containing passwords to the client. Keep internal thread, lease, and CODEX_HOME details out of ordinary user flows.

## 6. Documentation review scope

This round added only the information-architecture document and checked it against the PRD sections above and existing designs. No product page code was added, and no real model was requested or credential read. Delivery checks were Markdown relative-link existence and `git diff --check`. Voice reconnection, mobile background limits, stop/recovery, and the AO advanced view still need implementation and real validation; this document does not claim they are delivered.
