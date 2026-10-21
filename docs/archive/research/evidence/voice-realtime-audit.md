# 实时语音与账号池复核证据

日期2026-10-21，范围为研究。本文支持[03](../../../research/03-voice.md)，没有真实账号、通话、设备或收费API请求。

## 源码与协议

`git -C /tmp/cylo-research-voice rev-parse HEAD`和`git -C /tmp/cylo-research-voice ls-remote origin HEAD`均返回：

```text
6338ac478070aaa34bfe7a3c0469caf9e9174c02
```

精确定位（均为此SHA，不依赖未来main）：

- `internal/voice/service.go:628 buildSessionJSON`：`client_tools: []any{}`；`conversation_mode.kind=primary_assistant`；`history_and_training_disabled=false`；无Cylo tools/instructions桥。conversation/parent注释明确上游可能忽略字段，仍作新会话。
- `service.go:740 ProbeAccountToken`：settings/user 200 JSON→alive；401禁用；其他unknown。实现分支按status==401处理，虽然注释强调JSON，代码没有先检查JSON；不能把“所有HTML 401绝不会禁用”写成保证。
- `service.go:943 CreateSession`：live binding→account ID→durable账号→LRU；粘性账号失败不静默切换。`1085`的401分支禁用；其他非2xx/非SDP在`1113`之后统一502，不继续轮换、也不保留额度/重置字段。
- `internal/accounts/pool.go`：`Pick`是LRU选择而非slot预留，`PickByID`保留账号，`MarkInvalid`禁用；没有remaining/reset/cooldown字段。
- `internal/api/downstream.go:25`：Bearer key owner，下游只保留voice_session_id，不返回账号/token；`context`只存ID，不注入模型conversation内容。
- `static/voice.html:4081 buildBaseUserMessage`：构造`author.role='user'`；`3822 sendRelayMessageTo`将双层relay_message发至上游DataChannel。`4285 sendTextMessage`和`3925`附近preview greeting确实走此上行输入候选路径，并非客户端之间互传。底层`sendDcEventTo`再包`{type:"data_message", data:JSON.stringify(eventObj)}`；复用时必须保留这个外层，而非直接发送官方Realtime事件。
- `static/voice.html:3849 applyCaptionDelta`：读取`message.author.role`或`audio_transcription.direction(in/out)`、全量/append增量。`4193`附近接收字幕，`4224`附近transcript子串匹配仅兼容候选。没有可靠的final状态判断，`rememberDcText`也会保存partial；不能直接触发业务执行。
- `static/voice.html:3773 interruptAssistant`：只发私有`action_request/stop_speaking`，不本地mute；停止事件发出不证明音频已停，也不禁止下一次自主回复。
- `internal/tokenutil/jwt.go`：exp解码显式跳过签名验证，只用于显示；不是配额reset。

原有报告中MIT、媒体直连、无自动refresh、CSP禁止iframe、浏览器AEC请求、RMS/RAF打断、释放绑定不结束已建WebRTC等准确观察保留；“不采用账号/上游”“STT+TTS主线”属于已被本轮纠正的选型，不再有效。

## 实际执行的离线检查

```text
$ python3 docs/archive/research/evidence/voice-pool-poc.py /tmp/cylo-research-voice
PASS source assertions: empty client_tools, 401/502 branches, LRU/sticky, relay/stop construction
PASS mock: sticky reacquire; two slots; concurrent third selection rejected
PASS mock: exhausted account excluded; replacement gets new generation and clears upstream context
PASS mock: stale epoch rejected; repeated committed turn produces one command
PASS mock: local cooldown retry eligibility; unconfirmed media closure quarantined; disabled excluded
NOT TESTED: actual Go code execution, transactions/concurrency races, upstream quota/reset, WebRTC, final boundary, relay acceptance, tools, sideband, speech fidelity

$ node docs/archive/research/evidence/voice-static-check.cjs /tmp/cylo-research-voice
PASS: 2 inline scripts pass node --check
PASS: active stop_speaking, duplicate suppression, muted mic guard, text interruption
NOT TESTED: DOM, WebRTC, audio, upstream acknowledgement, Chinese recognition, mobile devices
```

Python脚本账号调度为**单进程串行内存mock**，只证明建议不变量在给定模拟转移下成立；不证明并发race/数据库事务实现、真实池逻辑执行或上游额度。脚本60秒只为本地重试策略示例，不是上游reset。JS测试仅抽取真实函数运行stub，无DOM/媒体。无Go工具链，未运行go test/编译。

## 官方比较材料：重新实际取回

环境没有文档搜索工具；尝试`https://developers.openai.com/search?q=Realtime%20function%20calling%20sideband`返回404，随后实际取回以下官方正文。本节仅证明官方API能力，不能为网页私有协议背书。

1. [Realtime server controls](https://developers.openai.com/api/docs/guides/realtime-server-controls)重定向至`voice-server-controls?voice-api=realtime`，本次HTTP取回原HTML SHA256：`d7fe797add3067c3e405923b77518b00e7a447b808a4c8955b269408dee1a4f3`。

   Realtime分支原文：

   > using that call ID with the URL wss://api.openai.com/v1/realtime?call_id=rtc_xxxxx

   > In this way, you are able to add tools, monitor sessions, and carry out business logic on the server instead of needing to configure those actions on the client.

   示例用`OPENAI_API_KEY`和`session.update`。同页Live的`/v1/live/sessions/{session_id}/attach`与`session.instructions.append`是另一分支，不能与Realtime或ChatGPT Web UUID混用。

2. [Realtime conversations](https://developers.openai.com/api/docs/guides/realtime-conversations)，HTTP200，SHA256：`9206ba0c522161e9c68bfcb3d67ed6f02de07f534885ab9167930ccf2f033f57`。

   原文：

   > Create a new text conversation item using the conversation.item.create client event.

   > The conversation item type is function_call_output

   > Once we have added the conversation item containing our function call results, we again emit the response.create event from the client.

   支持官方协议的工具结果和session instructions，但上游Web源码没有这些实现。

既有[官方摘录](voice-official-excerpts.md)保留此前实际抓取的转录/TTS/弃用等事实，现仅为比较/历史证据，不用于本轮主线选型或宣称Web语音能接独立STT/TTS。

## 必须真实验证的三个桥接门槛

1. 私有用户字幕可复现的final/完成信号、角色、修订、乱序和去重；沉默计时不能冒充上游final。
2. 真实Cylo回执经user-role relay进入**同一**实时会话并口头正确反馈；对数字、否定、任务状态逐条核对，不只验证send()或字幕出现。
3. 上游接收麦克风会自主先答，当前无禁止回复/授权身份注入证据；验证可否阻止先说“已完成”和重复应答。提示词和stop_speaking不能天然保证。

任一未过，能力状态只能是“候选原生实时语音对话 + Cylo独立任务状态”，**业务语音闭环未验收**；继续保留指定上游/账号池研究和改造，不改口以独立TTS算完成。
