# Repository guidance

## Documentation and product changes

- When changing product behavior or interfaces, follow [product requirements](docs/product.md) and [architecture](docs/architecture.md); update their applicable requirements or decisions when the approved baseline changes.
- When changing native UI, follow [product chapter 3](docs/product.md#3-client-requirements) and the approved [native assistant reference](docs/design/native-assistant-reference.png).
- When reporting implementation or verification, keep source review, fixtures, CI, installation, and end-to-end acceptance distinct; record applicable evidence and limitations in [the evidence register](docs/acceptance/evidence.md), with delivery status in [the master plan](docs/master-plan.md).

## Coordination and operational safety

- When delegating complex work, UI work, or independent review, use GPT-6.1 Sol at medium reasoning effort; when delegating simple work, use GPT-6 Luna at medium effort. Keep at most four implementation subagents and reserve an available slot for independent review.
- When delegating repository edits, assign explicit file ownership and coordinate shared API/data changes with affected owners before editing. Review each delegated commit before integration; do not edit outside the assignment or take over another owner's deletion scope.
- Before ending a delegated task or handing it to a fresh context, record branch/worktree, ownership, commit, completed and remaining work, verification, and limitations.
- When staging Mac task inputs, signing/install work, artifacts, or logs, use `~/Developer/Zoe/runs/<task-id>/` on the Mac. Install the application at `/Applications/Zoe.app`.
- When working with credentials or private account data, use secret names or deployment references; never put their values in source, documentation, logs, or fixtures.
- When modifying the repository, do not change external services or the existing CLIProxyAPI deployment unless explicitly authorized.
