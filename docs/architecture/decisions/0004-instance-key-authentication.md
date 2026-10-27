# ADR 0004: Shared instance-key authentication for a fresh deployment

Status: accepted architecture baseline; implementation and acceptance status remain in [the master plan](../../master-plan.md). Recorded during the 2026-10-27 documentation consolidation from existing decisions; this record does not introduce a new delivery claim.

## Context

The current deployment is private, single-owner, and single-instance with multiple native clients. Earlier owner-password, bootstrap/pairing, approved-device-session, and fixed-endpoint designs add identity/session behavior outside the selected fresh-deployment connection model.

## Decision

For a fresh deployment, one canonical 32-byte unpadded base64url instance key grants the fixed `owner` principal in workspace `default`. The server stores its hash and does not print the key on ordinary startup. Runner and memory-worker credentials stay separate. This decision supersedes earlier owner-password/device-session designs without retaining their schema/API compatibility.

Clients collect server URL and key. Opening an external installation link stages values for review and requires Connect. Explicit QR image import or camera scanning connects after validating address/key; an HTTP address requires confirmation before sending the key. iPhone/iPad request camera permission only when Scan QR code is chosen. Image import uses selected images without camera access; camera frames are decoded locally without saving or uploading them. The canonical link is `cylo://connect?server=<URL-encoded>#key=<key>` and the entire link/QR is a credential. Accept only canonical 43-character keys. Normalize scheme/host case, default ports, trailing slashes, and retained deployment path. Reject URL user information, query/fragment, traversal, encoded separators, and invalid ports.

Keychain is the only client credential store, scoped by normalized server URL. An authenticated snapshot validates owner/workspace; no password/session endpoint, approved-device list, fixed client endpoint, or saved-server list is part of this design. Replacement validates the new key before applying it and keeps the current server address. A different server requires explicit clearing and fresh connection.

When clearing/replacing a connection, discard its local durable queue, cached snapshot, exports, and drafts. Clearing does not revoke the shared key or stop server tasks. Push registrations use a separate stable random installation UUID and are scoped so one installation cannot overwrite/revoke another's token.

## Consequences

Every holder of the shared key has owner access; local clearing is not server-side revocation. Operator key rotation blocks future old-key access and requires redistribution. Losing the key needs operator replacement rather than password recovery. Rejected credentials close workspace access; ordinary transport errors preserve validated state with stale feedback. Import validation, Keychain denial, rotated keys, concurrent installations, protected transport, and native device interaction require separate acceptance evidence.

## Sources

[Product chapter 14](../../product.md#14-permission-and-security-requirements); [architecture identity boundary](../../architecture.md#identity-clients-and-notices). The decision was selected on 2026-10-25; rollout and acceptance remain separate in [the master plan](../../master-plan.md).
