# Budget enforcement

**Topic:** Budget enforcement.  
**Record scope:** The complete source, environment, observed checks, commands, revisions, results, and limitations formerly retained in the Component verification index.

This file is an evidence record, not an acceptance statement; category definitions are in [../evidence.md](../evidence.md#evidence-categories-and-acceptance-boundary).

## Source/environment and observed checks

The AO references in the retained observations below are historical only.

Owner and independent AO/control/runner review reported passing targeted tests; the integrated Go suite and relevant race packages passed on the owner checkpoint. These checks cover local ledger and control behavior.

## Acceptance limits

AO has no production per-provider-call reservation hook or actual input/output usage reporting across its internal provider/tool calls; only the initial prompt is currently counted as one `ModelCalls` reservation. It also has no production pre-call tool-step enforcement or bounded artifact sink. `MaxToolSteps` and `MaxArtifactBytes` currently exist as DTO, snapshot, or ledger values, not as end-to-end limits on actual tool loops and artifact writes. Hard-deadline termination needs an online fence and is not an offline hard-stop guarantee. Full usage/cost accounting requires provider-backed evidence.
