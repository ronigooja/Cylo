# Acceptance requirements

This page defines what must be demonstrated for acceptance of the current product baseline. A source check proves code, configuration, or a repeatable local invariant. Acceptance requires an observed scenario on the supported deployment and records the evidence link, timestamp, revision, environment, and limitations. Passing source checks alone never changes an item to accepted.

## Required scenarios

| ID | Acceptance requirement | Required evidence |
| --- | --- | --- |
| A | Text/voice task survives client disconnect, shows progress on another Apple client, continues, and stops safely. | Mac, iPhone, and iPad timeline; task/run links; result, failure, reconnect, continuation, and stop records. Voice gateway and account/media behavior are included. |
| B | An authorized long-running capability is installed, configured, run, paused/resumed, updated, stopped, and removed; unauthorized deployment is denied. | Negative authorization test plus real lifecycle and notice records. |
| C | A dated project decision is recovered across devices without inventing unsupported facts. | Source-linked versions, dates, restart/device query, and adversarial stale/missing evidence checks. |
| D | Parallel specialist work coordinates conflicts and reports aggregate outcomes truthfully. | Two runtime timelines, independent results, injected conflict, and failed/canceled branch. |
| E | Network, API, and process failures are detected and recovered without duplicate irreversible effects. | Fault timeline, receipts/query results, unknown-effect handling, recovery, escalation, and no-blind-retry evidence. |

The stable requirement index and component ownership are maintained in the [master plan](../master-plan.md#requirements-and-acceptance-index), with detailed stable IDs in the archived [requirements report](../archive/research/reports/01-requirements.md).

## Cross-cutting gates

Acceptance also requires durable commit evidence, permission and approval negative tests, single-instance fencing during migration/restore, source-linked memory versions and tombstones, stale-index rebuild, cross-device state convergence, APNs delivery on real devices, and measured service-quality targets. Explicit exclusions remain excluded: official Codex desktop integration, PWA/CAO implementation, macOS server, and Codex-specific recovery.

Authentication acceptance must follow the current instance-key baseline. Earlier owner-password, setup-code, device-login, and remote-revocation descriptions are historical rollout records and must not be used as current requirements. Chat recovery is source-reviewed but remote-unverified; it remains open until a live authorized chat continuation and recovery run is recorded.
