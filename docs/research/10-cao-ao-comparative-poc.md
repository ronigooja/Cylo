# 下一步：CAO / AO 同口径执行验证

日期：2026-10-21。**先完成离线 daemon 对照，再决定是否从 CAO 改选 AO。** 当前基线仍为 awslabs/CAO + Space Go；OrchestratorInc/AO Go 是优先候选，尚未批准替换。人工无缝接管可选，官方 macOS Codex App 不在范围，macOS/Linux CLI 仍需验收。

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

## 下一批执行顺序与通过条件

1. **双 daemon 离线最小任务。** 固定上述版本，在独立临时 HOME / CODEX_HOME / AO_DATA_DIR / CAO数据目录和独立 tmux socket 运行。给官方 Codex 配置仅 loopback 的假 Responses 后端，清除真实凭据；fixture 返回可识别文本/工具动作。通过真实 CAO assign/handoff、AO spawn/send 创建一个 Super 与两个 Worker，各自独立 worktree，改同名文件并保存产物。捕获 API、进程、工作区、thread ID 与任务关联。先验证一条路径能跑，再扩展第二条；遇到不支持的 fixture 协议应报告失败，不替换成自行写的调度器后声称项目通过。
2. **故障恢复和停止。** 分别重启 daemon、杀 Worker、删除原生历史、让工具执行后丢回执。区分存活进程重附着、同 thread resume、fresh launch、result-unknown；未知副作用不得自动重放。核对停止后进程/写入确实停止、dirty 产物和分支仍可读。AO 缺 native history 时必须显式记录 fresh fallback，CAO 若缺绑定则记未实现，不能用 CAO terminal ID 替代 Codex ID。
3. **权限和保全门槛。** 两方案均用显式受限配置，验证允许工作区写入、拒绝工作区外写入与未授权网络。CAO 默认 --yolo、AO default 权限分支绕过审批的问题已有源码证据，本轮未执行权限测试；仅更换配置名不算强制隔离通过。对普通清理/强制清理分别注入保存失败，失败时不得删工作区；检查 tracked、untracked、ignored、符号链接与恶意路径产物。宿主账号和任意真实项目目录不参与实验。
4. **真实执行验收后作选型决定。** 离线 fixture 只能证明接线和状态恢复。后续在明确授权的测试账号/预算中，跑一次真实拆分→执行→验收→恢复，记录模型版本、usage/费用、耗时、失败率和适配改动量。通过后再写 ADR 决定底座；当前不具备成本/可靠性排名证据。

每步交付同一格式的场景结果：`executed / simulated / static / not-tested`、进程和版本、任务→Worker→worktree→native thread 映射、故障注入、恢复模式、产物校验、权限拒绝证据、失败及残余进程。失败同样保留日志，不把手工补救计为自动恢复。

**语音高风险项应并行优先验证：** 保留 Space 与网页实时上游，做实时语音→稳定业务提交→真实任务回执播报及额度耗尽后新会话/上下文恢复，优先离线协议与幂等 fixture，再使用获准测试账号。此项与编排共用任务契约但独立验收；Go 同栈不能替代它。iPhone/iPad 音频、macOS/Linux 与跨环境迁移仍留实际环境验收。
