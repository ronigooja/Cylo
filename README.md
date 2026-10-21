# Cylo

Cylo 是一个跨设备、可长期运行、管理真实任务与记忆，并在授权下调用 Agent 和部署普通后台服务的开放式 AI 系统。本仓库当前是**实现前研究与架构决策材料**，尚未开始产品实现。

当前实施主线已由用户确定为 **OrchestratorInc/agent-orchestrator（AO，Go）+ 现有 CLIProxyAPI + Space3044/chatgpt-web-voice + Cylo 持久控制平面**，Cylo 新服务端优先采用 Go。AO 管多种 Agent runtime 的编排、代码执行与原生会话；Cylo 管业务任务账本、记忆、授权、长期普通服务和语音池调度。AWS CAO 研究与实验作为历史证据保留，不再是当前实施底座。详见 [需求符合性与选型](Cylo-开源项目选型比较.md) 和 [实施架构](Cylo-实现调研与架构方案.md)。

AO 的 coding agent runtime（如 Claude Code、Codex、OpenCode）与其底层 model/provider 是两层配置；Cylo 按任务选择 runtime 和可用模型，不要求全部经 Codex。会话恢复优先复用 AO 已有 adapter，无需先开发 Codex resume 补丁；各 agent 的原生恢复、fork 和 fresh launch 能力分别验收。见 [多 Agent 与模型路由边界](docs/research/16-ao-agent-provider-boundaries.md)。

**用户已选定 AO，以及 Space 的 Go 网关、ChatGPT 网页实时上游和账号池。** 语音稳定提交、业务回执和额度调度仍待集成验收。语音 Agent 负责自然交互，复杂任务由 Super 协调，任务与权限以 Cylo 控制平面为准。Codex 账号池与出站代理复用已有远程 CLIProxyAPI 实例（文档别名 `existing-cli-proxy`），不新增部署；每条 Codex route 由该代理选号，AO 无需重复轮换。Space 维持独立语音账号池。用户报告当前 Codex 登录和使用均可走代理；这是用户环境说明，不是本仓库实测，也不证明逐账号登录已自动隔离。AO 执行宿主使用可达的代理 API endpoint 与服务端密钥引用；`CODEX_HOME`、工作区和原生 thread 历史仍留执行宿主。官方 Codex 桌面 App 已取消；人工无缝接管可选；基础 CLI 执行、恢复、停止查看，以及 macOS/Linux 部署与跨环境迁移仍在范围。

| 文档 | 作用 |
|---|---|
| [开源项目总对比](Cylo-开源项目选型比较.md) | 当前需求符合性、已定选择与单主线实施建议 |
| [Cylo 完整产品需求](Cylo-完整产品需求说明书.md) | 当前需求基线；桌面App取消，人工无缝接管可选，基础CLI与实时语音保留 |
| [旧 Codex Super 讨论稿](codex-super-agent-requirements-and-architecture.md) | 历史参考，已由新版取代；其中技术建议不是新版硬约束 |
| [01 需求追踪](docs/research/01-requirements.md) | 全22章、稳定需求ID、场景A—E、逐项验收 |
| [02 编排与 Codex](docs/research/02-orchestration-and-codex.md) | 固定源码、CAO取舍、官方接口、离线POC与原生交接边界 |
| [03 语音](docs/research/03-voice.md) | 实时语音网关/账号池源码、Cylo桥接、额度调度与真机POC |
| [04 架构契约](docs/research/04-architecture.md) | 数据、任务、事件、权限、长期能力、记忆与迁移设计 |
| [05 可靠性与安全复核](docs/research/05-reliability-security-operations.md) | 故障恢复、隔离、撤权与生产验收矩阵 |
| [06 实时语音接入独立复核](docs/research/06-realtime-integration-review.md) | 源码边界、业务桥接门槛与最终一致性检查 |
| [07 CAO CLI接续改造](docs/research/07-cao-cli-handoff-plan.md) | 历史 CAO 基础CLI恢复与可选接管设计，供缺口参考 |
| [08 语音候选比较](docs/research/08-voice-project-alternatives.md) | 同源网页网关、LiveKit/Pipecat、协议/账号池/技术栈边界 |
| [09 编排候选比较](docs/research/09-agent-orchestration-alternatives.md) | AO、CAO、OpenClaw等实际源码、恢复与技术栈比较 |
| [10 编排需求符合性与实施路线](docs/research/10-cao-ao-comparative-poc.md) | 既有清理实验、AO 适配边界与单主线验收 |
| [11 语音真实 PoC 准备](docs/research/11-voice-real-poc-readiness.md) | 真实语音闭环的环境、账号及验收准备 |

研究日期为2026-10-21。Linux同机同隔离CODEX_HOME的合成文本已通过app-server→官方交互CLI→app-server三轮接续；这是两个官方入口的fixture实验，并非CAO已支持。基础CLI真实工具/审批恢复、iPhone/iPad音频、跨环境迁移、生产可靠性及性能成本仍须验收；同会话人工双向接续为可选增强。官方桌面App、深链及官方host handoff不再作为Go/No-Go门槛。

复现证据时，先看 [02 的版本与隔离脚本](docs/research/02-orchestration-and-codex.md) 和 [03 的实时语音取证](docs/research/evidence/voice-realtime-audit.md)。上游仓库不包含在本仓库中，需按固定SHA取回；不要只clone最新main就假定复现同一结论。02 的脚本仅使用临时Git目录和临时CODEX_HOME；运行前核对路径，不能改成真实用户会话目录。离线provider实验不证明成功模型调用或完整产品恢复。同原生thread三轮交接的脚本/实际history快照见02的P5；它通过官方CLI与app-server进行合成文字读写。

Codex/CAO隔离复现实验使用 Python 3、Git、报告固定版 Codex CLI 与 CAO 源码；先按02取得上游，再运行 `python3 docs/research/evidence/orchestration-poc.py`。它验证空thread、首输入持久化及临时worktree清理，不发送真实模型请求。可靠性实验的依赖、命令及证明边界见05及其附件。

语音离线检查的可执行复现方式如下：JS检查验证固定源码语法和中断分支；Python检查验证源码断言及内存mock调度不变量，均不连接上游，不证明真实并发/额度或语音效果：

```bash
git clone https://github.com/Space3044/chatgpt-web-voice.git /tmp/cylo-research-voice
git -C /tmp/cylo-research-voice checkout --detach 6338ac478070aaa34bfe7a3c0469caf9e9174c02
node docs/research/evidence/voice-static-check.cjs /tmp/cylo-research-voice
python3 docs/research/evidence/voice-pool-poc.py /tmp/cylo-research-voice
```

若临时路径已存在，先检查该目录来源与SHA，勿覆盖他人数据。账号池调度与 Super 桥接是待实现设计；现有网关尚无完整额度调度，网页私有协议不能套用官方 Realtime 的工具或 sideband 能力。API型号、弃用日期、支持状态和鉴权规则会变化，实施冻结前须重新查阅官方正文；报告中的访问成功或许可分析不代表已有账户准入或部署授权。
