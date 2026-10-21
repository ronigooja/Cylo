# Cylo

Cylo 是一个跨设备、可长期运行、管理真实任务与记忆，并在授权下调用 Agent 和部署普通后台服务的开放式 AI 系统。本仓库当前是**实现前研究与架构决策材料**，尚未开始产品实现。

先读 [实现调研与架构方案](Cylo-实现调研与架构方案.md)：包含总体推荐、两个指定候选的取舍、关键阻断、全需求覆盖与验收路线。推荐自有持久控制平面、按需 Super/独立 Worker、可替换语音；Codex app-server 是优先适配与 POC 主线，生产支持、鉴权和官方 App 双向原生互通仍须过门槛。CAO 不直接承担总控；chatgpt-web-voice 仅参考客户端局部模块。

| 文档 | 作用 |
|---|---|
| [Cylo 完整产品需求](Cylo-完整产品需求说明书.md) | 唯一当前需求基线，原《Rumi-完整产品需求说明书》只做产品更名；完整需求未被阶段裁剪 |
| [旧 Codex Super 讨论稿](codex-super-agent-requirements-and-architecture.md) | 历史参考，已由新版取代；其中技术建议不是新版硬约束 |
| [01 需求追踪](docs/research/01-requirements.md) | 全22章、稳定需求ID、场景A—E、逐项验收 |
| [02 编排与 Codex](docs/research/02-orchestration-and-codex.md) | 固定源码、CAO取舍、官方接口、离线POC与原生交接边界 |
| [03 语音](docs/research/03-voice.md) | 候选源码、当前官方API事实、替代路径与移动端POC |
| [04 架构契约](docs/research/04-architecture.md) | 数据、任务、事件、权限、长期能力、记忆与迁移设计 |
| [05 可靠性与安全复核](docs/research/05-reliability-security-operations.md) | 故障恢复、隔离、撤权与生产验收矩阵 |

研究日期为2026-10-21。源码事实使用固定SHA，官方事实来自实际获取的页面；建议、源码能力、离线实验和未验证项在各报告分别标识。Mac官方App双向接续、真实工具/审批完整恢复、iPhone/iPad音频、生产可靠性及性能成本均不能由当前材料宣称通过。

复现证据时，先看 [02 的版本与隔离脚本](docs/research/02-orchestration-and-codex.md) 和 [03 的官方证据](docs/research/evidence/voice-official-excerpts.md)。上游仓库不包含在本仓库中，需按固定SHA取回；不要只clone最新main就假定复现同一结论。02 的脚本仅使用临时Git目录和临时CODEX_HOME；运行前核对路径，不能改成真实用户会话目录。离线provider实验不证明成功模型调用或完整产品恢复。

Codex/CAO隔离复现实验使用 Python 3、Git、报告固定版 Codex CLI 与 CAO 源码；先按02取得上游，再运行 `python3 docs/research/evidence/orchestration-poc.py`。它验证空thread、首输入持久化及临时worktree清理，不发送真实模型请求。可靠性实验的依赖、命令及证明边界见05及其附件。

语音静态检查的可执行复现方式如下，仅验证固定源码的JS语法和中断分支，不连接上游：

```bash
git clone https://github.com/Space3044/chatgpt-web-voice.git /tmp/cylo-research-voice
git -C /tmp/cylo-research-voice checkout --detach 6338ac478070aaa34bfe7a3c0469caf9e9174c02
node docs/research/evidence/voice-static-check.cjs /tmp/cylo-research-voice
```

若临时路径已存在，先检查该目录来源与SHA，勿覆盖他人数据。API型号、弃用日期、支持状态和鉴权规则会变化，实施冻结前须重新查阅官方正文；报告中的访问成功或许可分析不代表已有账户准入或部署授权。
