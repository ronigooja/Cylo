# ADR 0007: Zoe-owned OpenCode runtime

Status: accepted architecture baseline; implementation and acceptance evidence are tracked in [the acceptance evidence register](../../acceptance/evidence.md), while delivery status remains in [the master plan](../../master-plan.md). This supersedes [ADR 0002](0002-ao-runtime-ownership.md).

## Context

The AO layer and Codex runtime ownership model add a second lifecycle authority and leave uncertain container effects difficult to prove. Zoe needs one runtime boundary with deterministic creation, fail-closed recovery, and termination evidence. The existing CLIProxyAPI deployment must remain an unchanged external provider.

## Decision

OpenCode is the only Zoe execution runtime. Codex is removed. A model selection is an OpenCode provider/model string. CLIProxyAPI is used only as an OpenAI-compatible OpenCode provider; its existing deployment, account selection, quota handling, and proxying remain unchanged.

The AO layer is removed. For each run, Zoe's runner creates exactly one container, with name and labels derived from `run_id`, attempt, and fence. The container runs exactly one OpenCode session. The runner authenticates `opencode serve` with a random password, grants only a scoped key, and provides no direct network access: provider traffic reaches the configured provider through a bridge or Unix socket. The runner remains the trusted host-side lifecycle authority and the control plane remains the business-state authority.

Creation is idempotent. Container names and labels are deterministic. If a create result is unknown, the runner resolves it by looking up the expected labels. If that lookup cannot establish the outcome, the run is `effect_unknown` and is not replayed automatically. The runner fails closed rather than creating a second container.

Stopping aborts the OpenCode session, force-removes the exact container ID, and verifies that the container is absent. Container removal is the termination proof; process exit, timeout, lease expiry, or an unverified API response is not.

Runtime contracts use neutral `Runtime*` names; AO-specific fields are compatibility data during migration only. Runtime proofs are limited to:

- `create-rejected`: a local deterministic failure before an external create effect;
- `no-effect`: no container with the expected run/attempt/fence labels exists;
- `terminated`: the exact container has been removed and its absence verified.

Forward migration maps `codex` and empty runtime values to `opencode`; unknown values are quarantined. Legacy `ao_*` columns remain until their data is backfilled. Before AO is uninstalled, stop new claims, reconcile the four uncertain runs and retained containers through the existing reconciliation process, and retain AO binaries and state for a rollback window. The [CY-05-* requirement rewrite](../../product.md) is follow-up work and is not part of this ADR change.

## Open verification

The sufficiency of the OpenCode serve API is pending the separate probe report at `/tmp/opencode-probe-report.md`. Recovery still requires verification of session persistence. Usage and cost metering through the provider bridge remains open. Existing budget gaps are unchanged.

## Consequences

Zoe owns runtime creation, session lifecycle, reconciliation identity, and stop evidence; one container and one OpenCode session define each run's runtime boundary. Deterministic labels make unknown creation outcomes queryable without granting replay authority. Container removal gives a concrete termination boundary, while unresolved identity or absence remains fail-closed.

The migration temporarily carries legacy AO schema and rollback state. Provider integration remains dependent on the unchanged CLIProxyAPI deployment. OpenCode serve, recovery persistence, bridge metering, and existing budget gaps require acceptance evidence before they become delivery claims.

## Sources

[ADR 0002, superseded](0002-ao-runtime-ownership.md); [architecture runtime admission and model routing](../../architecture.md#runtime-admission-and-model-routing); [CLIProxyAPI deployment boundary](../../architecture.md#system-and-authority); [product requirements](../../product.md); pending [OpenCode serve probe report](/tmp/opencode-probe-report.md).
