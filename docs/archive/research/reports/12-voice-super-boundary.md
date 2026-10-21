# Voice Agent and Super: Responsibilities, Concurrency, and Integration Boundaries

Date: 2026-10-21. **Space3044/chatgpt-web-voice is selected**, retaining the Go gateway, ChatGPT web real-time voice, and account pool. Later experiments address integration gaps; voice selection will not be reopened. This page supplements the architecture. No runtime was implemented, credentials read, or real account used. Media/account details are in [03](../../../research/03-voice.md), the independent source review is in [06](../../../research/06-realtime-integration-review.md), and the next integration step is in [11](11-voice-real-poc-readiness.md).

## 1. Is voice also an agent, and does it conflict with Super?

**Their understanding and response capabilities overlap; dividing responsibilities avoids scheduling conflicts, but answer consistency still needs separate handling.** The real-time voice model understands users, makes small talk, clarifies, and may even propose a plan aloud. It is not just a microphone and speaker. Make it the interaction agent: business intent is only a proposal; Super turns complex goals into formal plans, dependencies, and Worker scheduling; Cylo's persistent control plane validates plans, authorization, and execution evidence and remains the final source of task state.

In theory the real-time voice agent could itself be the only Super, provided it had reliable tool round-trips, persistent plans and task recovery, controlled result injection, and a way to execute after the call ends. A separate Super is not the only possible technical design. However, the current web session depends on account quota, account switching, media connectivity, and microphone/call lifetime. The fixed source also has no proven Cylo tool bridge or response control. Therefore, do not bind the long-running task planner to the call. Even if model roles are combined in the future, Cylo still owns tasks, permissions, and evidence.

| Role | Responsible for | Does not have authority to |
|---|---|---|
| Real-time voice interaction agent | Listening/speaking, small talk, clarification, expressing requests/results, proposing business intent | Dispatch Workers, grant permission, or declare completion based on a spoken plan |
| VoiceSessionAdapter (program) | Protocol events, turn confirmation, deduplication, submission, receipt association, generations, playback control | Infer authorization from captions or treat assistant text as execution evidence |
| Super | Formal planning for complex tasks, dependencies, coordination, and exception handling | Bypass Cylo authorization and durable commands to change authoritative task state |
| Cylo control plane / Worker | Durable control-plane state and authorization; Workers execute formal tasks and submit evidence | Treat model self-report, HTTP receipt, or an ended conversation as goal completion |

Prevent **double execution**: route each confirmed turn to one idempotent command; only a plan approved by the control plane may dispatch Workers. Do not give tool credentials to the voice model. Prevent **double planning**: spoken suggestions from the voice model do not become a second executable task tree; changes to existing work go through the original task/plan revision and its planner. Duplicate reasoning and inconsistent answers may still occur; permission isolation does not eliminate them. Preventing **false completion announcements** requires separate response/playback control. Prompts and on-screen corrections cannot retract a false promise the user has already heard.

## 2. Recommended topology and three request types

```text
User ←→ WebRTC / private DataChannel ←→ web real-time voice agent
 │                  ↑
 │ media and captions│ validated receipt-text injection (existing relay uses user role)
 └→ VoiceSessionAdapter → Cylo authentication, turn confirmation, sole command entry
                                ├→ deterministic query service → authoritative state/source
                                └→ Super formal plan → control-plane authorization → Worker
                                                      ↓
                                      task DB / events / execution evidence → receipt
Connection control: Cylo BFF ←→ Space Go gateway / account pool ←→ web upstream signaling
```

Keep the real-time speech-to-speech media path; do not add standalone STT/TTS. Routing is a new Cylo capability; do not assume the upstream already exposes structured intent/tool events. Initially distinguish modes using an explicit business entry point and confirmed submission. Automatic routing must be based on a confirmed user turn; clarify ambiguity first.

- **Simple query:** “What is the status of task T1?” After confirming the target and permission, query Cylo's task database and return status, observation time, and source; no second Super planning call is needed. The selected voice session can explain the receipt, but accurate readback still needs integration validation. The voice model may answer general knowledge questions directly, but must not impersonate a live business-status query.
- **Business command:** “Implement login and test it.” Confirm user turn → idempotent command → durable acceptance receipt → Super plan → authorization and Worker execution → durable evidence/acceptance → status receipt. Acceptance means only that the command was received; a running task must not be called complete. A plan or “it's done” spoken by the upstream must not trigger a second execution.
- **Small talk:** “I feel like talking about travel today.” The real-time voice model responds directly; do not call Super for every chat turn or create tasks automatically. If the conversation turns to “book me a ticket,” use the explicit command/approval path. The voice model cannot grant itself execution authority.

## 3. Long tasks, small talk, and interruptions

Tasks and media have separate lifecycles. After a task is durably accepted, the user can keep chatting while Super/Workers continue in the background; muting, hanging up, switching accounts, or recreating the upstream session does not cancel the task. Results first enter a durable notification inbox, then a playback queue delivers them according to the current session generation, priority, and user settings. Delay a notification while the user is speaking or a small-talk reply is still in progress. Do not announce the same receipt/revision twice; when a newer state supersedes an old one, do not announce stale progress.

This is desired behavior; it does not mean the current remote audio track can reliably distinguish “small-talk audio” from “task-receipt audio.” Without a source/response boundary, do not promise that users can chat freely while incorrect business answers are precisely blocked. Initially, explicitly enter business-submission mode, disable the entire remote playback track before speaking, and show the task card; natural replies will be unavailable during that period. Disabling playback only after finality arrives cannot ensure a premature answer was not leaked. Nor can playback be resumed as soon as a receipt arrives, which risks old buffered or unknown-response audio leaking. Recovery and buffer clearing require real tests. If necessary, keep the receipt on screen and record the voice-readback gap while retaining the selected real-time voice solution.

| User action | Media effect and task effect |
|---|---|
| Interrupt / “stop talking for a moment” | Stop the current response/playback; do not cancel the background task or roll back side effects. |
| Mute microphone / hang up | Stop capture or end media; accepted tasks continue and results remain in the inbox. |
| “Pause task T1” / “Cancel task T1” | Send a separate authorized command; update real state only after execution-side confirmation, and separately explain any side effects that already occurred. |
| Change the goal of a long task | Associate the change with the original task and plan version; let Super/control plane resolve conflicts. Do not change it by interrupting audio or replaying an old utterance. |

## 4. Fixed-source limits and implementation path

Fixed SHA: `6338ac478070aaa34bfe7a3c0469caf9e9174c02`. This read-only review checked:

| Source evidence | Design constraint |
|---|---|
| [service.go L628](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/internal/voice/service.go#L628): `client_tools:[]`, `primary_assistant`, empty model field | A real-time assistant exists, but Cylo tool registration/round-trip is not implemented. Do not assume a particular model or that the assistant can obtain scheduling tools. |
| [voice.html L3822](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/static/voice.html#L3822), [L4081](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/static/voice.html#L4081): relay and `author.role:user` | A receipt is text input, not a true tool result, system message, or exact read-aloud protocol. Guard against loops and rewriting. |
| [voice.html L3849](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/static/voice.html#L3849): caption merging/role inference | This is not reliable user-final detection. Require explicit confirmation first; do not execute each partial. |
| [voice.html L3773](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/static/voice.html#L3773): `stop_speaking`, with comment explicitly saying it does not locally mute | This is only a stop request, not business cancellation, a no-answer switch, or proof that playback stopped. |
| [downstream.go L25](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/internal/api/downstream.go#L25): rejects unknown fields | Do not add official tools/instructions parameters and claim the web API supports them. There is no verified custom system/no-answer mechanism. |

Implementation order: retain native calls and add confirmed submission and a durable task entry point; add query/complex-command routing, idempotency, and evidence receipts; then validate readback, response/playback control, and small talk during long-running work against the same upstream. Advance account leases/cooldowns/new-session recovery separately in 03. Treat a tool bridge, system/instructions, response suppression, or server-side peer as targeted enhancements for demonstrated gaps, and confirm each with real protocol evidence. Official Realtime documentation or an offline mock is not a substitute. A server-side peer may improve observation and media control but cannot create upstream tool capability by itself.

Acceptance should exercise actual behavior: a corrected utterance executes only once; an unfinished task is not announced as complete; a query does not trigger replanning; small talk does not dispatch a Worker; a disconnect does not cancel a task; only explicit cancellation changes task state; switching accounts does not replay a command. Each failure becomes an integration task and a corresponding release limitation for the selected solution, not a reason to select a different voice project. This round delivered documentation only; real calls, audio control, and concurrency remain unverified.
