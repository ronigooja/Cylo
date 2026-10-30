# Native Apple clients

**Topic:** Native Apple clients.  
**Record scope:** The complete source, environment, observed checks, commands, revisions, results, and limitations formerly retained in the Component verification index.

This file is an evidence record, not an acceptance statement; category definitions are in [../evidence.md](../evidence.md#evidence-categories-and-acceptance-boundary).

## Source/environment and observed checks

Source review confirmed the server/Apple event-ID and cursor contract, Save-authority checks, and invalidation paths (server `114f7b8`, Apple `e009e39`, fixture fix `0af961d`). At that earlier checkpoint, related tests were added but no Swift tests or builds ran. On 2026-10-24 a macOS Xcode build, installation, launch, relocated WebRTC loading, ad-hoc signature verification, and native backend reachability passed; see the [installation source record](https://github.com/ronigooja/Zoe/blob/95ee55a5bbd8dccfb7f14af9f9703bb83727da6c/docs/master-plan.md#historical-local-installation-evidence--2026-10-24). Existing APNs token registration, revocation, and inbox code is source implementation only.

## Acceptance limits

Current-key authenticated macOS app flows, distribution signing/notarization, iPhone/iPad installation, APNs delivery, and real-device voice/media acceptance.
