# Cylo 可靠性、安全与运维专项复核

研究与实际访问日期：**2026-10-21**。完整阅读[新版完整需求](../../Cylo-完整产品需求说明书.md)（取证时该工作树文件名仍为 `Rumi-完整产品需求说明书.md`，主线此前统一更名；现按用户决定取消官方Codex桌面App支持，首期仅保证Linux服务端/执行宿主，保留macOS客户端/浏览器访问及跨环境备份导出迁移；macOS后台运行后续按需评估，不纳入首期验收）、旧架构稿，并只读复核 [04 总体架构](04-architecture.md)、[02 编排取证](02-orchestration-and-codex.md) 和 [01 稳定需求编号](01-requirements.md)。本文补充 CY-04-03、CY-05-05、CY-07、CY-08、CY-09、CY-14、CY-15、CY-16、CY-17 的实现约束，不重新选择总体技术栈，也不代表运行产品已实现。

标识约定：**事实**=已获取的一手文档/固定源码；**实验**=本报告隔离脚本实际结果；**设计**=建议实施契约；**未验证**=尚无目标环境端到端证据。官方能力不能代替 Cylo 自身验收。本文未运行生产服务、部署程序或发出外部副作用。

## 1. 复核结论与对 04 的具体修正

04 的“权威数据库 + durable jobs/outbox + 独立 runner + 不可变能力 release”方向可保留。发布前必须补齐下列边界；行号按 architecture 工作树的初次落盘版本定位，后续以章节和原句为准。

| 优先级与位置 | 问题及建议修正 | 依据/影响 |
|---|---|---|
| 阻断：§4“SQLite foreign_keys=ON、WAL…”段（约97行）；§5 received ACK | 增加每个写连接显式 `synchronous=FULL`，启动核对实际返回值；成功 commit 后才发可靠接收 ACK。Mac 另验证 `fullfsync` 与存储栈。FULL 仍依赖 OS/VFS/硬件兑现 sync | WAL+NORMAL 只保证应用进程崩溃耐久；断电/系统崩溃可回滚已提交事务 [R1]。不能仅凭 WAL 声称所有确认不丢 |
| 阻断：§10首段“runner拒绝旧epoch操作”（约330行） | 写明 fencing 必须在**实际资源/工具网关**检查；已启动任意 shell、旧服务、官方独立 Codex 不必再经过 runner。无法让资源拒旧 epoch 时，确认旧进程树终止/网络与写路径隔离后才交接，否则保持 waiting/unknown | 数据库租约只约束合作方；实验 P3 证明拒绝数据库旧 epoch 不影响直接文件写。网络分区不能靠再发一张新租约保证单写 |
| 阻断：§11审批/撤销两段（约349—351行） | 审批绑定规范化参数、解析后的目标、grant/policy版本与目标环境；实际派发前重新检查。撤销明确区分“禁止新动作”“停止在途请求”“已发生不可撤回”。不能将取消RPC响应视为外部撤销证明 | CY-14-02/03、CY-15-02；防止旧通知、目标替换、授权与执行间竞态 |
| 高：§9离线服务段（约324行） | 给离线授权有效期与超时失效策略；失联到期停止新副作用并缓冲有界事件。长期第三方secret若已被程序复制，控制库撤权不能将其强制收回；需网关/提供者撤销/进程隔离 | 保留普通程序独立运行，同时给出有界撤权能力；不能承诺离线无限工作且立即撤权 |
| 高：§13备份段 | 明确先冻结备份引用集再延迟GC，避免DB快照引用的文件已被清理；恢复环境默认关闭触发和外发，先核对外部已发生事实 | 一致DB快照不自动提供DB+对象+外部世界共同事务；旧备份会遗漏恢复点之后已完成的外部effect |
| 高：§9更新/卸载与§11 CAO段 | 以完整进程组停止确认、checkpoint兼容和dirty产物清单为发布/回收门槛；默认禁用force清理；回滚代码与撤销effect分别处理 | [R10][R11] 与02固定CAO源码；避免旧版仍运行、未提交成果丢失和不可逆数据迁移回滚失败 |

上述问题已向协调、架构与整合岗位同步。本报告只修改自己的报告与研究附件。

## 2. 生命周期：控制意图、进程事实和产物分别保存

**设计。** 不以一个 `running` 字段同时表达任务目标、进程存活和服务健康。沿用04实体，补齐以下转移条件：

| 对象 | 持久状态与推进依据 | 停止/恢复/清理边界 |
|---|---|---|
| Task / TaskRun | task保存目标/验收；run保存attempt、runtime/host、原生执行ID、资源epoch、checkpoint与观测时间。run输出结束后才进行验收 | 暂停/取消先记desired，观察进程/工具停止后收敛；中断的修改和effect仍保留。恢复创建新attempt并关联原会话，不自动重跑已完成步骤 |
| CapabilityRelease | source revision、lockfile、构建环境、artifact digest、manifest、测试与审批摘要不可变 | 构建阶段无生产secret、无部署权限；已发布文件不能原地覆盖；有被使用的digest不得GC |
| Deployment | release+config version+grant+host generation构成期望部署；另存 rollout phase、旧版本、迁移与rollback声明 | 同一部署command重放不得安装第二份；并发更新用expected_version；扩权/换目标重新授权 |
| ServiceInstance | supervisor identity、进程启动身份、generation、observed、health、last heartbeat；服务每次触发记录invocation/job | stop须覆盖子进程；暂停默认停新触发，在途按drain/cancel策略处理；控制进程退出不等于服务退出 |
| Trigger / Schedule | 事件原生ID/逻辑时间槽唯一，timezone、misfire policy、游标和最后成功checkpoint | 禁用trigger与删除能力分开；过期触发按skip/latest/bounded catch-up；禁止恢复后无限补发 |

一次能力发布应留下可重放的**控制记录**，而非重放不受约束的安装shell：准备→验证artifact/config→核对审批与资源预算→冻结新触发→处理在途→保存checkpoint/旧配置→安装新实例→探针验证→切换generation→登记成功→延迟回收旧版。失败保存失败阶段，不能仅显示“部署失败，请重试”。

checkpoint需标注schema、producer release、资源范围、外部cursor及校验和；cursor推进与产生的job同事务，防止“记已读但没处理”。双版本并行只有在分离消费分区、provider支持消费组或资源端拒旧generation时启用。单活邮件监听、文件修改、数据库迁移默认停旧再启新；单纯“两个进程拿到不同worktree”不能保护共享邮箱或第三方API。

**回滚设计。** release可回退不代表状态可回退。采用expand/contract数据库迁移；若新checkpoint旧版读不了，回滚须使用已验证转换或前滚修复，不能把旧checkpoint覆盖在已经推进的外部资源上。数据库恢复、已发通知/邮件、已提交订单不由二进制回滚撤销。卸载先停触发/进程树、撤grant和凭证、封存结果，最后按保留策略删除安装；数据擦除与卸载分别授权，审计保留最少必要元数据。

**事实。** Linux systemd `KillMode=control-group` 会在停止时处理整个unit控制组；`process`只终止主进程，`none`不提供此停止保证 [R10]。这是可用的执行基础，不证明实际unit已正确配置。后续若支持macOS后台运行，launchd安装、用户退出/重启行为和子进程终止需真机验收；Linux配置名不能直接移植到Mac。

## 3. Durable队列：SQLite、PostgreSQL jobs与Temporal的选择

| 方案 | 适用条件（设计） | 官方边界与不能省略的工作 |
|---|---|---|
| SQLite WAL同库jobs | 单台控制主机、本地可靠磁盘、一个业务写入口、可接受单写者；先测queue lag和写锁等待 | WAL要求同主机共享内存且同一时刻一个writer [R2]；不是多节点共享盘队列。FULL、短事务、busy策略、checkpoint与磁盘水位仍需配置 |
| PostgreSQL jobs | 多控制进程/节点、高可用数据库或团队写并发确有需求；希望任务状态与入队同一事务 | `FOR UPDATE SKIP LOCKED`官方允许queue-like表多消费者避锁，但它产生不一致视图，不适合通用一致读 [R4]。领取事务提交后行锁不再保护整个执行，仍需持久lease/heartbeat/fencing和外部幂等 |
| Temporal | 大量长等待、定时器、复杂多步骤重试/补偿，团队需要成熟workflow运维并愿承担服务及SDK约束；由实际故障/开发复杂度触发 | Workflow靠Event History/replay恢复；Workflow代码须满足确定性，发布采用Worker Versioning或patching [R6][R8]。Activity仍可能重执行且应幂等 [R7]；不能代替资源隔离、审批、真实进程监督、第三方effect核对 |

不按“任务超过多少分钟”机械切Temporal，也不把PostgreSQL视为“内建工作流”。最初可使用SQLite durable jobs；当写锁等待、队列延迟、多主机需求或复杂恢复代码构成实际约束时再迁移。没有本仓库压测，不给SQLite并发任务数/数据库大小的虚构上限。

**最小job契约（设计）。** `job_id, workspace_id, trigger_key, payload_ref/hash, due_at, state, attempt, owner, lease_epoch, lease_expires_at, heartbeat_at, retry_policy, last_error, operation_ref`；唯一约束限定正确账号/connector/触发域。SQLite用短 `BEGIN IMMEDIATE` 或等价原子领取；PostgreSQL在一个短事务内锁候选并更新领取事实。不要在打开的DB事务内等待LLM或网络请求。

提交message/command、业务状态、event/outbox和需要的后继job同事务；派发在提交之后。崩溃发生在“已领取、尚未启动”也不能简单视为未启动，应使用稳定run/launch ID向runner查询；runner需持久记录同launch ID的去重结果。此机制仍不保证不合作的第三方原生协议幂等。

队列重试只处理明确transient且满足安全重试规则的动作，指数退避+jitter、最大尝试/总时长、每connector熔断和死信；auth/conflict/unknown单独处理。监控最老due job、过期lease、重试率、dead-letter和长时间WAL读事务。磁盘满/只读/事务提交失败时不发成功ACK、不继续消耗队列；保留独立有界应急日志/运维告警出口。

**耐久配置事实。** SQLite官方明确WAL+NORMAL在断电/系统崩溃可能回滚提交，应用崩溃仍耐久；FULL每次commit增加WAL sync [R1]。因此控制记录默认FULL，并核对连接配置，不只设置一次数据库文件。官方 `fullfsync` 默认为off且F_FULLFSYNC仅Mac支持；是否启用、性能和目标磁盘保证需实测。`foreign_keys=ON`、磁盘剩余空间、异常fsync返回与失败状态也需启动/健康检查；不能将单进程kill实验扩展为断电/存储损坏证明。

若引入Temporal，明确**单一调度所有者**：Cylo保留授权、任务投影和审计；workflow执行进度由Temporal历史负责，通过稳定workflow/activity ID映射；不要让原SQLite调度器与Temporal同时重试同一步。跨两者无法做单DB事务时通过outbox+幂等启动/回写收敛；未经历史兼容测试不更新活跃Workflow定义，权限撤销与成本限制仍在每个Activity实际派发前检查。

## 4. 并发、fencing与外部effect的未知结果

**设计。** 资源键至少覆盖原生session、可写checkout、共享业务资源（例如邮箱账号+folder）和service deployment。task级唯一活动run不等于这些资源均独占。epoch单调递增并绑定host generation，不能在备份恢复后回退到旧可接受epoch。PID须配启动时间/supervisor ID；仅凭复用PID不可杀进程或确认恢复。

数据库记录lease过期只表示“不再信任其续租”，不能证明旧执行已停。在分区后启动新writer之前必须满足至少一项：资源端拒绝旧epoch；网关撤销旧凭证且旧writer没有直通通道；或确认旧进程树和写路径/网络已终止隔离。否则宁可waiting，也不能同时宣称任务接管成功和数据不覆盖。主机本地monotonic时钟用于时限，数据库时间用于集中lease决策；记录UTC不代表时钟永不跳变，恢复时不以wall-clock超时单独证明执行死亡。

**effect账本（设计）。** 执行前持久 `operation_id + canonical_request_digest + target/account + effect_key + grant/policy_version`；预留预算→授权再校验→记录dispatching→调用→保存provider receipt/查询结果→confirmed。dispatching不是“肯定已发出”，confirmed也不能仅凭HTTP已接收而成立。结果不确定保存 `unknown`、最后证据、查询计划和是否允许重试；业务task进入waiting(effect_unknown)。

| 崩溃/返回情形 | 恢复行为 |
|---|---|
| 能证明未到提供者、无执行机会 | 安全重试相同operation，不换effect key |
| 提供者返回持久成功receipt | 查验receipt后补记confirmed，不再执行 |
| 请求超时/断线/成功后本地receipt未提交 | 若提供者支持幂等，确认其作用域、保留期和参数语义后重用同key；支持查单则先查；两者都没有则unknown，人工核对/业务补偿 |
| 提供者异步受理 | 保存原生operation/job ID轮询或对账，不能因202就完成业务 |
| 取消与外部成功交错 | 保留实际成功及取消请求时间；取消不改写已经成功的effect，可提新的补偿动作 |

**事实。** Stripe官方当前区分API v1/v2：v1同key保留首次执行结果（包括500），至少24小时后可清除key，清除后同key会产生新请求；参数不一致会报错 [R12]。这里只用来证明幂等协议有明确边界，不把Stripe行为推广为邮件或其他provider保证。内部operation/inbox记录不得按提供者短期key TTL随意删除；恢复多年旧任务也不能仅靠旧key自动重发。

**Temporal边界事实。** 官方说明已完成Activity不会在workflow replay中重执行，但Activity在返回/出错前不会记录进history；该窗口内故障可能导致重试 [R7]。即使换Temporal，发信成功而receipt未持久化的窗口仍需上述账本与provider能力。

## 5. 审批、撤权与隔离

**设计。** 审批是一份对具体动作的有限能力，不是可自由套用的“用户说过可以”。绑定actor/workspace、task/run/plan revision、规范化参数hash、真实target/account/environment、release/config digest、grant与policy version、预算范围、expiry和单次消费ID；相同幂等键不同参数拒绝。审批确认与消费采用compare-and-swap/唯一约束，双设备点击只一次有效。低风险且在已有授权范围内的动作无需无意义重复审批。

检查时点至少含提交与实际执行前；两者之间权限变更使旧批准失效。对文件操作解析路径并校验根目录/符号链接，不能批准路径后跟随攻击者替换的symlink；域名allowlist还需处理redirect、DNS解析/IP、localhost/云metadata目标与隧道绕路。无法强制实际访问范围的manifest仅是声明，不标作隔离通过。

| 撤销阶段 | 可承诺行为与必须暴露的限制 |
|---|---|
| 尚未派发 | 同一事务撤grant、失效待审批、取消待发command/job；后续实际工具调用拒绝 |
| 在途但经过受控网关 | 网关拒新请求/流量并请求终止；已经被provider接受的动作仍可能完成，需receipt核对 |
| 离线独立服务 | 短时lease/凭证到期停止新effect；示例初始TTL为60秒，实际保障取决于资源端凭证失效、时钟与隔离，并非已达SLO |
| 现有语音直连媒体 | 网关拒新建/控制并请求客户端停track、关RTCPeerConnection/DataChannel，记录确认；删除binding/撤token不证明现有WebRTC已断，强制服务端终止需上游能力实测或受控媒体relay |
| 长期secret已复制给任意程序 | 单改数据库无强制回收能力；撤第三方token/轮换、断网络、停进程/隔离主机。未完成这些动作只能显示撤销待确认 |

**执行边界设计。** Git worktree共享仓库元数据且进程仍有原OS身份；不能充当文件/网络/进程/secret隔离。独立OS用户也不是完整网络隔离。可信个人本机模式可使用受控工具、最小环境、单独数据目录，但应明确其高信任前提；自主生成程序先在无生产secret、默认无网络、只读依赖与有界临时目录的构建/测试环境运行。正式服务仅注入所需SecretRef、最小账号权限与有界资源，实际读写/外发经强制边界。

Linux可评估systemd `NoNewPrivileges`、`ProtectSystem`、`ProtectHome`、路径allowlist及cgroup资源限制；官方对具体namespace/权限能力有运行前提 [R9]，用户服务与系统服务不能默认同效。容器用非root、只读根、去capability、seccomp、无host network/PID、无Docker socket和最小mount；`--network none`官方只创建loopback [R13]，需要外联时另设受控出口。Docker官方明确控制daemon者可挂载主机根并修改文件 [R14]，因此不能让生成代码自由构造docker参数。强对抗/多租户场景优先独立VM/主机；共享内核容器不在本文获得逃逸安全保证。后续macOS执行隔离/网络限额与资源封顶未真机验证，不能抄Linux配置名宣称达标。

**CAO专项事实与处置。** 02已固定 `4e990a9d78b154ad76c6cf94c7b51e36fce69f21`（2.5.4）；本报告独立只读核对源码：Codex命令默认含 `--yolo`，并非Cylo默认应采用的授权模式；`worktree_service.remove_worktree`明确 `git worktree remove --force` 后 `git branch -d`，所以未提交/未跟踪文件被丢弃，但未合并已提交分支不是被强删 [R15]。不夸大为“所有提交都删光”。

若采用CAO，按[07](07-cao-cli-handoff-plan.md)的provider/session改造路线固定版本并验证最终命令，禁止默认yolo路径；prompt里的allowedTools不当作强制策略。删除terminal不能直接回收工作目录：先证明所有writer停止→登记tracked diff、binary patch、untracked文件清单/内容hash与必要ignored产物→原子归档并校验可读→验收/保留期→普通非force清理。ignored目录可能有secret或大依赖，应按分类处理，不盲目整目录打包。清理失败保留quarantine和容量告警；任何force销毁都是独立、可审阅、限定路径的授权动作。

## 6. 审计、数据保留与成本限额

**审计设计。** 记录command/operation/run、actor与认证来源、workspace/project、host generation/epoch、grant/policy/approval版本、输入与目标digest、provider原生ID/receipt、发生/观测/提交时间、结果证据hash与失败分类。保留请求、被拒、撤销、变更、导出/删除、恢复与人工核对的因果链。原始敏感正文与审计元数据分库/分存储权限；不把密钥、完整音频、邮件正文、authorization header或全部环境变量放入日志。

应用审计账号只允许追加不等于管理员无法篡改；hash链只能辅助发现变化，没有独立锚点仍可被整体重写。高保障部署可用独立追加存储/对象锁与独立访问控制；个人单机明确其保证范围。保留按数据类型配置（示例：原始调试日志7天、运行证据30天、授权/决策元数据更长，均待用户政策确定），不是无期限全存。删除传播到索引/缓存/派生记忆；备份采用有期限保留及恢复后再应用tombstone，避免已删数据复活。

**实时语音凭据与数据。** 采纳chatgpt-web-voice实时上游/网关/多账号池；账号管理与开麦分权，owner/workspace/project范围在建会话和辅助控制再次校验。永久凭据加密存网关后端、日志脱敏、备份独立保护，不交语音模型或浏览器。池中账号不是Cylo身份或Codex认证来源。向上游仅发送授权所需上下文；当前源码`history_and_training_disabled:false`，不能宣称上游默认无历史，跨账号新会话需核对数据范围与上游副本删除边界。

**成本与资源设计。** ChatGPT语音账号订阅费用/可观察额度、Codex用量与其他模型/API费用分账；订阅不等于API余额，真实分钟数/重置点未知时标unknown，不由JWT exp或settings/user成功推导。多账号仅增加候选资源，不构成无限额度；本地通话时长是估算。 按workspace/user/project/task/service/provider设调用数、并发、token/费用估算、重试深度、运行时长、CPU/内存/PID/磁盘/日志/网络/事件积压上限；派发前原子预留，并将幂等重试计入同operation。跨Worker派生不能新建task绕预算，服务调用Worker继承更严格预算；LLM调用上限与外部API、构建下载、存储成本分别计数。

预算触顶停止**新派发**、保留停止/撤权/告警的控制资源；usage缺失标unknown并保守预留，不能当0。估计最大成本无provider硬边界时，无法保证精确美元封顶；在途调用和取消后计费可能形成可测超额。需要硬封顶应组合provider侧限额、每调用可强制上限和最大并发，不宣称一个本地counter能撤销已发生费用。避免到达预算后再无限调用Super解释预算问题。

告警必须确定性生成；普通无任务待处理时零LLM维生调用。重启风暴限速、异常服务有重启预算；事件缓冲和通知积压有容量水位。日志轮转/GC不得清理仍被run/release/备份引用的数据；磁盘预留一部分给控制与恢复，不让产物写满后连停止状态都无法记账。

## 7. 备份、恢复与环境迁移

**事实。** SQLite Online Backup API对运行中的数据库创建一致性副本 [R3]；直接复制活动主db文件不能保证含WAL的提交都被复制。PostgreSQL官方区分SQL dump、文件系统备份、连续归档/PITR [R5]；需要更小RPO应评估WAL归档与base backup，而不是把“已迁Postgres”当作备份方案。

**备份设计。** 一个manifest关联一致DB快照（schema版本/事件水位/备份generation）、内容寻址产物/release、项目提交和未提交/未跟踪必要文件、配置/trigger/checkpoint、原生会话可支持恢复的材料及版本、权限/凭证引用、删除tombstone、对象校验和。原生session文件仅在官方支持和权限允许范围备份；不能从恢复一份数据库推断官方CLI原生会话与真实工具/审批完整恢复；官方桌面App同步不在范围。

先形成DB快照与引用清单，并pin引用对象防GC；复制并逐项校验后发布完成manifest，缺对象的备份不能标成功。执行中的项目文件采用冻结写入/文件系统一致性快照或声明弱一致范围；数据库、artifact和项目目录不是天然一个事务。secret单独加密与访问控制，恢复密钥走独立安全材料或重新授权；无解密材料的加密备份不能算可恢复。至少保留一份故障域外备份；RAID、同盘副本和Git远端均不能覆盖所有故障及未提交成果。

**恢复操作单（设计）。**

1. 在隔离环境读取manifest，验证hash、schema/外键、对象/目录关联、解密权限和删除策略；开始时禁用全部触发/外发，避免演练真的发信。
2. 生成新的installation/host generation与stream epoch，失效旧lease和未消费审批；已备份授权需重新核对身份与有效期，不把备份时已有效grant当永久有效。
3. 核对旧主机已停止/隔离；否则不启用相同资源的writer。仅清空lease表后启动新节点不解决双活。
4. 按provider原生ID查询所有恢复点附近的dispatching/unknown及可能已发effect；**恢复点之后也可能有外部成功**，不能把旧DB中“待发送”直接重发。无法对账保留人工核对。
5. 重建路径映射与索引，重新安装平台匹配的release，检测checkpoint兼容；先跑无副作用任务，确认状态/历史/权限/产物，再逐项启用服务和触发。
6. 记录真实恢复点、数据缺口、unknown清单、校验结果与耗时，才能宣布恢复；失败备份不能静默覆盖最后一份有效副本。

**迁移设计。** 首期Linux换host复用上述流程；macOS→Linux保留为后续按需研究，届时额外核对架构/动态库/锁文件、大小写路径、文件权限、时区/cron、凭证库、OS supervisor与Mac专用自动化。保留logical project ID，native path通过checkout映射重建；迁的是可恢复定义与数据，不是进程内存。涉及Mac专用能力时须明确兼容缺口，不将Mac runner列入首期部署要求。SQLite→PostgreSQL按停写逻辑导出/校验/切换；避免无需求双写，event/command/effect IDs保持不变，sequence/cursor重新建立明确epoch，所有租约先核对再启用。

## 8. 任务、事件与通知的恢复契约

**设计。** 启动恢复次序：暂缓活跃资源派发→核对runner/supervisor/原生runtime→补采结果/receipt与原始history→标记gap/unknown→重建outbox投递/调度→安全资源解除冻结。服务可独立运行，但控制面失联期间的事件须有有界持久spool与稳定source ID；溢出需明确gap并回源对账，不能丢了也报“已同步”。

业务event与状态同事务，outbox至少一次投递；consumer以持久inbox唯一键去重，处理状态与cursor同事务。网络通知成功不等于用户设备已收到、应用已落盘或人已读。客户端快照与水位同读事务，增量仅应用连续序号；权限变更/保留期过期触发reset和新授权快照。source/provider没有持久游标时显示可观测gap，不能编造连续性。

notification以 `logical_event + recipient + policy_version/notification_kind`去重，渠道attempt和receipt单独保存；服务端收件箱是权威，push只提示查询。投递成功但本地receipt未持久化仍可能重复推送；渠道collapse/dedupe是辅助，不能保证人只看一次。普通进度合并，异常/需要授权保留；通知点击重新读取任务/审批有效性，旧推送不能携带永久授权。用户明确取消通知与取消任务是不同command。

## 9. 最小运维SLO建议与告警

以下都是**设计起始目标，未压测、未达标声明**。计量以版本化部署、主机在线区间和实际计数为准；休眠/关机在可用性统计中单独列出，不隐藏为成功。安全不变量不以“99%允许一次越权”形式折中。

| 项目 | 初始建议及测量口径 | 失败处置 |
|---|---|---|
| 可靠接收 | commit后ACK；受支持的单进程故障恢复，已ACK command/event/job丢失数=0；FULL存储前提另测 | 任何丢失为最高优先级故障，停止宣称可靠接收 |
| API/同步 | 常开主机月可用性目标99.5%；无模型/外部依赖的command ACK p95≤1秒，前台状态同步p95≤5秒 | 连续5分钟超标告警；区分DB锁、磁盘、网络和离线host |
| 可派发队列 | 有资源且授权/预算有效时，due→领取p95≤5秒；最老job>60秒告警 | 队列饥饿/lease大量过期排查，未知effect不靠强重试清积压 |
| 健康与恢复 | heartbeat示例20秒；连续3次缺失标unknown（≤60秒），不标healthy；控制服务重启后60秒内开始reconcile | 明确host离线；10分钟仍未收敛的活跃记录升级人工处理 |
| 撤权 | 在线网关确认撤销后新派发=0；受控离线服务最大授权TTL示例60秒；已发请求独立对账 | 无法兑现TTL的长期secret服务不宣称有界强撤权 |
| 副作用 | 对明确幂等/可对账fixture：重复effect=0；所有故意制造的未知窗口均可见且不盲重放 | 任一重复高风险effect停止该connector自动重试 |
| 通知 | 重要事件→持久收件箱p95≤5秒；provider可用时首投递尝试p95≤30秒；逻辑收件箱重复=0 | push到达/已读另计，长期未投递在收件箱可见并支持替代渠道 |
| 备份恢复 | 个人起点RPO≤24小时、控制数据RTO≤2小时（从有效备份及凭证可用开始）；大产物总恢复另计 | 备份年龄>26小时/验证失败告警；每月及高风险schema发布前隔离恢复演练 |
| 成本/资源 | 24小时空闲普通维护LLM调用=0；超预算后新模型派发=0；估算/实际/unknown均可查 | 在途超额单独记录；磁盘<20%预警/<10%停止新大产物，仅示例可调 |

数据库崩溃恢复目标与灾难RPO不是同一概念：FULL旨在承受受支持存储上的已同步提交，整盘损坏仍受备份间隔影响。通知到设备和邮件provider最终送达不是单机服务可独自担保的SLO。

## 10. 故障验收矩阵

每次记录版本、OS/文件系统、配置、输入命令ID、DB快照、实际进程/副作用计数、外部receipt、时间线和恢复结果。表中除P1—P4外均为**待实施验收**，不能误读成已运行。

| 注入点/场景 | 必须满足的断言与证据 | 需求/现状 |
|---|---|---|
| command提交后断网，客户端同key补发 | 同一command/receipt，task/event/job均无重复；不同payload同key拒绝 | CY-08-01、16-01；待验 |
| 事务中途强退；commit后强退 | 前者三表全无，后者三表全有；不出现仅command无job | CY-16-01；P1已测简化SQLite模型 |
| OS崩溃/断电、WAL checkpoint途中故障 | FULL与Mac sync配置在目标磁盘复核；ACK记录可恢复；不能以kill替代此实验 | CY-15/16；未测 |
| job领取后、runner启动后但start ACK前崩溃 | 同launch ID核对，不多起进程；旧执行unknown时不再写 | CY-08-04、15-02；待验 |
| 旧lease过期但writer存活、分区、系统时钟回拨 | 资源端拒旧epoch或旧进程隔离确认；否则阻塞新writer | CY-05-05、14-04；P3仅证明DB隔离不足 |
| effect成功、本地receipt保存前崩溃 | 稳定key查单/去重；不支持则unknown，不新key重发 | CY-15-02；P4简化provider fixture已测，真实邮件未测 |
| 双设备批准/撤销竞态、旧通知、修改目标/release | 一次消费；旧批准失效；派发前检查grant/target digest | CY-14-02/03；待验 |
| 离线服务撤权、长期secret复制、已发HTTP取消 | TTL到期禁新effect；无法收回的凭据明确待确认；已发effect照实对账 | CY-07/14；待验 |
| 进程主PID退出但子进程写文件；恶意symlink/redirect | unit进程树真实停止；越界路径/网络被强制拒绝并审计 | CY-14-04、15-04；Linux待验；Mac后续按需 |
| release健康失败、checkpoint不兼容、不可逆schema迁移 | 不并行消费；只在兼容时回退；不兼容degraded并保留恢复材料 | CY-07-02；待验 |
| CAO terminal删除带dirty/untracked/未合并提交 | Cylo先归档和核验、默认拒force；已提交分支保留与dirty丢失边界分别确认 | CY-08/16；02有上游删除PoC，Cylo防护未实现 |
| outbox投递成功但ACK前崩溃、客户端落盘前断线 | inbox去重，cursor不跳过，快照+增量无gap；逻辑通知单份，渠道可重复 | CY-03-04、12；待验 |
| 休眠错过cron、DST重复/缺失时段、重复webhook | 逻辑时间槽和source ID去重；按misfire policy有界补跑 | CY-04-01、09；待验 |
| SQLite磁盘满/只读、长读导致WAL增长；provider限流 | commit失败不ACK；控制/停止可用；有界队列/日志/重试，隔离失败connector | CY-15-01、17；待验 |
| 活动WAL在线备份、对象GC与备份竞态、缺密钥 | 一致快照+对象pin/hash完整；不可解密/缺文件判备份失败 | CY-16-03；P2仅DB在线备份已测 |
| 恢复旧备份而旧host仍在线，恢复点后已有外发 | 禁止双活和盲重发；重新核对receipt，使用新generation/epoch | CY-04-03、15-02；待验 |
| Linux换host路径/架构/secret不同；已删数据恢复（Mac→Linux后续按需） | 能力按平台重建，专有能力明确缺口；tombstone重应用不复活索引 | CY-04-03、16-02/06；待验 |
| 语音账号slot竞争/网关重启、429/耗尽/401、切号或插话撞final | 原子租约与owner粘性；旧媒体unknown不盲释放；429分类/冷却不无限切号；切新媒体不重派command；上游assistant/回注不派任务；实际结果播报可核对 | CY-03/13/14/17；待验 |
| 预算并发竞争、usage缺失、递归Worker/重试风暴 | 原子预留、继承预算、有限重试；unknown不计零；停止新调用 | CY-17-03/04；待验 |

## 11. 本次隔离实验与可复现范围

附件 [reliability-probe.py](evidence/reliability-probe.py) 使用Python标准库、临时目录、SQLite和子进程；无网络、无真实凭证、无生产runtime代码。复现：

```bash
python3 docs/research/evidence/reliability-probe.py
```

2026-10-21实际环境SQLite **3.40.1**，结果：

```text
crash_atomic_command_event_job: PASS
online_backup_open_wal_database: PASS
stale_epoch_rejected_by_db_but_direct_file_write_succeeds: PASS
effect_receipt_gap_same_key_one_effect_new_key_two_effects: PASS
```

P1：子进程在commit之前/之后 `os._exit(17)`，重开连接检查command/event/job，未提交全无、提交全有。P2：保持source WAL连接打开，经backup API备份并做integrity_check与job核对。P3：数据库epoch由1升2，旧epoch条件更新0行，但同OS身份仍能直接写文件；这是危险边界的正例，不是fencing已实现。P4：独立SQLite表模拟幂等provider，在effect提交后、control receipt前退出；相同key计数1，换key计数2，验证unknown窗口与键稳定性的重要性。

实验不包含真实网络分区、并发writer压力、断电、存储故障、Temporal部署、macOS沙箱、provider幂等期限、真实发信/推送、完整产品恢复或SLO负载。未验证项仍阻断相应产品能力声明。

## 12. 一手资料与证据

以下均于 **2026-10-21实际取回**；动态文档可能变化，引用语义以附件 [官方摘录与hash](evidence/reliability-official-excerpts.md) 为本次证据。URL不是所有设计决策的背书，尤其TTL、SLO、保留期和技术升级条件均由本文建议。

- **R1** [SQLite PRAGMA synchronous / fullfsync](https://www.sqlite.org/pragma.html#pragma_synchronous)：NORMAL/FULL、应用崩溃与断电区别、Mac F_FULLFSYNC。
- **R2** [SQLite WAL](https://www.sqlite.org/wal.html)：同主机/单writer、checkpoint与WAL约束。
- **R3** [SQLite Online Backup API](https://www.sqlite.org/backup.html)：在线一致备份；不覆盖外部文件。
- **R4** [PostgreSQL SELECT locking clause](https://www.postgresql.org/docs/current/sql-select.html)：SKIP LOCKED与queue-like表边界。
- **R5** [PostgreSQL Backup and Restore](https://www.postgresql.org/docs/current/backup.html)、[WAL Reliability](https://www.postgresql.org/docs/current/wal-reliability.html)：备份类型及存储sync可靠性。
- **R6** [Temporal Workflow Execution](https://docs.temporal.io/workflow-execution)：event history与replay。
- **R7** [Temporal Activity Definition](https://docs.temporal.io/activity-definition)、[Activity Execution](https://docs.temporal.io/activity-execution)：幂等建议、history记录窗口、timeout/retry。
- **R8** [Temporal Safe Deployments](https://docs.temporal.io/develop/safe-deployments)：确定性、Worker Versioning/patching。
- **R9** [systemd.exec](https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html)：执行环境限制和能力前提。
- **R10** [systemd.kill](https://www.freedesktop.org/software/systemd/man/latest/systemd.kill.html)：KillMode及进程组停止。
- **R11** [Git worktree](https://git-scm.com/docs/git-worktree)：共享仓库/独立工作目录、dirty worktree force移除。
- **R12** [Stripe Idempotent Requests](https://docs.stripe.com/api/idempotent_requests)：版本差异、key保存期、结果缓存和参数校验。
- **R13** [Docker none network driver](https://docs.docker.com/engine/network/drivers/none/)：仅loopback，无网络出口。
- **R14** [Docker security](https://docs.docker.com/engine/security/)：daemon控制权限与host mount风险。
- **R15** 固定CAO源码：[CodexProvider](https://github.com/awslabs/cli-agent-orchestrator/blob/4e990a9d78b154ad76c6cf94c7b51e36fce69f21/src/cli_agent_orchestrator/providers/codex.py)、[worktree_service](https://github.com/awslabs/cli-agent-orchestrator/blob/4e990a9d78b154ad76c6cf94c7b51e36fce69f21/src/cli_agent_orchestrator/services/worktree_service.py)。在02已获取的该commit目录独立静态读取，未运行CAO fleet。
