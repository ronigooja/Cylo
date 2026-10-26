# Cylo

Cylo is a private, long-running AI task and service system. Implementation is underway: the repository contains a Go control plane, source-linked memory authority and controlled Python worker, voice turn bridge, and native Apple client source. End-to-end product acceptance remains incomplete.

The current architecture is a private Linux Go control server with SQLite in WAL mode and `synchronous=FULL`, an independent execution runner, and native SwiftUI clients for macOS, iPhone, and iPad. AO (OrchestratorInc/agent-orchestrator) owns creation and lifecycle of all agent runtimes, reached through Cylo's authorized gateway. The existing remote CLIProxyAPI deployment remains untouched. Space3044's `chatgpt-web-voice` is the selected real-time ChatGPT web voice gateway and account pool.

The Go control plane owns authoritative task, authorization, event, and memory records. Mem0 is a controlled worker with a rebuildable Qdrant retrieval index. See [the master plan](docs/master-plan.md) for decisions, milestones, requirements traceability, acceptance evidence, and known limits. Archived source material is under [docs/archive](docs/archive/).

Fresh deployments use one shared instance key. Native clients connect with a user-entered server URL and key, an installation link, or a QR image; the key is stored only in Keychain. Password login and device approval are outside the current model. This source candidate still requires CI and separate installation acceptance.

The recorded local installation is described in [local deployment operations](docs/local-deployment.md).

Install the Linux `cylo` operator CLI from a CI artifact or immutable release
using the [operator installation instructions](docs/local-deployment.md#operator-release-installation).
Run `cylo` for the interactive maintenance menu; explicit `setup`, provider,
key, and `status` commands support automation. Key creation and rotation display
a built-in terminal QR for native App connection. The supported external AO
provider route is Codex; an OpenCode example does not establish runtime support.
