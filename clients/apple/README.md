# Apple clients

Cylo provides native SwiftUI apps for macOS, iPhone, and iPad. The clients use the Go server as the authority and keep offline snapshots visibly stale. Product flows, voice and notification boundaries, current build evidence, and device acceptance are maintained in the [master plan](../../docs/master-plan.md#4-native-application-flows).

Complete app-target builds require an eligible Xcode destination. Direct SDK compilation provides source-level evidence only. Build, signing, device, APNs, and live voice acceptance must be reported separately; no source check establishes those outcomes. See [milestones and acceptance](../../docs/master-plan.md#12-milestones-and-acceptance) for the evidence boundary.
