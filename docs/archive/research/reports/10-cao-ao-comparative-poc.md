# 编排需求符合性与单主线实施路线

日期：2026-10-21。**用户已明确采用 OrchestratorInc/agent-orchestrator（AO，Go）；当前实施主线为 AO + Space + Cylo 控制平面，Cylo 新服务端优先 Go。** 不再安排 CAO/AO 双 daemon 选型对跑。下文保留已完成的 CAO 模块/AO 命令清理实验，采用 AO 不改变这些实验的证明范围。人工无缝接管可选，官方 macOS Codex App 不在范围，首期仅保证 Linux 服务端与执行宿主部署，其 CLI 仍需验收；macOS 客户端/浏览器及用户自行使用 CLI 保留，macOS 后台运行后续按需评估。

## 本轮交付：产物清理边界的可复现切片

已有 [02](../../../research/02-orchestration-and-codex.md) 实验覆盖 Codex 原生 thread 持久化与 CAO 强制清理；[09](../../../research/09-agent-orchestration-alternatives.md) 已有候选源码对照。本轮不重复原生文字交接，新增**相同 Git 场景下的 CAO 模块执行与 AO 命令契约执行**，以确定清理保护的实际 Git 后果。不是完整双 daemon PoC，也没有自动 Super 推理。

| 场景 | CAO | AO |
|---|---|---|
| 两个工作区修改相同文件 | 实际上游 create_worktree 创建、互不覆盖 | fixture Git 创建、互不覆盖；未执行 AO Create |
| 普通清理脏工作区 | 实际上游 remove_worktree 丢弃修改与未跟踪产物 | 从固定源码提取 worktreeRemoveArgs 并执行 Git，返回128、原产物仍在 |
| 强制清理边界 | 普通 teardown 已使用 force | 提取 worktreeForceRemoveArgs 后直接执行，会删除脏产物；未执行 ForceDestroy 前置保存链路 |
| 已提交产物 | 清理后未合并分支保留、可 git show 读回 | 命令清理后分支保留、可 git show 读回；未验证 daemon 后续分支策略 |
| 新进程重读 | 新 Git 进程能发现两个注册工作区 | 相同；仅 Git 注册可重读，不能称 daemon/任务恢复 |

**结论：AO 的普通 Git 删除命令比 CAO teardown 更保守，但两者都有危险清理路径。** AO Go 的 Destroy 还有 discard/reclaim、错误分类与保存逻辑，本轮没有执行这些分支；不能将命令层结果写成 AO 整体安全或端到端胜出。CAO 首期必须在 teardown 前保存并核验未提交/未跟踪产物；AO 首期必须确认只有成功保存后才可 ForceDestroy。普通服务日志、ignored 文件、符号链接及外部路径仍需独立产物清单，Git commit 不覆盖所有成果。

源码版本：CAO `4e990a9d78b154ad76c6cf94c7b51e36fce69f21`，AO `7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff`。脚本检查 HEAD 和实际执行/提取文件与 Git 固定版本一致；结果另记录文件 SHA256、Python/Git 版本。环境无 Go/tmux，本轮未安装整套工具链。AO go.mod 要求 Go 1.27.1，后续先确认官方工具链可取得，失败须记录阻断，不能降 go.mod 后声称原版通过。

复现依赖仅 Python 3.10+、Git 和固定源码，不需要 Codex、账号或联网。源码缺失时，先在新目录 clone，再 `git fetch origin <SHA>`、`git checkout --detach <SHA>`；已有目录先核对来源，勿覆盖。上游获取命令见02/09；路径可通过参数指定：

```bash
python3 docs/archive/research/evidence/cao-ao-worktree-poc.py \
  --cao /tmp/cylo-research-cao --ao /tmp/cylo-alt-ao \
  > /tmp/cao-ao-worktree-poc-result.json
```

[可执行脚本](../evidence/cao-ao-worktree-poc.py) 使用临时 HOME、禁用用户/系统 Git 配置，只有临时仓库被修改，退出后清理；不启动模型或访问真实会话。当前 [结果快照](../evidence/cao-ao-worktree-poc-result.json) 的 `false` 是实际观测到 CAO 丢弃脏产物的预期断言，**不是安全验收通过**。临时路径已归一化为 `$TEMP`。实际 Worker 写入由确定性 fixture 代替，不把它算作 LLM 工具调用。

## 按需求推进的下一批实施与验收

AO 提供 orchestrator/worker、独立工作区、Codex native ID/resume 与 host/reconcile，作为已选执行底座接入；Cylo 管业务账本、授权、记忆、长期普通服务与语音池调度。AO 的 fresh fallback 不能算同 thread 恢复，采用决定也不证明完整业务恢复或清理安全。AWS CAO 的研究与 [07](../../../research/07-cao-cli-handoff-plan.md) 改造方案保留历史用途，不再作为当前实施计划。完整映射见[选型文档](../../../project-selection-comparison.md)。

1. **落地 AO adapter。** 以 [09](../../../research/09-agent-orchestration-alternatives.md) 固定源码为起点，核验 Go 工具链和 daemon/API/事件契约，建立 task→run→AO session→host/Worker→worktree→native thread 映射，接入现有 Codex resume，约束显式权限并验证清理前保存/核验。仅一套执行 owner；Cylo 持有任务、授权、预算和结果权威。先不做可选的人工无缝交接状态机。
2. **贯通一条真实产品链路。** 先以临时工作区与离线 fixture 验证接线，再在获准环境验证复杂目标→Super 拆分→两个独立 Worker/worktree→产物与测试验收；简单请求可由 Cylo 直接路由 Worker，不额外调用 Super。消息送达、终端 idle 不算任务完成。无需为此再跑 CAO 副本。
3. **验收恢复、停止、权限和保全。** 重启控制服务、杀 Worker、缺 native history、工具执行后丢回执，分别记录存活重附着、原 thread resume、fresh/fork、result-unknown；未知副作用不自动重放。停止须确认进程树和写入终止。按 AO Codex adapter 的实际参数去掉默认审批/sandbox 绕过路径，验证实际允许/拒绝。普通/强制清理注入保存失败时不得删除；tracked、untracked、ignored、符号链接和外部产物按清单核验。首期在 Linux 验收后台常驻与主机迁移并留证；跨环境备份导出保留，macOS 后台验收后续按需安排。
4. **接入已定语音与长期能力。** Space 保留网页实时媒体和账号池，完成稳定语义提交→幂等 command→实际任务回执→播报，以及额度耗尽后新媒体会话/业务上下文恢复。这是集成验收，不是重新选择 Space 的门槛；无账号/设备时标未测，继续完成可离线开发。普通后台服务另交 OS supervisor，不能绑定 Super 或媒体会话的寿命；记忆与授权也不借用编排 daemon 的会话状态。

每步交付 `executed / simulated / static / not-tested`、版本、任务/进程/原生 thread 映射、故障注入、恢复模式、产物校验、权限拒绝证据与残余进程记录。离线 fixture 不证明真实模型或生产可靠性；已有实验失败与手工补救均保留，不能计为自动恢复。

当前不重开底座选型；实际缺口按 AO adapter/必要上游改造逐项落实。AO 的 Codex 账号池与 CLIProxyAPI 接入等待专门报告确认，不预先列为必需新增组件。语音真实通话、移动音频和费用实验仍按已授权环境单独验收，本次文档修订不运行 daemon 或连接账号。
