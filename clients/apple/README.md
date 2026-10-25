# Apple clients

Cylo provides native SwiftUI apps for macOS, iPhone, and iPad. The clients use the Go server as the authority and keep offline snapshots visibly stale after session validation. Product flows, voice and notification boundaries, current build evidence, and device acceptance are maintained in the [master plan](../../docs/master-plan.md#4-native-application-flows).

The server address is fixed at `http://debian.local:8787`. The first device sets the owner password with a server-admin setup code; additional devices sign in with that password. Successful sessions are saved in Keychain by default and validated before opening the workspace. Settings lists devices and supports revocation and sign-out. Both actions cancel queued tasks and request active tasks and runtimes authorized by the affected device to stop; forgetting the password requires server-admin recovery.

Complete app-target builds require an eligible Xcode destination. Direct SDK compilation provides source-level evidence only. Build, signing, device, APNs, and live voice acceptance must be reported separately; no source check establishes those outcomes. See [milestones and acceptance](../../docs/master-plan.md#12-milestones-and-acceptance) for the evidence boundary.

The redesigned workspace is shared by recent tasks, task search, projects, and linked notifications. On iPhone, Conversation, Tasks, and Projects are tabs; on macOS and regular-width iPad they are sidebar destinations. Open notifications from the toolbar and Settings, Memory, or Capabilities from the account menu. Push preferences are in Settings. Voice opens from the composer; review the transcript draft before submitting.

The macOS assistant uses compact project/task navigation, a bottom composer with a prominent microphone, and a collapsible task activity rail on wide windows. Tasks show progress, results, evidence summaries, and decisions. The [approved visual direction](../../docs/design/native-assistant-reference.png) is a design preview; native visual and interaction acceptance remain open.

The [visual reference](../../docs/design/apple-redesign.html) is a standalone design artifact. It does not replace native screenshots or device validation. The current redesign verification and remaining acceptance boundaries are recorded in the master plan.

For operator-controlled Xcode access from another host, use the repository's [Xcode MCP over SSH skill](../../.agents/skills/xcode-mcp-ssh/SKILL.md). It includes connection discovery, a batch helper, and real-device startup troubleshooting.
