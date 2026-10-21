# Super/Worker 开源替代：源码核验与技术栈选择

缩写：**CAO = awslabs/cli-agent-orchestrator（Python）**；**AO = OrchestratorInc/agent-orchestrator（Go）**。两者是不同项目。

日期：2026-10-21。范围：最多五个替代项目 + CAO 基线；只读联网与固定 HEAD 静态核验，未部署、未用真实账号、未提交 runtime。

## 当前优先级与结论

用户最新调整：**自动 Super 分解、独立 Worker、多任务/worktree、持久恢复、后台服务、扩展成本优先；人工无缝接管是可选增强。** macOS/Linux CLI 执行、查看进展、停止与会话可恢复仍保留。官方 Codex 桌面 App 不在范围。同技术栈是重要偏好，不能因此丢掉实时语音上游与外接账号池。

**有比 CAO 在具体方面更好的候选：AO 是最值得做替换对照的完整编排底座；OpenClaw 是 TypeScript 服务/扩展路线的强候选。** 两者均有真实自动派生 Worker、持久状态和 worktree，不能按“多终端 UI”或“聊天机器人”排除。现有 CAO + CLI 增强路线仍可执行；本次静态证据不足以直接宣布应全面替换。

- **Go 主线优先比较 AO + Space3044 语音网关。** AO 的 Go daemon 与 Space Go 服务在后端语言上更一致，原生 Codex ID/恢复、独立 host、产品界面现成；Cylo 仍要补业务任务、预算/产物、服务托管及权限约束。不是一个 Go 二进制即可合并两产品。
- **Python 主线保留 CAO。** 已有 FastAPI/MCP/SQLite/工作流与研究资产，适合配 Python 语音处理（如 Pipecat），或让 Go 语音网关独立运行；改造路径见 [07](07-cao-cli-handoff-plan.md)。人工无缝接管降级后，不必先投入完整交接状态机。
- **TypeScript 主线优先评估 OpenClaw。** 真 subagent/ACP、Codex app-server 插件、managed worktree、后台安装与扩展接口比自行拼客户端更完整；代价是接受其 Gateway/session/plugin 体系和更大的升级面。与 Node 语音 worker 可同语言，Space Go 上游/账号池仍可独立经接口接入。

短名单按条件选择，不按 stars 或臆造分数排序：**AO（Go 产品底座）、CAO（Python 最小改造）、OpenClaw（TS 扩展/常驻服务）**。VK 为开发工作台备选；Happy/Hapi 优先作为客户端能力参考。

## 取证范围与版本

实际对七个仓库调用 GitHub `/repos/{owner}/{repo}`，包含 OpenHands 发现检查；对五个候选 shallow clone，读取许可证、manifest 和关键实现。`ComposioHQ/agent-orchestrator` 实际重定向至 `OrchestratorInc/agent-orchestrator`，当前是 Go 重写架构，不能照旧 TS 插件文章描述。

GitHub repository search 尝试 `codex orchestrator worktree`、`codex supervisor worker cli`、`codex handoff remote cli`，均返回 **403 rate limit exceeded**；因此不是全网穷尽搜索。仓库 API 成功与 search 失败均保存在 [检索证据](evidence/09-agent-alternatives-discovery.json)，含 UTC 取回时间、metadata、clone 位置/HEAD。

| 对象 | 固定 HEAD；提交日期 | 许可证与维护证据 |
|---|---|---|
| CAO 基线 | `4e990a9d78b154ad76c6cf94c7b51e36fce69f21`；10-21 | Apache-2.0，沿用02/07固定基线；非“没有持久化” |
| AO | `7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff`；10-21 | 根 Apache-2.0；frontend manifest另标MIT，复用按实际文件/依赖审查；当日功能提交 |
| OpenClaw | `60bc1ec1cd5cf790b9d984e5d1f30404f3008c8f`；10-21 | MIT；当日Codex修复提交；API元数据与clone间main在移动 |
| Vibe Kanban | `d5cbb5380fa0b32e98ef9b8d987f63decce4be3a`；09-19 | Apache-2.0；最近HEAD约一月前，不据此宣称弃维 |
| Happy | `40bad529101f7064c9e9d47013b2f77ff1d6a309`；10-21 | MIT；当日iOS文档提交，不代表核心编排已验收 |
| Hapi | `5d8deb201189dc1f183c4f5bf38c87ec4d34dd70`；10-20 | AGPL-3.0根许可证，包标AGPL-3.0-only；近期修复 |

本次临时源码：`/tmp/cylo-alt-{ao,openclaw,vk,happy,hapi}`。维护只证明有近期活动，不证明 release 稳定性、响应 SLA 或安全性。

OpenAI 原生行为依 **OpenAI Docs** 技能实际获取 [CLI reference](https://developers.openai.com/codex/cli/reference)、[app-server](https://developers.openai.com/codex/app-server) Markdown。当前无官方搜索工具，直接取官方页面；`resume`、原生 thread 与 app-server 是不同运行表面。官方页面仍明确 app-server 命令/WS 实验及生产支持边界，第三方采用不消除该风险。本文没有重新运行02历史PoC。

## 技术栈：以 daemon/执行层判断

| 项目 | 后端/adapter 与存储 | 前端与通信 | 插件/运行依赖；部署含义 |
|---|---|---|---|
| CAO | Python、FastAPI、SQLite，CLI provider类 | API/终端Web界面；HTTP、WebSocket、MCP | Python注册provider、tmux/CLI/Git；适合Python控制面；OS service由Cylo部署 |
| AO | **Go 1.27.1** daemon、ports/adapters、SQLite migrations | Electron/React/TS，另移动端；HTTP REST、SSE、PTY WebSocket、CLI | 编译期Go agent/chatdriver/workspace/runtime adapters，不是旧NPM动态插件模型；PTY host、tmux fallback、Git、原生CLI；`ao daemon`可交OS service manager |
| OpenClaw | **TypeScript/Node** Gateway、plugin SDK、SQLite/Postgres状态族 | 当前 **SolidJS 2 + Vite** Control UI；Gateway WebSocket RPC、HTTP、MCP/ACP、Codex JSON-RPC | TS扩展manifest/SDK；Node版本约束、pnpm构建、CLI/Git，sandbox按配置；有systemd/launchd安装实现 |
| VK | **Rust Axum/Tokio/sqlx**、executor trait、SQLite，本地/远程分层 | React/TS、多包Vite与Tauri；HTTP/WS、MCP、Codex JSON-RPC | 编译Rust executor；npx只作发行入口；Git/原生CLI，有Codex npx版本pin；不是TS全栈 |
| Happy | TS/Node CLI、TS server、Prisma，另standalone模式 | Expo/React Native、原生移动组件；加密relay、Socket.IO/HTTP、Codex JSON-RPC | CLI harness adapter；完整server常见Postgres/Redis/对象存储依赖，不能只算一个TS进程；团队runtime另仓 |
| Hapi | TS/**Bun** Hono hub、runner、bun:sqlite | React/Vite Web、原生移动组件；Socket.IO、HTTP、Codex JSON-RPC | agent runner/MCP桥、单可执行构建；Bun与AGPL需接受；不是CAO同类监督器开箱即用 |

证据入口：[AO backend/go.mod](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/go.mod)、[AO frontend/package.json](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/frontend/package.json)、[OpenClaw package.json](https://github.com/openclaw/openclaw/blob/60bc1ec1cd5cf790b9d984e5d1f30404f3008c8f/package.json)、[UI package](https://github.com/openclaw/openclaw/blob/60bc1ec1cd5cf790b9d984e5d1f30404f3008c8f/ui/package.json)、[VK Cargo.toml](https://github.com/BloopAI/vibe-kanban/blob/d5cbb5380fa0b32e98ef9b8d987f63decce4be3a/Cargo.toml)、[Happy server package](https://github.com/slopus/happy/blob/40bad529101f7064c9e9d47013b2f77ff1d6a309/packages/happy-server/package.json)、[Hapi hub package](https://github.com/tiann/hapi/blob/5d8deb201189dc1f183c4f5bf38c87ec4d34dd70/hub/package.json)。

“同栈”只降低开发招聘、共享类型/工具链和运维成本，不使账号、数据库、媒体流、进程生命周期天然兼容。实时音频不宜经过监督LLM逐帧转发；网关应把语义命令/事件接入编排 API。各产品自己的模型账号管理也不能自动复用为 Cylo 语音账号池。

## 能力对照

| 维度 | CAO | AO | OpenClaw | VK | Happy / Hapi |
|---|---|---|---|---|---|
| 真Super/Worker | MCP handoff/assign/send、工作流 | 专职orchestrator提示 + ao spawn/send/review，独立会话 | sessions_spawn、subagent/ACP、结果回传、可见Worker | 有scoped orchestrator MCP+session执行，非纯看板；范围限制见下 | 多会话/runner为主；未确认此二仓自带同级Super闭环 |
| Codex执行 | 原生交互CLI/tmux，抓屏状态 | 原生TUI+hooks；Chat另走app-server | Codex扩展app-server，另CLI/ACP执行体系；非只重建API聊天 | app-server；follow-up默认**fork thread** | Happy app-server；Hapi共享app-server+官方remote CLI frontend |
| 原生ID/恢复 | 当前缺Codex显式绑定/resume | hooks native ID+resume命令；有fresh fallback边界 | 插件持久thread绑定、thread/resume；监督/快照另有fork语义 | executor session ID可持久，但fork不等于resume | 均有thread ID/恢复；不等于产品任务恢复 |
| 隔离 | 可选每terminal worktree | 项目会话独立worktree；无项目plain workspace | managed worktree、sandbox配置、owner/lease/GC | workspace/worktree级，多session未必每Worker独立 | Hapi runner可建worktree；Happy可用已有目录，未证实自动分配 |
| 重启 | SQLite/inbox/workflow journal；重关联存活tmux | SQLite、detached PTY/Chat hosts、startup reconcile | 持久subagent registry、state DB、restart recovery、plugin binding | DB记录与进程状态；未证明进行中执行跨服务重启连续 | relay/hub持久状态、runner恢复；无Cylo任务账本保证 |
| 后台/独立Worker | cao-server+独立tmux进程 | daemon+每session host；可脱离桌面交service manager | Gateway service、异步子会话/ACP；逻辑子会话不总等于独立OS进程 | 本地server+executor子进程；偏开发工作区 | daemon/runner+server/hub；更偏远程控制 |
| 审批/权限 | Codex默认--yolo，需改 | 原生/TUI与Chat审批；**Codex default权限分支危险绕过** | tool/exec策略、sandbox、Codex权限桥；需核验继承边界 | 默认workspace-write/on-request并有approval service | 有远程审批；不继承为Cylo全局授权系统 |
| API/UI复用 | 轻Python HTTP/MCP好封装 | 广泛OpenAPI/SSE/terminal/UI现成；产品耦合较大 | 扩展SDK/Gateway API/Control UI；接受其session体系 | 丰富看板/工作区/审核API，Rust扩展成本 | 移动/远程入口最有价值；无监督编排替代证据 |

“持久恢复”分任务记录、会话历史、存活进程重附着、失联副作用核对四层；任何一格都不表示 exactly-once 或任意后台工具可重放。

## AO：最强直接替代候选，尤其适合 Go 主线

已核验不是仅多终端面板：`orchestratorSystemPrompt`规定协调者只协调，用`ao spawn/send`委派独立workers，路由CI/review；不是靠内置subagent混在同进程。`Manager`提供create/restore/reconcile，workspace adapter分配Git worktree，daemon API可供Cylo封装。

原生Codex优势是已经实现07计划里的多项：hook上报AgentSessionID、`GetRestoreCommand`生成`codex resume`；Chat走app-server并可桥接同native UUID。切界面持久状态机有claim、drain/interrupt、pending decision fail-closed、重启recovery。**这部分确实比当前CAO成熟，但无缝接管已非首期决定条件。**

独立host和启动恢复也更接近长时任务要求：新会话优先持久PTY host，失败fallback tmux；Chat有分离host；SQLite保留controller generation与transition。常驻daemon不是普通任意service manager，Cylo的长期HTTP服务/构建watcher仍需单独ServiceRun契约。

必须保留的限制：

- `restoreArgv`在native会话不存在时可freshLaunch重放saved prompt，保住workspace不等于保同thread。Cylo必须识别RestoreMode、禁止静默把新会话算作原run延续；失联外部写操作不能自动重做。
- `CodexPermissionArgs`中accept-edits/auto走on-request，**default分支返回`--dangerously-bypass-approvals-and-sandbox`**。默认不能当安全配置；必须显式选权限并加执行隔离，不因项目有审批UI就跳过修复。
- `ao daemon`可独立跑；自托管setup文档提供Ubuntu用户服务，但remote-host流程标experimental，官方客户端远程发现/配对与AO account/cloud耦合。Cylo优先本地API/受控部署，不直接照搬其账号与隧道。
- 根许可Apache-2.0与部分包MIT需按复用范围处理；桌面、移动、云、review等大产品面是升级成本，不是免费可裁剪承诺。

固定源码：

- [Super提示/委派](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/session_manager/prompt.go#L208)。
- [Codex原生launch/resume/hooks](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/agent/codex/codex.go#L129)。
- [transition状态机](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/session_manager/interface_transition.go)、[恢复/fresh fallback](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/session_manager/manager.go#L6796)。
- [PTY runtime路由](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/runtime/runtimeselect/hybrid.go)、[worktree保留/清理命令](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/workspace/gitworktree/commands.go)。
- [权限映射](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/pkg/agentruntime/command.go#L199)、[API schema](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/httpd/apispec/openapi.yaml)。
- [CLI/daemon运行边界](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/docs/cli/README.md)、[self-hosted限制](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/docs/self-hosted-remote.md)、[LICENSE](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/LICENSE)。

迁移成本方向：**中高**。省去CAO Codex身份/恢复与部分host改造，换来Go adapter/SQLite/产品领域模型迁移；Cylo任务API先封装daemon可减少fork，不能假装与CAO MCP/API可无缝互换。

## OpenClaw：TS 常驻控制面强候选，不因接管可选而排除

`sessions_spawn`实际提供subagent/acp两类runtime、isolated/fork上下文、可见session、worktree参数与异步结果交付。subagent registry有generation/owner校验、持久写入及中断恢复，managed worktree有allocation lease、owner保护与lossless GC；不是只有聊天渠道或泛API agent loop。

Codex扩展有app-server harness、原生thread catalog、持久binding/lease及`thread/resume`；此HEAD managed版本pin为0.162.1，外部兼容floor0.149.0。**不能沿用旧印象说OpenClaw不保留Codex原生会话。** 其监督、snapshot fork与普通resume是不同路径，仍须以Cylo选择的路径验收。

相对CAO：动态TS插件/SDK、常驻Gateway、服务安装、多渠道与sandbox/managed worktree体系更适合扩展成广义助手；若Cylo偏TS、需要常驻自动任务而非只编码工作台，它可能更合适。代价是更宽的配置/插件/状态数据库边界；逻辑subagent或worker thread不自动等于Cylo要求的独立受监管OS Worker，需选Codex/ACP独立执行路径并验证。

- [spawn参数与执行分派](https://github.com/openclaw/openclaw/blob/60bc1ec1cd5cf790b9d984e5d1f30404f3008c8f/src/agents/tools/sessions-spawn-tool.ts)、[registry启动/恢复](https://github.com/openclaw/openclaw/blob/60bc1ec1cd5cf790b9d984e5d1f30404f3008c8f/src/agents/subagents/registry/subagent-registry.ts)。
- [持久lifecycle mutation](https://github.com/openclaw/openclaw/blob/60bc1ec1cd5cf790b9d984e5d1f30404f3008c8f/src/agents/subagents/registry/subagent-registry-lifecycle-persistence.ts)、[managed worktree service](https://github.com/openclaw/openclaw/blob/60bc1ec1cd5cf790b9d984e5d1f30404f3008c8f/src/agents/worktrees/service.ts)。
- [Codex thread lifecycle](https://github.com/openclaw/openclaw/blob/60bc1ec1cd5cf790b9d984e5d1f30404f3008c8f/extensions/codex/src/app-server/thread-lifecycle-io.ts)、[binding lease](https://github.com/openclaw/openclaw/blob/60bc1ec1cd5cf790b9d984e5d1f30404f3008c8f/extensions/codex/src/app-server/session-binding-meta.ts)、[Codex说明与限制](https://github.com/openclaw/openclaw/blob/60bc1ec1cd5cf790b9d984e5d1f30404f3008c8f/extensions/codex/README.md)。
- [systemd安装](https://github.com/openclaw/openclaw/blob/60bc1ec1cd5cf790b9d984e5d1f30404f3008c8f/src/daemon/systemd-install.ts)、[launchd安装](https://github.com/openclaw/openclaw/blob/60bc1ec1cd5cf790b9d984e5d1f30404f3008c8f/src/daemon/launchd-install.ts)、[插件类型](https://github.com/openclaw/openclaw/blob/60bc1ec1cd5cf790b9d984e5d1f30404f3008c8f/src/plugins/types.ts)、[LICENSE](https://github.com/openclaw/openclaw/blob/60bc1ec1cd5cf790b9d984e5d1f30404f3008c8f/LICENSE)。

迁移成本方向：**中高**。可少写Gateway/插件/后台安装，需对齐task/run与其session/subagent状态、建立独立Worker隔离与语音事件adapter。不能叠加两套Super同时控制同一thread。其认证/渠道账号也不是语音网关账号池替代品。

## Vibe Kanban：有编排工具，但隔离和恢复语义要重查

当前确实有`orchestrator_mode_router`，限定workspace上下文并暴露create/list/update session、run_session_prompt、execution状态；明确禁止在orchestrator自己的session运行coding agent。因此不能说“仅多终端UI”。

但工具`create_session`在已有workspace创建session；scoped orchestrator不能任意列/删workspace，**不同Worker session不自动获得不同worktree**。需要改工具范围/分配策略才能满足每Worker隔离。Codex `launch_codex_agent(resume_session)`实际调用`thread_fork`，不是`thread_resume`；历史接续产生新thread不能冒充同ID恢复，即便人工无缝接管可选也要标明。

其看板、代码差异、review、workspace/API更适合交互开发产品；Codex默认workspace-write/on-request有明确安全配置。数据库持久不证明server crash后活跃进程连续，本次未测该点。

- [orchestrator scoped工具](https://github.com/BloopAI/vibe-kanban/blob/d5cbb5380fa0b32e98ef9b8d987f63decce4be3a/crates/mcp/src/task_server/tools/mod.rs#L66)、[session创建/异步执行](https://github.com/BloopAI/vibe-kanban/blob/d5cbb5380fa0b32e98ef9b8d987f63decce4be3a/crates/mcp/src/task_server/tools/sessions.rs)。
- [Codex app-server、默认策略与fork](https://github.com/BloopAI/vibe-kanban/blob/d5cbb5380fa0b32e98ef9b8d987f63decce4be3a/crates/executors/src/executors/codex.rs#L594)、[DB migrations](https://github.com/BloopAI/vibe-kanban/tree/d5cbb5380fa0b32e98ef9b8d987f63decce4be3a/crates/db/migrations)、[LICENSE](https://github.com/BloopAI/vibe-kanban/blob/d5cbb5380fa0b32e98ef9b8d987f63decce4be3a/LICENSE)。

迁移成本方向：**中高到高**。若看板与代码review是核心可选；否则引入Rust与workspace模型并未比AO/CAO更省。归类：有条件直接替代/开发工作台，不是本轮Top3。

## Happy / Hapi：优先客户端补强，勿把多会话当监督器

**Happy：** 本仓README明确“multi-model sessions and teams”去独立`happy-desktop`与`happy-agent`仓库；当前`packages/happy-agent`是旧CLI远程控制工具，不能照名字算团队agent runtime。原CLI的Codex走app-server，有显式thread resume、daemon spawn/owner治理与移动端加密同步；Claude的local/remote切换不能外推成Codex原生TUI接管。未在本仓确认CAO级Super任务拆解/Worker回收闭环。

- [范围声明](https://github.com/slopus/happy/blob/40bad529101f7064c9e9d47013b2f77ff1d6a309/README.md)、[Codex app-server](https://github.com/slopus/happy/blob/40bad529101f7064c9e9d47013b2f77ff1d6a309/packages/happy-cli/src/codex/codexAppServerClient.ts)、[resume实现](https://github.com/slopus/happy/blob/40bad529101f7064c9e9d47013b2f77ff1d6a309/packages/happy-cli/src/codex/resumeExistingThread.ts)、[daemon](https://github.com/slopus/happy/blob/40bad529101f7064c9e9d47013b2f77ff1d6a309/packages/happy-cli/src/daemon/run.ts)、[LICENSE](https://github.com/slopus/happy/blob/40bad529101f7064c9e9d47013b2f77ff1d6a309/LICENSE)。

**Hapi：** 当前Codex不是简单切本地/远端两个进程；`runCodex.ts`转共享app-server frontend，官方CLI以`--remote … resume <thread>`接入同isolated server，额外terminal detach，owner终端退出停止server。runner有worktree，hub有SQLite恢复与work-graph事件存储；这些不自动组成自动Super分解/验收。其原生会话远程查看/控制可比CAO现成，但最新优先级下不是换编排的充分理由。

- [共享运行入口](https://github.com/tiann/hapi/blob/5d8deb201189dc1f183c4f5bf38c87ec4d34dd70/cli/src/codex/runCodex.ts)、[官方remote CLI启动与恢复](https://github.com/tiann/hapi/blob/5d8deb201189dc1f183c4f5bf38c87ec4d34dd70/cli/src/codex/shared/frontend.ts)、[thread ownership registry](https://github.com/tiann/hapi/blob/5d8deb201189dc1f183c4f5bf38c87ec4d34dd70/cli/src/codex/shared/registry.ts)。
- [runner worktree](https://github.com/tiann/hapi/blob/5d8deb201189dc1f183c4f5bf38c87ec4d34dd70/cli/src/runner/worktree.ts)、[hub会话恢复模型](https://github.com/tiann/hapi/blob/5d8deb201189dc1f183c4f5bf38c87ec4d34dd70/hub/src/sync/syncEngine.ts#L3125)、[work-graph API](https://github.com/tiann/hapi/blob/5d8deb201189dc1f183c4f5bf38c87ec4d34dd70/hub/src/web/routes/workGraph.ts)、[LICENSE](https://github.com/tiann/hapi/blob/5d8deb201189dc1f183c4f5bf38c87ec4d34dd70/LICENSE)。

迁移方向：单纯借客户端模式/协议经验成本低到中；替换CAO监督层反而高，因为还需自建分解/调度/验收。Hapi AGPL影响修改后网络提供服务的源码义务，应按具体复用范围确认，不能与MIT Happy当同一许可选项。

## 其他发现与候选边界

OpenHands/OpenHands仓库API确认存在、MIT、近期维护；本轮为控制深比数量没有clone或给同类能力结论，不能据此称其原生Codex CLI接续缺失/具备。若以后更重容器开发环境与API agent runtime，可单开选型；本轮不占直接替代推荐位。

LangGraph/AutoGen等API控制框架若用来做Super可以补持久业务流程；除非另有原生CLI adapter并保留ID/工作目录/权限/状态协议，否则只是**控制层补强**，不能与CAO/AO原生CLI fleet直接等价。人工接管降级允许考虑它们，但不免除独立Worker与恢复验收。

## 应否替换，以及最小验收门槛

**现在不把“发现更大项目”当作替换完成。** 若首期坚持Go并希望复用现成产品能力，AO进入第一对照位，证据足以改变“只查CAO”的候选范围；若要最小变动、Python团队主导，CAO仍合理。若团队偏TS、广义后台助理与插件扩展，OpenClaw比Happy/Hapi更值得投入。

| 决策条件 | 建议与会失去什么 |
|---|---|
| Go控制面 + Space Go语音服务 | 优先AO对照PoC；获得现成native/host/UI，失去CAO Python轻量适配与既有MCP/workflow直接兼容 |
| Python控制面 / 首期最小改造 | 保留CAO；得到最小迁移成本，仍需补CodexID/resume、安全清理、权限、任务outbox；无缝交接可后置 |
| TS控制面 + 多渠道/自动任务 | OpenClaw对照PoC；得到插件/后台体系，失去CAO简单tmux/provider模型，需接受Gateway/session与版本升级面 |
| 只需更好移动入口 | Happy/Hapi作客户端参考，不同时引入第二监督器控制相同Worker；会增加relay/hub边界 |

三条主线共同验收，不为人工无缝接管额外设首期否决项：

1. **真监督闭环**：一个Super拆两任务，两独立Worker/OS执行owner及不同worktree，能异步回报、失败重派；最终依据产物/测试验收，而不是屏幕idle或消息送达。
2. **长时与恢复**：关闭UI、重启controller、杀单Worker、重启主机，分别查任务/原生thread/进程/副作用四层；pending审批与外部写unknown不得盲重放。业务task/run、budget、artifact/outbox仍属Cylo。
3. **会话连续性**：捕获真实native ID并落盘；停止后显式resume恢复已有历史；fresh/fork必须分别标注，不得作为同thread成功。只要求可停止/查看/继续，无需首期无缝轮流接管。
4. **权限与产物**：去除危险默认，验证shell/file/MCP批准拒绝与配置继承；取消确认子进程停止；dirty/untracked worktree保全后再GC。Git worktree是文件隔离，不是安全沙箱。
5. **普通service与语音解耦**：长驻构建/HTTP服务独立监督/日志/stop，Worker退出不误杀或遗留写者；语音gateway账号池通过有鉴权幂等的命令/事件API接入，音频不进入监督模型循环。
6. **同栈实际成本**：选定一条后台主线先做一条端到端语义命令→任务→Worker事件→语音回应；统计需要fork的adapter/状态/API面。AO的Go、CAO的Python、OpenClaw的TS不能只因界面都是TS就当同栈。

本报告完成的是候选源码核验与可评审决策边界；没有宣称上述产品验收已通过，也没有把近期提交、测试文件存在或README演示算作端到端运行结果。
