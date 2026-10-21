# Repository guidance

## Language and documentation

- Write repository documentation in English and use English for routine progress updates.
- Use Chinese for final reports and important findings, decisions, or questions.
- Use English filenames for new repository files.
- Translate archived Markdown into English while preserving raw evidence and source quotations verbatim. Do not preserve all narrative prose verbatim when translating.
- Treat archived documents as historical evidence. Do not describe them as the current plan.
- Keep implementation status honest. A design, static check, fixture, or proof of concept is not production implementation or end-to-end acceptance.
- Keep decisions and acceptance boundaries in `docs/master-plan.md`; keep the README concise.

## Current product and architecture baseline

- The product is private and self-hosted: a Linux Go control server, independent runner, SQLite WAL with `synchronous=FULL`, and native SwiftUI macOS/iPhone/iPad clients.
- AO (OrchestratorInc/agent-orchestrator) owns creation and lifecycle of every runtime. Cylo calls AO only through its authorized gateway.
- Reuse the existing remote CLIProxyAPI deployment without changing its configuration or lifecycle.
- Space3044 `chatgpt-web-voice` is the selected ChatGPT web real-time gateway and voice account pool.
- The Go control plane is authoritative for tasks, permissions, events, and memory versions. Mem0 is a controlled worker; Qdrant is a rebuildable index.
- There is no PWA, CAO implementation, macOS server, or Codex-specific recovery feature in the current scope. The remote CLIProxyAPI remains an existing external dependency, not a Cylo deployment target.

## Work coordination and safety

- The main agent coordinates planning, assignments, dependencies, review, and integration. Delegate implementation and substantive independent verification to separate GPT-6 Luna subagents at high reasoning effort. Use at most four concurrent implementation subagents and reserve one independent review slot.
- Run concurrent work in separate branches and worktrees under `$HOME/subagents`, with explicit file ownership for each assignment. Do not edit outside assigned ownership or take over another agent's archive scope.
- Before ending a delegated task or handing work to a fresh context, leave a concise handoff with the branch/worktree, ownership, commit, completed work, remaining work, verification, and known limitations.
- Coordinate cross-module API and data changes with the main agent and affected owners before editing. Review delegated commits before integration; report relevant tests, commands, outcomes, and limitations without claiming checks that were not run.
- Never put credentials, tokens, private account data, or secret values in source, documentation, logs, or test fixtures. Refer to secret names or deployment-provided references only.
- Do not change external services or the existing CLIProxyAPI deployment as part of repository work unless explicitly authorized.
- Prefer evidence that can be repeated. Report commands and outcomes accurately; do not claim checks that were not run.
