# AO Agent, Model Routing, and Recovery Boundaries

Reviewed: 2026-10-21; fixed AO SHA: `7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff`. This was a local, read-only source review and documentation edit. No models or accounts were used, and no product implementation was changed.

Cylo adopts AO's multi-agent orchestration capabilities; Codex is not the executor for every task. Earlier work emphasized Codex recovery because it followed the initial Codex/CAO research. After adopting AO, a separately written Codex-resume patch should no longer be treated as a prerequisite.

## Three layers of responsibility

- **Cylo business layer:** Task/TaskRun, memory, authorization, acceptance, and long-running services. A business task's lifecycle is independent of an execution session.
- **AO coding-agent runtime layer:** for example, Claude Code, Codex, and OpenCode. It starts the corresponding native program and adapts its workspace, session, and recovery behavior.
- **Model/provider/route layer:** the model, provider, and protocol endpoint actually used by a runtime. Support for a runtime does not imply support for every model, nor that all runtimes use the same protocol or account pool.

The fixed version's [registry/Constructors](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/agent/registry/registry.go#L57) registers 37 adapters, including Claude Code, Codex, OpenCode, OpenCode v2, Cursor, Gemini, Copilot, Kimi, Droid, Amp, Aider, and Goose. This list proves that the source contains integrations; it does not prove that Cylo has installed or accepted each runtime. The complete list is in that fixed registry. Model/provider selection and native configuration are in the same version's [modelcatalog](https://github.com/OrchestratorInc/agent-orchestrator/tree/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/agent/modelcatalog).

The existing CLIProxyAPI can route model requests: Codex Responses is the reviewed example. Other runtimes can use it only when their protocol, configuration, tool semantics, and authentication are compatible; they can also use their own providers. The platform does not require every agent to use Codex. The proxy does not manage native session files or replace AO's runtime adapters. The Space voice upstream and account pool remain separate.

## Prefer existing adapters for recovery

| Fixed-source example | Existing adapter and boundary |
|---|---|
| [Codex](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/agent/codex/codex.go#L144) | Uses `codex resume <agentSessionId>`; already implemented, so a new recovery patch is not a prerequisite. |
| [Claude Code](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/agent/claudecode/claudecode.go#L382) | Has native session recovery and identity handling. |
| [OpenCode](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/agent/opencode/opencode.go#L145) / [OpenCode v2](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/agent/opencodev2/opencodev2.go#L63) | Build recovery commands from captured native IDs; the two major versions have separate adapters. |
| [Continue](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/agent/continueagent/continueagent.go#L179) | Continues selected history with `--fork <id>`; this is not equivalent to retaining the same native session ID. |
| [Aider](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/agent/aider/aider.go#L1) | The adapter says there is no native session ID or resume-by-ID. It inherits Base's unsupported-recovery behavior; Codex's recovery claim cannot be applied to it. |

Cylo's integration layer should record `task/run → AO session → agent native session`, along with runtime, model/provider/route, host, workspace, native configuration domain, and recovery mode. Distinguish native resume, fork, fresh launch with workspace only, and reattachment to a live process according to the selected agent. Label missing capabilities explicitly; never silently treat a new session as the original. Cylo remains responsible for the business ledger and side-effect reconciliation.

For integration, first reuse the existing adapter, then accept the chosen runtime's stop, inspection, persistent history, restart, and migration behavior one by one. Add adapter code only when a specific gap is demonstrated. Source code existing does not mean every version, tool, approval path, or cross-provider switch has passed runtime acceptance.
