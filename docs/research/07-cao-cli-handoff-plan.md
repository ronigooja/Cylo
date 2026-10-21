# CAO + 原生 Codex CLI 接续改造

日期：2026-10-21。范围：官方 **Codex CLI**，不支持官方 macOS 桌面 App。这里只做设计与源码核验，没有提交产品 runtime 或修改上游实现。语音继续采用 Space3044 实时上游 + 网关 + 多账号池，本报告不改语音路线。

## 直接结论

**最新优先级：首期验证自动 Super 拆分、独立 Worker、多 worktree、长任务、持久恢复、产物和成本。若沿用 CAO，先改 Codex adapter、原生会话关联、resume、停止查看及成果保留。路线 A 的人工同 thread 附着/交回控制和复杂交接协议降为可选后续，不作为选型或首期 Go/No-Go 前置。CAO 暂保留为拟采用候选，进一步候选比较仍在进行。** 不必为 CLI 接续重写 Codex 会话引擎，也不必先把 CAO 换成 app-server。

两种操作要分开：

- **CLI 还在运行：附着原 tmux。** CAO 已有这个能力，用户进入的是同一个原生 Codex 进程，历史、工具与当前界面都还在；无需新 CLI 或 thread ID 才能附着。新增重点是暂停 CAO 自动输入与回收，确保只有人可以写。
- **原 CLI 已退出：原 thread 恢复。** 要可靠记住官方 thread ID、host/store/worktree，再启动 `codex resume <id>`。当前 CAO Codex provider 没有这条通路，需要补代码。只恢复历史不等于恢复已死的工具进程或未解决审批。

| 与需求的匹配 | 复用与补足边界 |
|---|---|
| Super/Worker、多CLI、协作、可观察终端 | CAO 是匹配的编排底座；复用 provider、MCP handoff/assign/send_message、inbox、现有数据库与工作流，不重做整套调度 |
| 官方 CLI 双向接续 | 在 CAO 内补原生身份、resume、手动控制与安全保留；现有抓屏正则、默认 `--yolo`、force 清理是需改造的具体风险，不足以否定整个项目 |
| Cylo 完整产品 | Cylo 外层补用户业务 task/run、持久客户端事件、独立常驻服务、长期记忆和实时语音/账号池；CAO workflow/terminal 状态不自动等于这些产品契约 |

CAO terminal ID、tmux session/window 名称都是 CAO/终端身份，**不是 Codex thread ID**。CAO 的 MCP `handoff` 是 agent 协作方法，也不是这里的人机控制权交接。

## 已核验源码

CAO 固定提交 [`4e990a9d78b154ad76c6cf94c7b51e36fce69f21`](https://github.com/awslabs/cli-agent-orchestrator/tree/4e990a9d78b154ad76c6cf94c7b51e36fce69f21)，v2.5.4。以下路径均相对于 `src/cli_agent_orchestrator/`，类/函数名来自该提交。

| 现有实现 | 核验结论 |
|---|---|
| `backends/tmux_backend.py`：`TmuxBackend.attach_session`、`prepare_web_attach`；`api/main.py`：`terminal_ws`，`/terminals/{terminal_id}/ws` | 已能真正附着原 tmux/PTY；不是启动第二个 Codex。多个 tmux 客户端能同时输入，现有附着不构成独占接管 |
| `cli/commands/worker.py`：`attach` | `cao worker attach` 是 send/read 对话轮询，源码明确不是 PTY；不能把这个命令说成真正 tmux 附着 |
| `providers/codex.py`：`CodexProvider._build_codex_command`、`initialize`、`exit_cli` | 组装新 `codex`，默认 `--yolo`；退出命令 `/exit`。没有 Codex resume 参数与原生 ID 回传 |
| `providers/manager.py`：`ProviderManager.create_provider`、`get_provider` | `resume_session_id` 显式仅允许 Claude Code，Codex 会被拒绝；重建 provider 是重新关联 adapter，不等于重启原 thread |
| `clients/database.py`：`TerminalModel` | 有 cwd、provider、tmux 和自定义 metadata；无专用 Codex 原生 thread 身份与手动控制字段 |
| `services/terminal_service.py`：`dispatch_input`、`send_special_key`、`exit_terminal_cli`、`dismantle_terminal_runtime`、`delete_terminal` | 输入有状态检查，但不是人机独占控制；清理可杀 window 并移除 worktree |
| `services/inbox_service.py`：`InboxService.deliver_pending`、`_deliver_pending_locked`；`services/agent_step.py`：`run_agent_step`、`_best_effort_teardown` | inbox 自动投递与 step 结束清理也必须服从手动接管，不能只在 UI 上暂停 |

本次只做静态核验和官方文档/help 复核，未运行 CAO/tmux fleet。此前 [02 P5](02-orchestration-and-codex.md#p5-同thread双向交接三轮文字闭环新增) 是 app-server ↔ 官方交互 CLI 的同机假 provider 三轮文字验证，**不是 CAO 已支持接续**。

## 路线 A：CAO CLI/tmux 增强（首期基础子集，人工交接可选）

接口及字段以下均为**拟新增**；不是现有 CAO API。首期只需原生 binding/resume、执行 owner 与 generation、真实停止/查看、worktree/产物保留及安全清理。下表 `beginManual` / `returnControl`、manual/transferring/reconciling 人工状态与人机输入切换仅在选择可选交接增强时实现；worker lease/fencing、旧写者停止确认和必要审批仍为首期安全要求。

| 改动位置 | 最小改动目标与接口 |
|---|---|
| `clients/database.py`：`TerminalModel` + DB migration/读写函数 | 增加 server-owned native binding：`provider_thread_id, host_id, store_ref, working_directory, cli_version, binding_evidence`；持久控制状态 `orchestrated / transferring / manual / reconciling / unknown`、owner/generation。thread 绑定不能随 terminal 删除丢失，可独立建表与 Cylo task/run 关联 |
| `api/main.py`；`services/session_service.py:create_session`；`services/terminal_service.py:create_terminal` 及创建幂等 fingerprint | 新增明确的 Codex `resume_thread_id`，从 API 传到 provider；校验实际 ID、host/store/cwd，纳入幂等身份。已有 Claude `resume_session_id` 保持其语义，不把 CAO session ID 填进去 |
| `providers/manager.py:create_provider/get_provider`；`providers/codex.py:CodexProvider.__init__/_build_codex_command/initialize` | Codex 新建/恢复分支；用参数安全引用构造 `codex resume <id>`，恢复相同 cwd、CODEX_HOME、profile、MCP 与有效权限；失联/不存在显式失败，禁止悄悄新建或 `--last` 猜最新。`get_provider` 先判断原进程是否仍活着，活着只重关联，死亡才走显式 resume |
| `api/main.py:terminal_ws`、`TmuxBackend` attach 入口 + 拟新增交接 service | `beginManual(terminal,mode=attach|resume)` → `manualHandle`；`returnControl(binding,generation)` → `reconcileResult`。手动写入者唯一；只查看走只读 attach。本机直接 `tmux attach` 应先取得手动控制权；外部 SSH/tmux 输入不经过 API，平台 lease 无法直接拦截，只能停止调度并依约定操作，或用独立 OS 用户/受控入口强制隔离；无法核验时标 external/unknown |
| `terminal_service.dispatch_input/send_special_key`、重投路径；`InboxService._deliver_pending_locked`；MCP `handoff/assign/send_message` 调用链 | 在实际发键处原子检查 owner/generation；manual 时阻止监督、初始投递、自动重投和 inbox 唤醒，队列保留待处理。PTY 原始字节绕过 `dispatch_input`，故 `terminal_ws` 也要检查。已在途投递必须排空后才授予手动控制 |
| `agent_step.run_agent_step/_best_effort_teardown`；`terminal_service.exit_terminal_cli/dismantle_terminal_runtime/delete_terminal`；`session_service.delete_session`；`worktree_service.remove_worktree` | 拆开“停止 turn / CLI 退出 / 终端销毁 / 项目清理”。manual 时禁止自动退出/杀 pane/回收；用户 detach 只离开界面。退出后保留 thread binding、worktree、未提交与未跟踪成果，显式保全验收后才 GC；取消必须确认进程/工具终止，失败标 unknown |

### thread ID 怎样可靠取得

1. **优先精确 ID 导入。** 用户从原生 CLI 提供 ID，CAO 用官方 `thread/read` 查证（不 resume、不发 turn）并绑定相同 host/store/cwd。官方 CLI `resume <UUID>` 已有；不能只凭名字或最新时间自动绑定。
2. **CAO 新建 CLI 的自动发现。** 可用同 CODEX_HOME 的官方 app-server `thread/list` 分页发现，再 `thread/read` 核验；CLI 来源为 `cli`，另需发现 app-server 来源时显式包含 `appServer`。用创建前后集合、独立 worktree/cwd、时间等筛选，但这些不是原子 launch correlation；候选不唯一必须让用户确认，不能自动选“最新”。在取得可核验绑定前只开放同进程 attach，不宣称退出后一定可 resume。
3. 若固定 CLI 版本提供确切的官方 session ID 输出，可优先捕获并用 `thread/read` 验证；本次没有验证到交互 TUI 的稳定机器可读 launch-ID 输出。`codex exec --json` 的结构化事件属于另一执行模式，不能据此声称交互 CLI 已有同接口。
4. 路线 A 可以只增加一个只读 discovery helper 使用 app-server API，**不把 turn 执行改到 app-server**。这仍有协议版本依赖；如果不接受该依赖，先用用户显式 ID 导入。读取原生 JSONL/SQLite 只能是另行核验并固定版本的兼容 fallback，不把私有表结构当稳定 API；不复制正在写的 SQLite 来“同步”。

### 可选人工控制权流程

- `beginManual`：冻结 CAO 新派发及重投 → 等待当前回合/工具停稳，或由用户明确选择中断 → 确认无在途输入 → 持久授予 manual → 附着原 pane。只查看可并行，只有一方可输入。
- 若选择另起 `codex resume`：先优雅退出旧 CLI 并确认其及相关工具不再写入，再启动新的官方 CLI。不能只 detach 原 pane 就开第二个写者。thread/store 相同也不意味着跨进程天然互斥。
- `returnControl`：同 pane detach 时原 CLI 可保持存活；第二 CLI 模式须确认它停止。核对身份、权限配置、history 与 Git/文件变化后恢复派发；外部新增回合记为外部执行，不能自动当作 Cylo task 已完成。
- 断线/租约过期不代表手动 CLI 已退出；CAO 重启先恢复 manual/unknown 并 reconcile，不自动抢回。shell 子进程、MCP server 与后台程序的停止状态分别检查，不能用 tmux 客户端消失作证明。

**权限边界：** A 保留 CLI 原生工具与人类 TUI 审批；手动接管期间不必另建移动审批协议。无人值守 Worker 必须去掉默认 `--yolo`，使用明确 profile/sandbox 和受控执行环境；CAO allowedTools 的提示约束不是 OS 权限。恢复时不得静默重新注入更高权限或不同 supervisor prompt。CAO MCP 工具凭据需绑定 owner/权限，手动期间阻止不允许的派发；原生历史不携带“工具服务必然仍在线”的保证。

## 路线 B：结构化 app-server adapter（按能力需要选择）

这不是给 `_build_codex_command` 加一个参数。若 Cylo 必须直接显示逐条工具事件、远程审批、`turn/steer`、精确 turn 状态，B 更合适，但属于更深的 Codex 集成。

- **B1 在 CAO 内加入 adapter：** 抽象 `BaseProvider`/终端 service 中的启动、输入、状态、输出、终止边界，Codex 走 app-server，其他 provider 保留 tmux；`ProviderManager` 注册新实现。thread/turn/item ID 取结构化返回，不再从抓屏推断；现有 status/output API 作兼容投影。`agent_step`、inbox、MCP `handoff/assign/send_message` 保留 Super/Worker 协作语义，但派发/完成/取消须映射真实协议；终端 WebSocket不再直接等于该 runtime 的原生 TUI。比 A 更深 fork，需维护 CAO 上游终端假设与 Codex 协议两端兼容。
- **B2 Cylo 运行时 adapter：** Cylo 的 `AgentRuntime` 直接管理 app-server，复用 CAO 的 Super/Worker 角色设计或经过封装的协作入口。CAO 原有工具通过 gateway 映射到该 runtime，不能只“换命令”便复用全部 terminal/workflow 生命周期。同一 run 只选一个调度与执行 owner；不能 CAO 和 Cylo 分别派发同一 thread。若仍让 CAO 做总编排，给它的是外部 runtime 调用契约，不再伪装成已有 tmux provider。
- 协议映射：create/resume/list/read → `thread/*`；send/steer/cancel → `turn/start`、`turn/steer`、`turn/interrupt`；完成与用量 → turn/item 通知；command/fileChange 审批及 user-input → server-initiated request/reply，按 thread/turn/request/connection 与决策版本关联，不能复用过期 request ID。
- shell/file/MCP 由 Codex 自身执行。CAO 协作工具通过 Codex 的 MCP 配置暴露，或在固定版本验证后映射实验 `dynamicTools`/`item/tool/call`；两者不是天然等价。客户端承载的 dynamic tool 在独立 CLI 接手时未必有宿主可回应，优先采用 CLI 也可访问、鉴权一致的 MCP 服务。

B 仍须做 A 的执行互斥、保留目录、取消确认与恢复核对；人工交接状态机仅选做时实现；app-server 不替 Cylo 解决业务任务、预算和副作用账本。app-server 是进程协议名称，**不表示本产品支持官方桌面 App**。

## 必须验证什么，以及哪些不必为 CLI 先做

只做 CLI 接续，不必做桌面 App 发现/深链/项目注册、App SSH handoff、跨 host 搬迁、移动逐条原生审批桥或完整 app-server turn adapter。默认执行留在同一 host/store/worktree；远程终端可 SSH 到该 host 附着。完整产品的跨环境数据迁移仍保留，按04第13节/CY-04-03单独验收；取消的是官方桌面App支持，不是数据迁移要求。

产品仍须管理：业务 task/run 状态与真实完成证据、持久执行 owner/lease/fencing、进程停止/unknown、dirty worktree 与产物、工具服务凭据/权限、备份、预算、Cylo 自身工具审批与外部副作用核对。它们不能靠“CLI 能 resume”推定已完成。

首期必需验收：

1. 真 CAO/tmux 自动 Super 拆分、独立 Worker、多 worktree 与长任务；可查看真实进度/产物/成本，客户端退出和 CAO 重启后正确核对执行，重复 inbox/重投不导致双写。
2. 基础 Codex CLI 调用 → 保存原生 thread/host/store/cwd → 停止/退出或崩溃后确认旧写者已停 → 同 ID 恢复。目录消失明确失败，未提交/未跟踪文件保留，不能只用摘要声称已恢复。
3. 真实 Codex shell/file/MCP 与必要批准/拒绝；恢复后工具与权限配置正确，中断、进程异常和 pending 审批回收不重复批准/执行，旧请求不可误用。
4. 取消分别证明停止回合、退出 CLI、终止会继续写的子进程；无法证明则 unknown，拒绝第二写者，不能返回虚假“已停止”。

可选人工增强另验：同 pane 人工接管与交回、自动 inbox/重投和人工输入竞态、只读观察/断线/重启、CAO→官方 CLI→CAO 同 thread 双向新 turn，以及外部 thread 导入。只在宣称该能力时要求通过，未实现不阻断首期或完整必需范围。

官方事实依 **OpenAI Docs** 技能，2026-10-21 实际取回 [CLI reference](https://developers.openai.com/codex/cli/reference)、[app-server](https://developers.openai.com/codex/app-server) Markdown，并复核本机 `codex-cli 0.163.0` 的 `resume --help`。没有可用官方搜索工具，使用直接官方页面；官方 app-server 当前仍明确实验/生产支持边界，实施须固定版本与 schema。源码静态核验不是上述必需或可选产品验收通过。
