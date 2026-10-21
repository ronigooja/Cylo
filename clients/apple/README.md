# Apple clients

Cylo provides native SwiftUI apps for macOS, iPhone, and iPad. The clients use the Go server as the authority and keep offline snapshots visibly stale. Product flows, voice and notification boundaries, current build evidence, and device acceptance are maintained in the [master plan](../../docs/master-plan.md#4-native-application-flows).

The current source has nine passing Swift package tests and an unsigned macOS app build. On the available Mac, the iOS 18.2 SDK also compiled both shared Swift package targets and the `CyloMobileApp.swift` entry point. Xcode still exposes no eligible iOS destination, so this is direct source compilation rather than an Xcode iOS app-target build. These checks do not establish signed distribution, device behavior, or live APNs delivery. See [milestones and acceptance](../../docs/master-plan.md#12-milestones-and-acceptance) for the evidence boundary.
