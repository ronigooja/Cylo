# Development rules

This document owns build and test execution rules. Product decisions and acceptance boundaries remain in [the master plan](master-plan.md). Deployment operations are documented in [local deployment operations](local-deployment.md).

## Execution location

| Work | Required location |
| --- | --- |
| Linux compilation and Docker image builds | Vultr worker |
| Linux unit, race, integration, acceptance, and container tests | Vultr worker |
| Native macOS/iPhone/iPad builds and tests | Existing authorized macOS/Xcode environment |
| Documentation edits and lightweight static checks | Local controller permitted |

Local static checks must not compile code, run application/test code, or create containers. Examples include `python3 tools/check_docs.py` and `git diff --check`. Commands such as `go test`, Python test suites, and tests through Docker Compose run on Vultr.

## Required skills

- Before any Vultr build/test task or worker operation, read and follow `vultr-tasks` at `/root/.codex/skills/vultr-tasks/SKILL.md`.
- Use that skill for worker reuse, provisioning, source transfer, result collection, and cleanup. Its lifecycle procedure is not duplicated here.
- For the Apple workflow, read the `xcode-mcp-ssh` skill when applicable.

These rules alone do not authorize creating billable resources. Apply existing task authorization and the skill's defaults. Development workers do not change AO's ownership of product runtimes or authorize changes to external services.

## Cache and source handling

- Keep dependency downloads, Docker layers/volumes, and build/test caches on the worker.
- Transfer only required source inputs; exclude credentials and unrelated private data.
- Retrieve required logs and artifacts without copying dependency or build/test caches back to the controller.

## Verification reports

Record the source revision, uncommitted input changes, worker environment, exact commands, actual exit status, and saved log/artifact paths. Claim success only after collecting the successful result.

Follow the Vultr skill's cleanup requirements and report whether deletion was confirmed. If a worker remains, report its ID, reason, ongoing billing, and any verified cleanup schedule.

Linux results do not establish Apple build or test success. A successful build or test proves only the scope exercised; it does not by itself establish deployment or end-to-end acceptance.
