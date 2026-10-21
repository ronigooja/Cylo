# Cylo 长期记忆：开源组件与自有策略的边界

核查日期：2026-10-21。结论：**自写 Go 的 Cylo 记忆业务 API，复用开源提取/检索组件，复杂整理按需启用 Memory Agent；暂不直接引入完整记忆平台。** Mem0 可处理通用抽取/检索，也可作为 Agent 工具；采用它不等于免除整理职责，也不意味着必须额外运行一个 Agent。不是因为 Python 就排除。Graphiti 适合后期确有时间关系图谱需求；当前 Letta 与 AO 的 Agent runtime 职责重叠，且不能把其 Cloud 共享记忆当成本地开源能力。

此处“自写”不是重造向量数据库、embedding 模型或通用检索引擎，而是实现 Cylo 特有的事实来源、确认规则、作用域、权限、更新与删除策略。向量库只能承担索引，不是完整记忆系统。现阶段没有产品实现；以下是选型建议，不是已交付能力。

## 范围与固定版本

仅选择三个有区分度的候选：轻集成的 Mem0、时间图谱 Graphiti、带记忆的完整 Agent harness Letta。未增加 MemOS 等同类平台矩阵，也未将未实读的 Go 项目作为成熟替代品；没有结论声称“Go 没有记忆库”。Go 技术栈一致性是运维成本因素，不是唯一筛选条件。

| 项目 | 本次固定 SHA | 已检查根许可证与运行成本 |
| --- | --- | --- |
| Mem0 2.2.1 | `2c81af3bbe27d9bccb09c772d583df9e9be72ee5` | [LICENSE](https://github.com/mem0ai/mem0/blob/2c81af3bbe27d9bccb09c772d583df9e9be72ee5/LICENSE) Apache-2.0；[pyproject.toml](https://github.com/mem0ai/mem0/blob/2c81af3bbe27d9bccb09c772d583df9e9be72ee5/pyproject.toml) Python ≥3.10，Qdrant/OpenAI/SQLAlchemy 等；Go 接入需 Python worker/HTTP 服务和所选存储 |
| Graphiti 0.31.2 | `8e320f880e7d169a907dbf2a7ac4188586c03b55` | [LICENSE](https://github.com/getzep/graphiti/blob/8e320f880e7d169a907dbf2a7ac4188586c03b55/LICENSE) Apache-2.0；[pyproject.toml](https://github.com/getzep/graphiti/blob/8e320f880e7d169a907dbf2a7ac4188586c03b55/pyproject.toml) Python ≥3.10、Neo4j driver、OpenAI 等；另需选定图后端及其运维/许可证核查 |
| Letta Code 0.34.20 | `f1627b01605780872677cd6af8d4b3a8d13b1cdb` | [LICENSE](https://github.com/letta-ai/letta-code/blob/f1627b01605780872677cd6af8d4b3a8d13b1cdb/LICENSE) Apache-2.0；[package.json](https://github.com/letta-ai/letta-code/blob/f1627b01605780872677cd6af8d4b3a8d13b1cdb/package.json) TypeScript，Node ≥22.19，Bun 构建，原生 PTY 等完整 harness 依赖 |
| AO（既有基线） | `7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff` | 仅核查现有 Go 编排层是否已经提供 Cylo 记忆能力 |

重要版本变化：[letta-ai/letta 当前 README](https://github.com/letta-ai/letta/blob/8816d3a8516893bfdc0fa14bfb1235226012e295/README.md)（SHA `8816d3a8516893bfdc0fa14bfb1235226012e295`）明确当前源码迁到 `letta-code`，旧 V1 API server 退役在 archive。不能继续以旧 Python 服务架构描述当前 Letta。Graphiti 是本次核查的开源组件，不等同 Zep Cloud；Mem0 本地 `Memory` 也不等同 hosted `MemoryClient`。根 Apache 许可证不证明厂商云服务、所有依赖或托管能力都同样开源。

## Mem0：能省去关键工作，但仍需 Cylo 控制层

固定源码的 [Memory 实现](https://github.com/mem0ai/mem0/blob/2c81af3bbe27d9bccb09c772d583df9e9be72ee5/mem0/memory/main.py) 已有以下真实能力：

- `add()` / `_add_to_vector_store()`：取已有相关记忆和最近消息，用 LLM 抽取事实，批量 embedding、hash 去重并写入向量存储。**当前默认是 additive extraction，不是旧文章常见的自动 ADD/UPDATE/DELETE 决策流程。** 显式 `update()` / `delete()` 仍然存在。
- `search()`：作用域过滤、语义检索及本版附加检索逻辑；`get_all()`、`history()`、`delete_all()` 可供集成，不能只描述成向量数据库封装。
- `_update_memory()` / `_delete_memory()` 与 [SQLiteManager](https://github.com/mem0ai/mem0/blob/2c81af3bbe27d9bccb09c772d583df9e9be72ee5/mem0/memory/storage.py)：记录 old/new、ADD/UPDATE/DELETE、时间、actor/role。比从零实现摘要、提取、索引、历史更省工作。

对 Cylo 有四个实质限制：

1. **隔离不是鉴权。** 至少需 user_id/agent_id/run_id 之一；写入阶段检索已有记忆的 `search_filters` 只保留这三类 ID。`metadata.project_id` 即使能用于读过滤，也不能独自隔离写入抽取和去重上下文。Cylo 应生成受控 scope ID（可将项目作用域映射到一个库内 ID），不把“执行器 Agent ID”直接当唯一共享边界。全局、项目、任务分别检索后由 Cylo 合并。ID 级 update/delete/history 也需服务端验证访问者权限。
2. **有修改历史，不等于有权威决策版本。** metadata 可携 source_event_id 等来源，但默认提取不是逐事实引用原始消息片段的证据链，也没有替 Cylo 区分 confirmed/executed/inferred、确认人、决策 supersedes 关系。history 的文本变更日志不能替代这套业务规则。
3. **删除不等于原文彻底抹除。** `_delete_memory()` 删向量后把旧文本写入 DELETE history；最近消息另存 SQLite，实体清理注明 non-fatal。若需彻底删除，需处理历史、原始消息、实体索引、备份与重建输入，不能仅调用 delete API 就承诺完成。
4. **有进程内锁，不等于跨存储事务。** SQLiteManager 用 threading.Lock；向量写入与 history 写入是不同步骤，未见两者原子提交或完整的跨进程 scope 串行化保证。Cylo 需幂等事件、同 scope 写队列、重试/重建，避免并发提取覆盖或重复。

本地库不绑定某种 Agent runtime，适合给 AO 下不同执行器提供统一检索。迁移需同时导出 payload、history、原始依据及配置；换 embedding 模型要重建向量。[OpenAI LLM](https://github.com/mem0ai/mem0/blob/2c81af3bbe27d9bccb09c772d583df9e9be72ee5/mem0/llms/openai.py) 和 [embedding adapter](https://github.com/mem0ai/mem0/blob/2c81af3bbe27d9bccb09c772d583df9e9be72ee5/mem0/embeddings/openai.py) 可配置 base URL，但 CLIProxyAPI 支持聊天路由不证明也提供所需 embedding；需单独选 embedding provider。`infer=False` 可跳过事实提取 LLM，仍调用 embedding，不是零模型模式。

**何时引入：** 首个版本就要求自动抽取偏好/事实、持续后台整理或语义召回时，建议直接将 Mem0 OSS 放在受控 worker 后，减少实现成本；只允许它写入派生候选记忆，已确认决策仍由 Cylo 保存。若首版先交付显式记忆、原始历史、确认决策与来源检索，则暂缓 Python 服务，在 API 中保留适配口。不是承诺以后一定重写 Mem0，也不是先手写一套同等通用抽取引擎。

## Graphiti：来源与时间关系更强，成本也更明确

[Graphiti API](https://github.com/getzep/graphiti/blob/8e320f880e7d169a907dbf2a7ac4188586c03b55/graphiti_core/graphiti.py) 的 `add_episode()` / `add_episode_bulk()` 构建实体与关系；`search()` / `search_()` 提供混合检索、图邻近排序、过滤等。[EpisodicNode](https://github.com/getzep/graphiti/blob/8e320f880e7d169a907dbf2a7ac4188586c03b55/graphiti_core/nodes.py) 存 raw content、source_description；[EntityEdge](https://github.com/getzep/graphiti/blob/8e320f880e7d169a907dbf2a7ac4188586c03b55/graphiti_core/edges.py) 有 episodes 来源集合、valid_at/invalid_at/expired_at。它确实能减轻“何时事实有效、哪些资料支持该关系”的实现，不应把它简化成只有图检索。

- `group_id` / `group_ids` 提供分区；本版 `_resolve_request_scope()` 专门避免并发请求修改共享 driver 造成跨组串用。仍需 Cylo 在服务端授权；省略 group_ids 的调用不能作为安全默认。
- `remove_episode()` 会移除该来源、保留其他 episode 仍支持的边，删除仅由该来源支持的节点/边，并尝试重建存活实体摘要；**重建会调用 LLM，失败时恢复旧摘要并记日志**，不能宣称删除自动保证所有派生内容同步清净。另有 group/UUID 删除方法。
- 有 async、bulk 和 semaphore 并发限流，但源码明确限流是每个 gather 调用的限制，不是全局吞吐锁，更不是所有 ingestion 和删除操作的事务证明。
- 图谱不绑定 AO 或特定 Agent runtime；可导出图与 episodes，换库涉及图查询/索引与模型迁移，不能假设和普通 JSON 搬迁同成本。

LLM 实体抽取/消歧、embedding、可选 reranker 与图存储增加运行成本。Neo4j 不是唯一选择：当前依赖还列 FalkorDB/FalkorDBLite 等；Kuzu extra 已注明上游停止维护并计划移除，**不要用旧 Kuzu 方案作为新项目轻部署推荐**。

推荐触发条件：真实问题反复需要跨实体、多跳、时间关系检索，且普通元数据+全文/向量检索已经不足时再引入。Graphiti 能表示“SQLite 决策何时被 PostgreSQL 替代”，但该关系是否得到用户正式确认仍由 Cylo 决定；图谱推断不能自动写回权威决策。

## Letta：当前有 Git 记忆，但共享能力不能混同云与本地

当前 [README](https://github.com/letta-ai/letta-code/blob/f1627b01605780872677cd6af8d4b3a8d13b1cdb/README.md) 描述的是 stateful agent harness：记忆块、MemFS、后台学习、App Server、交互终端等。已有本地模式，不应再说“只能上云”。其记忆以 Git 跟踪，文本文件可读写、提交历史可追溯；[memory 子命令](https://github.com/letta-ai/letta-code/blob/f1627b01605780872677cd6af8d4b3a8d13b1cdb/src/cli/subcommands/memory.ts) 有导出，[memory-repository](https://github.com/letta-ai/letta-code/blob/f1627b01605780872677cd6af8d4b3a8d13b1cdb/src/cli/commands/memory-repository.ts) 可配置外部 Git 仓库。

但 [shared-memory.ts](https://github.com/letta-ai/letta-code/blob/f1627b01605780872677cd6af8d4b3a8d13b1cdb/src/cli/subcommands/shared-memory.ts#L249) 明确拒绝 local backend：`Shared memory requires a Letta Cloud agent; the local backend does not support it.` 开源客户端确有 org repository create/list/attach/detach/history 调用与本地挂载逻辑，不能据此声称完整共享后端已经开源可自托管。detach 也明示本地 mount 留存；Git 文件删除同样保留历史。

[memory-confinement](https://github.com/letta-ai/letta-code/blob/f1627b01605780872677cd6af8d4b3a8d13b1cdb/src/memory-confinement.ts) 有 unattended memory subagent 文件访问隔离；这是 Letta harness 的权限边界，不等于 Cylo 的项目/任务授权。Go 直接复用导出或文本约定可行，整套引入则增加另一套 Agent 生命周期、运行时、上下文和认证集成。本轮未运行本地 App Server，也不为其跨进程 Git 写入/删除一致性作运行验收承诺。

因此不选 Letta 作为 Cylo 首版记忆服务。若将来选择 Letta 作为 AO 下某个执行器，其原生记忆仍只是该执行器的上下文，不能成为所有 Agent 的唯一事实库。

## AO 已有什么，缺什么

不是“AO 完全没有记忆”：固定版本有持久会话/对话历史、原生 session 恢复、Agent 切换的 handoff 和历史引用。例如 [handoff_note.go](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/domain/handoff_note.go) 会从原任务、近期对话、变更文件和原生历史路径构造有限长度的交接说明；[side_context.go](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/service/chat/side_context.go) 能冻结并继承会话片段。

本轮检查 `backend/internal`、HTTP router、SQLite migrations/store 及 memory/embedding/vector/knowledge 相关命中，**未发现统一的跨 Agent、跨任务、按全局/项目/任务分层的长期事实提取、授权检索、确认版本与删除服务**。Codex adapter 生成协议中的 MemoryCitation/ThreadMemoryMode 属原生 runtime 协议，不是 AO 自建共享 memory API。这是固定版本和上述范围的源码结论，不是声称 AO 未来不会增加此能力。Cylo 应复用其历史/事件作为来源，另提供统一业务记忆层；不用重写 AO 会话恢复。

## 建议的首版责任分工

Cylo Go 层负责 scope/鉴权、原始事件与来源、已确认事实/决策版本、显式记忆 API、删除策略、context 预算及各执行器统一接入。关系型存储保存权威记录；全文索引和后续向量索引均视为可重建派生物。单机首版 SQLite 是可行候选，不将其固定为未来多写节点架构；中文全文分词、短词、混合中英文与语义召回必须单独验收，不能把 FTS 有表等同中文检索已达标。

PRD §10.3 用“可以”引入 Memory Agent，未强制独立角色。需要落实的整理职责包括：识别长期信息、提取事实、整理项目历史、建立关联、发现重复/过期/矛盾、提出更新建议。摘要、重复候选合并和复杂检索协助服务于这些职责；不能擅自合并互相矛盾的确认事实。显式记忆/决定登记、鉴权版本、规则去重和普通检索由 Go API 与规则完成；通用抽取和语义检索可由 Mem0 完成。独立 Agent 的增益主要是跨记录历史梳理、语义冲突/过期判断、关联和复杂项目总结。建议按实际价值启用后台整理角色，由 Super 或事件/定期任务经 AO 派发，不将其列为首版或每轮对话前置；专门职责不等于独立 Agent，更不等于常驻进程。runtime/model 可配置，不固定 Codex。

复杂整理拓扑为 **Cylo / Super → Memory Agent（按需）→ Cylo Memory API → Mem0 / SQLite**。API 在 Go 层执行 scope、权限、来源、版本和持久化规则；Mem0 负责可替换的抽取/检索步骤，SQLite 保存 Cylo 权威记录。快速查询和明确用户记忆/决定的授权写入可以受控直达 API，无需 Memory Agent 居中阻塞；用户已明确作出的决定直接登记来源，不重复确认。各 Agent 只共享任务授权范围内的相关记忆，不默认公开 Worker 私有历史。

避免同一来源先由 Agent 抽取，再让 Mem0 默认重新抽取一次。通用抽取直接委托 Mem0；已整理候选通过受控适配跳过重复提取。固定版 `infer=False` 能跳过事实提取 LLM，但仍调用 embedding；纯确定性事件走规则/API。

开源层可先采用成熟存储/全文组件；自动抽取或语义需求进入首版时采用 Mem0 worker，而非继续扩张自研通用 memory engine。API/作业层对同 scope 实施幂等/串行写入控制；Memory Agent 产出候选、来源和冲突提示，确定性更新和已有明确授权的记录依规则提交。它不在 Space 实时语音关键路径阻塞，也不获得修改权限、任务状态和正式决策的额外权力。账号池与 CLIProxyAPI 是模型访问层，不承担记忆 scope，也不能把提供商账号当用户或项目标识。完整分工见 [17 记忆设计](17-memory-requirements-and-design.md#memory-agentsuperapi-与引擎的分工)。

接入任何引擎之前，以同一小组无凭据样本验收：跨 Agent 同项目共享、跨项目隔离；SQLite→PostgreSQL 新旧决策及来源；推断不能覆盖确认事实；同事件重试/并发写无重复；删除后 history/索引/原始资料边界符合约定；重启恢复与导出重建；中文召回。此轮没有真实模型/账号，故未声称已经通过语义质量和端到端集成验收。

## 核验记录

固定源码文件 SHA-256、候选版本与本地无模型检查结果见 [memory-open-source-options-evidence.json](evidence/memory-open-source-options-evidence.json)。实测仅直接加载 Mem0 SQLiteManager 标准库文件，验证 history ADD/UPDATE/DELETE 保留与并发写入；没有安装候选完整依赖、联网调用模型、读取用户凭据或修改产品代码。源码静态检查不等于候选平台完整运行验收。
