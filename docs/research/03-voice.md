# Cylo 实时语音实施方案：上游网关、账号池与 Super 桥接

研究日期：2026-10-21。已完整阅读《Cylo-完整产品需求说明书.md》及本仓库总方案、04架构和既有语音证据。本文纠正上一版选型：**采用 Space3044/chatgpt-web-voice 的 ChatGPT Web 实时语音上游网关及多账号池，端到端双向实时语音是实施主线。** 账号语音额度有限，账号选择、会话粘性、额度耗尽后的恢复是必要组成。独立 STT+TTS 不作为该需求的替代实现。

本文只交付研究、设计和离线研究脚本；未写产品 runtime、部署网关、导入真实账号或进行真实通话。文中“源码已实现”“建议新增”“待 POC”严格区分。沿用 OpenAI Docs 技能核对官方资料，但官方 Realtime 能力仅作比较与协议备选，不自动适用于 ChatGPT 网页私有协议。

**最新决定：Space3044/chatgpt-web-voice 已选定，不再以业务桥接实验决定是否采纳。后续验证用于补齐该方案的集成缺口、确定能力交付范围。** 语音模型与 Super 的职责、并发及推荐拓扑见 [12 语音 Agent 与 Super 边界](12-voice-super-boundary.md)。

## 1. 采用范围与固定版本

上游 origin：`https://github.com/Space3044/chatgpt-web-voice.git`；本次实际 `git ls-remote origin HEAD` 与本地 `/tmp/cylo-research-voice` 均为 **`6338ac478070aaa34bfe7a3c0469caf9e9174c02`**。MIT许可应随实质复制保留；不 vendor 上游源码。32条可见提交，无tag，最新作者时间2026-07-25；这些事实不能推导维护SLA。Go module仍用 `github.com/dyhhhhhh/chatgpt-web-voice`，fork更新须按来源/SHA管理。

**最小采用方式（建议）：** 将固定版本 fork 作为独立内部语音网关进程，保留其 Go 信令服务、Web token账号池、加密、下游 API、粘性元数据及 WebRTC 客户端协议实现；Cylo通过服务端 adapter访问。客户端把原 `static/voice.html` 的媒体/字幕/打断部分拆成Cylo组件，保留MIT说明。管理端可先内网独立使用；无需iframe（源码CSP/X-Frame-Options默认阻止），不必把账号库并入Cylo任务库。按下文增量修补配额/租约/业务桥接，不重写上游为STT/TTS。

固定源码链接均基于上述SHA：

| 模块 | 已实现事实与位置 |
|---|---|
| [账号池](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/internal/accounts/pool.go) | `Pick`按最少最近使用排序，`PickByID`粘性选取，`MarkInvalid`禁用；`Stats.Available`仅按disabled/status计数 |
| [上游服务](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/internal/voice/service.go#L628) | `buildSessionJSON`、`ProbeAccountToken` L740、`CreateSession` L943；Web token、代理、浏览器身份/TLS指纹、SDP代理与账号绑定 |
| [下游API](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/internal/api/downstream.go#L25) | 服务端Bearer key；只返回SDP/session handle，不返回账号/token；context接口存上游ID |
| [路由](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/internal/api/server.go#L131) | `/v1/voice/sessions`建连、`/{id}/context`元数据更新、`DELETE /{id}`释放；管理端`/api/accounts`增删改/check |
| [通话元数据](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/internal/callsessions/store.go) | owner/account/upstream IDs持久化；active/released和last_seen；不是并发租约、真实媒体存活或上游额度账本 |
| [客户端](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/static/voice.html#L3773) | `interruptAssistant`、`sendRelayMessageTo` L3822、`applyCaptionDelta` L3849、`sendTextMessage` L4285；私有信令/字幕 |
| [公开媒体配置](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/internal/voice/config.go) | negotiated DataChannel `oai-events`, id=0；收音频、不收视频；中文/粤语和音色列表是配置声明，未证明真机效果 |

## 2. 精确的媒体、信令及凭据路径

```text
客户端麦克风 ←→ WebRTC/SRTP ←→ ChatGPT Web 实时语音服务
客户端私有 DataChannel(oai-events, negotiated id=0) ←→ 同一上游会话
       │ SDP offer/answer、会话控制、字幕/业务事件转送
       ▼
Cylo HTTPS/BFF + VoiceSessionAdapter ←→ 内部Go网关 /v1/voice/sessions
       │                               │ 选号/解密access_token
       │                               └→ POST https://chatgpt.com/realtime/wm?dcid=0
       │                                  multipart(sdp, session JSON)
       └→ Cylo command/task/event → 必要时Super → Worker/Codex
任务真实回执 → adapter → 客户端 → 私有relay_message → 上游实时语音回复
```

建连顺序：用户经Cylo登录并显式开麦→控制面分配会话/设备持有权及账号租约（新增）→浏览器取媒体、建PeerConnection与DataChannel→offer经Cylo BFF转到内部网关→网关用池内ChatGPT `access_token`发往`realtime/wm`→校验SDP answer再返回→浏览器设置远端描述，媒体直接连上游。网关不解码/存储音频，普通HTTP account proxy也不是WebRTC媒体代理；UDP受限/TURN和NAT路径必须真网测。

三个凭据不可混用：**Cylo登录凭据**鉴别用户/设备；**网关下游Bearer API key**鉴别Cylo后端（源码owner为`api_key:<id>`，是网关密钥，不是官方临时token）；**ChatGPT Web access_token**由网关持有并用于上游。该路径没有已证实的官方Realtime ephemeral key签发步骤，不能凭空添加“网关已签发官方临时令牌”。客户端只拿短期Cylo会话授权和SDP/非秘密handle；长期网关key、Web token、加密主密钥不进入浏览器、Super/Worker上下文或日志。

若Cylo全部用户共用一个网关key，网关只识别一个owner：BFF必须再次校验tenant/user/session归属；团队版优先每workspace隔离owner/key及账号池授权，未授权不得跨租户借账号。池内token AES-GCM密封、key哈希、敏感字段脱敏机制保留；加密主密钥独立于DB备份管理。账号导入、替换、启停、删除均走管理权限与审计。上游会话表默认`history_and_training_disabled:false`是源码事实，需真实验证可用隐私设置并按用户数据策略配置，不能宣称音频/上下文绝不进入上游历史。

## 3. 账号管理与额度：源码边界和必要新增

现有管理API支持逐账号导入`access_token`、email、proxy、status/disabled，更新token、启用/禁用、删除、探活；legacy import是显式迁移工具，不等于通用多平台OAuth。账号池不会自动refresh，过期需重新导入/替换。列表只给预览；`tokenutil/jwt.go`本地解码exp且不验签，**exp只是展示线索，不是身份验证、语音额度或重置时间**。

| 信号 | 源码真实语义 | 实施策略（新增） |
|---|---|---|
| `settings/user` 200+JSON | token被接受；没有解析剩余语音分钟/次数/重置字段 | `auth_health=alive`，`quota=unknown`，不显示“额度充足” |
| 账号`available`/PoolStats | 未手动禁用且status非禁用 | 改名/解释为“可尝试”，与已验证有额度分开 |
| wm 401 | `MarkInvalid`；新会话可尝试下一账号，已粘性会话直接报错不静默切换 | 保留；鉴权失效单列，替换凭据后重新探活 |
| wm 429或其他非2xx | 当前统一返回502；没有429轮换/配额冷却；网络错误也立即返回 | 保留原HTTP/错误分类和安全响应头，区分rate limit、明确quota exhausted、挑战/阻断、暂态网络 |
| 通话中额度耗尽 | 没有已核验的quota事件解析/恢复状态机 | 抓真实脱敏事件后建立白名单映射；未知错误保留unknown，不能凭字幕一句“限额”就断言耗尽 |
| quota reset/余额 | 当前源码无查询API、解析、存储或刷新器 | 未知保持null；不凭经验编“每日某时恢复”或将每个账号承诺固定分钟 |

建议新增`voice_account_state(account_id, auth_health, quota_state, quota_remaining nullable, unit nullable, reset_at nullable, reset_source, observed_at, cooldown_until, last_error_kind, next_probe_at)`。`reset_at`只接受服务端向正确上游发出请求后观察到、能说明字段语义的响应/受信协议事件，保存来源/时间/脱敏证据摘要；客户端报告、模型文本、JWT exp、管理员估算不能升格为“已知重置”。HTTP Retry-After仅解释为允许再尝试的时间，除非上游明确说明，不能当账户语音配额reset。

没有可信reset时，指数退避+jitter+有限半开探测，UI显示“额度未知/冷却至某时再尝试”，冷却结束只进入probing，成功建连才变`usable_observed`，仍不声称剩余量已知。明确耗尽而无重置来源时进入exhausted并按策略低频探测；管理者可请求探测，不能靠点击启用制造额度。全池不可用则等待/提示补充账号，不无限轮询；纯文字暂时可用，但不把它称为实时语音已交付。

### 并发租约与会话账号粘性（新增）

`Pick`加mutex只串行“选一次”，不预留并发slot；LRU不等于负载/额度调度。增加事务性`voice_account_lease(account_id, cylo_voice_session_id, owner, device_id, epoch, expires_at, state)`，候选同时满足账号启用、owner准入、无冷却、slot可用；POC初始每账号最多1通，真实并发支持未知，不能宣称上游硬上限为1。选号+预留同一事务，SDP失败释放；重试排除集、最大尝试数、总时间预算有界，epoch校验阻止旧请求改写新会话状态。

连接成功后把account、上游conversation/parent/voice ID绑定到`media_generation`，整段通话不逐轮轮换。心跳续租、明确挂断释放；断线短暂保留恢复窗口。同账号重连优先使用原ID，但源码注释承认上游可能忽略resume字段，因此“字段已发送”不是上下文真正恢复。网关重启把active元数据标released、内存绑定消失，不能凭SQLite记录声称媒体已断。

租约到期不能证明直连WebRTC已停；需客户端关闭track/PeerConnection的确认与保守quarantine，不能马上复用slot。恶意客户端不配合时，现有信令网关无法强制切断已建媒体；若要求可靠服务端终止/硬并发保证，需验证上游终止接口，或新增由Cylo持有的WebRTC端点/媒体relay（不是普通TURN转发即可提供应用会话控制）。即使加relay也需要协议POC，不宣称已经实现。

### 耗尽切号与上下文恢复（新增）

同账号瞬时网络错误先有界重协商；鉴权失效/明确额度耗尽→标记旧generation失效、停旧音、关闭旧媒体、冻结旧账号/进入冷却→分配其他可用账号，**建立新的上游会话**。清空旧账号的全部上游conversation/parent/voice ID，绝不能把A账号thread ID传给B；Cylo conversation/project/task IDs保持不变。换号会有建连间隙，不承诺音频流无缝迁移。

恢复包来自Cylo持久库：最近确认的用户文本/修订、已接收command IDs、真实任务状态、必要项目事实和权限范围内的摘要；不自动重放已提交业务命令。通过下节私有relay恢复上下文需POC验证；它是新会话的文本背景，并非模型KV、语音韵律或原ChatGPT历史迁移。没有可靠注入路径时明确显示“新语音会话，后台任务已保留”，请求必要简短复述，不能假称已恢复。旧generation迟到字幕、回执与音频不得污染新会话；客户端播放闸门按generation控制。

## 4. 实时语音模型如何接到Super/Worker

上游ChatGPT语音assistant本身具备理解与自主回复能力，在Cylo中定位为**交互Agent/会话入口**；VoiceSessionAdapter则是Cylo新增的确定性协议适配代码，二者不可混为一个组件。语音Agent负责实时听说、澄清、自然交谈，仅提出业务意图，复杂任务的正式规划与调度由Super负责。它不拥有Cylo任务状态、授权或Worker调度权。复杂业务请求交Super，简单真实状态读取走确定性服务；Worker按现有控制面授权独立执行。上游语音模型说“完成了”不改变任务状态，屏幕回执与任务账本以执行证据为准。

### 源码能力矩阵：不要混用协议

| 能力 | 此仓库/ChatGPT私有路径 | 官方Realtime比较（另一路协议/凭据） |
|---|---|---|
| tools/function calling | `buildSessionJSON`显式`client_tools:[]`，没有Cylo工具schema、函数调用分发/结果回传实现 | 官方支持tools/function calling；需官方API会话和服务端授权，不能把schema直接塞入Web私有接口并宣称可用 |
| instructions | 没有专门instructions传参或session更新；`primary_assistant`上游身份存在 | 官方`session.update`等可更新instructions；不证明Web接受 |
| 注入文本/上下文 | 已有`relay_message`双层封装+`buildBaseUserMessage(content_type:text,parts)`，来源角色是user；底层再包`data_message`+JSON字符串 | 官方`conversation.item.create`及function_call_output语义不能移植为私有payload |
| 字幕 | `chat_message_delta`全量/append；兼容transcript分支只是“若有” | 官方delta/completed边界不代表私有字幕具相同final语义 |
| sideband | 网关只代理SDP，未发现官方call_id或服务器attach/WS入口 | 官方WebRTC call_id可attach；Web端的vs_/uppercase UUID不是该call_id |
| 模型 | requested_default_model/model_slug为空，由上游分配；voice_mode=wingman | 不能把官方某型号能力/价格套到本账号实际被分配语音模型 |

### 主线最小桥接：字幕/relay adapter（建议、真实能力待POC）

1. 客户端收私有字幕，按provider message ID维护partial/revision；将原始协议事件的安全投影转发到Cylo认证WSS/HTTP。服务端为turn分配稳定ID。**现有`rememberDcText`会保存正在变化文本，不能直接当final任务入口**。先抓真实事件找可复现turn完成信号、修订与乱序规则；仅静音超时不能证明转写final。未找到可信final时POC显式按键确认/提交该句，不假称全免手持已完成。
2. 完成用户turn→持久化`voice.turn.committed`→幂等command提交→服务端返回durable ack。幂等键包含Cylo conversation/turn/revision；修订关联原command做更正策略，不能新起重复业务动作。未提交turn断线保持草稿；已提交但ACK丢失查询command状态，不重执行。
3. adapter将控制面`accepted/running/waiting/completed/failed`真实回执构成受限文本消息，带`receipt_id/task_id/revision/generation`及可信展示原文；经现有私有`relay_message`以**user角色文本**请求上游语音assistant解释结果。它是最小可尝试的语音反馈路径，不能谎称支持system/tool角色。合成的回执消息ID需列入排除集，不能将其字幕再次识别成用户业务指令形成循环。
4. 同一实时语音模型继续听说和接受打断。回执播报依赖当前Web语音会话，不另接独立TTS。它可能改写、提前回答、忽略提示或重复发声；POC需验证上游何时自动应答、能否停止当前发声、按回执触发语音、否定词/数字是否正确。提示词约束不是安全边界，控制面拒绝其任何无授权业务副作用；重大审批绑定具体屏幕对象/command摘要。
5. **两次回答问题需显式验收**：上游可能先自行回应用户，稍后又播Cylo结果。最小版本可明确先说“已接收/待核对”，但源码尚无可保证自动回复抑制/自定义身份的接口。若不能约束，记录为响应/播放控制集成缺口，继续在选定上游内修补；此时只能标记“实时语音对话 + Cylo独立任务状态”，业务语音闭环未验收，不把原生通话可用冒充“Cylo执行闭环可用”。

桥接接口建议：输入`voice.turn.partial/committed/interrupted`, `voice.session.error`；输出`business.receipt.available`, `voice.context.bootstrap`, `voice.playback.stop`；公共字段`workspace_id/user_id/device_id/cylo_conversation_id/cylo_voice_session_id/media_generation/turn_id/provider_message_id/revision/event_id`。`business.receipt`含真实状态/证据引用，声音不提供授权。服务端将客户端事件视作该用户提供的输入，客户端转发不能当上游签名证明；不允许转发伪造的“task completed”改变权威数据。

### 进一步协议路径（同一需求主线不变）

**首选增强POC：** 在独立fork实验分支探索Web私有`client_tools`的实际schema、tool请求/完成事件、instructions/context角色和自动回复开关。必须取得真实脱敏请求/响应证据，逐项验证拒绝/错误与副作用边界；单凭字段名或官方样例不能宣称支持。若发现私有tool接口，绑定少量`cylo_submit_request/cylo_get_task_status`工具，仍由控制平面执行，禁止直接把Worker凭据暴露给上游。

**需要服务端直接观测时：** 探索私有上游是否有真实sideband；目前没有证据。可改为Cylo后端作为上游WebRTC peer并向客户端桥接媒体/DataChannel，使业务控制不依赖前端在线，但会新增媒体运维/延迟/音频处理，属于更大改造POC，且不能凭媒体接管创造不存在的tools/instructions能力。

**比较用协议备选：** 官方Realtime WebRTC+server sideband具文档化工具/上下文控制，可实现相同Cylo adapter契约，但使用官方API凭据与独立计费，不沿用Web token账号语音额度。仅当用户后续选择时启用该替换provider；当前实施依然保留指定Web网关/账号池，不能用比较项取代主线。官方Live和Realtime当前同页出现，也要逐分支固定协议，不能交叉使用事件。

## 5. 双向通话、打断、VAD、字幕与连续性

| 能力 | 保留/补充与验收边界 |
|---|---|
| 双向实时通话 | 保留WebRTC双向媒体与上游语音生成；用户讲话无需先独立STT再TTS。中文、粤语和中英混说、回声/耳机均需真机测，不从语言列表推导准确率 |
| VAD/免手持 | 上游分段行为待实测；本地RMS+校准/hold/cooldown只辅助barge-in，是音量启发式，不是语义VAD。RAF后台可能停止，不能承诺锁屏免手持 |
| AEC | 保留浏览器echoCancellation/noiseSuppression请求；默认/失败路径audio:true需统一约束并记录getSettings；硬件兑现和双讲需实际测 |
| 打断 | 原码只发`action_request:stop_speaking`，故意不本地mute；不能保证扬声器立即停止。新增本地播放闸门/代次及明确恢复条件，配合上游停止观察/超时，不再发生永久静音；实际音轨连续时如何区分迟到旧音与新音必须POC |
| 任务取消 | 打断默认只停当前发声；明确“停止某任务”才提交业务cancel，执行端ack后才说停止；不能挂电话就取消后台工作 |
| 字幕与执行回执 | partial即时展示，final/修订/来源/可靠接收分别标示；任务卡片由Cylo事件驱动，上游语音文字是对话内容，不能冒充执行结果证据 |
| 跨设备 | 共享Cylo conversation/task/context，设备语音持有权租约防双播；新设备重新建媒体。同账号恢复尽力，换账号新建会话+恢复包，不搬迁活跃PeerConnection |
| 远程/前后台 | Mac/iPhone/iPad受信HTTPS、显式开麦、前台PWA先验；切后台/锁屏/来电/被杀后重新核验track/AudioContext/连接。后台任务和通知收件箱独立继续；持续锁屏通话需原生音频会话真机POC |
| 撤权/结束 | 关闭所有track、播放器/缓冲、DataChannel、PeerConnection，释放租约。网关DELETE只解除绑定，不是上游媒体终止证明；参见第3节relay/终止接口门槛 |

## 6. 已选方案的集成与能力验收顺序

| 阶段 | 最小改造/依赖 | 准出证据 |
|---|---|---|
| V0 固定与原生通话 | 固定SHA、内部Go网关、账号管理/加密、客户端WebRTC组件、Cylo认证BFF | 单账号真实中文双向语音、字幕、文本relay、stop事件；保留脱敏协议trace，确认上游实际模型/能力未知项 |
| V1 池与恢复 | 在现有pool/service/call_sessions/schema增加账号状态、错误分类、事务租约、有限重试、粘性、新generation恢复；不改真实额度 | 2账号测试：禁用/401、并发冲突、已建会话耗尽、429类别未知、冷却半开、全池不可用、重启、跨账号不传旧ID；真实限额行为与mock结果分列 |
| V2 Cylo执行桥 | 稳定final边界、事件/command幂等、Super/Worker契约、回执relay、防合成消息回环、恢复上下文 | 说“开发登录模块”只建一任务；进度来自任务库并语音返回；丢ACK/重连不重执行；换账号后仍能查询同任务；两次回答/错误完成陈述被测出且处理 |
| V3 自然交互与多端 | VAD/AEC/打断播放闸门、设备持有权、网络切换、前后台恢复、上下文权限 | Mac Safari/Chrome、iPhone/iPad Safari/PWA扬声器/耳机；每种至少20次插话；前后台/锁屏1/5/15分钟记录；旧generation迟到音/字幕不复活 |
| V4 协议增强（按V2结果） | 探索真实client_tools/instructions或服务端peer；保持同一上游账号池；官方协议仅另行比较 | 每项支持独立trace/错误/恢复证据；没有受支持接口时明确能力缺口，不能用mock或官方文档代替 |

目标（建议、未达成）：暖连接说完→首音p50≤1.5秒、p95≤3秒（业务Super推理单列）；插话→扬声器停止旧音p95≤350ms；100条中文/数字/否定/代码名样本分列语义错误。记录speech_end、真实final、durable_ack、Super结果、上游首音、实际播放和打断停止时间；不能把SDP成功、字幕更新或网络首字节当听感延迟。

费用/额度观测：Web账号剩余语音额度不是API token价目表。按账号记录已观察成功/失败/时长/切换次数与来源；不能用本地通话秒数推导实际剩余分钟或保证多账号无限使用。Super/Worker费用、主机带宽、媒体relay（若采用）另计。可强制的是本系统会话数、并发、空闲截止和停止新派发；无法靠本地账本增加上游配额。

## 7. 已执行研究验证与复现

新增[账号/协议离线PoC](evidence/voice-pool-poc.py)是设计状态机的内存mock，并对固定源码字段/分支做静态断言；既有[JS检查](evidence/voice-static-check.cjs)从真实源码抽取打断函数执行stub。均不连接上游、不证明真实quota、tools、sideband、中文、延迟、打断音效或真实通话。

```bash
git clone https://github.com/Space3044/chatgpt-web-voice.git /tmp/cylo-research-voice
git -C /tmp/cylo-research-voice rev-parse HEAD
git -C /tmp/cylo-research-voice ls-remote origin HEAD
# 均6338ac478070aaa34bfe7a3c0469caf9e9174c02
python3 docs/research/evidence/voice-pool-poc.py /tmp/cylo-research-voice
node docs/research/evidence/voice-static-check.cjs /tmp/cylo-research-voice
```

实际结果见[本轮取证附件](evidence/voice-realtime-audit.md)。当前环境无Go工具链，未运行go test/编译/网关；不能称上游Go测试失败。旧[官方摘录](evidence/voice-official-excerpts.md)保留为比较证据，已明确不作为Web私有协议依据。没有真实凭据/设备/收费请求，全部真实媒体、真实配额与Cylo语音执行回执能力尚待上述POC。
