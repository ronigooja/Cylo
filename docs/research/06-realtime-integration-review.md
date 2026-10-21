# 实时语音接入独立复核

复核日期：2026-10-21。本轮遵循用户指定主线：采用 **Space3044/chatgpt-web-voice 上游网关 + 多 ChatGPT 账号池 + 原生实时语音**；Cylo 持久控制平面与 Super 执行业务工具。本文审查方案与源码边界，不代表已完成运行实现。未另行调用官方 API，也不将其字段视为网页协议能力。

## 1. 固定源码事实

独立只读检查 `/tmp/cylo-research-voice`，固定 SHA `6338ac478070aaa34bfe7a3c0469caf9e9174c02`。以下链接均锁定此版本。

| 项目 | 可见实现与精确边界 |
|---|---|
| 账号选择 | [pool.go](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/internal/accounts/pool.go)：`Pick` 按最久未用选启用账号，`PickByID` 恢复指定账号，`MarkInvalid` 将 401 账号禁用。`last_used_at` 不构成占用锁；无排他租约、单账号并发上限、额度余额或自动恢复时间。 |
| 建连与失败 | [service.go#L943](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/internal/voice/service.go#L943)：新会话可在 401 后尝试其他账号；已绑定账号失败则返回错误，不静默换号。429 等非成功响应进入通用 502 分支，不代表已处理额度冷却或自动换号。 |
| 粘性与续聊 | 同文件通过 owner、`voice_session_id`、账号 ID 及持久 `call_sessions` 恢复绑定。`conversation_id`/`parent_message_id` 是同账号续聊线索；源码自己说明上游可能忽略。保留 ID 不证明恢复语义完整，更不证明可把旧账号线程迁到另一账号。 |
| 媒体路径 | [voice.html#L2274](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/static/voice.html#L2274)：浏览器创建 `RTCPeerConnection` 与协商 DataChannel；网关 HTTP 向 `/realtime/wm` 转交 SDP 并返回 answer。音频与 DC 使用 WebRTC 媒体路径，可能有 ICE/TURN 中继，Go 网关不是必经媒体代理。服务端若要接管 DC 或精确阻断音频，需新增并验证相应路径。 |
| 释放 | [service.go#L549](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/internal/voice/service.go#L549)：release 删除内存绑定并标记记录，不向 WebRTC 对端发终止请求。绑定 TTL 也不是媒体时限或排他租约。 |
| tools 与指令 | [service.go#L628](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/internal/voice/service.go#L628)：`client_tools:[]` 固定为空，无自定义 tools 注册、函数调用往返、可配置 system instructions 或 sideband 实现。公开下游接口严格拒绝未知字段，不能在请求里加官方 `tools`/`instructions` 就认为生效。 |
| 文本上行 | [voice.html#L3822](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/static/voice.html#L3822)、[L4285](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/static/voice.html#L4285)：`relay_message` 经 DC 向上游发送；外层是 `data_message`，内层再包装 `relay_message`。文本输入与音色预览使用此路径，`buildBaseUserMessage` 固定 `author.role:user`。这是上游文本输入的实现证据，不能提升为 tool-result、system 指令、精确朗读或服务端 sideband 的证据。 |
| 字幕边界 | [voice.html#L3849](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/static/voice.html#L3849)：`chat_message_delta` 解析 `author.role` 或 `audio_transcription.direction`，并合并 append；没有可靠 final/完成事务判定。其他 transcript 分支只按事件名子串分类，不能据此自动派发一次业务任务。 |
| 打断 | [voice.html#L3773](https://github.com/Space3044/chatgpt-web-voice/blob/6338ac478070aaa34bfe7a3c0469caf9e9174c02/static/voice.html#L3773)：`stop_speaking` 请求停止上游当前说话，未撤销 Cylo command、Super turn 或外部副作用，也没有禁止上游随后生成答案的证据。 |

## 2. 最小接入方案与必须补齐的边界

采用指定网关维护账号与信令；Cylo 负责账号租约、额度分类与冷却、任务事件及工具执行。浏览器承担音频/DC，向已鉴权的 Cylo 后端报告用户轮次；客户端事件不授予额外业务权限。

可研究的最小桥接是：**明确完成的用户轮次 → Cylo 幂等 command → Super/工具 → 已落库结果 → 经当前客户端 DC `relay_message` 请求上游播报**。这是一条新增工程路线，现成源码没有完整闭环。没有稳定完成信号时，可以先展示候选字幕并由用户明确提交；此时只能验收“实时语音 + 确认提交”，不能称免手持自动任务闭环。不能用沉默超时或每个 partial 直接触发业务执行。

上游先收到麦克风，本就可能先产生自主回复；写一句“不要编造执行结果”的用户提示不等于可靠控制。正式业务闭环须验证响应控制或可信播放门控，保证执行尚未完成时不向用户播放“已完成”的虚假报告；结果来源、任务状态与失败均以 Cylo 记录为准。若只证明文本能送达而没有控制先答及结果忠实度，只能验收实时对话通道，不能宣称业务播报完成。自动关闭整个远端音轨又会破坏自然实时对话体验，需测延迟与插话体验后决定实现。

换账号时：关闭旧 PeerConnection、释放旧租约、创建新 voice_session 与上游线程；保留 Cylo 的 task/command 和本地文字上下文，在新会话中使用经验证的输入方式重建所需摘要。不能携带旧账号 conversation ID 冒充新账号续聊；任何摘要注入、工具回注都须单独验证。网络重连也必须按幂等键防止任务重放。

## 3. 验收清单

1. **两账号额度实验**：分别模拟 401、429、普通限流、网络错误和真正语音额度耗尽，留存脱敏响应/事件，证明分类、冷却与恢复；不能把所有 429 永久禁用，也不能把通用 502 当已知额度耗尽。耗尽切换有可见重连提示，旧媒体关闭、新账号可继续，业务任务不重做。
2. **租约及粘性**：两个终端并发、网关重启、客户端消失、租约过期、已释放会话再次拨号；核对账号容量与 owner 隔离，证明过期租约不会在旧媒体仍活跃时重复出租。源码本身尚无此保证。
3. **轮次事务**：从真实脱敏 DC 事件建立 fixtures，覆盖角色区分、重复、修订、乱序、无完成事件、同一句含否定更正；只明确提交一个 command，不将 assistant 或回注文本再次当用户命令。
4. **工具与播报**：实际说“建任务/查进度/取消”，验证 Super 的执行成功、等待审批、失败和未知状态分别播报准确；工具未运行、断网、结果回注重复时不得报成功。证据包含 command/event ID、真实结果、上行消息、收到的字幕与录音，不能只验文本发送函数。
5. **插话与取消**：静音、停播、断开语音、取消某业务任务分别验证；用户说“停一下”不隐式撤销长期任务。明确业务取消须进入授权与幂等取消状态，已发生外部副作用须如实报告。
6. **移动与恢复**：iPhone/iPad Safari、Mac 扬声器双讲、锁屏/切后台、麦克风权限变化及多设备抢占；测首次出声、插话停止和换号重连延迟。性能目标须以实测分位数记录，不能由 WebRTC 协议名称推断。

## 4. 本轮实际验证范围

在固定源码上独立执行：

```bash
node docs/research/evidence/voice-static-check.cjs /tmp/cylo-research-voice
```

结果：2 段内联脚本通过语法检查；当前 `stop_speaking`、重复抑制、麦克风静音保护和文本打断分支通过。该离线实验不检查 DOM、WebRTC、上游确认、音频、中文识别或设备行为。

尝试 `GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local go test ./internal/accounts ./internal/voice ./internal/api`，当前环境没有 `go`（exit 127），所以没有独立复跑 Go 测试。不使用现存测试文件来冒充测试通过；本轮无账号登录、真实上游建连、额度消耗或音频实验。

最终整合复核基于主线 `1d35ae3`（包含语音 `b3d9746` 与 Codex 接续 `8f3e8ed`）。再次执行 `python3 docs/research/evidence/voice-pool-poc.py /tmp/cylo-research-voice` 与上述 JS 检查，均通过。Python 结果仅是固定源码静态断言和单进程串行内存 mock，不证明真实 Go 调度、并发互斥或上游额度。

全仓 Markdown 本地文件链接检查通过；扫描 README、总方案、01—05 及证据附件，未发现仍被推荐的“仅参考客户端/拒用上游账号池/独立 STT+TTS 主线”。旧官方语音摘录已标为比较/历史证据。03、04 与本文对无可靠 final、user-role 回注、先答控制、账号新建媒体切换及撤权边界一致。

按 **OpenAI Docs** 技能另做窄范围官方链接复核：没有文档检索工具，官方 `/search?q=Codex%20app%20SSH%20handoff` 返回 404，随后实际获取 [CLI reference](https://developers.openai.com/codex/cli/reference.md)、[commands](https://developers.openai.com/codex/reference/commands.md)、[remote connections](https://learn.chatgpt.com/docs/remote-connections.md)，均 HTTP 200（前两者重定向至 learn.chatgpt.com）。正文确认 `/app` 打开同一 saved chat、本地 thread 深链、App 经 SSH 启动远端 app-server、host handoff 转移 chat 与 Git state 并先中断当前 response，与 [02](02-orchestration-and-codex.md) 一致。Linux 假 provider 三轮原生同 thread 接管以 02 的已存实验为证，本复核未重跑；最新用户决定已取消官方Codex桌面App支持；上述App、深链和host handoff资料仅作历史事实，不是推荐依赖或Go/No-Go门槛。最新优先级将人工 CLI 双向同会话交接改为可选增强；macOS/Linux 基础 CLI 执行、停止查看、持久恢复、真实工具/必要审批与跨环境迁移仍需验收；远端CLI可SSH进入同一backend环境。三turn实验是app-server↔CLI，并非CAO已完成接续。语音上游/网关/账号池主线不变。
