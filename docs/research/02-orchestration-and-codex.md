# Cylo 编排与 Codex 原生接续调研

访问/实测日期：2026-10-21。输入：旧 `codex-super-agent-requirements-and-architecture.md`、新版 `Cylo-完整产品需求说明书.md`。本文是选型证据，不是运行时实现或生产就绪声明。需求编号沿用需求报告 `CY-05-01…06`、`CY-06-*`、`CY-08-*`、`CY-14-*`、`CY-15-*`。

## 1. 决策

**当前建议：保留 CAO Super/Worker，先做原生 Codex CLI/tmux 接续增强；具体源码改动与 A/B 路线见 [07 改造方案](07-cao-cli-handoff-plan.md)。** 当前范围仅官方 CLI；用户明确不要求官方 macOS 桌面 App。下面历史 App 文档、入口和实验限制作为范围外证据保留，不是产品需求、准入或交付阻断条件。macOS 上运行 CLI 与浏览器不因此排除。

本次调整基于用户取消官方桌面 App 范围，以及 CAO 已有真实 tmux 附着、协作与持久化的源码事实；不是证明所有 Cylo 产品要求已被 CAO 覆盖。抓屏正则、默认 `--yolo`、强制清理的风险仍保留在下文，作为具体改造项。

CAO 已具备真实 tmux 附着，可手动操作同一运行中的原生 CLI；要补的是 CAO 自动输入/回收互斥，以及 CLI 退出后的官方 thread ID 绑定和 `resume`。CAO 自己的 terminal/session ID 不能当 Codex thread ID。推荐路线 A 复用其编排，app-server adapter 仅在需要结构化工具/审批/turn 控制时采用路线 B，不因桌面 App 范围外能力重构整个运行时。

**已有实测限定：** Linux 同主机、同 CODEX_HOME 的“app-server 首轮 → 官方交互 CLI 同 thread 新一轮 → app-server 第三轮”通过隔离假 provider 三轮文字闭环；不是 CAO 改造已完成，也不替代真实模型、shell/MCP、审批与并发交接验证。app-server 自身重启恢复已实测；同进程 tmux 附着与退出后同 thread resume 是两类接管。

## 2. 获取方式、版本与证据分级

依照 **OpenAI Docs** 技能读取官方资料。当前工具清单没有官方 docs search、web search 或浏览器检索工具，故使用 `curl` 实际读取官方 URL/Markdown（不是搜索摘要），再以官方 GitHub 源码交叉核验；第一次 `python` 下载因环境仅有 `python3` 未执行，随后改用 `curl`。官方旧 URL 现在部分重定向至 `learn.chatgpt.com`，报告保留原入口与实际内容语义，不用旧文章推断现状。

| 对象 | 实际获取与固定版本 | 许可证/维护证据 |
|---|---|---|
| awslabs/cli-agent-orchestrator | `/tmp/cylo-research-cao`，`4e990a9d78b154ad76c6cf94c7b51e36fce69f21`，pyproject `2.5.4` | Apache-2.0；该提交 2026-10-21，依赖合并 #1015；只能说明当前有维护活动，不能据此保证 SLA |
| openai/codex | `/tmp/cylo-research-codex`，`d138398cef7d49e46fe33f1d0ddbd2ca0ed16aa4` | Apache-2.0；2026-10-21，企业 MCP 授权测试提交；本机二进制独立为 `codex-cli 0.163.0`，未声称由此 main commit 编译 |
| openai/openai-agents-python | `/tmp/cylo-research-agents-sdk`，`64cd5dfc5a1579870dcbed0ea42b27aa84033fee`，pyproject `0.24.0` | MIT；2026-10-19 docs 提交；这是 SDK，不是持久后台控制服务 |

精确获取命令（仓库未 vendor 进 Cylo）：

```bash
git clone --depth 1 https://github.com/awslabs/cli-agent-orchestrator.git /tmp/cylo-research-cao
git clone --depth 1 --filter=blob:none --sparse https://github.com/openai/codex.git /tmp/cylo-research-codex
git -C /tmp/cylo-research-codex sparse-checkout set codex-rs/app-server codex-rs/app-server-protocol codex-rs/cli codex-rs/core/src/rollout sdk/typescript sdk/python
git clone --depth 1 https://github.com/openai/openai-agents-python.git /tmp/cylo-research-agents-sdk
git -C /tmp/cylo-research-cao rev-parse HEAD
git -C /tmp/cylo-research-codex rev-parse HEAD
git -C /tmp/cylo-research-agents-sdk rev-parse HEAD
codex --version
codex app-server --help
codex resume --help
curl -L -sS https://developers.openai.com/codex/app-server.md -o /tmp/cylo-codex-app-server.md
```

下文“官方”=已读官方文档或源码；“实测”=本文隔离实验；“推论/建议”=设计判断；“未验证”=没有端到端证据。源码 main 与本机发布二进制不同，源码能力不自动记作二进制通过。

## 3. CAO 实际架构及适用边界

核心源码永久链接统一固定于本次 commit：

- [CODEBASE](https://github.com/awslabs/cli-agent-orchestrator/blob/4e990a9d78b154ad76c6cf94c7b51e36fce69f21/CODEBASE.md)、[LICENSE](https://github.com/awslabs/cli-agent-orchestrator/blob/4e990a9d78b154ad76c6cf94c7b51e36fce69f21/LICENSE)、[pyproject](https://github.com/awslabs/cli-agent-orchestrator/blob/4e990a9d78b154ad76c6cf94c7b51e36fce69f21/pyproject.toml)。
- [provider 枚举](https://github.com/awslabs/cli-agent-orchestrator/blob/4e990a9d78b154ad76c6cf94c7b51e36fce69f21/src/cli_agent_orchestrator/models/provider.py)、[CodexProvider](https://github.com/awslabs/cli-agent-orchestrator/blob/4e990a9d78b154ad76c6cf94c7b51e36fce69f21/src/cli_agent_orchestrator/providers/codex.py#L1362)、[Codex 文档](https://github.com/awslabs/cli-agent-orchestrator/blob/4e990a9d78b154ad76c6cf94c7b51e36fce69f21/docs/codex-cli.md)。
- [backend factory](https://github.com/awslabs/cli-agent-orchestrator/blob/4e990a9d78b154ad76c6cf94c7b51e36fce69f21/src/cli_agent_orchestrator/backends/factory.py)、[provider 重建](https://github.com/awslabs/cli-agent-orchestrator/blob/4e990a9d78b154ad76c6cf94c7b51e36fce69f21/src/cli_agent_orchestrator/providers/manager.py#L231)。
- [SQLite 模型](https://github.com/awslabs/cli-agent-orchestrator/blob/4e990a9d78b154ad76c6cf94c7b51e36fce69f21/src/cli_agent_orchestrator/clients/database.py)、[workflow journal](https://github.com/awslabs/cli-agent-orchestrator/blob/4e990a9d78b154ad76c6cf94c7b51e36fce69f21/src/cli_agent_orchestrator/services/workflow_journal.py)、[step replay](https://github.com/awslabs/cli-agent-orchestrator/blob/4e990a9d78b154ad76c6cf94c7b51e36fce69f21/src/cli_agent_orchestrator/services/step_replay.py)。
- [MCP 协作工具](https://github.com/awslabs/cli-agent-orchestrator/blob/4e990a9d78b154ad76c6cf94c7b51e36fce69f21/src/cli_agent_orchestrator/mcp_server/server.py)、[收件箱投递](https://github.com/awslabs/cli-agent-orchestrator/blob/4e990a9d78b154ad76c6cf94c7b51e36fce69f21/docs/inbox-delivery.md)、[事件机制](https://github.com/awslabs/cli-agent-orchestrator/blob/4e990a9d78b154ad76c6cf94c7b51e36fce69f21/docs/event-driven-architecture.md)。
- [worktree 生命周期](https://github.com/awslabs/cli-agent-orchestrator/blob/4e990a9d78b154ad76c6cf94c7b51e36fce69f21/src/cli_agent_orchestrator/services/worktree_service.py#L147)、[进程内事件历史](https://github.com/awslabs/cli-agent-orchestrator/blob/4e990a9d78b154ad76c6cf94c7b51e36fce69f21/src/cli_agent_orchestrator/services/event_log_service.py)。

| 关注点 | 核验结果 | 对 Cylo 的含义 |
|---|---|---|
| provider | Kiro、Claude Code、Codex、Kimi、Copilot、OpenCode、Hermes、Cursor、Antigravity、OMP、Grok、MiniMax；mock 仅测试 | 支持的是 CLI adapter，不是统一模型 API；不能将 provider 数量等同于原生协议一致性 |
| CLI/tmux | FastAPI `cao-server` → session/terminal service → backend → CLI provider；默认 tmux，herdr 为 opt-in experimental | 每个 agent 是完整 CLI 进程；本环境没有 tmux，未运行完整 CAO fleet |
| Codex 状态 | `--no-alt-screen --disable shell_snapshot`，capture-pane/渲染屏幕与版本相关的 footer、spinner、prompt 正则 | “COMPLETED”是终端状态推断，不是结构化 turn/completed 或业务验收；文档还指出重启重建 provider 未 initialize 会退回旧规则 |
| 生命周期与恢复 | SQLite 保留 terminal/session/inbox，provider manager 可按 DB 元数据重建 adapter；workflow journal/step replay 有持久恢复与人工裁决机制 | CAO server 重启与 tmux pane 存活可以接回；未证明主机重启后能恢复原 Codex thread、进行同一工具进程接管 |
| 通信与回调 | MCP handoff/assign/send_message，DB inbox + 即时投递/文件watchdog/30秒reconcile；结构化 supervisor_terminal_id/callback 辅助路由 | handoff 是等待/捕获结果，assign 是异步协作；Codex 输入默认需 idle/completed，不能冒充 `turn/steer`；文档承认重复唤醒竞争与 DELIVERED 不等于处理完成 |
| 事件持久化 | live event bus 为进程内 pub/sub；fleet EventLog 是500条、24h内存ring；业务表与workflow journal另有持久化 | 不能直接作为 Cylo 客户端跨重启事件游标/审计日志；应接入自己的 durable outbox |
| 权限 | Codex `_build_codex_command` 默认 `codex --yolo`；只有 codexProfile 且无 unrestricted `*` 时改为 `--profile`；allowedTools 约束部分通过提示注入 | 默认形态不满足 CY-14 权限边界。必须使用外部执行隔离和明确配置；提示词不能当强制权限 |
| 审批 | 文档明确要求非交互 permission tier；当前 boxed 审批 UI 不能自动交互，会导致超时 | CAO 自有 workflow plan approval 不等于逐条 Codex command/fileChange 审批，不足以实现移动决策收件箱 |
| 多worktree | `use_worktree=True` 为每terminal生成 `.cao/worktrees/<id>` 与 `cao/<id>`；默认 workers仍可共享目录 | 隔离是可选的；不是调度器天然保证。git是worktree记录源；未实现自动合并/集成验收 |
| 清理 | teardown `git worktree remove --force`，分支仅安全 `-d` | 已提交且未合并分支保留，但未提交/未跟踪成果被丢弃（实测）；Cylo必须先保存产物/验收再清理 |
| 原生接续 | Codex provider运行官方CLI但未建立Cylo任务→官方threadId的可靠显式映射，创建命令非resume | CLI自身可能写原生日志；需07中的显式身份、resume、交接控制，不能因此保证CAO已双向接续 |

CAO 的真实复用价值是多家 CLI 终端适配与协作工具、可观察界面、工作流经验。当前保留 CAO 作为首选编排基础；原生结构化协议作为按需增强，不以仓库“不活跃”或“没有持久化”为替换理由。

## 4. app-server、Agents SDK 与适用替代比较

官方文档已实际读取：[app-server](https://developers.openai.com/codex/app-server)、[Codex SDK](https://developers.openai.com/codex/sdk)、[Agents runtime 对比](https://developers.openai.com/api/docs/guides/agents)、[Agents SDK](https://developers.openai.com/api/docs/guides/agents/sdk)、[认证](https://developers.openai.com/codex/auth)。

| 选项 | 会话/审批/工具与持久化边界 | 结论 |
|---|---|---|
| Codex app-server | JSONL stdio、WS、Unix socket；thread/start/list/read/resume、turn/start/steer/interrupt、items与usage、server-initiated审批。官方持久历史属于Codex；任务、预算、通知、幂等仍由Cylo负责 | **路线B的候选协议**；固定二进制+生成schema，做兼容测试。官方仍将命令和WS标experimental/unsupported production，须接受明确技术风险，不能写成熟稳定SLA |
| OpenAI Agents SDK | 运行应用自己的agent loop、handoffs/tools/guardrails/审批与session存储；[SQLiteSession源码](https://github.com/openai/openai-agents-python/blob/64cd5dfc5a1579870dcbed0ea42b27aa84033fee/src/agents/memory/sqlite_session.py)、[run_state](https://github.com/openai/openai-agents-python/blob/64cd5dfc5a1579870dcbed0ea42b27aa84033fee/src/agents/run_state.py) | 可选用于Super/非Codex Worker；不能把SDK session当作官方Codex thread，也不应再包一层对每个Codex工具步骤做LLM协调。SDK本身不是任务库/队列/后台daemon |
| **适用替代1：官方Codex SDK** | TS当前源码通过`codex exec --experimental-json`和`resume <threadId>`；Python当前官方文档称稳定，底层app-server并附带固定runtime。不同语言绑定能力不能混为一谈 | Python绑定若覆盖所需审批/steer/history可降低协议维护成本；仍须验收，不改变持久化/互斥责任。TS适合批任务，不能仅凭resumeThread推断完整审批桥接 |
| CAO | 自己的session/terminal/workflow/memory体系 + TUI CLI adapter | 当前推荐复用Super/Worker编排并做路线A改造；不与app-server共同控制同一Codex运行 |

Codex SDK源码证据：[TS exec封装](https://github.com/openai/codex/blob/d138398cef7d49e46fe33f1d0ddbd2ca0ed16aa4/sdk/typescript/src/exec.ts#L95)、[Python客户端](https://github.com/openai/codex/blob/d138398cef7d49e46fe33f1d0ddbd2ca0ed16aa4/sdk/python/src/openai_codex/client.py)、[Python包元数据](https://github.com/openai/codex/blob/d138398cef7d49e46fe33f1d0ddbd2ca0ed16aa4/sdk/python/pyproject.toml)。未引入第二个替代项目：当前聚焦 CAO CLI 增强，不为范围外桌面 App 扩大候选。

### 4.1 传输与认证的准确范围

本机 `0.163.0` help明确支持 `--listen stdio://`（默认）、`unix://`/path、`ws://`、`off`；CLI `resume --remote`支持 `ws/wss/unix`。官方文档一致，但明确 **“The app-server command and WebSocket transport are experimental and aren't supported for production workloads.”** Unix socket存在不能推导成整套app-server获得生产支持；远程CLI能力存在也不能证明Mac App支持第三方远程服务器。

官方 `app-server.md` 的 Auth endpoints 原文：

> “If you’ve built a local or open-source application using Codex app-server authentication, you can continue using it…”
>
> “App-server authentication has never been permitted for commercial or hosted services. We launched Sign in with ChatGPT to support these use cases…”

此处限定 **app-server authentication**。开源许可证不授予ChatGPT订阅转售/托管鉴权权利；也不能反过来将此句扩成“Codex所有商业使用均禁止”或“API key全部禁止”。本地/个人Cylo可依用户自己的受支持本地认证方式设计，但新应用推荐迁移路径及具体适用性仍应核实。未来商业/托管部署应单独评估官方 [Sign in with ChatGPT](https://developers.openai.com/siwc)（本文仅作为官方指向，未实测接入/可用性）或许可适配的API/provider凭据，不复用用户缓存订阅token充当多租户服务认证。

认证文档另明确推荐API key用于程序化Codex CLI/CI/CD；API计费、数据保留、功能可用性与ChatGPT订阅不同。不得共享整份用户CODEX_HOME给不相关租户。认证本身不迁移线程、文件或项目。

## 5. 新版第5章：原生双向接续的完整性

### 5.1 官方支持、推论与实测矩阵

| 要素 | 官方可确认范围 | 本次实际验证/限制 | Cylo设计与验收责任 |
|---|---|---|---|
| 会话身份 | `thread.id`、`sessionId`（root/fork语义不同）、source/originator/cwd；thread/list分页与过滤；CLI `resume <id>` | 同主机app-server→CLI同ID历史已验证；Mac App是否显示第三方thread未验证 | 保留host/store/thread/session四维身份，不仅存显示标题；别根据“刚才的项目”猜一个会话 |
| 发现已有CLI工作 | thread/list/read/turns/items；默认sourceKinds仅cli/vscode，显式考虑appServer/exec等；cwd精确匹配 | PoC app-server创建的source实际为`vscode`，不能假设自建client必为`appServer` | 结合repo identity、原生ID、originator、worktree；歧义时让用户选；不能只按目录或最新mtime |
| turn历史/目标 | 原生持久turn/items；目标可能来自conversation或Codex goal，也可能仅在Cylo任务库 | userMessage保留、被终止turn恢复interrupted已验证；真实模型/工具turn未验证 | task goal/验收权威留Cylo，使用映射读取增量，不把summary当原始历史 |
| 正在运行的会话 | 当前源码有loaded thread resume订阅、活跃snapshot及pending request replay | **同一app-server进程**可以重新订阅，不等于跨进程/跨主机抢占；未做多client竞态实测 | 唯一写租约、fencing token、移交确认；原生工具不受Cylo租约强制约束，需共同daemon或运行端锁合作，做不到时禁并发写 |
| 审批 | command/fileChange/permissions requestApproval、tool requestUserInput、serverRequest/resolved | 当前源码同进程resume会replay pending server requests；未证明进程死亡后审批可原样恢复 | 业务决定持久存；原生requestId绑定连接/turn，重连核验未解决请求，旧批准不得跨新turn复用 |
| 工具记录 | commandExecution/fileChange/MCP等items持久历史分页读取；工具定义/配置因版本与环境而异 | 未跑真实命令/MCP/图片/审批；历史记录不等于工具进程存活或外部副作用可重放 | 明确recorded/active/result-unknown，保留原始证据，工具凭据和端点另验 |
| 文件/代码 | cwd/gitInfo是身份线索，worktree是实际文件资源；原生resume不撤销/复制文件 | worktree清理丢未提交成果实测；未做文件工具恢复 | 校验repo、branch、HEAD、dirty manifest和实际worktree路径，运行期间禁止移除；保存patch/未跟踪文件后才可移交 |
| Mac官方App→Cylo及反向 | 当前官方CLI `/app`明确打开same saved chat；App支持`codex://threads/<thread-id>`本地深链，名称现为ChatGPT desktop app | 无macOS硬件/App，**第三方会话双向均未验证**；共享本地store是候选条件，不是端到端通过 | 验证App版本、source过滤、同账户/store、项目/worktree注册、daemon与权限；不能将新建thread深链当resume |
| Linux官方CLI→Cylo及反向 | 官方CLI resume与app-server原生store API存在 | **P5已通过同thread三轮文字闭环**：app-server→交互CLI新turn→app-server新turn；loopback模拟Responses，无真实模型/tools/审批 | 实现互斥与状态回收；真实工具、审批策略和断线副作用仍需单独验收 |
| 跨主机 | 官方CLI `--remote`；App SSH projects访问远端文件/命令；官方host handoff转chat+Git state，目的地须匹配saved project | 官方文档确认；本次未做远程连接、App发现Cylo thread或host handoff实测 | 首选留在Linux原host远程访问；迁移采用经验证官方路径，核对身份映射/产物，不手拷活跃SQLite/WAL或承诺活进程迁移 |

官方实现证据：[loaded thread resume](https://github.com/openai/codex/blob/d138398cef7d49e46fe33f1d0ddbd2ca0ed16aa4/codex-rs/app-server/src/request_processors/thread_lifecycle.rs#L719)、[pending request replay](https://github.com/openai/codex/blob/d138398cef7d49e46fe33f1d0ddbd2ca0ed16aa4/codex-rs/app-server/src/request_processors/thread_lifecycle.rs#L939)、[thread resume与store读取](https://github.com/openai/codex/blob/d138398cef7d49e46fe33f1d0ddbd2ca0ed16aa4/codex-rs/app-server/src/request_processors/thread_processor.rs#L3894)。当前还有针对指定远程mobile client的[MCP/image响应redaction](https://github.com/openai/codex/blob/d138398cef7d49e46fe33f1d0ddbd2ca0ed16aa4/codex-rs/app-server/src/request_processors/thread_resume_redaction.rs)，说明“同thread”也不能笼统等同各客户端显示字节完全一致。

官方 [worktrees](https://developers.openai.com/codex/app/worktrees) 描述的worktree/环境能力不能替代Cylo自身资源保留策略。项目文件通过Git同步、把摘要贴入新会话、导入JSON文本或复制聊天消息，均只能标“上下文重建/导入”，**不是同一原生thread接续**。官方 App 的 host handoff 是另一个明确文档化的迁移机制（见5.3）；复制会话store的实验不能冒充该机制或升级为受支持同步。

### 5.2 可审查的原生交接协议（设计建议，尚未实现）

1. `requestHandoff(taskId, targetClient)`：控制服务阻止新派发；保存消息、task revision、threadId、turnId、hostId、CODEX_HOME标识、repo/worktree与dirty manifest。若仍在执行，等待terminal turn或请求interrupt并收到确认；仅数据库标pause不够。
2. `sealHandoff`：确认无活跃工具/审批/未知副作用；保存原始history游标与产物。存在pending审批时，绑定原request和有效期限；无法安全交接则保留阻塞，不自动作批准。
3. `openNative`：CLI 存活时附着原 tmux；退出后提供精确官方 CLI resume 命令。用户操作期间 Cylo/CAO停止自动输入和回收。
4. `reconcileExternal`：Cylo重新list/read/paginate，读取更新turn/items、Git与文件变化、原生状态；验证仍同thread与worktree后收回写租约。官方工具中新增工作记为外部执行记录；Cylo业务任务是否完成仍需验收。
5. 发现原thread丢失、目录已删、工具不可用或并发写入，进入`needs_reconciliation`；只能提供明确标记的重建选项，不悄悄新建thread伪装接续。

### 5.3 历史能力分级（当前仅CLI，B/C/E及App相关描述为范围外证据）

**接续可以实现。优先做“同一个执行环境、轮流接管”：Cylo 先做，人随时转入官方 Codex继续，退出后交回Cylo。** 同机Linux文字闭环已有P5证据；可靠产品还需围绕原生接口实现少量但关键的交接控制。难度不在复制聊天，而在明确谁能写、如何保住工作目录、如何核对未知工具结果。

| 级别与用户体验 | 当前证据等级 | 可交付形态及条件 | 有条件的难度判断 |
|---|---|---|---|
| **A：Linux原生CLI轮流接管**。Cylo任务页点“交给Codex”，出现同thread的`codex resume <id>`命令；继续追问后退出，再点“交回Cylo”，原记录和后续工作接上 | **已实测**P5：本机官方交互CLI+app-server，假provider三轮文字闭环 | 可先做可靠基础版；同host/store/worktree、无同时写入；新输入落盘后才显示“可恢复” | 基础路径较低；加异常恢复、进程互斥、真实工具验收后中等。原生resume已有，无需重做会话引擎 |
| **B：Mac本地官方App同会话打开**。任务页点“在Codex中打开”，官方App打开原chat；完成后交回 | **官方入口明确，需Mac真机**：`/app`与`codex://threads/<id>` | App已安装/运行；Cylo与App实际访问相同会话store/执行环境且该source可发现；按钮仅在兼容版本验证后开放 | 条件成立则是入口与回收适配；若App隔离第三方store/daemon，则需调整集成部署，不能承诺仅加深链即可 |
| **C：Mac App远程继续Linux后台任务**。Mac App先添加Linux SSH host和远端项目，然后在该host打开原会话；文件和命令继续留在Linux | **官方SSH路径明确；Cylo会话互通需Mac真机** | Mac可SSH；Linux已安装/认证Codex且login-shell PATH可找到；App通过SSH管理远端app-server。须验同远端用户/store、source与daemon协同；无需搬迁代码/聊天来远程访问原host | 官方连接设置较直接；Cylo后台与App共享/交替使用运行端、发现原thread及控制权核验是中等集成工作；不能假定App会连接任意Cylo WS监听器 |
| **D：Mac终端远程CLI作为直接入口**。Mac终端用`codex resume <id> --remote <endpoint>`接到Linux同会话 | **官方文档+本机help；未实测远程** | Linux app-server，SSH转发或认证/TLS，匹配版本；执行继续在Linux，非官方App专用入口 | 在A基础上增加传输/认证与连接恢复；比跨机迁移少一层文件与环境转移 |
| **E：把执行位置从Linux搬回Mac，之后再搬回**。官方App chat footer选择run location→目的host→Hand off | **官方host handoff明确；未实测** | 两端已连接、保存同repo project（同子目录），App转chat+Git state并建/复用worktree；Cylo需验迁移后thread/host映射、未跟踪产物与工具依赖 | 高于远程访问；应复用官方流程，先验完整性再纳入Cylo。文档未保证技术threadId恒定、任意非Git产物或工具进程迁移 |
| **F：两端同时写、不断工具进程跨机瞬移、任意账号/任意store自动同步** | **不能承诺** | 当前方案不提供此语义；需要独立且尚未建立的并发/迁移保证 | 不列为“加一个按钮”的后续小功能 |

A的第一版完整操作应是：

1. 用户在Cylo点“交给Codex”。Cylo停止派发新turn，等待当前turn结束或interrupt完成，核对后台工具；显示会话已暂停、执行host和工作目录。
2. 控制服务封存checkpoint：原生thread/turn/history游标、任务验收目标、权限配置、Git HEAD/分支/dirty patch、未跟踪文件和附件引用。checkpoint不是内存/进程快照；有未知副作用须显示待核对。
3. 用户通过原生入口继续。基础版给精确CLI命令；Mac版验收后提供App深链。Cylo显示“由Codex接管”，不再发turn/自动重试，任务资源与worktree继续保留。
4. 用户退出原生工具并点“交回Cylo”。运行端确认无另一写进程，重新读原生turn/items与文件变化；Cylo展示新增执行/产物，随后恢复调度。用户不必重贴聊天，任务验收仍由Cylo判断。
5. App自行修改了权限、项目路径或迁移host时，先做配置差异核对；原请求的批准不能自动复用。pending审批要保持原绑定且确认仍有效，进程已退出时不得假装审批/工具仍活着。

Cylo数据库租约约束不了用户绕过Cylo启动的独立官方CLI。基础产品应采用明确“退出再收回”的流程，并由执行端锁/进程核验合作；发现无法排除并发时阻止恢复派发。不能用UI上一个“已交接”标签充当强制互斥。

### 5.4 最新官方App/SSH/迁移入口证据（2026-10-21实际读取）

- [CLI reference：`/app`](https://developers.openai.com/codex/cli/reference#continue-in-the-desktop-app-with-app)：原文“On macOS and Windows, type `/app` to open the current session…”及“Expected: The desktop app opens the same saved chat so you can continue there.” App须安装或运行；这是同chat接续的官方声明，并非Cylo第三方source实测。
- [App commands/deep links](https://developers.openai.com/codex/reference/commands#deep-links)：原文“keeps the `codex://` URL scheme for compatibility”；`codex://threads/<thread-id>`明确为**local chat**。`codex app <path>`及`codex://threads/new?...`打开项目/新chat，不能替代同thread深链。官方有`codex://settings/connections/ssh/add?name=<ssh-config-host>`，未在该页给远端thread深链参数，不编造`hostId`参数。
- [Remote connections：SSH](https://learn.chatgpt.com/docs/remote-connections#connect-to-an-ssh-host)：原文“The app starts the remote Codex app server through SSH, using the remote user's login shell.” 设置顺序：本机`~/.ssh/config`具体host alias→确认`ssh devbox`→远端安装认证Codex且PATH可发现→App **Settings > Connections**添加/启用SSH host并选远端project。官方说remote project chats在远端文件系统/shell执行；因此“Mac客户操作Linux任务”有官方路径，**无需先迁移会话到Mac**。但官方文档未确认任意Cylo stdio进程可被App直接接管，仍须验证同store发现与运行端单一控制。
- [Remote connections：host handoff](https://learn.chatgpt.com/docs/remote-connections#hand-off-a-chat-between-hosts)：原文“Handoff moves an existing chat and its Git state between your local computer and a connected remote host.” 两端保存匹配repo project（项目是子目录则相同子目录），chat footer选择当前run location→destination→review destination/branch→**Hand off**。App建/复用目的worktree，转移chat+Git state；“If the chat is running, handoff interrupts the current response before transferring it.” 不能以此承诺运行中子进程、所有未跟踪文件、MCP凭据或技术threadId不变；目的地Codex cloud不支持。
- [Remote connections：移动端与官方relay](https://learn.chatgpt.com/docs/remote-connections)：官方移动端host设置要求macOS/Windows desktop App，同账号/workspace及rollout/admin权限，配对从App开始；SSH远端项目是另一节。CLI `remote-control`/`pair`文档明确其服务managed clients与SSH workflows，**不替代第三方app-server协议客户端**。因此不能把“Linux CLI能remote-control”推成Cylo可借用官方relay/用户云权限，更不能把Cloud task/文本导入称为原生同thread。

这些页面通过官方入口的Markdown版本实际获取（`.../cli/reference.md`、`.../reference/commands.md`、`https://learn.chatgpt.com/docs/remote-connections.md`）。官方已提供能力、源码存在机制、Cylo已端到端实测是三种证据，表中不混用。App实际名称当前为ChatGPT desktop app，产品入口文案可继续称“官方Codex”。

## 6. 集成接口与持久化边界

`AgentRuntime`是Cylo内部唯一会话持有者（可由多个执行host上的adapter实现）；Super提出意图，控制服务验证/持久化后执行。长服务不依赖Super会话存活。

| 接口 | 输入/输出 | 必须的语义 |
|---|---|---|
| `capabilities()` | runtime/version/schemaHash、discover/resume/steer/interrupt/approvals/nativeHandoff flags | 逐版本和provider声明；能力缺失显式降级 |
| `discover(filter)` / `inspect(ref,cursor)` | hostId/storeId/threadId、source、cwd、status、turn/items分页 | 无副作用读；不自动resume所有被发现thread，不外泄其他项目 |
| `create(context, permissions, workspace)` / `resume(ref)` | 原生thread/session/执行实例引用 | create响应不等于已durable；只有核实首输入/历史已持久化才能承诺可恢复 |
| `startTurn(runId,idempotencyKey,input)` | turnId + received/applied状态 | app-server未被假定提供业务幂等；发送前持久意图，结果未知时先查询，禁止盲重放 |
| `steer(ref,expectedTurnId,input)` / `interrupt(ref,turnId)` | ack与最终turn状态 | turn不匹配要拒绝；interrupt ack不等于副作用撤销/子进程全停 |
| `respondApproval(binding,decision)` | native request + Cylo decision revision | 权限校验、有效性、去重、原生resolved证据；过期请求不可批准 |
| `events(cursor)` / `reconcile(ref)` | raw eventRef、normalized event、usage与完整性标志 | Cylo独立durable event/outbox编号；原生stream断线不视为可无损续播 |
| `handoff` / `reclaim` / `retainWorkspace` | 所有权状态与产物保留记录 | 不和另一adapter重复持有，不把worktree当临时目录自动销毁 |

建议持久记录 `taskId/runId/attemptId ↔ hostId/runtimeVersion/storeId/threadId/sessionId/turnId/worktreeId`，另存写租约epoch、提交/补丁/未跟踪产物、审批绑定、原生history游标、已观察工具结果、预算估算与账单用量来源。Cylo任务/权限/已确认决定是权威；Codex线程历史是原生执行记录；memory summary只是可追溯派生材料。令牌统计可能延迟，不能把usage通知当精确硬上限；达到预算要停止新派发并处理已在途执行。

## 7. 隔离PoC、实际结果和复现

环境：Debian 12 aarch64，Python3，git，`codex-cli 0.163.0`；没有tmux，没有macOS App。所有repo/worktree/CODEX_HOME均为`/tmp/cylo-research-*`；未改用户会话，未部署。以下脚本为研究复现，不是Cylo runtime。

### P1 空thread不等于已保存

使用隔离CODEX_HOME，经stdio发送initialize/initialized → thread/start `{cwd,approvalPolicy:"never",sandbox:"read-only",ephemeral:false}` → thread/list → terminate/wait → 新app-server initialize → thread/resume。

实际：返回threadId和JSONL path，但list为空；重启resume返回`-32600: no rollout found for thread id …`。**仅拿到thread/start成功不能对用户承诺“会话已持久化”。** 未跑首turn时应允许Cylo保存“待初始化”的权威任务。

### P2 首输入持久化、进程恢复与history读取

首轮探索未覆盖provider，空CODEX_HOME的turn请求意外尝试官方Responses endpoint并获401，无成功模型输出/工具执行。随后改为明确离线provider `base_url=http://127.0.0.1:1/v1`、无auth依赖，所有有效持久化结论以此隔离重跑为准，不能称已通过真实模型链路。

复跑实际threadId `01a15ebd-e082-7d12-9fd7-a68d7db6eaee`；在首userMessage后终止进程并等待退出，再启动resume成功；`thread/turns/list itemsView=full`返回同turn `01a15ebd-e08a-7ec1-b3e3-f71742ec10de`、同userMessage、status=`interrupted`；cwd、Git身份和threadId保留。此为进程终止恢复，不是断电/磁盘损坏实验。未生成模型回复；没有验证工具副作用重放。

### P3 官方CLI原生显示历史

在P2同一CODEX_HOME上执行：

```bash
CODEX_HOME=/tmp/cylo-research-poc-kk7bvmi8/codex-home codex resume 01a15ebd-e082-7d12-9fd7-a68d7db6eaee --no-daemon --no-alt-screen -C /tmp/cylo-research-poc-kk7bvmi8/repo
```

Python pty设40行×120列、响应终端位置查询，5秒后停止，无新增模型请求。实际屏幕含：

```text
OpenAI Codex (v0.163.0)
› Cylo isolated offline persistence probe; no external operation requested.
■ Conversation interrupted - use /feedback if something went wrong
```

`history_visible=True`。首次PTY未设winsize导致每字一行，字符串检查False；修正winsize后重跑通过。这次P3只证明app-server→Linux CLI原生读取；后续P5另行验证反向新turn文字闭环，多工具/审批、MacApp仍未通过。

### P4 CAO worktree teardown

直接import上游worktree_service（仅stdlib，不安装CAO、不启动provider），在独立空git repo创建`pocworker` worktree、写`uncommitted.txt`，调用`remove_worktree`。输出：

```text
worktree-before True
worktree-after False
```

worktree目录被删除；该分支只有基础提交，安全`branch -d`也成功。本次不测试有新提交分支；源码明确该情况安全`-d`失败并保留分支。结论限定为未提交/未跟踪文件丢失。

### 可独立重跑的P1/P2/P4核心脚本

规范复现脚本为 [`evidence/orchestration-poc.py`](evidence/orchestration-poc.py)，执行 `python3 docs/research/evidence/orchestration-poc.py`。默认使用不可达loopback假provider，并移除继承的OpenAI/Codex凭据和配置覆盖；源码路径来自第2节clone。P1/P2分别使用独立临时store/repo，脚本输出其路径，便于P3接续；只删除自己创建的临时worktree。

核心脚本已从报告提取到上述单一文件，避免两份代码漂移。初次从报告直接提取复跑，以及最终独立脚本复跑均核对：空thread恢复失败、含首输入thread恢复成功且turn为interrupted、临时worktree被清理。

P3的PTY核心复现（把前三个变量换成上个脚本`CASE True`输出；以下实际执行过的值保留供核对）：

```python
import fcntl, os, pathlib, pty, select, struct, subprocess, termios, time
home = "/tmp/cylo-research-poc-kk7bvmi8/codex-home"
repo = "/tmp/cylo-research-poc-kk7bvmi8/repo"
tid = "01a15ebd-e082-7d12-9fd7-a68d7db6eaee"
# 仅在隔离临时CODEX_HOME写配置；不得指向用户真实配置。
pathlib.Path(home + "/config.toml").write_text(
    'model_provider="cylo_offline"\n[model_providers.cylo_offline]\n'
    'name="Cylo offline"\nbase_url="http://127.0.0.1:1/v1"\n'
    'wire_api="responses"\nrequires_openai_auth=false\n'
    '[projects."' + repo + '"]\ntrust_level="trusted"\n')
env = os.environ.copy()
env.update(CODEX_HOME=home, TERM="xterm-256color")
master, slave = pty.openpty()
fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
p = subprocess.Popen(["codex", "resume", tid, "--no-daemon", "--no-alt-screen",
    "-C", repo], stdin=slave, stdout=slave, stderr=slave, env=env)
os.close(slave)
output = b""
end = time.monotonic() + 5
while time.monotonic() < end:
    if select.select([master], [], [], .2)[0]:
        try:
            part = os.read(master, 65536)
        except OSError:
            break
        output += part
        if b"\x1b[6n" in part:
            os.write(master, b"\x1b[1;1R")
p.terminate()
p.wait(timeout=5)
os.close(master)
print("history_visible=", b"Cylo isolated offline persistence probe" in output)
print("interrupted_visible=", b"Conversation interrupted" in output)
```

**未运行事项与原因：** 完整CAO/tmux fleet（tmux未安装，已有直接源码与隔离worktree证据，未为选型安装整套依赖）；真正模型成功turn/命令/MCP/审批/跨provider（没有必要花费真实模型或操作用户环境，不能据离线实验推定通过）；SIGKILL/断电与SQLite一致性（本次仅terminate，未做掉电测试）；MacApp双向原生互通（无Mac/App）；远程CLI网络端到端（仅文档/help）；CLI新turn反向回Cylo（本次只显示既存原生历史）。其中 CLI 真实工具/审批与 CAO 互斥仍是后续验收；官方桌面 App 及其 host handoff 不属于当前范围；跨环境数据迁移仍按04第13节与01的CY-04-03验收，区别于SSH连接同一执行host。CLI 反向文字闭环随后已由 P5 补验，仍不能伪写 CAO 已支持。

## 8. 风险与Go/No-Go门槛

| 风险 | 影响 | Go条件 / No-Go触发 |
|---|---|---|
| 原生工具双写或另一进程仍运行 | 覆盖代码、重复操作 | 同daemon或可证明的执行端锁/独占交接；并发发现后拒绝写；无法协调就No-Go于自动接管 |
| thread/start未落盘 | 接收成功却无法恢复 | create标记初始化中；首输入持久化确认；杀进程后按映射可恢复；无证据不得标“已保存” |
| WS/命令实验状态与版本漂移 | 兼容中断、无生产支持 | 固定发布版本+schema，start/resume/steer/interrupt/审批/分页回归通过，明确接受支持风险；不满足可靠性要求则No-Go生产依赖 |
| 订阅鉴权用于托管商业 | 不符合官方app-server authentication范围 | 本地模式与托管产品认证隔离；托管完成适用SIWC/API方案可用性及条款核验，否则No-Go共享订阅token路线 |
| CAO默认yolo与提示约束 | 越权写/网络/外部操作 | 禁默认unrestricted路径、实际沙箱/容器与权限审计测试；若无法逐操作权限和审批闭环则No-Go主路径 |
| CAO worktree自动force清理 | 成果丢失 | 停止运行与产物保留确认后才显式清理；在未改造前不让CAO拥有Cylo关键worktree生命周期 |
| 断流/回调重复 | 虚假完成、重复外部副作用 | durable inbox/outbox、原生turn/result核验、结果未知进入人工/补偿路径；自然语言完成不够 |
| 工具与审批在交接后失配 | 历史在但无法继续/错误批准 | 相同工具配置与权限manifest；原生审批重新校验；不持久复用陈旧requestId；涵盖命令/file/MCP/拒绝/超时 |
| 跨主机迁移工作区或store | 文件、路径、WAL、凭据失配 | 优先连接同执行host；搬迁必须停写、一致备份、文件与环境验证；不支持热复制当同步 |
| token/运行预算延迟 | 超额成本/空转 | 控制服务硬限制新派发、并发与工具预算；记录缓存/输入/输出/重试/Super开销，不能仅靠模型自觉 |

建议核验门槛清单：

1. **可继续调研/隔离开发（Go）**：优先路线A CAO CLI/tmux 增强，app-server为按需路线B；Cylo持有业务权威，单个run仅一个执行owner。版本与源码及离线原生恢复证据已有。
2. **Codex CLI“原生双向接续”产品承诺（尚未Go）**：通过07四组验收，涵盖真CAO/tmux互斥、精确thread恢复、真实shell/file/MCP与批准/拒绝、停止/重启/未知结果。P5文字闭环不替代这些；原生steer仅路线B承诺时必测。
3. **跨环境迁移与部署验收**：迁移仍为完整产品要求，按04第13节验证停写、一致备份、目录/凭据重映射与旧host停止；区别于remote attach。官方App host handoff不在范围，托管鉴权按实际部署单独核验。

本选型按用户最新要求排除官方桌面 App 支持，不把未验证 CLI 能力降为文本摘要“接续”。它明确目前能证明什么、缺哪些真实环境证据，以及哪一层应承担补足责任。

### P5 同thread双向交接三轮文字闭环（新增）

可复现脚本：[`evidence/handoff-poc.py`](evidence/handoff-poc.py)，执行`python3 docs/research/evidence/handoff-poc.py`。无需上游源码clone或额外Python依赖，仅需本机官方Codex二进制。每次新建临时CODEX_HOME/repo、清除继承OpenAI/Codex凭据与覆盖，只用`127.0.0.1`的fake Responses SSE provider；没有登录用户真实账号，没有真实模型、工具或审批调用。

实际步骤：app-server建立thread→发`CYLO_FIRST_USER_MARKER`完成第一轮→终止并wait→PTY启动**官方交互CLI** `codex resume <id> CLI_SECOND_USER_MARKER --no-daemon --no-alt-screen -C <repo>`→CLI显示第一轮原记录并完成第二轮→退出CLI→新app-server同ID resume/read两轮→发`CYLO_THIRD_USER_MARKER`完成第三轮→读取历史，断言三轮均completed、三个用户标记与三条fake回复全在。每次所有权交替，未同时控制同一thread。

最终结果与历史快照：[`evidence/handoff-poc-result.json`](evidence/handoff-poc-result.json)。最终复跑thread `01a15f02-4eeb-7d81-a74a-2a99331921be`，运行端保留证据`/tmp/cylo-handoff-poc-9rq0uxgb`（可随环境清理消失）；3 completed turns、4次loopback provider请求（含CLI辅助请求，不能写成3次真实模型调用）。结果快照存三轮items/status及实际版本，脚本保留完整events/requests/PTY日志在各次临时目录。

第一次新脚本运行因假设第N个HTTP请求必是第N轮而断言第三条fake回复失败；CLI辅助请求使编号偏移。已修正为按用户标记选择fake回复，并验证真实原生history，随后上述完整运行通过；未重写旧P1/P2/P3证据。之后只加三轮completed显式断言及异常进程清理，最终复跑结果见快照。此PoC验证**原生会话持久化/读写与顺序交接**，不验证真实推理、shell/MCP、图片附件、权限批准、工具未知结果恢复、MacApp或远程连接。
