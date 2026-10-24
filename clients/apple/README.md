# Apple clients

Cylo provides native SwiftUI apps for macOS, iPhone, and iPad. The clients use the Go server as the authority and keep offline snapshots visibly stale. Product flows, voice and notification boundaries, current build evidence, and device acceptance are maintained in the [master plan](../../docs/master-plan.md#4-native-application-flows).

Complete app-target builds require an eligible Xcode destination. Direct SDK compilation provides source-level evidence only. Build, signing, device, APNs, and live voice acceptance must be reported separately; no source check establishes those outcomes. See [milestones and acceptance](../../docs/master-plan.md#12-milestones-and-acceptance) for the evidence boundary.

The redesigned workspace is shared by recent tasks, task search, projects, and linked notifications. On iPhone, Conversation, Tasks, and Projects are tabs; on macOS and regular-width iPad they are sidebar destinations. Open notifications from the toolbar and Settings, Memory, or Capabilities from the account menu. Push preferences are in Settings. Voice opens from the composer; review the transcript draft before submitting.

The [visual reference](../../docs/design/apple-redesign.html) is a standalone design artifact. It does not replace native screenshots or device validation. The current redesign verification and remaining acceptance boundaries are recorded in the master plan.
