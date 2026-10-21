# AO 的 Agent、模型路由与恢复边界

核查日期：2026-10-21；固定 AO SHA：`7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff`。本次仅本地只读源码核查和文档修订，没有运行模型、登录账号或修改产品实现。

Cylo 采用 AO 的多 Agent 编排能力，不把 Codex 设为所有任务的执行器。此前着重说明 Codex 恢复，是沿用了早期 Codex/CAO 研究的叙述；采用 AO 后，不应继续把另写 Codex resume 补丁当作前置条件。

## 三层职责

- **Cylo 业务层**：Task/TaskRun、记忆、授权、验收、长期服务；业务任务生命周期独立于执行会话。
- **AO coding agent runtime 层**：例如 Claude Code、Codex、OpenCode；负责对应原生程序的启动、工作区、会话与恢复适配。
- **model/provider/route 层**：runtime 实际访问的模型、提供商和协议端点。支持某个 runtime 不代表它支持任意模型，也不代表所有 runtime 使用同一协议或账号池。

固定版本的 [registry/Constructors](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/agent/registry/registry.go#L57) 注册 37 个 adapter，包括 Claude Code、Codex、OpenCode、OpenCode v2、Cursor、Gemini、Copilot、Kimi、Droid、Amp、Aider、Goose 等。该列表证明源码内有接入，不证明 Cylo 已安装或验收每个 runtime；完整名单以该固定注册表为准。模型/provider 选择与原生配置见同版本 [modelcatalog](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/agent/modelcatalog)。

现有 CLIProxyAPI 是可复用的模型请求路由：Codex Responses 是已核查实例；其他 runtime 只有在支持的协议、配置、工具语义和认证兼容时才可使用它，也可使用各自 provider。平台不要求所有 Agent 经 Codex，代理也不管理原生会话文件或替代 AO 的 runtime adapter。Space 语音上游与账号池继续独立。

## 恢复优先复用已有 adapter

| 固定源码实例 | 已有适配与边界 |
|---|---|
| [Codex](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/agent/codex/codex.go#L144) | `codex resume <agentSessionId>`；已有实现，不需要先重写恢复补丁 |
| [Claude Code](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/agent/claudecode/claudecode.go#L382) | 已有原生 session 恢复命令及身份处理 |
| [OpenCode](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/agent/opencode/opencode.go#L145) / [OpenCode v2](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/agent/opencodev2/opencodev2.go#L63) | 已有按捕获的 native ID 构造恢复命令；两个 major 分开适配 |
| [Continue](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/agent/continueagent/continueagent.go#L179) | 使用 `--fork <id>` 接续指定历史，不能等同保留同一原生 session ID |
| [Aider](https://github.com/OrchestratorInc/agent-orchestrator/blob/7c83bd68c1e60c85b6a90e3ffa1340c859e4a3ff/backend/internal/adapters/agent/aider/aider.go#L1) | 当前 adapter 说明没有原生 session ID/resume-by-ID；继承 Base 的不支持恢复行为，不能照搬 Codex 承诺 |

Cylo 集成层统一记录 `task/run → AO session → agent native session`，连带 runtime、model/provider/route、host、工作区、原生配置域和恢复模式。按所选 agent 区分原生 resume、fork、仅工作区 fresh launch、存活进程重附着；缺少能力明确标识，不静默把新会话当作原会话。业务账本和副作用核对仍由 Cylo 负责。

实际接入先复用已有 adapter，再逐个验收所选 runtime 的停止、查看、持久历史、重启与迁移；只有验证出具体缺口才补适配。源码存在不等于所有版本、工具、审批或跨 provider 切换都已通过运行验收。
