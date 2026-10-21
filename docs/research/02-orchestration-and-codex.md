# Cylo 编排与 Codex 原生接续调研

访问/实测日期：2026-10-21。输入：旧 `codex-super-agent-requirements-and-architecture.md`、新版 `Rumi-完整产品需求说明书.md`（产品统一称 Cylo）。本文是选型证据，不是运行时实现或生产就绪声明。需求编号沿用需求报告 `CY-05-01…06`、`CY-06-*`、`CY-08-*`、`CY-14-*`、`CY-15-*`。

## 1. 决策

**建议 Codex 主路径采用 Cylo 控制平面 + 单一 Codex Runtime adapter + 官方 app-server；不采用 CAO 作为 Cylo 的总调度器或 Codex 主运行时。** Super 是按事件调用的规划/协调者，Worker 可直接执行有明确边界的专业工作，常驻服务承担队列、健康检查、预算和通知。Cylo 数据库持有权威任务/权限/服务状态，Codex 持有原生会话；两者通过稳定关联连接。

CAO 当前版本已远超过“tmux + 简单 supervisor”原型，包含数据库、工作流恢复、计划审批、Web UI、多后端与记忆，不能称其没有持久化。但 Codex adapter 仍依赖 TUI 抓屏/正则，默认 `--yolo`，交互审批存在明确限制，终端删除会强制清理 worktree。重用其整套控制面会制造两套 Super、工作流和任务权威。仅当将来确实需要其他 CLI provider 时，再把经过限权与生命周期改造的 CAO 当成可选 fleet adapter，且单个运行实例只允许一种会话持有者。

**原生接续结论必须分层：** Linux 同主机、同 CODEX_HOME 的“app-server 创建带首条输入的持久会话 → 官方 CLI resume 显示原始历史”已隔离实测；app-server 自身重启恢复也已实测。macOS 官方 App 的发现/双向接续、真实工具与审批完整性、跨主机 App 接管均未实测，不能宣称第 5 章全部满足。官方 CLI `--remote` 现在确有文档与本机 help 支持；它是连接同一个远端执行环境，不是将代码、进程和会话任意同步到另一台主机。

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
| 原生接续 | Codex provider运行官方CLI但未建立Cylo任务→官方threadId的可靠显式映射，创建命令非resume | CLI自身可能写原生日志；不能因此保证CAO重建/官方App发现/双向关联完整 |

CAO 的真实复用价值是多家 CLI 终端适配与协作工具、可观察界面、工作流经验。放弃其 Codex 主路径是因原生结构化协议更贴近第 5 章与可靠性要求，不是因为仓库“不活跃”或“没有持久化”。

## 4. app-server、Agents SDK 与适用替代比较

官方文档已实际读取：[app-server](https://developers.openai.com/codex/app-server)、[Codex SDK](https://developers.openai.com/codex/sdk)、[Agents runtime 对比](https://developers.openai.com/api/docs/guides/agents)、[Agents SDK](https://developers.openai.com/api/docs/guides/agents/sdk)、[认证](https://developers.openai.com/codex/auth)。

| 选项 | 会话/审批/工具与持久化边界 | 结论 |
|---|---|---|
| Codex app-server | JSONL stdio、WS、Unix socket；thread/start/list/read/resume、turn/start/steer/interrupt、items与usage、server-initiated审批。官方持久历史属于Codex；任务、预算、通知、幂等仍由Cylo负责 | **首选Codex adapter协议**；固定二进制+生成schema，做兼容测试。官方仍将命令和WS标experimental/unsupported production，须接受明确技术风险，不能写成熟稳定SLA |
| OpenAI Agents SDK | 运行应用自己的agent loop、handoffs/tools/guardrails/审批与session存储；[SQLiteSession源码](https://github.com/openai/openai-agents-python/blob/64cd5dfc5a1579870dcbed0ea42b27aa84033fee/src/agents/memory/sqlite_session.py)、[run_state](https://github.com/openai/openai-agents-python/blob/64cd5dfc5a1579870dcbed0ea42b27aa84033fee/src/agents/run_state.py) | 可选用于Super/非Codex Worker；不能把SDK session当作官方Codex thread，也不应再包一层对每个Codex工具步骤做LLM协调。SDK本身不是任务库/队列/后台daemon |
| **适用替代1：官方Codex SDK** | TS当前源码通过`codex exec --experimental-json`和`resume <threadId>`；Python当前官方文档称稳定，底层app-server并附带固定runtime。不同语言绑定能力不能混为一谈 | Python绑定若覆盖所需审批/steer/history可降低协议维护成本；仍须验收，不改变持久化/互斥责任。TS适合批任务，不能仅凭resumeThread推断完整审批桥接 |
| CAO | 自己的session/terminal/workflow/memory体系 + TUI CLI adapter | 仅可选隔离fleet后端；不与app-server共同控制同一Codex运行；不作为总控 |

Codex SDK源码证据：[TS exec封装](https://github.com/openai/codex/blob/d138398cef7d49e46fe33f1d0ddbd2ca0ed16aa4/sdk/typescript/src/exec.ts#L95)、[Python客户端](https://github.com/openai/codex/blob/d138398cef7d49e46fe33f1d0ddbd2ca0ed16aa4/sdk/python/src/openai_codex/client.py)、[Python包元数据](https://github.com/openai/codex/blob/d138398cef7d49e46fe33f1d0ddbd2ca0ed16aa4/sdk/python/pyproject.toml)。未引入第二个替代项目：现有证据已足以决定Codex路径，继续扩大候选会增加维护面而无助于解决官方App互通缺口。

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
| Mac官方App→Cylo及反向 | 官方App文档已读；旧app入口目前重定向到ChatGPT desktop app，不能以旧名/旧截图当现有承诺 | 无macOS硬件/App，**双向均未验证**；共享本地CODEX_HOME是候选条件，不是官方端到端保证 | 真实App版本、source过滤、相同账户/store、App管理worktree/项目注册及恢复工具依赖必须实测 |
| Linux官方CLI→Cylo及反向 | 官方CLI resume与app-server原生store API存在 | app-server→CLI显示原消息已验证；CLI新turn再回Cylo未跑，不把协议存在标为双向全通过 | 增补CLI写新turn→Cylo发现/读取/继续，验证工具与审批策略继承 |
| 跨主机 | 官方CLI可`--remote`连接远端app-server；应配置WS认证+TLS，localhost/SSH转发有文档 | 本次仅help/官方文档，未实际远程连接；Mac App远端接续未证实 | 首选同远端执行环境访问，避免复制正在运行的SQLite/WAL/store；迁移需停写、一致备份、路径/文件/依赖验证，不承诺任意主机无缝迁移 |

官方实现证据：[loaded thread resume](https://github.com/openai/codex/blob/d138398cef7d49e46fe33f1d0ddbd2ca0ed16aa4/codex-rs/app-server/src/request_processors/thread_lifecycle.rs#L719)、[pending request replay](https://github.com/openai/codex/blob/d138398cef7d49e46fe33f1d0ddbd2ca0ed16aa4/codex-rs/app-server/src/request_processors/thread_lifecycle.rs#L939)、[thread resume与store读取](https://github.com/openai/codex/blob/d138398cef7d49e46fe33f1d0ddbd2ca0ed16aa4/codex-rs/app-server/src/request_processors/thread_processor.rs#L3894)。当前还有针对指定远程mobile client的[MCP/image响应redaction](https://github.com/openai/codex/blob/d138398cef7d49e46fe33f1d0ddbd2ca0ed16aa4/codex-rs/app-server/src/request_processors/thread_resume_redaction.rs)，说明“同thread”也不能笼统等同各客户端显示字节完全一致。

官方 [worktrees](https://developers.openai.com/codex/app/worktrees) 描述的worktree/环境能力不能替代Cylo自身资源保留策略。项目文件通过Git同步、把摘要贴入新会话、导入JSON文本或复制聊天消息，均只能标“上下文重建/导入”，**不是同一原生thread接续**。复制会话store的实验也不能擅自升级为受支持跨主机同步。

### 5.2 可审查的原生交接协议（设计建议，尚未实现）

1. `requestHandoff(taskId, targetClient)`：控制服务阻止新派发；保存消息、task revision、threadId、turnId、hostId、CODEX_HOME标识、repo/worktree与dirty manifest。若仍在执行，等待terminal turn或请求interrupt并收到确认；仅数据库标pause不够。
2. `sealHandoff`：确认无活跃工具/审批/未知副作用；保存原始history游标与产物。存在pending审批时，绑定原request和有效期限；无法安全交接则保留阻塞，不自动作批准。
3. `openNative`：提供可复制的官方CLI resume命令；Mac App只有实测通过的发现/深链方式才显示为可用。允许用户自己使用官方工具；Cylo停止写入。
4. `reconcileExternal`：Cylo重新list/read/paginate，读取更新turn/items、Git与文件变化、原生状态；验证仍同thread与worktree后收回写租约。官方工具中新增工作记为外部执行记录；Cylo业务任务是否完成仍需验收。
5. 发现原thread丢失、目录已删、工具不可用或并发写入，进入`needs_reconciliation`；只能提供明确标记的重建选项，不悄悄新建thread伪装接续。

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

`history_visible=True`。首次PTY未设winsize导致每字一行，字符串检查False；修正winsize后重跑通过。这只证明app-server→Linux CLI的原生会话读取，反向CLI新turn→Cylo、多工具/审批、MacApp未由此通过。

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

**未运行事项与原因：** 完整CAO/tmux fleet（tmux未安装，已有直接源码与隔离worktree证据，未为选型安装整套依赖）；真正模型成功turn/命令/MCP/审批/跨provider（没有必要花费真实模型或操作用户环境，不能据离线实验推定通过）；SIGKILL/断电与SQLite一致性（本次仅terminate，未做掉电测试）；MacApp双向原生互通（无Mac/App）；远程CLI网络端到端（仅文档/help）；CLI新turn反向回Cylo（本次只显示既存原生历史）。这些属于下一步必须有设备/凭据/场景后才能完成的技术验收，不应伪写“已支持”。

## 8. 风险与Go/No-Go门槛

| 风险 | 影响 | Go条件 / No-Go触发 |
|---|---|---|
| MacApp未发现第三方thread/项目 | CY-05-02/04不能成立 | 真实受支持App版本上双方各创建一个含工具的thread，另一方能发现同thread并继续；失败则仅声明CLI原生接续，Mac能力不可宣传通过 |
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

1. **可继续调研/隔离开发（Go）**：选择app-server为候选adapter、Cylo为唯一权威；本报告已提供版本与源码，离线恢复基础已通过。
2. **Codex Linux“原生双向接续”产品承诺（尚未Go）**：补成功模型turn和工具记录，CLI新turn回读，拒绝/批准、steer、interrupt、进程崩溃、重启、重复请求与未知结果测试；P2/P3通过不能代替这些。
3. **Mac官方App互通承诺（No-Go，直到实机通过）**：同主机/同store双方各发起、发现、接续至少两轮；项目身份、worktree、dirty文件、工具历史与权限都核对，且Cylo不可用时仍能独立官方恢复。
4. **跨主机/托管承诺（No-Go，直到专门验证）**：区分remote attach和迁移；认证、传输支持风险、凭据隔离、host路由与任务权限均通过。官方CLI remote不自动给官方App远程互通保证。

本选型不缩减新版完整需求，也不把未验证能力降为文本摘要“接续”。它明确目前能证明什么、缺哪些真实环境证据，以及哪一层应承担补足责任。
