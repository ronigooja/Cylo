# Apple clients

Cylo provides native SwiftUI apps for macOS, iPhone, and iPad. The clients use the Go server as the authority and keep offline snapshots visibly stale. Product flows, voice and notification boundaries, current build evidence, and device acceptance are maintained in the [master plan](../../docs/master-plan.md#4-native-application-flows).

The current source has nine passing Swift package tests and an unsigned macOS app build. The available Xcode does not expose an eligible iOS build destination, so iOS compilation remains unverified. These checks do not establish signed distribution, device behavior, or live APNs delivery. See [milestones and acceptance](../../docs/master-plan.md#12-milestones-and-acceptance) for the evidence boundary.
