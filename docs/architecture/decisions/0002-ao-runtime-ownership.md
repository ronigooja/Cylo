# ADR 0002: AO runtime ownership and external-service boundaries

Status: accepted architecture baseline; implementation and acceptance status remain in [the master plan](../../master-plan.md). Recorded during the 2026-10-27 documentation consolidation from existing decisions; this record does not introduce a new delivery claim.

## Context

Cylo must support different specialist agents and model routes without confusing orchestration with authoritative business state. Multiple runtime creators or a separate custom recovery dispatcher would risk duplicate work and inconsistent ownership. Codex is an important development runtime but not the entire execution system.

## Decision

Whenever Cylo creates or manages a runtime, including Super, Workers, and memory curator work, use AO (OrchestratorInc/agent-orchestrator) through the authorized Cylo gateway. Cylo validates task/run identity, permissions, context, budget, and admission; AO owns runtime/session creation and lifecycle. Agent output and runner observations cannot directly mutate business authority.

Keep role, runtime, and model/provider route separate. Count all active runtime work against the shared four-run ceiling. Prefer AO configuration/adapters; extend AO only for measured gaps. Do not add a Codex-specific recovery subsystem. Official Codex desktop-app integration is excluded; native CLI handoff remains optional.

For model routes supplied by the existing remote CLIProxyAPI deployment, keep account selection, quota handling, and proxying there and reuse the deployment unchanged.


When integrating external services, isolate their failures and lifecycle from unrelated Cylo modules. The existing remote CLIProxyAPI remains an external dependency, not a Cylo deployment target. Space3044 supplies the selected real-time voice gateway/account pool; Cylo owns the voice-to-command/task bridge and safe lease policy. Mem0 is a controlled memory worker and Qdrant a rebuildable index, with authority retained in Go. Secrets are deployment-provided references; never copy their values into source, documentation, logs, or fixtures. Do not change external services or the existing proxy deployment during repository work without explicit authorization.

## Consequences

One controlled path links Project → Task → Run → AO Session → Runtime Native Session. Workers may be invoked directly when judgment is local; Super is reserved for complex coordination. Cylo must retain safe claims/fences and evidence across AO uncertainty. Adapter registration or a direct model probe does not establish supported execution, provider usage, artifacts, recovery, or stop. External CLIProxyAPI remains outside Cylo deployment lifecycle.

## Sources

[AO upstream](https://github.com/OrchestratorInc/agent-orchestrator); [Product chapters 5–6, 13, and 17](../../product.md); [architecture admission and routing](../../architecture.md#runtime-admission-and-model-routing); historical evidence: [AO/provider boundaries](https://github.com/ronigooja/Cylo/blob/95ee55a5bbd8dccfb7f14af9f9703bb83727da6c/docs/archive/research/reports/16-ao-agent-provider-boundaries.md).
