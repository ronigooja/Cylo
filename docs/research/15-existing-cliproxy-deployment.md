# 现有 CLIProxyAPI 部署与 Cylo 复用方案

核查日期：2026-10-21。**现有代理已部署运行，Cylo 优先复用它，不需要先安装另一套 CLIProxyAPI。** 本机 Codex 已选用自定义 Responses provider。用户报告登录与使用均可走代理；这是用户提供的使用经验，本轮没有复测登录、模型调用或自动切号。

本文以 `existing-proxy` 指代现有远端部署，不记录公网地址、个人目录详情或服务域名。源码能力和兼容性边界见 [CLIProxyAPI 与 AO 的账号池接入核查](14-cliproxyapi-integration.md)。

## 本轮只读证据

| 项目 | 核查结果 |
|---|---|
| 连接 | 使用已有信任的 SSH host key，BatchMode=yes、ConnectTimeout=10；连接成功，未绕过主机密钥检查 |
| 用户提供的部署目录 | 存在；内含部署脚本及说明，非独立 CLIProxyAPI 源码 checkout；父仓库 SHA 不作为代理版本证据 |
| 运行方式 | `CLIProxyAPI` 进程运行于 Docker 容器，容器运行中，restart policy 为 `unless-stopped` |
| 运行镜像引用 | `eceasy/cli-proxy-api:latest` |
| 运行镜像 ID | `sha256:114ea95cadc393f679016285d5f8dc0b735126a25a018307c033e8c3b1b801d2`；这是本机镜像内容标识，不是已核实的 registry manifest digest |
| 二进制版本 / 代理源码 SHA | 未核实；不能用 `latest` 推断版本，也不能把研究文档14的源码 SHA 当作部署版本 |
| 监听与发布 | 容器配置监听所有容器接口的8317端口；宿主仅 loopback 发布8317/tcp。此结果不表示宿主公网直接开放该端口 |
| 路由配置 | `weighted-round-robin`；未据此推断具体账号数量或运行时实际选择结果 |
| 本机 Codex 配置 | 已选择自定义 provider，`wire_api="responses"`；端点结构为 HTTPS 远端 `/v1`，未输出域名、userinfo、query或其他配置内容 |
| Codex 可选开关 | 当前选中 provider 的 `requires_openai_auth`、`supports_websockets` 未显式配置；没有把缺省字段视为 false |
| 最小可达性探测 | 在远端宿主对 loopback 发布端口执行一次无认证 HTTP GET `/`，返回200；未采集响应体。仅证明根路径可达，不证明模型、认证、账号池或完整健康状态 |

只读取部署目录元信息、进程标识、Docker 白名单字段、选定非敏感配置字段和本机 Codex provider 字段。没有输出进程完整参数、完整配置、环境变量、认证文件、密钥或代理凭据，没有读取账号池内容。没有安装、重启、修改远端配置、刷新 token、登录或发出模型请求。以上根路径请求不是付费模型请求。

本机配置与远端容器是分别核查的事实；本轮没有解析私有域名、检查 TLS 入口或追踪反向代理，不能仅凭这些证据证明当前 HTTPS 端点最终路由到该容器。该对应关系来自用户提供的部署背景，接入时应由部署所有者确认。

## Cylo 接入路径

沿用 `AO → native Codex CLI/app-server → existing-proxy Responses → Codex OAuth pool`。现有本机 Codex provider 是可复用的接入基线；AO 使用的二进制、运行用户、`CODEX_HOME` 及启动环境需要单独对齐。AO 的 app-server stdio RPC 保持作为控制协议，模型请求才指向代理的 `/v1/responses`。

先为 Cylo/AO 建立隔离的 provider 配置和下游访问凭据注入方案，确认 AO 每账号共享 config 链接的影响，再做配置变更。不要复制 OAuth 凭据到 Cylo，也不要让 AO 与代理同时维护同一路由的自动换号权威。现有代理继续拥有账号池和 refresh writer；Cylo 只持有访问该路由所需的下游凭据，真实值不进入仓库或命令参数。

远端 loopback 8317 不能直接写为另一台主机的 `127.0.0.1:8317`。优先复用用户已经使用的 HTTPS 入口；若 Cylo 最终与代理同宿主，再采用宿主可达的 loopback 或受控容器网络。端点、TLS 与访问控制需按部署位置确认，不在本轮更改现有网络。

登录代理已有源码实现支持，且用户报告已实际使用；本轮不再验证。源码中登录进程的全局网络代理、运行时每账号代理及浏览器自身出口是不同配置层次，详见文档14。复用现有成功路径，不因研究中未见专用每账号登录开关而重新要求引入登录代理能力。

## 后续验收边界

当前证据足以选择“复用已有部署”的架构方向，不足以宣布 Cylo 接入完成。后续经用户授权实施时，先固定运行版本与配置基线，再验证 AO TUI/app-server 的工具往返、完整历史、compact/resume，以及生成前429切号和已输出后失败恢复。HTTP Responses/SSE 与 WebSocket 分开验收；文档14所述流中断、`previous_response_id` 和跨账号状态边界继续适用。

本轮没有执行这些业务验收，也没有证明并发上限、真实可用账号数量、额度、订阅切号、实际网络出口或重启恢复。Space 语音网关及其凭据生命周期继续独立，不因文本代理已存在而替换或共用 token 文件。
