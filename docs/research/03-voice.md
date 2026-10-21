# Cylo 语音交互选型与验证

查阅与验证日期：2026-10-21。产品现名 **Cylo**；已完整阅读《Rumi-完整产品需求说明书.md》1313行及旧《codex-super-agent-requirements-and-architecture.md》。本文是研究结论，不代表运行实现或部署完成。证据等级：**源码**指固定commit可见实现；**官方文档**指本日实际HTTP抓取；**实验**仅指明确记录的离线检查；其余标为**建议/推断**。使用了 OpenAI Docs 技能，OpenAI API事实依据实际打开的官方页面；未使用模型记忆填补型号、价格或平台保证。

## 1. 结论与需求约束

**推荐主线：可替换的流式STT → Cylo权威控制平面 → 必要时Super/Worker → 可替换TTS。** 手机、平板和Mac共享持久化文字、任务与决定；语音会话不拥有任务生命周期。语音播报打断只取消当前播放，不隐式取消Codex/后台任务。这符合新版3.3、6、8、12、13.3、14、15、17节；旧方案“所有用户默认与Super交流”不能进一步推导为每次VAD或状态查看都调用Super。

截至调研日可执行的选型建议：

1. STT优先POC官方Realtime **transcription模式**（当前文档推荐`gpt-live-transcribe`）；客户端VAD断句，转写final可靠提交后才成为指令。fallback为同一adapter下的按句上传转录（兼容当前`gpt-transcribe`能力）或本地sherpa-onnx中文流式ASR；纯文字始终可用。
2. TTS先以独立接口评估sherpa-onnx Matcha中文/中英模型，测真实Mac/Linux机器；云端替代评估官方推荐的`gpt-realtime-2.1-mini`音频工作流，但必须验证仅朗读Super定稿、数字及否定词忠实度。两者都未实测，不能把模型存在写成可交付音质保证。旧OpenAI Speech API只适合已有集成短期兼容，不适合新长期绑定，见第4节弃用时间。
3. 双向原生Realtime语音作为可替换的自然对话模式，需POC证明与Super权限边界成立再启用；不把其语音模型升级成第二个任务管理者。
4. `chatgpt-web-voice`只参考/选择性移植客户端小模块，不整项目嵌入，不把ChatGPT订阅语音网关当官方API或纯STT/TTS。

## 2. 用户指定仓库的实际核验

实际clone：`/tmp/cylo-research-voice`，origin为`https://github.com/Space3044/chatgpt-web-voice.git`，HEAD **`6338ac478070aaa34bfe7a3c0469caf9e9174c02`**。最新提交作者时间`2026-07-25T00:23:56+08:00`，标题`fix(voice): hangup ending UI, barge-in without local mute, text stop_speaking`。本次取得32条提交，最早作者时间2026-07-23；无tag。**这只证明clone可见历史与三个月内未见后续提交，不证明项目停止维护或存在维护SLA。** `go.mod`模块名仍为`github.com/dyhhhhhh/chatgpt-web-voice`，与origin不同；更新时需自行跟踪来源。

固定源码入口：

- [LICENSE：MIT](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/LICENSE)：允许修改/商业使用，复制实质代码须保留版权及许可；上游ChatGPT服务授权不由MIT授予。
- [架构说明](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/docs/ARCHITECTURE.md)：项目自己明确定位为ChatGPT Web Voice网关。
- [Go依赖](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/go.mod)：Go 1.24.1，SQLite、tls-client等。
- [上游会话服务](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/internal/voice/service.go#L25)：`https://chatgpt.com/realtime/wm?dcid=0`、`backend-api/settings/user`、`backend-api/conversation`。
- [客户端完整实现](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/static/voice.html)、[公开能力配置](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/internal/voice/config.go)、[安全头](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/internal/app/static.go#L66)。

| 核验项 | 固定源码事实 | 对Cylo的含义/限制 |
|---|---|---|
| 上游绑定 | ChatGPT Web `access_token`账号池，浏览器身份头/TLS指纹模拟；token不自动refresh；不是OpenAI API key | 依赖用户Web账号会话、订阅/配额和未公开接口；不能推断官方API兼容、SLA或稳定计费；未核验账号条款及实际可用额度 |
| 媒体/协议 | 浏览器`RTCPeerConnection`，SDP经Go网关；音频直接到上游；协商DataChannel `oai-events`, id=0；事件为`state_update`、`chat_message_delta`、`relay_message`、`action_request` | 同为WebRTC不表示与官方Realtime事件兼容；后端没有音频STT引擎，也不能独立替换其上游语音模型 |
| 中文/移动端 | 简中/英文UI；配置列普通话、粤语、简繁体；响应式布局、WebKit AudioContext和移动音频路由best-effort | 是能力声明和适配代码，未验证识别准确率、中文音色、Safari/iPhone/iPad真机；浏览器API受权限与设备限制 |
| 自动转写 | 收`chat_message_delta`字幕及兼容transcript事件，本地文本会话幂等更新 | 流式字幕不是已验证的独立流式ASR接口；未核验稳定final边界、修订及乱序处理能否形成安全任务指令 |
| VAD/免手持 | WebAudio analyser的RMS阈值、持续时间、校准和冷却触发；`requestAnimationFrame`驱动 | 本地音量启发式，不是语义VAD；噪声/扬声器回声会误打断；后台RAF节流/停止是明确风险 |
| AEC | 指定deviceId路径请求`echoCancellation:true, noiseSuppression:true`；默认路径及失败fallback是`audio:true` | 借用浏览器AEC，未自带AEC；约束是请求，不能保证设备实现或消除回声，需`getSettings()`和扬声器双讲实测 |
| 插话打断 | [interruptAssistant](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/static/voice.html#L3773)只发送`action_request:stop_speaking`；最新修复明确不在本地mute | 未收到上游停止前可能继续响；离线通过的控制分支不证明真实中断延迟；不等于取消Cylo任务 |
| TTS/LLM | 上游ChatGPT直接进行语音理解/回复，网关只转协议 | 不是“接收Super文本再合成”的通用TTS服务；另有语音LLM会产生任务控制分歧 |
| 图像/视频 | `ReceiveVideo:false`、mic `video:false`；图片申请Azure SAS后独立上传 | 图像上传不是实时视频通话或屏幕共享，不能宣称音视频全协议支持 |
| 远程与身份 | HttpOnly会话、CSRF、owner隔离、token AES-GCM密封、key仅hash；CSP/`X-Frame-Options:DENY` | iframe嵌入默认就被拒绝；现有admin/API-key owner不是Cylo未来组织/用户/项目权限模型 |
| 撤销边界 | 架构第5.3节明确release不强制掐断已建WebRTC、撤销API Key不能终止已连媒体；网关不持有媒体路径 | 控制平面撤销凭据后仍需主动关客户端轨道/连接，或选择可被服务端终止的媒体会话/relay；不能用网关TTL当可靠录音/播报停止保证 |

**复用建议/推断：**可抽取麦克风选择、WebRTC连接清理、字幕展示、移动播放解锁、打断状态机思路；改为独立组件并替换全部上游事件，补final提交和清空播放队列。MIT实质复制保留声明。账号池、浏览器指纹、ChatGPT Web信令、ChatGPT历史库与Cylo权威状态不复用。整项目即使fork也需要重写核心上游与身份，收益小于模块参考，且iframe嵌入绕不过其frame限制。

## 3. 只比较三条适用路线

| 路线 | 职责/协议 | 优势 | 主要代价与选择条件 |
|---|---|---|---|
| **A 独立STT→控制平面/Super→TTS（主线）** | 浏览器WebRTC或WSS音频到STT；标准化partial/final；控制平面幂等入库；TTS合成服务端定稿 | 不新增对话决策LLM，文字是可审计执行入口；最容易替换供应商，适配现有Codex/Super | 需自行处理VAD、分句、播放缓冲、双讲、取消；总延迟分多段。STT和TTS各自收费；云STT可配本地TTS |
| **B 官方Realtime原生语音/WebRTC** | 临时客户端凭证或服务端统一信令；浏览器媒体直连，事件DataChannel；业务工具留服务端，支持时使用sideband | 双向低延迟、语音语调与原生打断体验好；官方接口优于Web私有协议 | 带额外语音对话模型；再调Super可能双重推理/额外token。必须工具白名单、服务端权限/任务库，回答以真实状态为准；语音LLM不得替Super提交执行。服务器sideband不天然控制客户端播放 |
| **C 自托管sherpa-onnx组件栈** | 本机/服务端流式中文ASR + Silero VAD + Matcha中文/中英TTS，外加Cylo媒体网关/WebSocket适配器 | 可离线、音频可留本地、无语音供应商API key；核心仍可走云Super | 不是现成全双工语音产品，不自带Cylo身份/控制平面/AEC。需模型权重许可核验、CPU/内存/实时率测试、端点与播报队列工程；手机后台限制仍存在 |

路线C实际sparse clone：`/tmp/cylo-research-sherpa`，固定 **`a63f82130d91d99a23a43f346104706881b31ff1`**，可见最新作者时间`2026-10-21T11:48:14+08:00`；本次是浅clone，不能据此统计完整维护历史。[README](https://github.com/k2-fsa/sherpa-onnx/blob/a63f82130d91d99a23a43f346104706881b31ff1/README.md#L99)列streaming/non-streaming ASR、TTS、Silero VAD及Linux/macOS/iOS等，展示中英流式Zipformer/Paraformer、中文Matcha例子。[引擎LICENSE](https://github.com/k2-fsa/sherpa-onnx/blob/a63f82130d91d99a23a43f346104706881b31ff1/LICENSE)为Apache-2.0；**该许可不自动覆盖所有下载的模型/声音/训练数据**，选定权重必须逐项留存模型卡与许可。仅核验源码/README，未下载模型、构建或做推理。因此C是具体可POC替代，尚非性能结论。

## 4. 当前OpenAI官方事实与快速变化边界

官方页面均于2026-10-21实际取得HTTP 200，精确原文、URL和HTML SHA-256见[取证附件](evidence/voice-official-excerpts.md)。没有API账户、真实音频请求或价格实验。

- [Realtime transcription](https://developers.openai.com/api/docs/guides/realtime-transcription)当前写明：`type:"transcription"`，推荐`gpt-live-transcribe`，浏览器WebRTC/服务端WebSocket，无助手语音回复；返回delta和completed。原文：**“The model doesn’t support server_vad or semantic_vad.”** 这是`gpt-live-transcribe`的限制，**不是所有Realtime转录模式都不支持VAD**。[VAD指南](https://developers.openai.com/api/docs/guides/realtime-vad)说明取决于所选转录模型。此模型需client VAD + `input_audio_buffer.commit`；当前文档还明确不提供词级时间戳、speaker label或confidence，不应设计伪置信度阈值。
- completed可跨turn乱序，按`item_id`关联；不要按回调到达顺序直接派任务。`partial`可修订，仅展示；以应用自己的turn序号和committed item关联恢复顺序，缺失final留待恢复/重说，不静默当命令。
- [Speech-to-text](https://developers.openai.com/api/docs/guides/speech-to-text)与转录指南提供文件/已提交音频工作流；adapter须表达`supportsStreaming`、`supportsServerVad`、`supportsTimestamps`等能力。fallback可在客户端按句缓存后上传当前兼容模型；这会牺牲即时字幕，不应冒充真流式。网络与许可允许时可切本地流式ASR。
- [Realtime WebRTC](https://developers.openai.com/api/docs/guides/realtime-webrtc)实际重定向到`/api/docs/guides/voice-webrtc?voice-api=realtime`：浏览器可用短期凭证，永久API key留服务端，SDP与媒体/事件分离。短期token是减小泄露窗口的机制，不是Cylo登录/权限的替代，也不能推导撤销即刻结束已建媒体。
- [Realtime conversations](https://developers.openai.com/api/docs/guides/realtime-conversations)说明语音对话VAD可自动中断；WebRTC/SIP由服务端管理播放缓冲并截断未播音频，WebSocket需客户端停止播放并按实际播放进度truncate。禁用自动回复的`create_response:false`用于兼容该字段的speech-to-speech模式；不能将它套到所有转录模型上。
- [服务端控制](https://developers.openai.com/api/docs/guides/realtime-server-controls)实际重定向到`/api/docs/guides/voice-server-controls?voice-api=realtime`，当前页面混有新Live与Realtime内容：两者字段和事件不可混用。其Realtime段落介绍WebRTC sideband；服务端业务鉴权仍需自行实现。页面明确sideband本身不控制播放，不能撤回用户已听音频；blocking音频需客户端或媒体relay配合。所有型号/字段应在POC固定版本请求验证，不能仅因页面段落存在就宣称可运行。
- [TTS指南](https://developers.openai.com/api/docs/guides/text-to-speech)及[弃用公告2026-10-01](https://developers.openai.com/api/docs/deprecations#2026-10-01-text-to-speech-models)双源确认：`tts-1`、`tts-1-hd`、`gpt-4o-mini-tts`系列将于**2027-01-06**关闭；公告列2025-03-20与2025-12-15快照。推荐`gpt-realtime-2.1-mini`，**不能**把它填到`/v1/audio/speech`，需要改API工作流。不要复制旧“mini TTS长期首选”的选型结论。
- 同一[弃用公告2026-08-26](https://developers.openai.com/api/docs/deprecations#2026-08-26-transcription-models)指`whisper-1`、`gpt-4o-transcribe`、`gpt-4o-mini-transcribe`、`gpt-4o-transcribe-diarize`于**2027-02-26**关闭，推荐`gpt-live-transcribe`或`gpt-transcribe`。fallback应选择仍支持账户/地区的当前接口，不能硬编码已弃用型号作为长期后备。

**Realtime可否只用转录+TTS：**纯转录已有明确官方session模式，适合路线A；TTS适配器与之独立。完整Realtime语音模型可接文本生成音频，但本次文档证据不足以保证“字字不改的纯合成器”，也未实验验证。把Super文本交另一LLM要求朗读仍可能改写/重复计费，须POC验证，关键授权、命令、金额以屏幕原文和控制平面为准。只依赖语音模型“不要改写”的prompt不能形成安全边界。

## 5. 桌面/手机远程接入与免手持边界

**建议：**PWA/响应式客户端作为跨设备访问入口；个人部署优先私网VPN/受控隧道 + 稳定受信HTTPS域名。公网部署经反向代理终止TLS，统一Cylo认证、短期会话、CSRF/origin校验、设备撤销、租户/项目授权、速率/会话/费用限制；仅暴露控制API和语音网关，不能直接暴露Codex、数据库、管理端或永久供应商key。VPN不代替应用鉴权。

麦克风[要求secure context及显式权限](https://developer.mozilla.org/en-US/docs/Web/API/MediaDevices/getUserMedia)；手机不能访问电脑的`localhost`，局域网明文IP也不能视为可生产录音入口。受信TLS与用户点击建立会话后再采集，结束/撤权/退出应停止所有track、清缓冲、关闭连接并更新UI。供应商临时凭证仅按用户授权会话签发，API key留后端；ChatGPT订阅与OpenAI API授权/账单分开，不能假设订阅抵扣API。

| 场景 | 可承诺的产品语义/建议 | 不能承诺的部分 |
|---|---|---|
| 前台免手持 | 用户点击开始后持续监听，VAD自动分句，partial即时字幕，final可靠保存；显眼录音状态与一键停麦；空闲自动结束/计费上限 | 不等于全天候后台唤醒词；RMS不能分清人声与回声 |
| 锁屏/切后台/浏览器被杀 | 服务端任务继续；当前采集/播放状态显式失效或待恢复，回前台检查track、AudioContext与连接，再由用户恢复；通知提供任务入口 | 不保证Safari/PWA后台持续麦克风/WebRTC/自动播报；未做iOS版本真机验证，不能从普通网页API存在推导锁屏能力 |
| 原生iOS/macOS | 若锁屏持续通话为硬需求，单独评估原生音频会话、系统中断、后台模式及平台审核要求 | 原生也不是无限后台常驻；此研究没有完成后台模式合规/真机验证 |
| 跨设备继续 | 从服务端恢复conversation/turn、final transcript、任务/决定及已播文本进度；新设备重新授权建媒体，旧设备可移交麦克风持有权 | 不迁移活跃WebRTC/麦克风权限，不以同一voice session保证同一模型上下文 |
| 通知 | 任务重要事件进入持久通知/决策收件箱；打开后读同一权威任务；播报需尊重用户设置与平台权限 | 推送不是锁屏持续语音链路，不保证强制发声或被杀时自动开麦 |

[Page Visibility API](https://developer.mozilla.org/en-US/docs/Web/API/Page_Visibility_API)明确后台RAF通常停止、timer节流，且screen off属于hidden；候选仓库基于RAF的RMS打断因而特别不适合作为后台承诺。这里对iOS的具体采集生命周期为**保守产品边界/待测**，并非声称所有iOS版本后台必定立即断音。[Web Speech Recognition](https://developer.mozilla.org/en-US/docs/Web/API/SpeechRecognition)仍属limited availability，部分浏览器会送云端且不能离线，不作为统一中文ASR后备；系统键盘听写可作用户自行使用的文字入口。

## 6. 与Super、任务及记忆的契约（建议）

- `dialogue_session_id / turn_id / provider_item_id / client_message_id / device_id`与`project_id / task_id / execution_id`分开；语音会话ID只定位媒体，不是业务权限。服务端生成有序turn并保存final修订号/来源；原始音频默认不留存，若调优需独立授权、保留期限和删除能力。
- `partial`只用于字幕；`final`经用户可见文本/必要确认后，控制平面持久化并回“已接收”。`client_message_id`幂等，重复completed/重连补发不能创建两项任务。跨设备读取这份权威文本；用户更正另存revision，不覆盖原始审计。
- 明确状态查询、停止播报、切设备等由确定性程序处理；复杂新目标才唤醒Super；任务状态只读控制平面真实记录。语音会话断开、token过期、供应商故障不得取消后台任务。
- TTS只能播报已经确定的`assistant_message_id + revision + segment_id`，服务端允许的文本源绑定任务记录；新revision到达时废弃旧队列。先播“已收到”与“任务完成”使用不同事实依据，禁止提前口头承诺已执行。
- barge-in：本地VAD确认人声后立即停播放器/清未播队列，取消当前TTS或Realtime response、记录实际播放游标；迟到旧chunk以generation id丢弃。保持接收用户后续语音。候选仓库仅发上游stop的做法不满足可测本地停止SLO。
- “停一下”默认停播报；“停止登录模块开发任务”才进入业务取消命令。目标歧义、转写的否定词/金额/资源不明确时展示原文确认；高风险操作走现有授权政策，不让语音模型自行降低要求。取消经执行端ack后才显示已停止；中断不撤销已产生副作用。
- adapter契约至少包含STT的start/feed/commit/partial/final/error、能力矩阵与音频format；TTS的synthesize/cancel/chunk及播放游标；Realtime的session/control/playback能力单列。不能强迫所有实现模拟不存在的confidence/server VAD。

## 7. 延迟目标与费用计算（建议，均非已实测）

测量用单调时钟，分别记录speech onset、speech end、VAD commit、partial、final、durable ack、Super首token、首个可播音频、实际播放、barge-in onset/stop，附设备/网络/供应商配置。不能拿服务端首字节代替用户听到声音。

| 指标 | POC起始目标（暖连接、正常网络） |
|---|---|
| 说话后首partial | p50 ≤500ms，p95 ≤1000ms；中文准确率同测 |
| 说完→final+持久接收ack | p50 ≤1000ms，p95 ≤2000ms |
| 确定性进度查询说完→首音 | p50 ≤1500ms，p95 ≤3000ms；不强制经过LLM |
| 需要Super的简短回答说完→首音 | p50 ≤2500ms，p95 ≤5000ms；长任务先持久接收，实际执行另计 |
| 插话开始→扬声器停止旧音 | p50 ≤200ms，p95 ≤350ms；无误取消任务 |

`端到端延迟 = VAD尾静音 + STT final + 控制持久化/排队 + (必要Super推理) + TTS首块 + 播放缓冲`。三路线在同一中文样本/网络下对比；另测冷启动和弱网，不能把以上目标当供应商保证。

不填未经核验的单价。部署日以账户实际可用模型、官方价目与账单为准，记录币种/地区/缓存/计费单位/日期：

- 路线A云费用：`音频输入计费量×STT费率 + Super输入/缓存/输出token×对应费率 + TTS输出计费量×TTS费率 + 重试/网络/存储`。按分钟、字符或token分别计量，不强行统一成分钟；纯STT/TTS不增加一个对话决策LLM，但自身仍有模型计算/计费。
- 路线B：`Realtime音频/文本输入、缓存、音频/文本输出×各费率 + 被调用Super/Workers的费用`。另一个语音LLM重复理解、长会话上下文和静音是否计费均需实际usage/账单验证，不以订阅额度估算API成本。
- 路线C：`主机/加速器摊销+电费+运维+带宽/存储 + 仍需云端的Super/Workers`，API key可免于语音组件但不等于免费；记录并发下real-time factor、内存、首音、功耗。混合STT/TTS分别计算。
- 每会话预算/空闲截止、每用户/租户并发及语音日额度在控制平面执行；语音连接不能常驻调用Super维持心跳。断线重连不盲目重传可能已被接收的turn，token与任务费用分账。

## 8. 已执行验证、复现与必须POC

实际命令与结果（2026-10-21；全部临时目录，无服务部署/真实语音/API调用）：

```bash
git clone https://github.com/Space3044/chatgpt-web-voice.git /tmp/cylo-research-voice
git -C /tmp/cylo-research-voice rev-parse HEAD
# 6338ac478070aaa34bfe7a3c0469caf9e9174c02
git -C /tmp/cylo-research-voice log -5 --format='%h %aI %s'
git -C /tmp/cylo-research-voice rev-list --count HEAD
# 32；git tag无输出
node docs/research/evidence/voice-static-check.cjs /tmp/cylo-research-voice
# PASS: 2 inline scripts pass node --check
# PASS: active stop_speaking, duplicate suppression, muted mic guard, text interruption
# NOT TESTED: DOM, WebRTC, audio, upstream acknowledgement, Chinese recognition, mobile devices
command -v go
# 无路径；未运行go test、编译或网关启动，不把缺失工具链写成项目测试失败
git clone --depth 1 --filter=blob:none --sparse https://github.com/k2-fsa/sherpa-onnx.git /tmp/cylo-research-sherpa
git -C /tmp/cylo-research-sherpa rev-parse HEAD
# a63f82130d91d99a23a43f346104706881b31ff1
curl -L --fail --max-time 30 https://developers.openai.com/api/docs/guides/realtime-transcription -o /tmp/cylo-doc-live-transcription.html
curl -L --fail --max-time 30 https://developers.openai.com/api/docs/guides/text-to-speech -o /tmp/cylo-doc-tts.html
curl -L --fail --max-time 30 https://developers.openai.com/api/docs/deprecations -o /tmp/cylo-doc-deprecations.html
# 实际取证以Python urllib并行抓取为主；上述curl可复现相同URL抓取，动态页面hash可能改变
```

[离线脚本](evidence/voice-static-check.cjs)固定HEAD，检查两个内联JS语法，并从源码提取打断函数，仅stub事件发送/状态。四项控制分支通过只证明代码在这些stub下发送预期事件与防重/静音行为；**不证明浏览器播放停止、AEC有效、ASR/TTS可用或移动兼容**。无Go工具链，未安装系统依赖以避免把研究变成运行部署。

必须在实际开通账户与Mac/iPhone/iPad设备后完成以下POC，方可确认交付能力：

| POC | 最小验收 |
|---|---|
| 中文与混合领域语音 | 至少100条真实人声：普通话、口音、中英混说、代码名/日期/金额/否定词、静音/噪声。建议整体CER≤10%作为起始门槛，关键语义错误单列，危险指令必须触发确认；partial不可执行，空白/截断不能变成任务。目标是否足够由产品定，不是已测成绩 |
| VAD/AEC/双讲 | Mac Safari/Chrome、iPhone Safari/PWA、iPad扬声器及耳机；安静/电视/厨房噪声下每种至少20次插话，p95停止≤350ms，10分钟单向播报误打断≤1次；记录AEC getSettings，检查否定句尾不被过早截断 |
| 模型/接口与TTS | 真实创建当前transcription session、commit、delta/final、跨turn乱序；逐项确认gpt-live无server VAD；替代模型/本地断句fallback。TTS逐条对照Super原文的数字/否定/专名，Realtime改写判失败；测本地TTS冷/暖首音、实时率和并发 |
| 可靠控制 | 重复final/丢ack/断网重连各20次，任务只能建一次；播报中断旧chunk不得复活；“停播”不取消任务，明确任务取消必须有执行ack；语音模型伪造完成也不改变任务状态 |
| 安全/撤销 | 无登录/跨owner/跨项目不得发临时凭证或控制会话；撤权时浏览器关闭track与媒体，恶意不配合客户端验证服务端终止/relay机制。测token过期、超额与活跃会话断开行为，不能只测下一次HTTP请求401 |
| 前后台/远程 | Wi-Fi/蜂窝切换、VPN、NAT/UDP受限、锁屏/后台1/5/15分钟、来电/耳机断连、系统回收后恢复；前后台语音边界清晰，后台任务保持；测试需记录iOS/macOS/浏览器版本。WebRTC另测TURN需求，不把仅STUN配置当复杂网络可达保证 |
| 成本与长会话 | 30分钟中文对话含10分钟静音、3次重连/打断，同时运行后台任务；逐turn usage与账单对账，验证预算边界、重试/静音实际计费，记录缺失终止usage而非编造精确成本 |

POC未测项：上述全部真实媒体/账户/设备场景；没有测延迟、中文CER、音色、实际配额/费用、锁屏行为、TURN可达、撤销后的媒体终止或生产恢复。不将仓库README截图、commit日期、官方示例存在等同这些实验通过。
