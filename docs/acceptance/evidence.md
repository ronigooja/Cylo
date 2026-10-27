# Acceptance evidence register

This register separates repeatable source checks from product acceptance. A source check is evidence about repository code or configuration. An acceptance record must identify the environment, revision, command or user action, observed result, and limitation; otherwise status remains open.

## Current evidence

| Area | Evidence | Status and boundary |
| --- | --- | --- |
| Linux deployment | Service/readiness checks and LAN address resolver in [`docs/local-deployment.md`](../local-deployment.md) and [`deploy/linux/resolve-server-address.py`](../../deploy/linux/resolve-server-address.py) | Source/operational verification. Reachability only; no model, memory, voice, or task acceptance. |
| AO deployment | Pinned AO source/build and readiness notes in [`docs/master-plan.md`](../master-plan.md#ao-startup-deployment--2026-10-26) | Source/build and host verification. No end-to-end task execution claim. |
| Key and export tooling | Operator CLI and bounded export/destroy implementations | Source checks. Live migration, deletion, and restore remain unaccepted. |
| Apple client | CI Swift tests/archive and installation notes in [`docs/local-deployment.md`](../local-deployment.md#macos-application) | Build/source checks only. Native interaction, iPhone/iPad installation, push, and live login remain open. |
| Voice gateway | Pinned upstream and deployment reference in [`docs/local-deployment.md`](../local-deployment.md#voice-gateway) | Source/deployment review. Account import, trusted media, interruption, and Cylo voice acceptance remain open. |
| Chat continuation/recovery | AO gateway, runner reconciliation, and client continuation paths reviewed in source and runbook | **Source-reviewed; remote-unverified.** No live authorized chat recovery has been observed. Do not claim chat recovery acceptance. |

## Evidence rules

Do not promote archived research, fixtures, static checks, health responses, or a successful build to product acceptance. Link primary source files or immutable CI runs when citing implementation facts. Keep credentials, private account data, installation links, and raw audio out of evidence. When evidence conflicts, retain the uncertainty and record the next required observation rather than rewriting history.

The archived reliability report is useful design context but is not current acceptance evidence: [report 05](../archive/research/reports/05-reliability-security-operations.md). Historical owner-password rollout notes remain in [local deployment history](../local-deployment.md#historical-installed-owner-password-release--2026-10-25) for provenance only; they do not describe the current authentication baseline.
