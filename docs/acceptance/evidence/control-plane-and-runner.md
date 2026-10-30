# Control plane and runner

**Topic:** Control plane and runner.  
**Record scope:** The complete source, environment, observed checks, commands, revisions, results, and limitations formerly retained in the Component verification index.

This file is an evidence record, not an acceptance statement; category definitions are in [../evidence.md](../evidence.md#evidence-categories-and-acceptance-boundary).

## Source/environment and observed checks

The AO references in the retained observations below are historical only.

On integrated source commit `c0380c7`, the owner reported PASS for `PATH=/usr/local/go/bin:$PATH go test ./cmd/zoe-server -run TestComposedServerRegistersDerivedWorkerCredentialAndFailsClosedWithoutVoiceGateway -count=1` and `PATH=/usr/local/go/bin:$PATH go test ./...`; `git diff --check 2d35a86..HEAD` also passed on the later candidate containing this source. Independent review passed `go test ./internal/capabilities -run TestCapabilityTriggerRunnerAOResultChain -count=1`. The AO-chain fixture uses fake AO HTTP. The initial composition failure was a test-fixture omission; review confirmed production startup registers the maintenance migration. No remaining concrete findings were reported for this integration scope. The earlier live AO create attempt returned HTTP 409 without a retained allowlisted rejection code, so the result is treated as uncertain. A later read-only inspection observed zero sessions, but that does not establish that this request had no external effect; no artifact, provider outcome, or termination proof was recorded.

## Acceptance limits

A successful real AO task chain with provider/tool/artifact evidence and confirmed termination; recovery against a real AO deployment; provider-side usage/cost evidence; production deployment and cross-host migration fencing; complete scenario acceptance.
