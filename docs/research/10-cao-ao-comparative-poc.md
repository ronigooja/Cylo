# 编排需求符合性与单主线实施路线

日期：2026-10-21。**当前建议保持 CAO + Cylo 控制平面，不再安排 CAO/AO 双 daemon 对跑。** 用户已选定 Space Go 网关、ChatGPT 网页实时上游和账号池，尚未指定改用 AO。下文保留已完成的清理实验事实；原双项目运行计划已按需求收敛为单主线适配。人工无缝接管可选，官方 macOS Codex App 不在范围，macOS/Linux CLI 仍需验收。

## 本轮交付：产物清理边界的可复现切片

已有 [02](02-orchestration-and-codex.md) 实验覆盖 Codex 原生 thread 持久化与 CAO 强制清理；[09](09-agent-orchestration-alternatives.md) 已有候选源码对照。本轮不重复原生文字交接，新增**相同 Git 场景下的 CAO 模块执行与 AO 命令契约执行**，以确定清理保护的实际 Git 后果。不是完整双 daemon PoC，也没有自动 Super 推理。

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
python3 docs/research/evidence/cao-ao-worktree-poc.py \
  --cao /tmp/cylo-research-cao --ao /tmp/cylo-alt-ao \
  > /tmp/cao-ao-worktree-poc-result.json
```

[可执行脚本](evidence/cao-ao-worktree-poc.py) 使用临时 HOME、禁用用户/系统 Git 配置，只有临时仓库被修改，退出后清理；不启动模型或访问真实会话。当前 [结果快照](evidence/cao-ao-worktree-poc-result.json) 的 `false` 是实际观测到 CAO 丢弃脏产物的预期断言，**不是安全验收通过**。临时路径已归一化为 `$TEMP`。实际 Worker 写入由确定性 fixture 代替，不把它算作 LLM 工具调用。

## 按需求推进的下一批实施与验收

CAO 已有 Super/Worker 委派、独立执行/worktree、收件箱和 workflow 基础，匹配产品 §6、8；继续使用的条件是补齐明确的基础恢复、停止、权限和清理缺口，而非认定其已经可靠。AO 的 native ID/resume 与独立 host/reconcile 更现成，但仍需 Cylo 业务账本、普通服务监管、记忆和授权，且 fresh fallback 不能算同 thread 恢复。现有证据不足以支持为这些优势更换整套 daemon/领域模型；Go 同栈仅是维护加分项。完整映射见[选型文档](../../Cylo-开源项目选型比较.md)。

1. **落地 CAO 最小 adapter。** 按 [07](07-cao-cli-handoff-plan.md) 固定版本、建立 task→run→Worker→worktree→native thread 映射，补 Codex resume、显式受限权限和清理前产物保存/核验。仅一套执行 owner；Cylo 持有任务、授权、预算和结果权威。先不做可选的人工无缝交接状态机。
2. **贯通一条真实产品链路。** 先以临时工作区与离线 fixture 验证接线，再在获准环境验证复杂目标→Super 拆分→两个独立 Worker/worktree→产物与测试验收；简单请求可由 Cylo 直接路由 Worker，不额外调用 Super。消息送达、终端 idle 不算任务完成。无需为此再跑 AO 副本。
3. **验收恢复、停止、权限和保全。** 重启控制服务、杀 Worker、缺 native history、工具执行后丢回执，分别记录存活重附着、原 thread resume、fresh/fork、result-unknown；未知副作用不自动重放。停止须确认进程树和写入终止。去掉默认 `--yolo`，验证实际允许/拒绝。普通/强制清理注入保存失败时不得删除；tracked、untracked、ignored、符号链接和外部产物按清单核验。Mac/Linux、后台常驻与迁移按对应环境留证。
4. **接入已定语音与长期能力。** Space 保留网页实时媒体和账号池，完成稳定语义提交→幂等 command→实际任务回执→播报，以及额度耗尽后新媒体会话/业务上下文恢复。这是集成验收，不是重新选择 Space 的门槛；无账号/设备时标未测，继续完成可离线开发。普通后台服务另交 OS supervisor，不能绑定 Super 或媒体会话的寿命；记忆与授权也不借用编排 daemon 的会话状态。

每步交付 `executed / simulated / static / not-tested`、版本、任务/进程/原生 thread 映射、故障注入、恢复模式、产物校验、权限拒绝证据与残余进程记录。离线 fixture 不证明真实模型或生产可靠性；已有实验失败与手工补救均保留，不能计为自动恢复。

若实施证据表明 CAO 的必需接口无法满足，或具体适配工作量明显高于 AO 集成，再只针对该缺口作替换决策；不把继续横向调研当作所有开发的前置条件。语音真实通话、移动音频和费用实验仍按已授权环境单独验收，本次文档修订不运行 daemon 或连接账号。
