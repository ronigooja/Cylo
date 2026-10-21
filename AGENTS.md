# Repository guidance

## Language and documentation

- Write repository documentation in English.
- Use Chinese for routine progress updates, decisions, findings, questions, and final reports to the user.
- Treat archived documents as historical evidence. Preserve archived source documents verbatim; do not describe them as the current plan.
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

- The main agent coordinates implementation and delegates implementation and independent verification to GPT-6 Luna at high reasoning effort.
- Use separate worktrees under `$HOME/subagents` for delegated work; keep each worktree's ownership narrow and report its branch and commit.
- Never put credentials, tokens, private account data, or secret values in source, documentation, logs, or test fixtures. Refer to secret names or deployment-provided references only.
- Do not change external services or the existing CLIProxyAPI deployment as part of repository work unless explicitly authorized.
- Prefer evidence that can be repeated. Report commands and outcomes accurately; do not claim checks that were not run.
