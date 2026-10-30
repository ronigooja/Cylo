# Repository guidance

## Documentation and product changes

- When deleting, compressing, or merging documentation content (deduplication or restructuring), first confirm item by item that every deleted detail (field, table row, value, exception, or example step) has an equivalent statement in the retained text and that the link points to the real location containing it (not to itself); an independent reviewer must verify this before merge; when it cannot be confirmed, retain the original text.

## Coordination and operational safety

- When delegating complex work, UI work, or independent review, use GPT-6.1 Sol at medium reasoning effort; when delegating simple work, use GPT-6 Luna at medium effort. Keep at most four implementation subagents and reserve an available slot for independent review. These requirements supplement the global subagent rules; worktree locations still follow the global rules.
- When delegating repository edits, assign explicit file ownership and coordinate shared API/data changes with affected owners before editing. Review each delegated commit before integration; do not edit outside the assignment or take over another owner's deletion scope.
- Before ending a delegated task or handing it to a fresh context, record branch/worktree, ownership, commit, completed and remaining work, verification, and limitations.
- When staging Mac task inputs, signing/install work, artifacts, or logs, use `~/Developer/Zoe/runs/<task-id>/` on the Mac for Mac task materials; this differs from the global `/tmp` location for temporary files. Install the application at `/Applications/Zoe.app`.
- When working with credentials or private account data, use secret names or deployment references; never put their values in source, documentation, logs, or fixtures.
- When modifying the repository, do not change external services or the existing CLIProxyAPI deployment unless explicitly authorized.
