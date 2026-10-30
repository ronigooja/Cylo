# Memory and export

**Topic:** Memory and export.  
**Record scope:** The complete source, environment, observed checks, commands, revisions, results, and limitations formerly retained in the Component verification index.

This file is an evidence record, not an acceptance statement; category definitions are in [../evidence.md](../evidence.md#evidence-categories-and-acceptance-boundary).

## Source/environment and observed checks

Existing stub-backed Go→Python→Mem0→ephemeral-Qdrant tests cover component composition, not live AO/provider behavior. Bounded export checks passed for >64 MiB ZIP, hash/count, oversized rows, cancellation, permission, and concurrent snapshots; cursor/event-ID alignment passed source review. Restore candidate `59c61d93` passed owner Go/Python/restore-drill checks and independent review with no blocking findings. Cache/export-temp commits `fa4930d`/`9041464` passed independent review; owner reported seven Go-package and two race-package checks PASS. Independent commands passed: `/usr/local/go/bin/go test ./internal/memory ./internal/control ./tests/acceptance ./tests/integration -count=1` and `/usr/local/go/bin/go test -race ./internal/memory ./internal/control -count=1`. Backup purge/manifest commit `f3c8ab6`/`4642ff9` passed independent review and 13 tests: `PYTHONDONTWRITEBYTECODE=1 PYTHONPATH=tools /tmp/zoe-memory-export-venv/bin/python -m unittest data_lifecycle.test_retention data_lifecycle.test_backup_manifest -v`. The integrated local backup fixture also exercised encrypted temporary-backup create/delete, retention, latest-head staging, and restore preparation/reconciliation; it confirmed Mem0/Qdrant remain `adapter_missing`, the restore gate stays closed, and one artifact remains. The backup lifecycle/worker checks are local; they do not establish physical deletion from production external stores.

## Acceptance limits

The maintenance user's current authority generation/head/hash remains the trust root; journal continuity does not prove every restore transition independently. Real Mem0/Qdrant cleanup and physical external-store deletion remain pending; unsupported resources stay `adapter_missing` and cannot be manually cleared. Production backup erasure, a measured retention guarantee, and restore rehearsal remain open.
