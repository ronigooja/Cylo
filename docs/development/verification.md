# CI verification

When validating a change, select checks by affected behavior and risk. The
[scope selector](../../tools/ci_scope.py) is the executable source of truth;
[scope regressions](../../tools/test_ci_scope.py) protect omission boundaries.
Linux checks use GitHub-hosted Linux runners and Apple checks use GitHub-hosted
macOS runners. Environment files and installed tools live under
`$HOME/environment` on the runner.

## Automatic checks

The [verification workflow](../../.github/workflows/ci.yml) runs on pull requests
and pushes to `main` and `codex/**`. It compares the complete changed-path set,
including deleted files and both sides of renames. Missing history and unknown
paths select all existing check groups. A new component still needs checks
appropriate to its behavior; selecting existing groups does not create coverage.

| Change | Selected checks |
|---|---|
| Markdown, documentation assets, documentation checker | Documentation checks |
| Files under the macOS app directory | macOS build |
| Files under the iOS app directory | iOS build |
| Other Apple files, including packages, project configuration, and shared resources | Swift package tests and both app builds |
| Go source | Full Go suite and the existing authorization/concurrency race package set |
| Control, voice, memory, or capabilities API packages; shared protocol; integration fixtures; or Go dependencies | Go/race checks plus Swift tests and both app builds |
| Linux deployment shell scripts | Release-script and CLI-installation regression checks |
| CI configuration, scope selector, unknown files, or unavailable diff history | All existing check groups |

Selections are combined for mixed changes. Source comments are not inferred from
diff text: a comment-only source change follows its source component's checks.
The current Go suite remains whole; the race package set also remains unchanged
until dependency evidence supports a safe reduction. Python services and external
AO runtime checks retain their existing coverage limitations; full selection is
not complete product or external-service acceptance.

Each selected Apple build, Swift suite, Go suite, race suite, and script suite is
an independent job. Routine Apple checks build apps without producing an archive;
routine Linux checks do not package operator release binaries. Logs are uploaded
separately, with failure excerpts printed to the console.

## Completion gate and manual checks

When configuring branch protection, require **Required verification** from the
verification workflow. It requires successful scope selection and success for
every selected group; an unexpectedly skipped, failed, or cancelled selected
group fails the gate. This repository change does not alter remote branch
protection settings.

When impact cannot be bounded, run **Scoped verification** manually to execute
all existing check groups for the selected ref:

```sh
gh workflow run ci.yml --ref main
```

When unsigned Apple artifacts or Linux operator packages are needed, use the
corresponding workflow's manual `release` option. Each runs its complete relevant
validation before publishing artifacts for the same source revision:

```sh
gh workflow run apple.yml --ref main -f release=true
gh workflow run linux.yml --ref main -f release=true
```

Without `release=true`, those manual workflows perform full platform checks
without release packages. Apple artifacts remain unsigned; artifact creation does
not install, deploy, or establish end-to-end acceptance. Server and AO artifact
workflows remain separate; see [deployment operations](../operations/deployment.md).

When reporting verification, link the immutable workflow run, state the source
revision and selected checks, and distinguish fixtures, builds, installation, and
end-to-end acceptance. Use the [evidence register](../acceptance/evidence.md) for
retained observations and limitations.
