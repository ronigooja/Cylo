# CLIProxyAPI 与 AO 的账号池接入核查

核查日期：2026-10-21。结论：**CLIProxyAPI 已存在于用户基础设施，当前本机 Codex 也已配置自定义 Responses provider；Cylo 应优先复用现有部署，无需先引入第二套代理。** 用户报告登录与使用均已能走代理；本轮只读核查部署与配置，没有复测登录或模型调用。部署证据及复用方案见 [现有部署核查](15-existing-cliproxy-deployment.md)。AO 已有 Codex 多账号管理；若目标包括订阅额度耗尽后无人值守换号，仍须验收现有代理与 AO/Codex 的组合行为。AO 管会话与任务，CLIProxyAPI 管这一条模型请求路由的上游账号选择；它不是 AO 的必需依赖，也不是已选 Space 语音网关的替代品。首轮无需 fork AO，复用自定义 Responses provider 后验收工具、续接与失败恢复。

## 固定基线与证据范围

复用已有干净 clone `/root/CLIProxyAPI`，没有访问现存用户认证文件，没有登录、刷新真实 token 或调用模型。未 pull，结论只适用于本次已有快照，不声称最新发布。

| 项目 | 已核查值 |
|---|---|
| 仓库 | https://github.com/router-for-me/CLIProxyAPI.git |
| SHA | `091ff27f31285ec4fd1dd5823b44f7f51fb8f48b` |
| 提交时间 | `2026-10-12T06:38:29+08:00` |
| 提交标题 | `feat(antigravity): persist model capability hints in auth metadata` |
| module / Go | `github.com/router-for-me/CLIProxyAPI/v8` / `go 1.26.0`，见根 [go.mod](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/go.mod#L1) |
| 许可证 | 根 [LICENSE](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/LICENSE#L1) 实际为 MIT；不是从 README 或依赖许可证推断 |
| AO 比较基线 | `OrchestratorInc/AO`，`7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff`，本机 `/tmp/cylo-alt-ao` |
| Space 比较基线 | `Space3044/chatgpt-web-voice`，`6338ac478070aaa34bfe7a3c0469caf9e9174c02` |

下列 CLIProxy 源码链接均固定在上述 SHA。阅读了 OpenAI Docs skill，按其顺序搜索并实际抓取[官方 Codex 配置参考](https://developers.openai.com/codex/config-reference/)；官方资料只用于 Codex 配置语义，不证明第三方代理获得订阅接入授权、模型访问或额度。

## AO 已有多号，为什么仍可能需要代理

AO 的 `backend/internal/adapters/agentaccount/codex.go`、`backend/internal/service/agentaccounts/service.go` 与 `backend/internal/session_manager/agent_account.go` 已有账号导入、默认账号、每会话绑定及显式换号；每账号使用独立 `CODEX_HOME/auth.json`，配置和历史等共享。Chat 与 TUI 支持路径不同，不能把全局 `/codex/accounts` switch 当成任意 agent 的账号切换。

固定 AO 的 `usage_limit.go` 路径是对特定 Chat 失败轮次人工开启等待 reset 后以同号恢复，未证实遇到 429 就自动扫描另一个账号。**“管理多个账号”“手动切换同 thread”“请求级自动冷却/故障切换”是三个能力。** CLIProxy 主要补第三个，以及多客户端集中池、多协议转换。若仅需要账号隔离、人工挑号，AO 原生功能已足够；若要求订阅耗尽后继续无人值守执行，应复用现有代理验证自动切号，但已经开始输出的 turn 仍需要 AO/Codex 错误恢复。

## CLIProxy OAuth 账号池：实际实现与边界

| 能力 | 源码核查 | 对 Cylo 的含义 |
|---|---|---|
| Codex/ChatGPT OAuth 登录及刷新 | [sdk/auth/codex.go](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/sdk/auth/codex.go#L40) 实现 PKCE/state/browser callback，并有 device flow；[token.go](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/internal/auth/codex/token.go#L19) 保存 access/refresh/id token、account_id、plan_type；[executor_auth.go](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/internal/runtime/executor/codex_executor_auth.go#L34) 刷新 | 是 Codex OAuth 上游能力，不是把 ChatGPT 订阅转换成官方 API key；具体账号能否用具体模型仍需真实验收 |
| 多凭据选择 | [selector.go](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/sdk/cliproxy/auth/selector.go#L614) 实现 round-robin、weighted round-robin、fill-first，先筛可用/优先级 | 不是简单把一个 token 放在 base_url 后；有优先级和模型可用性约束，也不等于多个号额度合并 |
| 会话粘性 | [selector.go](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/sdk/cliproxy/auth/selector.go#L1050) 与 session resolver/cache；[示例配置](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/config.example.yaml#L143) 默认 affinity=false、TTL=1h、启用时 subagent 默认继承 | 建议 PoC 启用粘性；header/session/prompt_cache_key 等决定绑定。绑定失效能切备用号，但不是跨账号共享上游 conversation/cache |
| 429 / quota 冷却 | [executor_terminal.go](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/internal/runtime/executor/codex_executor_terminal.go#L403) 区分 `usage_limit_reached`，解析 `resets_at` / `resets_in_seconds`；[conductor_cooldown.go](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/sdk/cliproxy/auth/conductor_cooldown.go#L909) 记录不可用/恢复时间，支持模型/credential 范围 | quota 默认可冷却整账号，模型级 cooling 为显式选项。普通速率限制不等同订阅耗尽；无 reset 提示有 fallback/backoff，不能承诺精准额度余额 |
| 重试与切号 | [conductor_execution.go](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/sdk/cliproxy/auth/conductor_execution.go)、[conductor_stream.go](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/sdk/cliproxy/auth/conductor_stream.go) 筛候选及记录结果；配置为额外轮数、每轮凭据上限、最长冷却等待 | 默认额外3轮、每轮0表示尝试所有可用凭据、最长等待30秒；不是永远阻塞直到有账号恢复。业务 deadline 应控制总等待 |
| 冷却持久化 | `routing.cooldown.save-cooldown-status` 默认 false，启用时写 `.cds` | 默认重启丢失内存冷却，应显式决定是否持久化，避免重启后再次打耗尽账号 |
| 并发 | selector/cache 有锁，可服务并行请求；[home_concurrency.go](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/sdk/cliproxy/auth/home_concurrency.go#L23) 有 Home credential admission/lease/busy 类型；[配置说明](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/config.example.yaml#L108) 强调 Home 的策略权威 | 轮询或线程安全不等于每号并发=1，也不证明 standalone 有可直接填写的每号硬并发上限。初期仍在 AO/Cylo 限制 worker 并发，并验证实际账号在途数；跨实例池要单独验收 Home 分布式策略 |

## Responses、工具与续接：不能承诺无缝换号

对 Codex OAuth 路由，[HTTP SSE executor](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/internal/runtime/executor/codex_executor_stream.go#L24) 默认请求 `https://chatgpt.com/backend-api/codex`；[Responses translator](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/internal/translator/codex/openai/responses/codex_openai-responses_request.go#L19) 强制 `store=false`、处理 function_call 等项。native Codex 工具 schema、parallel_tool_calls、reasoning、apply_patch、compact 等有专门适配，但不能据此承诺所有版本/所有代理协议工具语义等价。

**HTTP 路径明确删除 `previous_response_id`**（stream 与 non-stream 都有）。因此客户端只发 `previous_response_id` 加增量、期望服务端找回旧 response 的使用方式不能直接视为兼容；应让原生 Codex 携带完整历史/工具 call_id 配对，并实测 reasoning/encrypted item、compact 后历史。粘性有利于缓存与身份稳定，不能修复缺少的历史，也不创造跨账号的 response 存储。

[下游 WebSocket 请求归一化](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/sdk/api/handlers/openai/openai_responses_websocket_requests.go#L94) 有两条路径：支持增量时保留 `previous_response_id`；否则合并该 socket 的 lastRequest/lastResponseOutput/input，再删除 previous ID。完整 transcript 和 compaction 另有替换/去重处理。这是连接上下文中的适配，不能假定代理重启或新 socket 仍保存全部旧状态。

[WS duplex](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/internal/runtime/executor/codex_websockets_duplex.go) 与 `upstream.codex.response-steering` 是实验功能，配置注释明确一条 socket 固定一个上游账号/模型，已接受输入不重放。WS 路由与重连不是任意时刻切账号续同一个上游 response 的承诺。PoC 初期显式关闭 Codex provider WebSocket，只验证 HTTP Responses/SSE，再单独启用 WS。

**已开始的流不做透明 replay。** [上游现有 mock 测试](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/sdk/cliproxy/auth/conductor_stream_quota_test.go#L21) 明确先输出 `response.created` 后发生 quota 时保留 payload+error，attempts=1，随后新请求才选备用号。默认关闭的 `stream-bootstrap-buffering` 可以暂存早期空帧、在生成前的 overload/rate limit 切号，但受 frame/byte budget 及输出提交影响；配置 timeout 的检查也随帧到达，不是独立定时硬中断。不能将此描述为生成一半也能自动续写。

PoC 应覆盖：A 返回带 reset 的 429→B 接管同一尚未输出请求；A 已输出→明确失败且下一请求选B；tool call→本地执行→tool output，在切号后不重复执行且 call_id 对应；完整历史与 previous-ID-only 两种请求；compact/restart/resume；WS断线、身份绑定与取消。**这些测试均未在本轮执行。** 不应靠重放整轮来“修复”非幂等工具。

## 接入 AO：先配置，无需先 fork

路径是 `AO → native Codex CLI/app-server → CLIProxy /v1/responses → Codex OAuth 账号池`。AO 仍负责调度和 agent 会话，Codex 负责本地 thread、历史与工具执行；代理只改变模型上游。

官方 Codex 当前配置说明支持 `model_provider`、`model_providers.<id>.base_url/env_key/wire_api/requires_openai_auth/supports_websockets`，`wire_api` 当前只支持 `responses`。以下为待验证的最小用户配置（沿用实际已获访问的模型，不在示例硬填型号）：

```toml
# 写入该 AO/Codex 实际使用的 CODEX_HOME/config.toml
model_provider = "cylo_pool"

[model_providers.cylo_pool]
name = "Cylo local pool"
base_url = "http://127.0.0.1:8317/v1"
wire_api = "responses"
env_key = "CYLO_CODEX_PROXY_KEY"
requires_openai_auth = false
supports_websockets = false
```

`CYLO_CODEX_PROXY_KEY` 是 **CLIProxy 下游 access key**，不是上游 OAuth token。通过受控进程环境传给 AO 启动的 Codex；不要把真实值写入仓库、prompt、命令参数或文档。官方参考明确 provider 等相关字段不能放在项目本地 `.codex/config.toml`；应使用用户/该 `CODEX_HOME` 配置。当前官方文档可能新于 AO 使用的 Codex 版本，部署时须固定 CLI 版本再次验证。

- **TUI**：AO 固定源码 `backend/pkg/agentruntime/command.go:313` 的 `buildCodexLaunch`、`:322` 的 restore 启动原生 `codex`/`codex resume`。该进程读取自己的配置；不需要把 AO agent 类型替换成一个 HTTP agent。
- **Chat/app-server**：`backend/internal/adapters/chatdriver/codexappserver/driver.go:516` 的 connect 传 runtime env，`:575` 与 `:724` 启动同一 binary 的 `app-server`。该 server 的 stdin/stdout RPC 是 AO 与 Codex 的控制协议，**不能把其地址指向 CLIProxy**；配置改变的是 app-server 内部发给模型的 Responses 请求。
- AO 原生每号 `CODEX_HOME` 的 `config.toml` 是共享链接，直接改会影响这些账号。初次试验宜隔离专用 HOME/实例；上线前检查配置覆盖顺序、启动环境、AO model/list 与模型选择。不要删除 AO 状态或用户 auth 文件来实现接入。
- 一条 route 只保留一个账号选择权威：proxy 管这条模型路由的 OAuth 池时，AO 不再对同路由做第二套自动换号；AO 里显示的绑定名可能只是本地配置身份，并不代表代理最终选中的上游账号。为审计记录脱敏 provider/route/auth-id，勿记录 token。

只有当现有 AO 缺少专用 provider 配置入口、密钥传递或需要显示代理选择结果时，才评估小型 adapter/UI 扩展；普通 `base_url` 接入本身不构成 fork AO 的理由。

## 每个账号独立网络代理：支持，但登录和运行要区分

**登录代理是实现支持的能力，用户已报告实际使用；本轮未复测。** 以下区分登录进程全局出口与运行时每账号出口，未发现专用每号登录选项不等于登录不能走代理。

这里的网络代理（HTTP/HTTPS/SOCKS 出口）不同于 `model_providers.*.base_url`（模型服务端点）。CLIProxy OAuth 文件顶层可设置 `"proxy_url": "http://127.0.0.1:19081"`；不同账号文件填不同值。它是已有 auth record 的补充字段，**不是可独立使用的完整凭据文件**。API key credential 组则用配置字段 `proxy-url`。

[Auth.ProxyURL](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/sdk/cliproxy/auth/types.go#L77) 是正式字段，[file synthesizer](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/internal/watcher/synthesizer/file.go#L174) 从 OAuth JSON 读入。运行时优先级为 **内部 request proxy override > 每号 auth.ProxyURL > 全局 requests.proxy-url > 底层 transport/environment 默认行为**，见 [HTTP helper](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/internal/runtime/executor/helps/proxy_helpers.go#L127)；[WS dialer](https://github.com/router-for-me/CLIProxyAPI/blob/091ff27f31285ec4fd1dd5823b44f7f51fb8f48b/internal/runtime/executor/codex_websockets_connection.go#L184) 同样取该优先级。`direct`/`none` 表示显式直连，绕开全局/环境代理。不要把内部 request override 误写成任意客户端可发送的已支持 header。

刷新 token 用 `NewCodexAuthWithProxyURL(cfg, auth.ProxyURL)`，所以会采用每账号出口。**初次登录不同**：`sdk/auth/codex.go` 创建 `NewCodexAuth(cfg)`，`cmd/server/main.go:654` login options 只有 NoBrowser/CallbackPort 等，本轮未发现每号 login proxy 选项。初次 code/device exchange 使用登录进程的全局 `requests.proxy-url`；要给新账号固定登录出口，可用临时隔离配置逐个登录，再让对应 auth record 保留相同 proxy_url。外部浏览器打开授权页面的出口仍受浏览器自身设置控制，不能由 Go HTTP proxy 自动保证。

错误代理配置不应当作可靠的 fail-closed 策略：HTTP helper 建代理失败会记录脱敏 warning/debug 后尝试 context transport；应在部署前校验代理地址并实测 HTTP/SSE、WS及refresh各自出口。AO 本身是否支持每号代理另见 AO 专项核查，不能从 CLIProxy 的能力反推。

## 与 Space 语音账号池的关系及安全边界

Space 固定源码 [service.go](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/internal/voice/service.go#L27) 用 `/realtime/wm?dcid=0` SDP/信令、网页 access token 与账号 proxy，并维护绑定、语音失败与会话。CLIProxy Codex 路由用 `/backend-api/codex/responses`、Codex OAuth token/refresh/account_id 与 Responses 文本/工具流。虽然两者都可能出现 Bearer token、ChatGPT 域名甚至同一登录主体，**字段名相似不能证明 token audience/scope、刷新生命周期或额度可互换**。

因此不共用 token 文件、refresh writer、可用性/冷却状态或运行中租约；同一个人的账号也不能假定“语音冷却=Codex耗尽”。可以将账号别名、代理出口配置、脱敏状态汇总到同一控制面，但保留 `voice-web` 与 `codex-oauth` 独立 provider capability 和凭据所有者。若未来要复用同一账号，先验证授权/范围与冲突，不靠复制 token 来实现。

CLIProxy 快照虽也包含 `internal/client/codex/live` 等实时相关代码，本次仅核查 Codex Responses 路径；未证明这些模块可直接替代已选 Space 的网页实时媒体协议、手机WebRTC、播放/打断、Cylo工具桥或语音账号租约。**保留 Space 语音网关。**

安全部署边界：

1. 默认示例 `server.host=""` 绑定所有网卡；本地 PoC 显式绑定 `127.0.0.1`，设置独立随机 `access.api-keys`。这与 `management.secret-key` 是不同权限，管理 API 默认空 key 时禁用；保持远程管理关闭，生产还需网络访问控制。
2. OAuth 是明文 JSON。`internal/auth/codex/token.go` 用目录0700但 `os.Create` 写文件，不能声称所有保存路径都强制0600；新文件依赖 umask，已有文件权限也需校验。专用用户、umask077、加密备份，禁止 git/普通日志/工单收集认证目录。此条来自代码审阅，没有查看实际用户文件权限。
3. proxy URL 可能含认证；token、HTTP bodies、工具参数及代理密码均视为敏感。示例默认 request-log=false 不代表所有日志/管理导出绝对无泄露；按实际部署配置最小化日志与管理面。两个服务不要并发改写同一个 refresh token 文件。
4. 固定源码、Go toolchain、依赖、配置和可回滚版本。MIT 仅是软件代码许可，不是订阅服务授权或额度保证。本次没有从第三方代码推导 OpenAI 对该使用方式的官方承诺。

## 本轮检查与下一步验收

本轮完成 `git status/remote/log/show`、根许可证与 go.mod 核查、以上源码/现有测试只读审计、官方 Codex 页面抓取。宿主 `go` 不在 PATH，常见安装位置也未找到可执行文件，因此**未运行 Go 测试，未执行 mock，未进行真实订阅端到端测试**；阅读的 upstream mock 测试只用于明确设计边界。

在隔离 Go1.26 环境可先运行已有纯 mock 目标（逐项审查所选测试无真实网络/凭据依赖后执行）：`TestCodexQuotaErrorCredentialScope`、`TestParseCodexRetryAfterQuotaLayouts`、`TestSessionAffinitySelector_FailoverWhenAuthUnavailable`、`TestExecuteStreamQuotaFailurePreservesCooldownAndScope`。通过它们仍只证明本地逻辑。真实 PoC 再验收两个受控账号、明确每号网络代理、订阅429切号、native TUI与app-server各一条完整工具往返及compact/resume，最后才考虑无人值守上线。
