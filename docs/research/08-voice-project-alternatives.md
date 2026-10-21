# 语音项目替代比较：哪些更好，哪些不能替代

核验日期：2026-10-21。先读并沿用 [03 语音实施方案](03-voice.md)、[06 独立复核](06-realtime-integration-review.md) 与[语音证据](evidence/voice-realtime-audit.md)。目标仍是 **ChatGPT 网页账号实时双向语音 + 网关 + 多账号池 + Cylo Super 执行闭环**，覆盖浏览器/手机和打断。本文只做研究，没有修改主线、接入账号或写产品 runtime。

**结论：本轮检索未找到已核验更好直接替代。** 同协议候选中，Python 原作者版有值得借鉴的断线/额度事件重连入口，但没有解决真实额度调度，甚至可能一直重选耗尽账号。**LiveKit Agents、Pipecat 在可编程业务闭环、轮次与播放控制方面更强**；它们是另一层基础设施，现有插件不能直接接 ChatGPT 网页 token，也不自带本需求的账号池。若坚持现有上游，首选继续增强 Space 网关；若未来接受更换上游协议、凭据和商业计费，再比较 LiveKit / Pipecat，不能把该选择默认为用户已接受。

## 1. 同类网关与基础设施必须分开比较

A 类沿用 ChatGPT 网页 `/realtime/wm`、Web `access_token`、浏览器直连上游 WebRTC。B 类负责 Agent、媒体、turn/工具生命周期，可以使用原生 speech-to-speech provider，**不必走 STT→LLM→TTS**，但现成接入通常需要相应 provider API 凭据。B 类拥有 function calling 不证明 A 类私有 `client_tools` 可用；官方 API 协议的 completed 事件也不证明网页字幕具有同一 final 语义。

| 项目 / 类别 | 比 Space 更有价值的地方 | 不能替代的部分 / 对 Cylo 的实际判断 |
|---|---|---|
| **Space3044/chatgpt-web-voice / A 基线** | 已采用：Go 网关、加密账号库、管理/下游 key、LRU、持久会话元数据，沿用原上游及账号池 | 无 quota/cooldown/租约；`client_tools:[]`；final、先答抑制、user-role 回注闭环仍待验证。保持 03 的改造计划 |
| **dyhhhhhh/chatgpt-web-voice / A** | Python/FastAPI 便于协议实验；客户端有 `usage_update` / `tool_update` / ICE 断线重连入口、有限重试、清旧线程 ID | JSON 明文账号池按首个启用账号取号；usage 重连不隔离旧账号；无 quota reset、租约、可信 final 或业务 tools。适合参考恢复触发器，整体不优于当前基线 |
| **victorli-cn/chatgpt-web-voice / A** | 在 Python 版增加受保护 `/import`、ChatGPT session JSON 导入、原子写入与脱敏列表；4 个导入测试实际通过 | 仍沿用同一首账号选择/401 分支/usage 重连缺口；不是新协议实现，也未比 Go 版补齐更关键能力。不建议为导入页面迁移整个网关 |
| **LiveKit Agents / B** | 最适合进一步评估浏览器/移动媒体层：多平台 SDK/room，服务端 AgentSession、工具执行、转录与打断生命周期；可自建 LiveKit server | 不接受现有 Web token 网关的 SDP 即获得 tools。需换成 room→Agent→provider 媒体路径，或开发私有 WebRTC provider。无 ChatGPT 订阅账号 quota 池；框架 Apache-2.0，但 turn 模型单独许可 |
| **Pipecat / B** | 最适合协议/桥接实验：Python frame pipeline、自建 SmallWebRTC、可注册工具、转录 completed、取消与截断处理；可选原生 realtime provider | SmallWebRTC 是客户端→Pipecat 的媒体通道，不是网页 `/realtime/wm` 适配器。沿用原上游需新增上游 WebRTC peer、私有 DC 解析与音频桥；无订阅账号池 |
| **TEN Framework / B** | 有 voice-to-voice graph、MLLM 工具结果接口与中断事件，适合已选 Agora 栈的团队 | 当前示例需 Agora App ID + 模型 provider key；现有 Web token/池不支持。**HEAD LICENSE 为 Apache-2.0 加额外限制**，包括终端部署和竞争 Agora 限制；不宜作为 Cylo 默认首选 |

“更强”仅指上表可读源码与现有边界，不是延迟、中文识别、手机锁屏通话或真实执行成功率的实测排名。

## 2. 固定版本、维护与许可证

实际 clone 到 `/tmp/cylo-voice-alternatives/<owner>--<repo>`，没有 vendor 到产品。表内链接锁定本轮 HEAD；日期使用 git **committer 时间（UTC）**，不混同 GitHub push 时间。完整 SHA、作者/提交时间和 LICENSE 摘要见[取证清单](evidence/voice-alternatives-heads.json)。

| 仓库 / 固定 HEAD | 最近提交时间 UTC / 内容 | LICENSE 与维护判断 |
|---|---|---|
| [Space3044](https://github.com/Space3044/chatgpt-web-voice/tree/6338ac478070aaa34bfe7a3c0469caf9e9174c02) `6338ac4` | 2026-07-24 16:23；hangup / barge-in 修复，既有 03 已核验 | MIT；本轮 API `pushed_at=2026-07-24T16:24:37Z`，不能推导目前仍兼容上游 |
| [dyhhhhhh](https://github.com/dyhhhhhh/chatgpt-web-voice/tree/9eb61526118d272beeb20ed18c0df071274d73e8) `9eb6152` | 2026-07-23 06:46；README 清理 | MIT；HEAD 无近期协议修复证据 |
| [victorli-cn](https://github.com/victorli-cn/chatgpt-web-voice/tree/9916f10368038fcebf0d17d6b6653d96aae0fd1b) `9916f10` | 2026-07-27 15:07；增加 web account import | MIT；新提交改善导入，未证明语音协议更可靠 |
| [LiveKit Agents](https://github.com/livekit/agents/tree/7c99532ffa629a32326f8e8fd2ad0bad8f909544) `7c99532` | 2026-10-21 15:50；Speechify 插件模型流式更新 | Apache-2.0；本轮可见当天提交，API push 15:50:07Z；[MODEL_LICENSE](https://github.com/livekit/agents/blob/7c99532ffa629a32326f8e8fd2ad0bad8f909544/MODEL_LICENSE) 要求模型随 LiveKit Agents 使用，不能独立/用于其他框架 |
| [Pipecat](https://github.com/pipecat-ai/pipecat/tree/32d63cb97de2bb2ecc732d0427ac1fb4e0c7e8c6) `32d63cb` | 2026-10-21 15:26；changelog 命名 | BSD-2-Clause；API push 15:45:48Z，当天活跃，不代表 Web token 支持 |
| [TEN](https://github.com/TEN-framework/ten-framework/tree/18a449ea707352f9697b5b27062b218bc7167e13) `18a449e` | 2026-10-21 12:22；FDB benchmark graph 测试 | [LICENSE](https://github.com/TEN-framework/ten-framework/blob/18a449ea707352f9697b5b27062b218bc7167e13/LICENSE) 有附加条款，GitHub API `NOASSERTION`；API push 12:22:30Z。活跃不能消除许可/集成成本 |

维护依据是已读取的 HEAD/提交内容和部分 API push 时间，没有以 stars 判断。近三个月未见 HEAD 变化只描述观察，不等于项目已废弃；当天提交也不证明任何具体通话场景已通过。

## 3. A 类为什么尚不足以换掉 Space

原作者版和 victor 版服务端 [voice_service.py](https://github.com/dyhhhhhh/chatgpt-web-voice/blob/9eb61526118d272beeb20ed18c0df071274d73e8/app/voice_service.py#L191) 的 `create_voice_session`：每个新请求 `excluded=set()`；401 才 `mark_invalid` 并换下一个；其他非 2xx/非 SDP 直接 502。`_build_session_json` 同样固定 `client_tools:[]`。没有账号剩余语音余额/重置查询、可信 quota 事件账本、cooldown 或并发占用租约。

客户端 [usage / failover](https://github.com/dyhhhhhh/chatgpt-web-voice/blob/9eb61526118d272beeb20ed18c0df071274d73e8/static/voice.html#L1196) 会读取 `usage_update` 的 hang_up、rate_limit_message、audio_s 等候选字段并安排重连；这些是**代码已实现的解析假设**，不是本轮获得的真实上游事件 fixture。`runAccountFailover` 关闭旧媒体、release binding、清 session ID 后重连；`startCall({failover:true})` 还清 conversation/parent IDs，值得借鉴。但 [accounts.py#L59](https://github.com/dyhhhhhh/chatgpt-web-voice/blob/9eb61526118d272beeb20ed18c0df071274d73e8/app/accounts.py#L59) 每次取第一个启用账号，客户端没有把耗尽账号标冷却/排除；**未遇 401 时可能重新选中同一个已耗尽账号**。README 的“换另一个池账号”不能当验收结论。

本轮用真实 `AccountPool`、临时文件及两个 fake token 执行：连续两次 fresh `pick()` 都选 A；`mark_invalid(A)` 后才选 B。该结果证明首账号选择行为，不证明真实上游 quota 或通话成功。victor 的 [导入代码](https://github.com/victorli-cn/chatgpt-web-voice/blob/9916f10368038fcebf0d17d6b6653d96aae0fd1b/app/accounts.py#L114) 与 [4 个测试](https://github.com/victorli-cn/chatgpt-web-voice/blob/9916f10368038fcebf0d17d6b6653d96aae0fd1b/tests/test_accounts.py) 已读/已运行；导入存 JSON、原子 rename，不是 Space 的加密 SQLite 管理体系。

额外检查 [sivaneh fork](https://github.com/sivaneh/chatgpt-web-voice/tree/fab99f96d3c544161e68e2164e6b210302ff6520)：HEAD 2026-09-27 10:41:07Z，比 Space 新，但关键 diff 为中文 UI/状态改英语、时区改 Asia/Kolkata、上游语言头改 en-US。`pool.go`/`service.go` 未新增 quota/tool/final 能力；状态字符串还涉及旧库兼容检查。因此不因日期新就推荐替代。

A 类部署均仍要 HTTPS、浏览器麦克风权限与上游可达性；Python 版依赖 FastAPI、uvicorn、curl_cffi、Pydantic、Pillow，迁移会丢失或重做 Go 管理/密钥/持久会话层。更合理的增量是把实际有价值的恢复触发器移植到现有网关/客户端，同时按 03 完成租约、错误分类、账号隔离与幂等桥接，**不把候选字段提升为可信额度事实**。

## 4. B 类如何接 Super，以及保留原上游的代价

| 维度 | LiveKit Agents | Pipecat | TEN |
|---|---|---|---|
| 业务工具桥 | Agent 的 `function_tool` / 工具生命周期 → Cylo adapter → durable command → Super；已存在工具运行与输出接口 | `LLMService.register_function` / `run_function_calls`、FunctionCallResultFrame → Cylo adapter → Super | MLLM server function call / client function output → Cylo adapter → Super |
| 轮次与转录 | realtime provider 的 input transcription completed 与 generation/item ID；框架有 user-input/transcription/turn 管理 | 原生 realtime 插件把 delta 转 InterimTranscriptionFrame、completed 转 TranscriptionFrame | 扩展分发 MLLM 服务事件/转录；需要核对选用 provider 的 final/修订语义 |
| 打断 | AgentSession 管理 speech handle、响应取消、音频清理；实际插件实现取消/截断路径 | InterruptionFrame、provider response cancel/truncate 与 transport 播放队列控制 | 扩展接收中断并通知下游；当前 openai_mllm 的一段 truncate 代码被注释，不能据此宣称精确已播时长截断完成 |
| 媒体路径 | 浏览器/移动 SDK → LiveKit room/SFU → Python Agent → realtime provider | 浏览器 → SmallWebRTC/aiortc → Python pipeline → realtime provider；也可选 Daily/LiveKit transport | 当前 realtime 示例：前端/Agora RTC → TEN graph → provider |
| 原 ChatGPT Web 上游 | 无现成适配；新增服务端作为网页 WebRTC peer，桥接音频/DC，或保留原直连并仅借鉴控制层 | 同左；适合写小型私有 provider 实验，但不能只改 `base_url` | 同左；还需处理 TEN graph/Agora 集成及许可证条件 |
| quota/账号切换 | 未核验到本需求的 ChatGPT Web 订阅账号池/剩余语音额度切换 | 同左 | 同左 |
| 部署与改造量 | Python Agent worker + LiveKit server（自建或 Cloud）+ SDK/token 服务；自建还需 ICE/TURN/网络运维；整体改造大 | Python≥3.11 + aiortc/音频依赖 + 信令/ICE/TURN；本地 SmallWebRTC 可少一个商业媒体依赖；私有 provider 改造仍大 | TEN runtime/扩展、Python、前端、示例 Agora 凭据与 provider 凭据；改造及许可审查成本高 |

这些工具回调均须接 **Cylo 持久任务平面**，不能直接把 Worker/Codex 凭据交给语音模型。provider 工具调用先持久化 command 再执行，重连/重播保持幂等；工具取消不等于外部副作用已撤销。插件若真正给出 completed，也只适用于该插件对应协议，不能拿来修饰网页私有字幕。

固定源码证据：

- LiveKit：[README 的 SDK/自建/AgentSession 示例](https://github.com/livekit/agents/blob/7c99532ffa629a32326f8e8fd2ad0bad8f909544/README.md)、[agent_activity.py](https://github.com/livekit/agents/blob/7c99532ffa629a32326f8e8fd2ad0bad8f909544/livekit-agents/livekit/agents/voice/agent_activity.py)、[实际 realtime 实现](https://github.com/livekit/agents/blob/7c99532ffa629a32326f8e8fd2ad0bad8f909544/livekit-agents/livekit/agents/llm/_realtime/openai.py)。后者读取 `OPENAI_API_KEY`/API URL，处理 completed、function call；不是 ChatGPT Web token 网关。插件目录中的旧路径存在转接，证据以实际实现为准。
- Pipecat：[realtime/llm.py](https://github.com/pipecat-ai/pipecat/blob/32d63cb97de2bb2ecc732d0427ac1fb4e0c7e8c6/src/pipecat/services/openai/realtime/llm.py#L933)、[SmallWebRTC transport](https://github.com/pipecat-ai/pipecat/blob/32d63cb97de2bb2ecc732d0427ac1fb4e0c7e8c6/src/pipecat/transports/smallwebrtc/transport.py)、[LLMService](https://github.com/pipecat-ai/pipecat/blob/32d63cb97de2bb2ecc732d0427ac1fb4e0c7e8c6/src/pipecat/services/llm_service.py)。realtime 插件构造 API WebSocket + Bearer api_key 并调用 run_function_calls，未实现 `/realtime/wm` SDP/私有 relay。
- TEN：[voice-assistant-realtime 示例及凭据](https://github.com/TEN-framework/ten-framework/blob/18a449ea707352f9697b5b27062b218bc7167e13/ai_agents/agents/examples/voice-assistant-realtime/README.md)、[MLLM 扩展](https://github.com/TEN-framework/ten-framework/blob/18a449ea707352f9697b5b27062b218bc7167e13/ai_agents/agents/ten_packages/extension/openai_mllm_python/extension.py#L526)。`send_client_function_call_output`/`send_server_function_call` 可用作桥接入口；示例说明 Agora 与 provider 依赖，不能据“多模型”宣称支持网页订阅。

**商业模型必须单列：** LiveKit/Pipecat 的开源代码许可不包含模型调用额度。使用其现成 API provider 路径，改为 provider API 凭据及相应商业计费；LiveKit Cloud、Daily、Agora 等媒体服务也可能另计费，自建媒体则需运维成本。这不是把 ChatGPT 订阅语音分钟迁过去。若坚持网页账号池，三个框架均不能消除私有协议的 quota、tools、final、先答控制缺口，且服务端终止/媒体桥带来额外工程与延迟，仍需真实协议 POC。

## 技术栈与组合成本（按用户最新偏好补充）

| 项目 | 核心服务 / Web 前端 | 扩展与 transport / 真实运行依赖 |
|---|---|---|
| Space Go 版（含 sivaneh fork） | Go 1.24.1，HTTP API、modernc SQLite；静态 HTML/CSS/原生 JS，无需把页面误认成 React 项目 | Go package 扩展；浏览器 RTCPeerConnection/私有 DC；TLS fingerprint 客户端；Docker 构建还包含 curl-impersonate/glibc 运行依赖，不只是复制一个 Go 二进制 |
| dyhhhhhh / victor Python 版 | Python 3.11+、FastAPI/uvicorn、Pydantic、curl_cffi、Pillow；原生 HTML/JS，victor 加 import 页面 | Python 模块扩展；JSON 文件池；浏览器仍直连上游 WebRTC；Docker 用 python:3.12-slim；与 Python 编排服务可共语言，但管理/持久层功能较 Space 少 |
| Pipecat | Python≥3.11 frame pipeline；Web/移动客户端是独立 JS/React/React Native/Swift/Kotlin SDK 生态，核心仓不是全栈 React 服务 | FrameProcessor/LLMService/transport 插件；SmallWebRTC 使用 aiortc，可选 Daily/LiveKit；媒体编解码与 ICE/TURN 仍需独立部署考量 |
| LiveKit Agents Python | 本轮主核验 repo 为 Python，pyproject 要求 ≥3.10,<3.15；AgentSession/provider 插件；Web/移动 SDK 与 SFU 分属独立包/服务 | Python worker 通过 LiveKit room；需 LiveKit server 或 Cloud、URL/key/secret；同 Python 不等于去掉 SFU/SDK 或其服务边界 |
| LiveKit AgentsJS（补核验） | **独立仓库** TypeScript/Node.js：`@livekit/agents`，不是 Python repo 内的 Node 目录；monorepo 用 pnpm/Turbo/tsup/Vitest | 独立 TypeScript Agent/provider 插件；不能假定与 Python 实现所有能力逐项相同。本轮只核验包/运行生态，不用它替代上述 Python 协议源码证据 |
| TEN | 核心 runtime/工具含 C/C++ 与 Rust；agent 扩展包括 Python；示例 API server 为 Go；playground 是 TypeScript/Next.js/React | Taskfile 同时 `tman install`、Python deps、`go build`、`bun install/run`；示例需 Agora SDK。属于多语言栈，不能因前端 React 或扩展 Python 就称与 Cylo/CAO 完全同栈 |

栈证据：[Space go.mod](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/go.mod) / [Dockerfile](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/Dockerfile)，[Python requirements](https://github.com/dyhhhhhh/chatgpt-web-voice/blob/9eb61526118d272beeb20ed18c0df071274d73e8/requirements.txt)，[Pipecat pyproject](https://github.com/pipecat-ai/pipecat/blob/32d63cb97de2bb2ecc732d0427ac1fb4e0c7e8c6/pyproject.toml)，[LiveKit Python pyproject](https://github.com/livekit/agents/blob/7c99532ffa629a32326f8e8fd2ad0bad8f909544/livekit-agents/pyproject.toml)，[TEN 示例 Taskfile](https://github.com/TEN-framework/ten-framework/blob/18a449ea707352f9697b5b27062b218bc7167e13/ai_agents/agents/examples/voice-assistant-realtime/Taskfile.yml)。AgentsJS 实际补 clone 于同一 /tmp 根目录，HEAD `391489c73ac564e4e43f888e9ffe6f63cc73fea8`，提交时间 2026-10-21 13:13:55Z，[agents/package.json](https://github.com/livekit/agents-js/blob/391489c73ac564e4e43f888e9ffe6f63cc73fea8/agents/package.json) 声明 Node.js 包，[根 package.json](https://github.com/livekit/agents-js/blob/391489c73ac564e4e43f888e9ffe6f63cc73fea8/package.json) 声明 pnpm/Turbo，LICENSE 为 Apache-2.0。

**组合建议：** 如果编排侧最终选 CAO Python，原 Python 网关 + CAO 可共享语言/镜像基础、测试工具和 adapter 开发习惯；Pipecat 也可沿用 Python，但并不共享 ChatGPT 私有协议，不能因同栈就把它插进直连媒体链。原 Python 网关仍需补加密存储、管理/owner 隔离、LRU/租约/quota 与可靠业务桥，成本须与保留 Go 网关比较。若编排候选最终是 Go 实现，Space 网关可获得相同语言收益；具体 AO 是否为 Go 以编排专项的固定仓库证据为准。**当前更稳妥的是保留功能更完整的 Space Go 网关，通过小型 HTTP/event adapter 接 Python/其他语言的 Super；是否统一语言是次级工程偏好，实时上游与账号池是硬约束。** 仅在团队明确愿意补齐 Python 版管理/隔离差距，并完成真实通话/quota 验收后，才因同栈考虑迁移；不为 CLI 人工交接增加语音媒体复杂度。

## 5. 有条件推荐与下一步

1. **当前需求优先：保留 Space，借鉴 Python 原作者版 failover 入口。** 先拿真实脱敏事件核验语音额度、final、relay 回注及先答控制；再做租约/冷却/换号新会话/幂等 Super 桥。A 类换仓不能跨过 03/06 的验收门槛。
2. **将来需要服务端掌握媒体和移动 SDK：优先比较 LiveKit Agents。** 条件是用户接受新增媒体服务器/room 路径，并明确选择 API provider 商业方案，或为原网页上游单独投入适配 POC。不能承诺手机后台/锁屏持续通话，需原生音频会话和真机验收。
3. **将来优先做私有上游协议实验/最小服务端音频桥：比较 Pipecat。** SmallWebRTC + frame pipeline 更便于观察和实现 adapter；条件是接受 Python 媒体栈及自写 WebRTC/DC provider。先用固定协议 trace 证明兼容，再谈替代；现有 realtime 插件不算已经适配。

TEN 仅在明确选 Agora、其许可条件符合交付形态时进入后续比较。本轮不把它列为前三推荐，因为许可和路径增加了 Cylo 当前不需要的约束。

## 6. 搜索范围与实际验证边界

通过公开 GitHub REST API 实际检索：`chatgpt voice access_token`、`chatgpt-web-voice in:name`、`chatgpt voice account pool`、`chatgpt voice gateway`、`realtime/wm`；查询 Space 的 forks（返回 lililib、sivaneh、d1752472377、leoleo166），并检查原作者与独立 victor 仓库、上述三个框架。查询 `chatgpt realtime wm` 遇 GitHub 未认证 rate limit 403，该查询没有有效结果，不伪称检索成功。`chatgpt voice gateway` 还出现面向 ESP32/LAN 的 snowball，本轮未深入核验，不把它算合格网页/手机替代。搜索不是世界范围完备证明。

复现入口：[Space forks API](https://api.github.com/repos/Space3044/chatgpt-web-voice/forks?per_page=100&sort=newest)、[名称检索 API](https://api.github.com/search/repositories?q=chatgpt-web-voice%20in:name&per_page=50)、[token 检索 API](https://api.github.com/search/repositories?q=chatgpt%20voice%20access_token&per_page=30)。随后 `git clone --depth 1 --filter=blob:none https://github.com/<owner>/<repo>.git`，`git log -1 --format='%H %aI %cI %s'` 固定版本，直接读 LICENSE 和上述源码，不依赖 README 宣传。

实际执行：真实 Python AccountPool 的双假账号首选/禁用行为核验；victor `python3 -m unittest discover -s tests -v` 共 4 项通过；对 sivaneh 与 Space 做文件/关键函数 diff。未安装大型框架、未跑其媒体/云端测试、未登录、未使用真实 token、未发收费模型请求、未真机通话。本轮没有新增官方 OpenAI 产品能力/价格断言；API/Web 凭据差异以已接受 03/06 和本轮 provider 源码为依据，不重新把框架文档当官方协议证明。
