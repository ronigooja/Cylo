# Repository guidance

## Documentation and product changes

- When changing product behavior or interfaces, follow [product requirements](docs/product.md) and [architecture](docs/architecture.md); update their applicable requirements or decisions when the approved baseline changes.
- When changing native UI, follow [product chapter 3](docs/product.md#3-client-requirements) and the approved [native assistant reference](docs/design/native-assistant-reference.png).
- When reporting implementation or verification, follow [the evidence register](docs/acceptance/evidence.md) for evidence categories and the acceptance boundary, with delivery status in [the master plan](docs/master-plan.md).
- When deleting, compressing, or merging documentation content (deduplication or restructuring), first confirm item by item that every deleted detail (field, table row, value, exception, or example step) has an equivalent statement in the retained text and that the link points to the real location containing it (not to itself); an independent reviewer must verify this before merge; when it cannot be confirmed, retain the original text.

## Coordination and operational safety

- When staging Mac task inputs, signing/install work, artifacts, or logs, use `~/Developer/Zoe/runs/<task-id>/` on the Mac. Install the application at `/Applications/Zoe.app`.
- When working with credentials or private account data, use secret names or deployment references; never put their values in source, documentation, logs, or fixtures.
- When modifying the repository, do not change external services or the existing CLIProxyAPI deployment unless explicitly authorized.
