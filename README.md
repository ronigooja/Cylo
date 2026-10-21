# Cylo

Cylo is a private, long-running AI task and service system. Implementation is underway: the repository contains a partial Go control foundation and native Apple client source, while product acceptance remains incomplete.

The current architecture is a private Linux Go control server with SQLite in WAL mode and `synchronous=FULL`, an independent execution runner, and native SwiftUI clients for macOS, iPhone, and iPad. AO (OrchestratorInc/agent-orchestrator) owns creation and lifecycle of all agent runtimes, reached through Cylo's authorized gateway. The existing remote CLIProxyAPI deployment remains untouched. Space3044's `chatgpt-web-voice` is the selected real-time ChatGPT web voice gateway and account pool.

The Go control plane owns authoritative task, authorization, event, and memory records. Mem0 is a controlled memory worker; Qdrant is a rebuildable retrieval index. See [the master plan](docs/master-plan.md) for decisions, milestones, requirements traceability, acceptance evidence, and known limits. Archived source material is under [docs/archive](docs/archive/).
