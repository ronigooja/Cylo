# Cylo 开源项目选型比较

实际查阅日期：2026-10-21。这里回答“除了 Space3044/chatgpt-web-voice 和 awslabs/cli-agent-orchestrator，还有没有更好的项目”。候选源码版本、许可证和细节分别见 [08 语音替代研究](docs/research/08-voice-project-alternatives.md)、[09 编排替代研究](docs/research/09-agent-orchestration-alternatives.md)；本页保留2026-10-21的候选研究，并按用户最新澄清更新当前决策；历史比较不替代[产品需求](Cylo-完整产品需求说明书.md)。

**当前建议：保持 CAO 最小增强这一条编排实施主线，按需求补缺；不再以双项目对跑作为推进条件。用户已选定 Space3044/chatgpt-web-voice 的 Go 网关、ChatGPT 网页实时上游及账号池，尚未选择 AO。** 语音 Agent 负责听说、追问与表达结果，Super 负责复杂规划/跨 Worker 协调；两者都不替代 Cylo 的任务、授权和执行事实。

**先区分名称：`awslabs/cli-agent-orchestrator`（CAO）是 Python/FastAPI；`OrchestratorInc/agent-orchestrator`（AO，旧 Composio 项目）当前核心 daemon 是 Go。它们是两个项目。** Space 网关是 Go，因此前者组合为 Python+Go，后者组合为 Go+Go。

本轮按用户最新意见调整权重：**人工无缝接管可选**，首期更看重自动 Super/Worker、恢复、后台任务、产物和成本。保留 Codex CLI 执行、停止、查看及可恢复身份；不再要求候选首先证明任意 CLI 与 Cylo 无缝交回。实时双向声音、ChatGPT 网页上游与多账号额度问题仍必须正面评估，独立 STT/TTS 或另付费 API 不能算等价满足。

## 1. 历史候选研究（不构成当前实施计划）

| 项目与定位 | 相对基线强在哪里 | 对 Cylo 仍缺什么、换路成本 | 本轮判断 |
|---|---|---|---|
| **[LiveKit Agents](https://github.com/livekit/agents)**；实时语音基础设施候选 | 有房间/媒体与 Agent 会话抽象；OpenAI Realtime 实现确有 completed 转录事件、function call 分发和结构化上下文，较容易把真实任务回执接入语音；不是只能 STT→文字 LLM→TTS | 已核验的 OpenAI 路径读取 `OPENAI_API_KEY`，不是 ChatGPT 网页 access_token。需改提供者/计费及媒体拓扑；未内置 Cylo 所需网页账号额度池。保留网页上游则需自行实现私有协议 adapter，final/tool 缺口不会自动消失 | **若愿意改语音提供者与费用模式，优先做对照 POC**；重视多设备媒体控制时尤其值得看 |
| **[Pipecat](https://github.com/pipecat-ai/pipecat)**；可组合的实时语音运行管线 | Realtime 服务有音频实时收发、completed 转录、函数执行及结果回传；帧处理与传输层适合定制 Super 桥接、中断和观测 | 官方/API 或其他语音服务的接入配置不是订阅账号池。迁移要重新实现 Cylo adapter、事件持久化、权限和移动端媒体验收；也不会把网页私有字幕升级成可信 final | **若更重视服务端定制、可替换提供者，优先比较它**；与 LiveKit 二选一验证核心链路，无需首期叠两套 |
| **[Agent Orchestrator / AO](https://github.com/OrchestratorInc/agent-orchestrator)**；CAO 的直接竞争方案 | 源码有项目级 orchestrator→worker spawn/send、隔离工作区及开发闭环；Codex 原生 TUI launch/resume 和 app-server Chat 两条执行路径，界面切换还有持久状态与恢复逻辑；比 CAO 更接近现成开发产品 | 当前项目已由旧 Composio 仓库迁至 OrchestratorInc，并演变为 Go/TS 系统，不能套用旧 TS 插件文章。Codex 权限参数的 default 分支仍会绕过审批和 sandbox，不能视为默认更安全。采用它意味着引入完整 daemon、存储和客户端体系；其会话/项目权威需与 Cylo 明确归属。长期普通服务、记忆、业务授权仍需 Cylo 补 | **最值得与 CAO 做正面对照的候选**；若近期重点是并行软件开发与PR/CI，可比继续补 CAO 更省产品工作 |
| **[OpenClaw](https://github.com/openclaw/openclaw)**；完整个人助手/编排底座候选 | 当前源码有 `sessions_spawn`、subagent/ACP、持久子运行状态、managed worktree/清理保护、Codex app-server thread resume，以及系统后台安装；TS核心有利于现有TS方向团队 | 比替换CAO大得多：引入自己的Gateway、会话/记忆/工具/插件权威，需决定Cylo是其产品扩展还是另建控制层。实时网页账号语音仍需Go/Python网关，不能实现全TS；现有Codex扩展恢复不等于任意官方TUI接管 | **若愿整合成熟个人助手平台，作为第二条架构路线比较**；只想替换CLI调度时AO更聚焦 |

这些是能力与适配成本的比较，没有做实时语音质量、吞吐、费用或完整 fleet 的横向基准，不能宣称某个已证明更快、更便宜、更稳定。

## 2. 已选 Space 的复用与集成边界

**当前没有找到源码证据足以支持“直接替换后，额度轮换与 Super 桥都已解决”的更优项目。** 用户已明确保留 Space 网关；账号池、租约、额度状态与稳定业务桥作为集成工作按 [03](docs/research/03-voice.md) 补。

最相关的是同源 Python 项目 [dyhhhhhh/chatgpt-web-voice](https://github.com/dyhhhhhh/chatgpt-web-voice) 和 [victorli-cn/chatgpt-web-voice](https://github.com/victorli-cn/chatgpt-web-voice)：它们使用真实网页 `access_token` 和 `realtime/wm`，不是把官方 API key 误写成网页 token。客户端确有 usage/挂断/网络信号触发重连、清旧会话标识的代码，值得借鉴故障状态机。

但“重连”尚不等于“额度耗尽换账号”：后端创建会话主要在 **401** 时禁用 token 再尝试；429 等非成功状态直接报错。新请求从空排除集开始，客户端只清绑定，没有让耗尽账号进入持久冷却或排除；专项用两个假账号实际验证，未禁用首账号时 fresh pick 仍可选回同一个。保留字幕历史也不证明新上游会话收到旧业务上下文。它们没有证明稳定 final→幂等 command→真实工具回执的完整闭环。**可移植局部机制，不据此把 Go 网关换成 Python 重写。**

[sivaneh/chatgpt-web-voice](https://github.com/sivaneh/chatgpt-web-voice) 的较新提交主要是界面/语言调整，不能按更新时间认定额度调度更好。上游与 fork 的具体 SHA、差异和实验见08。

LiveKit/Pipecat 另一条路的优势正是更明确的实时事件和函数结果接口，属于历史替代研究；当前无需继续比较，因为用户已选定网页上游。若以后重新授权换路，则需要接受不同上游或自行维护网页 adapter。后者仍承担私有协议、账号调度和桥接研究，不能把换框架算作这些难题已经完成。

## 3. CAO、AO 与客户端工具怎么选

**继续 CAO 的依据是需求匹配，而非已有文档数量。** §6 要求按需 Super、专业 Worker 可直接调用、独立并行执行；CAO 的 handoff/assign/send、tmux、独立 worktree、inbox/workflow 可作为这一执行层。§4/5/8 要求的持久恢复、真实停止与结果核对仍有明确缺口：按 [07](docs/research/07-cao-cli-handoff-plan.md) 补 native thread 映射/resume、去除默认危险权限、停止进程核验、清理前保全产物。人工无缝接管可后置，不能让可选增强驱动底座迁移。

| 产品需求 | 当前适配判断与实施责任 |
|---|---|
| 按需 Super / 直接 Worker / 独立 worktree（§6、8） | CAO 已有委派与执行基础；Cylo 决定何时调用 Super，简单动作直接路由，不强制所有请求经过 Super |
| 基础 CLI 身份/恢复、停止与成果（§4、5、8） | CAO 必须补上述缺口；AO 已有 native ID/resume、host/reconcile，确实更现成，但 fresh fallback、危险权限与清理仍需约束，未证明整体业务恢复 |
| 普通长期服务、记忆、权限、迁移（§7、9—11、14—17） | 两者都不替代 Cylo 控制平面、OS supervisor、证据/授权与恢复账本；AO 的开发产品功能不能抵扣这些需求 |
| 实时网页语音与账号池（§3、12） | 已定 Space，经幂等 command/事件接入 Cylo；语音交互与后台任务生命周期分离，功能验收不重开提供者选型 |

因此先实施 CAO adapter 与一条目标→Worker→产物→恢复链路。**若实际适配发现必需接口无法满足，或有具体工作量证据表明 AO 能显著减少必需实现，再针对该缺口复核替换；不预设必须同时运行两套 daemon。**

AO 是最有意义的更换评估对象，因为它同时有真实监督/Worker 逻辑和开发产品层，而不仅是终端 UI。若要沿用它，应先选清边界：让 AO 管代码执行与开发工作流，Cylo 管用户业务任务、授权、长期服务与记忆；同一运行不能由两套系统独立调度。其 Codex 普通恢复有“原生历史不存在则 fresh launch 保留工作区”的路径，**保住文件不等于恢复同一 thread**，Cylo 必须显示恢复模式与上下文丢失，不能统一叫续接。

[Happy](https://github.com/slopus/happy) / Hapi 值得作为移动/远程客户端研究。Happy 当前 Codex 代码确实使用 `codex app-server`、thread resume、审批与中断；因此不能说它只是截图终端，也不能把它宣传的其他 Agent 本地/远程切换直接套到 Codex。它们主要改善访问与交互，不自动替代 Super/Worker 调度、任务账本或后台服务监管。源码与 Hapi 的具体边界见09。

OpenClaw 不能再按旧印象视为 API 聊天壳：本轮版本已有真实子Agent派发、持久 owner 检查、worktree分配/清理和Codex原生app-server恢复，值得作为完整平台比较。代价是重划Cylo与其Gateway/会话/记忆的职责，不能只当给CAO插provider。OpenHands本轮只做发现核验，未深读源码，不占本轮替代推荐位。

Vibe Kanban也是有真实编排能力的开发工具：`orchestrator_mode_router`限制工作区范围，session工具可创建/运行Coding Agent且拒绝在orchestrator自身执行；因此不能仅归为UI。但其Rust/Axum/sqlx后端增加维护栈，Codex后续启动走`thread_fork`而非同thread resume，优先级低于本轮AO/完整平台OpenClaw路线。

## 4. 技术栈：维护偏好与历史组合

同栈是实际加分项，不能掩盖能力缺口。AO Go daemon + Space Go 网关在服务端语言上较统一；这是维护偏好，不能单独推出采用 AO。Cylo 新服务端语言尚未决定，现阶段没有 runtime；下表保留历史组合分析，当前执行路线为 CAO + Space。 两个现有 Go 服务仍通过 API/事件接入，不应直接合库、共享账号表或随意 import `internal` 模块。统一语言主要减少构建、调试和服务端维护成本，不能省掉协议、存储与权限边界设计。

| 组合 | 核心后端 / Agent集成 | 客户端与其他运行时 | 选择与代价 |
|---|---|---|---|
| **A：OrchestratorInc/AO + Space，Cylo服务端Go（历史候选）** | OrchestratorInc/AO Go daemon/CLI/SQLite、Go Codex adapter；Space Go HTTP网关/SQLite | AO Electron/React/TS，移动端TS；Space HTML/JS/WebRTC；独立Codex二进制、Git及终端运行时仍存在 | **保留网页订阅语音且服务端同栈最整齐**；AO比CAO带来更多现成开发产品，但要核验是否能只复用daemon/API、避免双重任务权威。AO当前go.mod要求Go1.27.1；Space要求Go1.24.1且镜像含curl-impersonate/glibc，不能当纯静态二进制组合 |
| **B：awslabs/CAO + Space（最低底座迁移成本）** | awslabs/CAO Python/FastAPI/SQLAlchemy/MCP与tmux；Space仍Go独立网关 | Cylo仍可沿用原TS建议；若新服务端选Python可与CAO同语言；Python+Go+Codex/Git/tmux | 复用既有研究最多，接受语音网关异构；隔进程API边界后不必为语言一致重写成熟网关 |
| API语音扩展：awslabs/CAO + Pipecat，Cylo服务端Python | awslabs/CAO Python；Pipecat Python实时服务，CAO>=3.10、Pipecat>=3.11需统一环境基线 | 浏览器JS/TS/WebRTC、音频原生库与Codex等外部进程 | **需要改API/其他实时上游的费用与鉴权**；无现成网页额度池。CAO+原Python网页网关虽也同栈，但其池/安全/桥接不足，没有证据值得仅为同栈替Space |
| **C：OpenClaw + Space（TS平台备选）** | TS/Node Gateway、工具/插件与Codex app-server；Space仍Go网关 | TS控制台/客户端；Node、Go和Codex/Git等独立运行时 | 若希望直接扩展个人助手平台、减少自建Gateway/会话/消息入口，可认真比较；保留网页账号语音因此不全TS；需明确替换Cylo哪部分职责 |
| API语音扩展：OpenClaw/Happy/Hapi + LiveKit Node | OpenClaw为TS/Node Gateway与Codex插件；Happy/Hapi主要客户端角色；LiveKit Node来自独立agents-js仓库，已有API key、completed与function-call实现 | LiveKit媒体server另有独立运行时；保留网页语音仍需Space Go或Python私有网关；Codex二进制仍独立 | 可作为愿改完整助手平台的TS备选，但网页网关仍异构；若换API语音可更统一TS应用层，同时仍需媒体server与原生RTC组件 |
| Vibe Kanban + 任一语音 | **Rust** Axum/sqlx/SQLite与Codex app-server执行器 | TS/React前端，Codex/Git等外部进程 | 不是TS同栈；若选它，应因开发编排/审查价值接受新增Rust维护面 |

LiveKit Node Agents是独立的`livekit/agents-js`仓库。本轮又独立读取其`391489c73ac564e4e43f888e9ffe6f63cc73fea8`的Realtime实现：确有API key鉴权、completed和function-call处理；依然不是网页订阅账号池，Node/插件能力需按固定版本分别验收。Pipecat与LiveKit均能走实时音频模型，Python同栈不代表必须退回STT/TTS。

## 5. 单主线实施与复核条件

**推进 CAO + Space + Cylo 控制平面。** 先补基础 CLI 执行/恢复、停止、显式权限和产物保全，同时按已选语音路径完成稳定提交、回执播报及额度耗尽后新媒体会话恢复。用户没有选择 AO，也没有要求重新比较语音框架。功能未验收须标明缺口，不能因此宣称底座选择尚未成立。

AO 的 native 恢复和独立 host 是实质适配优势，保留作具体缺口的替代参考；OpenClaw、LiveKit/Pipecat 和客户端项目保留历史证据，不新增并行验证路线。[10](docs/research/10-cao-ao-comparative-poc.md) 记录已有清理实验及本次按需求收敛后的实施顺序。
TEN Framework 本轮不进入前两名：当前根许可证是在 Apache 2.0 上附加部署限制，包含 End User devices、与 Agora 竞争及允许第三方开发/部署应用等条件；不应写成纯 Apache 2.0。该判断针对本轮固定版本根 LICENSE，不代表每个依赖都采用同样条款。

## 6. 独立复核证据与限制

整合者实际读取下列固定源码；具体版本元数据及更完整证据见08/09。许可证以根文件为入口，不能替代各子包及模型/资产的独立条款；例如LiveKit的turn模型另有MODEL_LICENSE，AO前端package元数据也不能覆盖根许可证。

| 固定版本 | 独立读取的决定性实现 | 支持的结论 |
|---|---|---|
| LiveKit `7c99532ffa629a32326f8e8fd2ad0bad8f909544` | [Realtime实现](https://github.com/livekit/agents/blob/7c99532ffa629a32326f8e8fd2ad0bad8f909544/livekit-agents/livekit/agents/llm/_realtime/openai.py)：API key构造、completed分发、function channel；根 LICENSE | 官方/API路线有结构化实时桥，Apache-2.0；不是网页账号池 |
| LiveKit Node `391489c73ac564e4e43f888e9ffe6f63cc73fea8` | [TS Realtime实现](https://github.com/livekit/agents-js/blob/391489c73ac564e4e43f888e9ffe6f63cc73fea8/agents/src/llm/openai_realtime/realtime_model.ts)、agents/package.json | 独立TS/Node实现，也用API key；completed与function_call实际存在，不是Python证据外推 |
| Pipecat `32d63cb97de2bb2ecc732d0427ac1fb4e0c7e8c6` | [Realtime服务](https://github.com/pipecat-ai/pipecat/blob/32d63cb97de2bb2ecc732d0427ac1fb4e0c7e8c6/src/pipecat/services/openai/realtime/llm.py)：Bearer API key、completed、`run_function_calls`、`function_call_output`；根 LICENSE | 不仅支持分段STT/TTS，BSD-2-Clause；仍不是网页账号池 |
| AO `7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff` | [Codex adapter](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/agent/codex/codex.go)、[界面转换](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/session_manager/interface_transition.go)、`prompt.go`、`manager.go:restoreArgv`、app-server driver与根 LICENSE | 真原生CLI/Chat双路径、监督角色及持久转换；Apache-2.0。普通恢复可能fresh launch，不能一概称同thread恢复 |
| OpenClaw `60bc1ec1cd5cf790b9d984e5d1f30404f3008c8f` | [spawn工具](https://github.com/openclaw/openclaw/blob/60bc1ec1cd5cf790b9d984e5d1f30404f3008c8f/src/agents/tools/sessions-spawn-tool.ts)、`subagent-registry-lifecycle-persistence.ts`、`worktrees/service.ts`、`extensions/codex/src/app-server/thread-resume.ts`、package.json | 真子Agent与managed worktree、原生app-server resume；TS/Node核心，MIT。不是只做UI |
| Vibe Kanban `d5cbb5380fa0b32e98ef9b8d987f63decce4be3a` | [orchestrator工具路由](https://github.com/BloopAI/vibe-kanban/blob/d5cbb5380fa0b32e98ef9b8d987f63decce4be3a/crates/mcp/src/task_server/tools/mod.rs)、`sessions.rs`、`crates/executors/src/executors/codex.rs:launch_codex_agent`、Cargo manifests与根LICENSE | 真workspace级编排；Codex后续走fork；Rust后端，Apache-2.0 |
| victorli `9916f10` | `app/voice_service.py:create_voice_session` 与 `static/voice.html:runAccountFailover/startCall` | 网页凭据与重连真实存在；仅清绑定未实现可靠quota换号 |
| TEN `18a449ea` | 根 `LICENSE` 首段及条款1—5 | 当前根许可确有附加条件，未把约束泛化成所有组件 |

维护判断看实际提交、测试和实现，不以 Star 或最新 push 自动推定可靠。LiveKit/Pipecat/AO 本轮固定 HEAD 均有2026-10-21代码提交；这表明活跃开发，同时意味着需固定版本和升级回归，不证明维护 SLA。OpenClaw、Vibe Kanban及客户端的固定版本、维护/许可证见09。Space/Python fork 的具体提交与差异见08；已有“in-call failover”注释也不能替代配额实验。

本轮没有导入真实账号、真实付费通话、运行完整 AO/CAO fleet 或进行 macOS/iOS 真机实验。结构化 completed 也仍需 Cylo 的幂等、修订、授权和账本，开源框架不会自动赋予业务上的 exactly-once。当前实施建议是按需求推进 CAO 单主线，产品是否满足仍按实际验收证据判断。
