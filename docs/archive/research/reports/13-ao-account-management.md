# AO 的 Codex 多账号管理与自动账号池边界

核验日期：2026-10-21。AO 指 **OrchestratorInc/agent-orchestrator（Go）**，不是 AWS CAO。用户已选 AO，本报告只判断账号能力与缺口，不重新选编排器。

## 结论

**AO 已有真实的多 ChatGPT/Codex 订阅账号管理，可以保存多个登录、设置新会话默认账号、给不同 Worker 绑定账号，并让已有会话显式切到另一个账号。不能回答“AO 没有账号池，所以必须装 CLIProxyAPI”。**

但如果“账号池”要求 **额度耗尽/429 时自动挑另一个可用账号，继续任务**，此固定版本的 AO **还不够**。现成的是账号目录、额度展示、会话绑定与切号执行机制；未发现跨账号自动筛选、轮询/负载均衡、耗尽冷却和自动故障转移调度器。其“自动恢复”是在用户对某条失败消息开启后，等**原绑定账号**额度重置再重试，不能称为自动换号。

建议：先复用 AO 的原生账号能力。需要自动轮换时，比较“Cylo 在 AO 上补账号调度”与“Codex 请求走 CLIProxyAPI 的请求级池”；后者是可选上游网关，不能直接替代 AO 的会话/任务恢复。CLIProxyAPI 具体源码审查见独立报告，本报告不声称已验收它的接入。

## 固定版本、范围和证据方式

- 实际读取本地 `/tmp/cylo-alt-ao`，HEAD：`7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff`；提交时间 `2026-10-21T21:11:36+05:30`，主题 `feat: switch agents mid-session in chat and terminal (#6588)`。与 [09 选型证据](../../../research/09-agent-orchestration-alternatives.md) 相同，不以移动的 main 代替它。
- 阅读 [产品需求](../../../product-requirements.md) 与 09；仓库及其适用祖先未发现 AGENTS.md。本报告是独立 worktree 中的文档变更。
- 对 backend、frontend 进行静态调用链核查：账号 API → service → session manager → 原生 Codex TUI/app-server；同时检查额度恢复与相关测试源码。未读取用户凭据，未执行登录、真实模型或账号请求，未修改 AO 源码。
- 本轮没有运行 AO 测试。当前 shell 的 `go` 不在 PATH；AO `backend/go.mod` 要求 Go `1.27.1`。下文测试名称只作为实现意图与覆盖范围的源码证据，不能当作本轮测试通过或真实订阅账号验收。

以下 AO 链接全部固定到上述 SHA。

## 能力逐项核验

| 问题 | 固定源码结论 | 主要证据 |
|---|---|---|
| 是否保存多个 ChatGPT 账号 | 是。`AgentAccount` 明确是 CLI 的订阅登录，provider 为 Claude/Codex，账号有独立 home；不是不同模型名或 AO 用户登录 | [账号领域模型](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/domain/agent_account.go) |
| 是否能添加/重新登录/删除 | 是。有登录终端、verify/cancel、重新登录与删除 API；通过 provider 原生 CLI 登录 | [账号 controller](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/httpd/controllers/agent_accounts.go#L58)、[service](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/service/agentaccounts/service.go#L375) |
| 如何选账号 | 设置 provider 的新会话默认账号；会话绑定持久化。内部 `SessionCreateConfig.AgentAccountID` 支持指定，HTTP 有对现有 session 的显式切号入口。不能推断所有 CLI/UI 创建入口均暴露同样参数 | [创建配置](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/ports/session.go#L53)、[默认账号](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/service/agentaccounts/service.go#L357)、[BindSession](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/service/agentaccounts/service.go#L681) |
| Worker 是否可用不同账号 | 是，绑定按 session/provider 记录，启动环境按绑定生成；改默认值不迁移存活会话。多个独立 Worker 也可以绑同一号，但共享该账号上游额度 | [会话启动环境](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/service/agentaccounts/service.go#L920)、[SQLite store](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/storage/sqlite/store/agent_account_store.go) |
| 是否显示额度/重置时间 | 是。Codex 通过 app-server account 方法读身份、capacity、usage、reset credit；不是 AO 自算剩余额度。能力受本机 Codex 协议版本影响 | [Codex account adapter](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/agentaccount/codex.go#L94)、[capability probe](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/chatdriver/codexappserver/account.go#L125) |
| 耗尽/429 自动选下一账号 | 未发现。默认账号不可用时拒绝启动，绑定账号读额度时也不 fallback；没有“收到任意429就轮换”的调用链 | [默认拒绝](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/service/agentaccounts/service.go#L701)、[仅绑定账号额度](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/service/agentaccounts/service.go#L813) |
| 已有会话切号是否同 thread | 执行设计是同 native conversation ID，先检查目标能看到历史，再 drain/interrupt、停止原 owner、目标 resume、成功后提交绑定，失败回滚 | [StartAccountTransition](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/session_manager/agent_account.go#L13) |
| TUI / Chat 都能切号吗 | 是，transition 保持原会话模式；TUI 原生进程与 Chat app-server 都接 session account 环境。额度定时自动重试则仅 Chat | [TUI/Chat 测试源码](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/session_manager/agent_account_test.go#L114)、[Chat host 启动](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/chatdriver/codexappserver/driver.go#L569) |

## 两套账号表面必须区分

此 SHA 同时存在两组 API，不能合并描述成“一个全局活动账号决定全部 Worker”：

1. **`/api/v1/agents/accounts`：会话账号目录。** 覆盖 Claude/Codex，账号 home 位于 AO state dir 的 `agent-accounts/<provider>/<id>`；提供设置默认与 `PUT /agents/accounts/sessions/{sessionId}` 会话切号。daemon 实际构造 `agentaccounts.Service` 并接到 session manager 与 Chat 额度读取，见 [daemon wiring](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/daemon/daemon.go#L523)、[构造函数](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/daemon/daemon.go#L1366)。
2. **`/api/v1/agents/codex/accounts` 与 `.../account-switches`：Codex managed credential slots / device-global credential 切换。** 有导入/核对、登录、capacity、SSE、持久 switch journal、idempotency 与全局 admission gate。其 coordinator 明确写明：**existing controllers outside transaction，只改变并验证 device credential**。所以成功的 global switch 不代表所有存活 Worker 已切号。全局切换要求 file-backed credentials，遇 keyring 等不支持情况会返回 typed error。见 [controller](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/httpd/controllers/codex_accounts.go#L43)、[coordinator](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/service/agent/codex_account_switch_coordinator.go#L121)。

它们之外的 AO 云账号/设备配对、daemon API 密码鉴权、GitHub/GitLab OAuth/MCP OAuth、模型 provider 配置均有自己的职责，不计入 ChatGPT 上游账号池。原生账号功能的证据来自真实 credential home 与账号生命周期，而非字段恰好叫 account/provider。

## 登录与 CODEX_HOME：凭据隔离、历史共享

会话账号 adapter 的 Codex 登录命令是 `codex login --device-auth`，可在浏览器选择 ChatGPT 账号；`Identity` 只有 app-server 报告授权且 `Method == chatgpt` 才认为是订阅登录。其 `LaunchEnv` 设置独立 `CODEX_HOME`，并在 daemon 存在这些变量时清空 `OPENAI_API_KEY`、`CODEX_API_KEY`、`CODEX_ACCESS_TOKEN`，避免宿主机的 API key 覆盖选中的订阅登录。见 [完整实现](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/agentaccount/codex.go#L42)。

另一组 managed-slot 登录入口的内部 `ao codex-login` 菜单支持 ChatGPT 浏览器、device auth、API key、access token，并对 managed home 加 `cli_auth_credentials_store="file"`；不能把这一入口支持 API key 推广成“会话订阅账号入口支持任意上游身份”。见 [登录菜单与 argv](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/cli/codex_login.go#L29)。

AO 不是为每号复制一份完全独立 Codex 历史：

| 账号内保留私有 | 链接到 computer/device Codex home |
|---|---|
| `auth.json`、`models_cache.json`、`tmp`、`log`、`memories`、`app-server-control`、`app-server-daemon`、`ipc`、部分全局 UI 状态等 | `sessions`、`archived_sessions`、`sqlite`、`shell_snapshots`、`worktrees`、`skills`、`plugins`、`cache`、`logs`、`mcp-oauth-locks`、`thread-writer-locks`、`config.toml`、`history.jsonl` 等 |

证据：[共享/私有清单与 Prepare](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/agentaccount/codex.go#L15)、[链接与历史迁移规则](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/agentaccount/share.go#L20)。缺共享目标时创建，只有账号一侧有内容时尽量将它移到 shared target；已有非空本地副本不直接删掉。因此“能看见原生历史”仍需 preflight 实证，并非所有已有目录都无条件合并成功。无法建立共享返回 `ErrAgentAccountUnshared`；切号前还会按 native ID 查历史是否存在。

共享历史支持跨账号 `codex resume <native-id>`，私有 daemon 控制目录避免不同账号误附着到同一 app-server。共享 `thread-writer-locks` 配合 session transition，目标是同 conversation 同时只有一个 live writer。不同 Worker/thread 可以并发，但这不提供每个账号的额度预留或公平分配；也不是多租户的数据隔离方案。共享 `config.toml` 还意味着代理/base_url 改动可能同时影响多个账号 home，部署时必须核对实际配置边界。

## 额度耗尽时实际发生什么

[Chat usage_limit.go](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/service/chat/usage_limit.go#L44) 明确限制：

- 对一条失败的 human prompt 开启 `SetTurnAutoResume`，不是 session 全局自动政策；要求 Chat 模式、可恢复失败、已知 `ResetsAt`、有效分支与明确 delivery 记录。
- 到期前不重新发模型请求；到期读取 **绑定账号** 的额度，最多延后已知截止时间。账号变化会终止该次自动恢复意图，不静默改用别号。
- 使用 4 个 recovery workers、30 秒暂时失败退避。这些是重试调度限制，不是账号池并发租约或耗尽账号冷却表。
- 自动恢复复用 durable retry/idempotency 与分支校验；手动 retry、账号迁移、新消息都可能消耗/取消原意图。未知 reset 不会自动猜时间。

相关源码测试：[绑定账号与deadline](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/service/chat/usage_limit_accounts_test.go)、[并发恢复](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/service/chat/usage_limit_concurrency_test.go)。TUI 有额度提示/失败识别，但不能因此声称此 Chat 重试器对 TUI 也生效。

查阅账号选择、switch、quota/429、frontend 的账号 hooks 和额度恢复调用链，未发现自动挑选另一个账号的触发器。故本报告结论是“此固定版本没有找到相应实现”，不是对未来版本的保证，也不是把源码检索的未命中当真实运行验收。

## Worker 与会话恢复的影响

显式切号不是把正在流式输出的请求透明搬到另一登录。AO 会先验证目标账号及历史，在原进程仍运行时做能做的检查；再 drain 当前 turn 或 interrupt，停止原进程，目标账号恢复同 native conversation，成功后提交绑定。失败路径重启旧账号；Chat 被中断的 turn 被标记 `account.switched`，不假装原请求已完成。见 [transition](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/session_manager/agent_account.go#L13)、[中断记录](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/service/chat/account_switch.go#L13)。

恢复同 thread 也不自动证明最后一次 shell、提交、远端写入没有产生副作用。Cylo 需要保留 task/run、最后确认步骤与未知副作用状态，决定是否继续或重试失败 prompt。不能对任意 429 重播整项任务，更不能因切号成功把 run 标为完成。

删除账号会先停止该号运行中的 agents；启动中的会话导致删除拒绝，避免删除其正在读取的 home。账号与会话绑定持久化；账号目录、共享 device history 和 SQLite 都需要同一部署边界备份，只有 worktree 不够恢复。

## macOS / Linux 范围

会话账号 adapter/service 没有仅 macOS 可用的 build tag 或运行时限制，daemon 无条件构造 Codex account provider；其符号链接与权限处理适用于 Linux/macOS 常规文件系统。原生 CLI 必须存在且 app-server protocol 提供所需 account/capacity 方法，设备认证/浏览器交互仍需完成。Linux 无桌面环境可采用 device auth，但本轮没有执行真实登录，不能声称每种部署或 keyring 组合已验证。

全局 managed switch 的 file-store 要求与会话账号的独立 home 是两个条件。旧 Codex 二进制缺 capability、登录策略限制或 home 无法共享都可能失败；AO 有 unknown/unsupported 状态，不应统一显示为“还有额度可用”。

## 每个账号能否配置独立网络代理

**此固定 AO 版本未发现每账号独立网络代理的原生配置。** `AgentAccount` 只有 ID、Provider、Home、Email、Plan、CreatedAt；账号 API 没有 proxy 字段。Codex `LaunchEnv(home)` 只设置 `CODEX_HOME` 与清除凭据 override，不根据账号设置 `HTTP_PROXY`、`HTTPS_PROXY`、`ALL_PROXY` 或 `NO_PROXY`。证据：[数据模型](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/domain/agent_account.go#L41)、[启动环境](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/agentaccount/codex.go#L56)、[账号 API](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/httpd/controllers/agent_accounts.go#L58)。另一组 managed Codex slot 的公开 snapshot/context 也未发现每号 proxy 字段。

AO 支持项目环境变量，原生进程也有宿主环境继承路径；可以按部署/项目给进程配置代理，但这不是“账号 A 固定代理 A，账号 B 固定代理 B”的产品能力，也不能证明身份探测、额度读取、重新登录等辅助进程都跟随该项目代理。[runtimeEnvChecked](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/session_manager/manager.go#L6059) 先组装 project env，再合入账号环境；[account client](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/chatdriver/codexappserver/account.go#L85) 则单独按 home 启动 app-server。不同进程是否采用代理还取决于本机 Codex 对相应环境变量/协议的支持，本轮未实测。

`model_provider` / `base_url` 指模型 API 的目的地址，与 HTTP/SOCKS 网络出站代理是两件事。AO 会话账号默认共享 `config.toml`，所以不能仅凭不同 `CODEX_HOME` 就声称每号拥有独立 provider/base_url；擅自编辑该共享文件可能影响所有号。即使改成不同 endpoint，也不等于登录与 token refresh 都走独立网络出口。

若保留 AO 原生账号，最小改造是给账号持久化受控 proxy 配置，并在启动 TUI、Chat host、登录终端、身份/额度 probe、refresh 相关原生进程时统一注入该账号的代理环境；同时处理大小写变量、NO_PROXY、宿主代理覆盖及代理凭据脱敏。切号前后的辅助检查也应走目标账号代理，而不能只给最后启动的 Worker 加 env。可通过账号上下文扩展 `AgentAccountProviderCLI`/`CodexAccountContext`，不要全局修改 daemon 环境使并发账号互相覆盖。这是待实现方案，AO 当前未提供。

## 如果 Cylo 要自动轮换，补在哪里

| 路线 | 适用目标 | 仍须补充/验收 |
|---|---|---|
| **AO 原生账号 + Cylo 调度** | 保留官方 Codex TUI/Chat、真实 ChatGPT 登录、原生历史和可观察的每 Worker 账号归属 | 账号 allowlist/预算、型号/plan兼容、健康与额度、并发租约、reset冷却、会话粘性、换号次数上限；复用 `StartAccountTransition`，确认同 native ID 后安全继续 |
| **AO 执行 Codex + CLIProxyAPI 请求级池** | 希望多个上游身份由同一个兼容 API 网关选择，多个客户端共用池策略 | 核实当前 Codex 自定义 provider/Responses/stream/tool/模型兼容；验证故障切换与会话粘性、流中失败和重复工具副作用、额度归属观测。AO 的 ChatGPT 账号面板不再等于代理实际用的上游号 |

推荐先做第一条的最小验证：两个隔离测试账号/模拟 account adapter，两个独立 Worker，预设一个账号耗尽；调度器租用可用账号，只对受影响 session 发起持久 transition，检查旧 writer 已停、native ID 保留、未知副作用不自动重放、失败回滚不丢 worktree。再测同号并发耗尽时不会所有 Worker 同时抢同一备用号。真实账号验收属于后续，本文没有执行。

账号调度可以首先作为 Cylo 调用 AO API 的组件，降低修改 AO 核心的范围。若主要需求是代理多种客户端的模型请求，才把 CLIProxyAPI 引入为独立服务；不要让 AO 自身和代理同时无协调地决定同一会话的上游身份。

## 与 Space 网页实时语音池的区别

Space 路线处理网页实时语音登录/实时媒体连接、音频流与打断；AO 此处管理的是 **Codex/Claude CLI 的模型订阅登录和编程会话**。即使两者都出现 ChatGPT 账号，也不能推断 token、credential home、WebRTC session、额度分类、锁与恢复方法通用。

Cylo 应维持两条资源账本：编排 Worker 使用 AO/Codex 账号策略；语音 gateway 使用 Space 的实时语音账号及连接策略。二者交换任务命令和事件。引入 CLIProxyAPI 也不能据此替换 Space 网页实时语音账号池或宣称实现其会话迁移。

## 未验证事项

真实多 ChatGPT 订阅登录、refresh token 并发更新、跨账号恢复时上游计划/模型兼容、Linux 无桌面设备登录、各 Codex 版本 account/capacity API、共享 SQLite/锁在高并发和崩溃后的行为，均尚未运行验收。源码的 rollback/恢复测试覆盖不替代这些验证。上述“支持”仅说明固定版本存在明确实现与实际 daemon wiring；上线仍需执行账号与进程恢复切片。
