> Historical product requirements baseline, version V1.0. The original Chinese source is retained in repository history; consult [the current master plan](../../master-plan.md) for current direction.

# Cylo — Complete Product Requirements Specification

**Document version:** V1.0  
**Project name:** Cylo (tentative)  
**Document type:** Complete product requirements definition

This document describes only **what Cylo is, what capabilities it needs, how its parts work together, and what outcomes the system should achieve**.

It does not cover an MVP, development phases, an implementation plan, technology-stack selection, or development priorities.

---

# 1. Product Positioning

## 1.1 What Cylo Is

**Cylo is a cross-device, long-running AI system with persistent memory, autonomous execution, task coordination, and mechanisms for expanding its capabilities.**

It interacts with users through natural language and speech, understands requests, performs tasks, manages projects, coordinates different AI agents, and, when necessary, creates and deploys ordinary programs so the system gains new capabilities that can run over time.

Cylo is not a chat tool that works only when the user issues a command.

It has a background runtime independent of its clients and can continuously maintain tasks, memory, services, and runtime state.

Authorized background work can continue even when the user does not keep Cylo open.

## 1.2 Core Design Principles

Cylo is not intended to make AI responsible for everything. Instead, different kinds of execution units should take on the work best suited to them.

The system follows these principles:

1. **AI provides intelligence; ordinary programs handle deterministic, continuous execution.**
2. **Super handles complex coordination and does not intervene in every simple operation.**
3. **Workers can complete specialist tasks independently; not everything must pass through Super.**
4. **The system can create, deploy, and manage new long-running capabilities.**
5. **Tasks, memories, and execution state must be stored long term and must not depend on a single conversation.**
6. **Users can continue the same work from different devices.**
7. **Cylo should not lock users into its own client.**
8. **AI models, agents, voice services, and other components should be independently replaceable.**
9. **Accuracy, reliability, and efficient token use take priority over unnecessary AI calls.**
10. **The product is not limited to personal use; its design leaves room to expand to teams and other scenarios.**

## 1.3 Product Boundaries

Cylo does not need to replace macOS, Linux, or Windows.

It runs on an existing operating system and uses the operating system's computing, file, network, and process-management capabilities.

Cylo also does not need to recreate a full code editor, terminal, or operating-system desktop.

For existing specialist software such as Codex, Cylo should make direct use of native capabilities whenever possible and remain interoperable with the official tools.

---

# 2. Overall System Composition

Cylo separates its clients from its server.

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

---

# 3. Client Requirements

## 3.1 Cross-Device Access

Cylo should support access to the same backend system from different devices.

Target clients include:

- macOS
- iPhone
- iPad

The architecture should leave room to add other devices and platforms in the future.

Different devices accessing the same server should see consistent projects, tasks, memories, and runtime state.

Clients should not maintain separate, conflicting task states.

## 3.2 Minimal Interaction Interface

The Cylo client is primarily for communicating with the system, not for managing large amounts of technical detail.

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

Ordinary users should be able to use Cylo without understanding how Super, Workers, databases, or background processes work.

Advanced status and operation details should still be available when needed.

## 3.3 Natural Voice Interaction

Natural speech is one of Cylo's primary interaction methods.

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

Code, execution logs, complex reports, and task lists should not have to be expressed entirely through speech.

## 3.4 Proactive Interaction

Cylo should not merely wait for users to ask questions.

It should be able to proactively notify users when important events occur in the background.

Examples include:

- A development task has completed
- A service has encountered an error
- An important email needs attention
- A task requires user authorization
- Scheduled work has produced an important result
- A background operation failed and could not recover automatically

A notification should identify its event source and link to the relevant project or task.

---

# 4. Server Requirements

## 4.1 Independent Background Operation

The Cylo server should be able to run as an independent background system.

For the first release, the server and execution host use **Linux**. They can be deployed on a Linux server or another supported Linux environment to execute agent tasks and run ordinary background services.

Running the Cylo server, AO, or Worker background services on macOS is not required for first-release acceptance and can be evaluated later as needed. macOS clients and browser access remain in scope.

The server should not depend on a graphical client remaining open.

Closing the Cylo client should not stop background services that are already running independently.

If the execution environment becomes unavailable because a device is powered off, asleep, or disconnected, the system must be able to identify that condition and continue processing according to task rules when conditions recover.

## 4.2 Separation of Client and Server

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

## 4.3 Environment Migration

Cylo should not be tightly bound to one operating system.

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

---

# 5. Deep Codex Integration Requirements

**First-release Codex integration must support CLI execution, viewing real status and artifacts, stopping, and persistent recovery. Seamless human takeover within the same thread, live switching of input control, and a complete bidirectional handoff entry point are optional future enhancements; they are not hard requirements for first-release selection or delivery.**

The first release prioritizes automatic Super planning, independent Workers, multiple worktrees, long-running tasks, and ordinary background services while preserving task recovery, artifacts, and cost records.

## 5.1 Cylo Invoking Codex

Cylo should be able to assign software-development tasks to Codex.

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
- Including Codex tasks in Cylo's unified task system

Cylo should not guess whether a task is complete based only on Codex chat content.

## 5.2 Official Codex App (Out of Current Scope)

Under the user's latest scope decision, support for discovering, resuming, deep-linking, or handing off sessions from the official macOS Codex desktop app across hosts is not required. This section number is retained for historical traceability.

The macOS Cylo client, browser access, and users' independent use of Codex CLI are unaffected. First-release server and execution-host support is defined in §4.1; cross-environment data migration remains in scope.

## 5.3 Codex CLI Continuation

The first-release Linux execution environment must preserve basic CLI invocation and a recoverable session association. A macOS client can connect to that environment over SSH; acceptance of running Cylo/AO/Workers locally on macOS can be evaluated later as needed. The following experience of manually continuing the original session through the official CLI is an optional future enhancement:

A user can:

1. Create a Codex development task through Cylo.
2. Have Codex perform development work in the background.
3. Later enter the same background execution environment directly; the remote environment can be accessed over SSH.
4. Use the official Codex CLI's supported session-recovery mechanism.
5. Find the relevant session and continue working.

The project, code, and execution results should remain accessible. Human continuation of the same native session is subject to separate verification within the official support boundary and does not block the first-release automatic execution loop.

## 5.4 Bidirectional Continuation

As an optional enhancement, in addition to continuing sessions created by Cylo through the official Codex CLI, the system may support the reverse direction.

For example:

A user first develops a project in the official Codex CLI.

Later, the user opens Cylo and says:

“Continue that project from earlier and finish the tests.”

Cylo should identify the relevant project, existing session, and work state as far as possible, then continue the work when the required support and permissions are available.

**Bidirectional native continuation is optional; basic CLI invocation and persistent task recovery remain required.**

## 5.5 Integrity of Native Continuation

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

If a session is still running in another client, Cylo must not modify the same work state simultaneously without coordination.

It must prevent duplicate execution, session conflicts, and code overwrites.

## 5.6 Codex Independence

Cylo coordinates development tasks; it is not the only way to access development tools.

Even when Cylo is temporarily unavailable, users should be able to access existing projects and work products directly through the official Codex CLI as far as possible.

Likewise, Cylo should be able to recognize and synchronize changes made by users in the official Codex CLI.

**Clarification: CLI execution, viewing, stopping, and task recovery under §5.1, along with preservation of work products, remain required. Manual same-session continuation under §§5.3–5.5 and automatic synchronization of external changes under §5.6 are optional enhancements; their absence does not block first-release or full required-scope acceptance. Basic access to code and artifacts must not depend on the human-takeover feature. Session discovery and control are limited by what official interfaces support and what has been tested; recovery must never create uncoordinated concurrent writes.**

---

# 6. Agent System Requirements

## 6.1 Super Agent

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

## 6.2 Worker Agents

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

## 6.3 Direct Invocation of Workers

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

## 6.4 Multi-Agent Collaboration

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

---

# 7. Autonomous Capability Expansion

This is one of Cylo's most distinctive requirements.

## 7.1 Creating New Capabilities

Cylo should not only use existing features; it should also be able to create new system capabilities in response to user needs.

For example, a user says:

“Check my important email every day from now on, and tell me when something important arrives.”

Cylo should understand that this is a request for a long-term capability, not a one-time email lookup.

The system can arrange for a Worker to write a program or configure existing tools and services.

After obtaining the necessary authorization, it can deploy the corresponding background service.

The service can then run independently.

## 7.2 Lifecycle of a New Capability

Cylo must be able to manage the complete lifecycle of an added capability, including:

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

## 7.3 Independent Operation of New Capabilities

An ordinary program that has been created should not need Super to participate continuously in order to work.

For example, an email-checking service can continuously monitor a mailbox.

Most of the time it does not need to call any large language model.

It calls the relevant Agent only when it encounters content that needs semantic understanding or complex decisions.

This means:

**AI can create a capability without necessarily being responsible for its ongoing execution.**

## 7.4 Scope of Capability Expansion

Cylo should be able to gain a range of capabilities through modules, external services, or programs it creates autonomously.

Examples include email, calendars, file management, development tools, GitHub, browser automation, third-party apps, system operations, scheduled tasks, information monitoring, and other software services that can be authorized.

Adding a capability should not require changing the entire system's core architecture.

---

# 8. Task Management Requirements

## 8.1 Unified Task System

Cylo needs a unified system for managing all tasks.

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

## 8.2 Task Lifecycle

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

## 8.3 Long-Running Tasks

Cylo should support tasks that take a long time, such as software development, complex research, or multi-step automation.

Users do not need to keep a conversation open.

They can ask at any time:

“How far along is that task from earlier?”

The system should retrieve its actual current status and explain it using context when needed.

## 8.4 Task Recovery

When task execution is interrupted, the system should preserve as much as possible of:

- Completed steps
- Execution results
- External resource state
- Uncompleted steps
- Information needed for recovery

Recovery should avoid unnecessary repetition.

For steps that may have irreversible effects, the system must take special care to prevent duplicate execution.

## 8.5 Association Between Tasks and Projects

A project can have multiple tasks.

Tasks can have:

- Sequential dependencies
- Parallel relationships
- Parent-child relationships
- Shared-resource relationships

Cylo should understand what the user means by “that earlier project,” “the task from a moment ago,” or “yesterday's development work.”

If several objects are possible, it must not choose one arbitrarily.

---

# 9. Long-Running Background Services

## 9.1 A Service Is Not a Task

Cylo must clearly distinguish between a **one-time task** and a **long-running service**.

For example:

“Check my email once” is a task.

“Monitor my email from now on” is a service.

They need different lifecycle management.

## 9.2 Background-Service Management

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

## 9.3 Event Triggers

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

## 9.4 Proactive Notifications

When a background service produces an event that deserves the user's attention, it should be able to send a notification.

Notifications should distinguish ordinary information, task completion, runtime errors, and events requiring a user decision.

The system should avoid duplicate notifications and frequent, meaningless reminders as far as possible.

---

# 10. Long-Term Memory Requirements

## 10.1 Persistent Memory

Cylo needs long-term memory that works across conversations, tasks, projects, and devices.

Memory must not depend on the context window of a single model call.

Important information must remain recoverable after a client closes, a session ends, or the service restarts.

## 10.2 Memory Layers

At minimum, memory includes the following categories.

### Global Memory

Store generally applicable information that remains valid over time, such as:

- User or organization preferences
- Communication habits
- General working rules
- Long-term goals
- Confirmed ongoing constraints

### Project Memory

Store project-specific information such as:

- Project goals
- Product requirements
- Architecture decisions
- Historical changes
- Design rationale
- Project background
- Important discussions

Memories for different projects need clear boundaries.

### Task Memory

Store the following for a specific task:

- Objective
- Status
- Execution history
- Results
- Unresolved issues
- Recovery context

### Raw History

Store necessary source material, including:

- Conversation records
- Operation records
- Agent output
- Important events
- Sources of decisions
- Execution logs

Raw history should be available as evidence for other memory content.

## 10.3 Memory Agent

Cylo can use a separate Memory Agent to organize memory.

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

## 10.4 Memory Accuracy

This is a core requirement.

Cylo must distinguish between:

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

## 10.5 Separate Authoritative Data from AI Memory

Formal task status, permissions, system configuration, and confirmed decisions must be saved in reliable, persistent records.

A Memory Agent can organize this information but must not arbitrarily modify authoritative records without permission.

In particular, an automatic summary must not overwrite a confirmed user decision.

## 10.6 Retrieval on Demand

Cylo should not send the entire history to the model on every call.

The system needs to retrieve only information relevant to the current task.

Goals include:

- Reducing token use
- Avoiding irrelevant context
- Preventing information from different projects from being confused
- Improving traceability of historical facts
- Improving accuracy when continuing work

When retrieval is insufficient or results conflict, the system should state the uncertainty instead of inventing missing history.

---

# 11. Context Management

Long-term memory and the current execution context need to be managed separately.

Cylo should be able to provide each Agent with context relevant to its task.

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

---

# 12. Unified State and Cross-Device Continuity

## 12.1 Unified Source of Data

Cylo should have one authoritative source of state.

Different clients viewing the same task must not show unexplained conflicting states.

For example, if a Mac shows a task as completed, an iPhone should not incorrectly continue to show that task as running.

## 12.2 Continuing Work Across Devices

A typical scenario:

A user tells Cylo on a Mac:

“Develop a new user authentication module for me.”

The user then leaves the computer.

On an iPhone, the user asks:

“How is the authentication module from earlier coming along?”

Cylo should identify the original task and report its actual progress.

The user then says:

“When the code is done, check it for security issues too.”

The system should associate this request correctly with the original task or a follow-up task.

When the user returns to the Mac, they can continue interacting through Cylo and, where supported, continue development directly in the official Codex tool.

## 12.3 Separate State from Conversation

A task's existence should not depend on a particular conversation window.

Existing tasks remain present even when the user starts a new conversation.

A conversation is only one way to access and control tasks.

---

# 13. Modular AI and Services

## 13.1 Independence from AI Models

Cylo must not bind its core capabilities to a single model.

The system should support different kinds of AI models and services.

Replacing a model in the future should not require rewriting the entire task-management, long-term-memory, and background-service system.

## 13.2 Agent Independence

Codex can serve as a development Worker.

Other Agents can handle other work.

Different Agents do not need to share the same internal implementation.

Cylo needs to know what capabilities they provide, how they execute, how they return results, and how permissions constrain them.

## 13.3 Independence of Voice Services

The voice system should be independently replaceable.

Changing speech recognition, speech synthesis, or a real-time voice service should not affect:

- Long-term memory
- Task state
- Agent coordination
- Background services
- Project data

## 13.4 Independence of External Services

Email, calendars, GitHub, file systems, and other third-party services should be independently integrated, updated, or removed.

When an external service is unavailable, it should not cause unrelated modules to fail as a whole.

---

# 14. Permission and Security Requirements

Cylo can run programs, modify files, and operate external applications, so it must have clear permission controls.

## 14.1 Separate Permissions

Different Agents and background services should receive permissions relevant to their work.

A permission held by Super must not automatically give every Worker the same permission.

## 14.2 Operation Authorization

Different operations have different levels of risk.

For example, reading email and sending email are different permissions.

Reading a file and deleting a file are different permissions.

Writing a program and deploying it directly are also different permissions.

Installing software, running high-risk system commands, changing sensitive configuration, and performing irreversible external operations must follow the user's authorization rules.

## 14.3 User Control

Users should be able to:

- View existing permissions
- Adjust the scope of permissions
- Revoke permissions
- Stop tasks
- Stop background services
- View important operation records
- Confirm high-risk operations

A single vague authorization must not give a long-running service unlimited operating permissions.

## 14.4 Isolation and Audit

Different projects, Workers, and background services should have appropriate data and execution boundaries.

Sensitive information must not be exposed casually to unrelated Agents through shared context.

Important operations need traceable execution records.

---

# 15. Reliability and Exception Handling

Cylo is a system that needs to run over long periods.

It must therefore handle more than successful tasks.

## 15.1 Exception Detection

The system needs to detect:

- A background service exiting
- A Worker execution failing
- An unavailable external API
- A network interruption
- An execution timeout
- Insufficient permissions
- Insufficient resources
- Conflicting data or state

## 15.2 Fault Recovery

For operations that can be retried safely, the system should support retry or recovery.

For operations that may produce duplicate side effects, it must prevent blind retries.

For example, recovery after an email-send failure must avoid sending the same email again if it was actually sent successfully.

## 15.3 Observable Actual State

Cylo should distinguish between:

- A task has been received
- Execution has been scheduled
- Execution is in progress
- Agent output has been received
- The actual operation succeeded
- The result requested by the user has been achieved

These states must not be conflated.

When users ask about progress, Cylo should answer from actual records.

## 15.4 Background Health Management

Long-running services need to report their runtime status.

Where necessary, they can recover automatically or notify the user to take action.

For problems that cannot be recovered, the system should clearly save the reason for failure and relevant context.

---

# 16. Data Management Requirements

## 16.1 Persistence

Important Cylo data must be saved long term and must not exist only in model context or process memory.

## 16.2 Data Migration

The system should support migration of projects, history, tasks, memories, and service configuration.

Data should not be lost when the AI model changes.

## 16.3 Data Backup and Recovery

The system needs to save necessary backups and be able to recover important data.

## 16.4 Data Sources and Versions

Important decisions, task records, and memories should retain appropriate timestamps, sources, and version information.

## 16.5 Data Security

The system needs to protect:

- User content
- Project files
- Conversation history
- Keys and credentials
- External-service authorizations
- System configuration

## 16.6 Data Control

Users or authorized administrators should be able to view, export, and delete data as appropriate to its type and permission requirements.

---

# 17. Resource and Cost Management

Cylo should pay attention to long-term operating costs, especially token use.

## 17.1 Invoke AI When Needed

Prefer ordinary programs for work they can perform.

Examples include:

- Scheduled triggers
- Checking whether a file exists
- Explicit rule evaluation
- State updates
- Simple data conversion
- Service health checks

Invoke an Agent only when intelligent understanding or reasoning is needed.

## 17.2 Context Efficiency

Agent calls should receive only the context they need.

Avoid repeatedly sending large amounts of the same history.

## 17.3 Call Management

The system should record and manage resource consumption caused by Agent calls.

Where necessary, it should be possible to set call limits to prevent abnormal tasks from consuming resources indefinitely.

## 17.4 Efficiency During Continuous Operation

Background services should not depend on frequent large-language-model calls to stay running.

Long-term monitoring, scheduling, and event handling need mechanisms that can run continuously with stability and low cost.

---

# 18. Openness and Scope for Expansion

Cylo is not defined as a product that can serve only one person.

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

---

# 19. Complete Usage Scenarios

The scenarios below show how the requirements work together in Cylo; they are not development steps.

## Scenario A: Software Development

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

## Scenario B: Creating a Long-Term Automation Capability

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

## Scenario C: Long-Term Project Memory

A user discusses a project with Cylo.

At first, they decide to use a particular technical approach.

After development, they change that decision.

Several weeks later, the user asks:

“Why did we move away from that approach?”

Cylo should find the relevant discussion and formal decision using long-term memory and historical records.

Its answer should include not only the approach currently in use, but also the change process, timing, and rationale.

If a rationale was not recorded, Cylo should say that it cannot confirm it rather than make one up.

## Scenario D: Parallel Work by Multiple Workers

A user says:

“Check this project's security and organize its API documentation at the same time.”

Cylo can create the corresponding tasks.

It assigns them to different Workers.

The two Workers do not have to wait for Super to direct each individual step in sequence.

The system preserves their execution status and results.

Super coordinates if there is a resource conflict, a major dependency, or a complex decision.

The user receives one consolidated result.

## Scenario E: Failure and Recovery

A long-running background service fails because of a network interruption.

Cylo should detect the exception.

Once the service recovers, it should continue or retry according to the safety requirements of the specific task.

If automatic recovery would not be safe, it should notify the user to take action.

Even if the client was closed during the failure, the related event and failure record should be retained.

---

# 20. Overall Quality Requirements

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
| Cross-platform support | macOS, iPhone, and iPad clients access a unified backend; first-release server and execution hosts use Linux, with macOS background support evaluated later as needed |
| Controllability | Users can understand, authorize, pause, and stop related work |
| Security | Run with appropriate permissions and protect data and external services |
| Observability | Runtime state, exceptions, results, and their sources are traceable |
| Cost efficiency | Reduce unnecessary model calls and repeated context |
| Ease of use | Make natural interaction primary; do not require users to understand internal technology |
| Long-term evolution | Integrate new capabilities without repeatedly rebuilding the system from scratch |

---

# 21. Key Boundaries of Cylo

Several distinctions need to remain clear to avoid future misunderstandings.

**First, Cylo is not an ordinary chat assistant.**

Conversation is its interaction method. What Cylo actually manages are projects, tasks, memories, and execution capabilities that persist over time.

**Second, Cylo is not a centralized Agent that requires Super to direct everything.**

Super handles complex coordination, Workers can execute independently, and ordinary background services can keep running.

**Third, Cylo is more than an Agent orchestration tool.**

In addition to invoking and coordinating Agents, it can create, deploy, and manage ordinary programs under authorization, giving the system new capabilities that keep running.

**Fourth, Cylo is not a new code editor.**

It should use native development tools such as Codex and preserve CLI execution, stopping, viewing, task recovery, and access to code artifacts. Seamless human takeover and bidirectional native-session handoff are optional future enhancements.

**Fifth, Cylo is not an operating system in the traditional sense.**

It does not replace macOS or Linux; it builds an AI system with long-term execution, memory, coordination, and extensibility on top of existing operating systems.

**Sixth, Cylo is more than a memory system.**

Long-term memory is one foundational capability, but Cylo must ultimately be able to act on memory, real-world state, and user instructions.

**Seventh, Cylo is not limited to a personal product.**

The system can serve individuals, but its product definition should not prevent future development for teams or other scenarios.

---

# 22. Final Complete Product Definition

**Cylo is an open AI system that can run long term, continuously accumulate knowledge, coordinate intelligent execution units, and gain new capabilities over time under authorization.**

It has the following complete capabilities:

**Interaction:** Communicate with users through natural speech and text and support continuity across devices.

**Understanding and decision-making:** Super handles complex requirements, planning, and coordination; Workers handle specific specialist tasks and local intelligent decisions.

**Execution:** Invoke tools such as Codex to perform development and other real work, and manage actual task status.

**Native interoperability:** As far as possible, let Codex sessions created through Cylo resume and continue directly in the official Codex CLI, while also recognizing and continuing existing work in official tools, so development is not confined to Cylo.

**Continuous operation:** Ordinary background programs handle scheduled work, monitoring, event response, and long-running services without requiring AI to repeatedly perform deterministic operations.

**Autonomous expansion:** Under user authorization, create, deploy, register, and manage new software services so the system gradually gains new functions that remain available.

**Long-term memory:** Store history, project context, formal decisions, task state, and experience, then retrieve and use them reliably and traceably.

**State management:** All clients, Agents, tasks, and background services share one consistent and trustworthy state system.

**Security controls:** Different execution principals have clear permissions, and important operations are controlled and traceable.

**Modularity:** AI models, Agents, voice services, and third-party applications can be added, replaced, and upgraded independently.

**Long-term evolution:** Expand to new scenarios while preserving the core architecture and continuity of existing data.

## Cylo's Core Operating Logic

**The user sets a goal; Super performs complex planning and coordination when needed; Workers perform specialist execution; ordinary programs handle work that can run reliably over time.**

All execution results enter a unified task and state system, and important information enters reliable long-term memory.

Users can view, intervene in, or continue work at any time from different devices, and can also continue tasks directly in native tools such as Codex where supported.

As it is used over time, Cylo should gain more than a larger conversation history: it should accumulate knowledge, preserve work products, manage more continuously running services, and gain more reusable practical capabilities.

**Ultimate goal: to make Cylo an AI system that understands and executes instructions into a system that can run long term, accumulate knowledge continuously, expand its capabilities autonomously under control, and coordinate intelligent and non-intelligent programs to do real work.**

---

*This complete product requirements definition was organized from requirements expressed earlier. It does not assume a specific technology framework or describe unverified Codex interoperability as implemented. It does not divide the product into development phases or an MVP; all sections together describe the capabilities the complete Cylo product should have.*
