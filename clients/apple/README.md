# Apple clients

Native SwiftUI targets share [CyloKit](Packages/CyloKit/Package.swift) for API/domain state, synchronization, credentials, and voice protocols. Use the [product requirements](../../docs/product.md#3-client-requirements) for connection and interaction behavior, [architecture](../../docs/architecture.md#identity-clients-and-notices) for authority, and [evidence register](../../docs/acceptance/evidence.md) for build/install results and remaining device gates.

## Build and platform tools

Use the [Apple workflow](../../.github/workflows/apple.yml) on a GitHub-hosted macOS runner for Swift tests, macOS archives, and iOS target builds. The [Xcode project](Cylo.xcodeproj/project.pbxproj) defines app targets and [package manifest](Packages/CyloKit/Package.swift) defines dependency pins. Prepare required tools/environment under `$HOME/environment` on the runner.

WebRTC owns capture/playback; platform audio/session/lifecycle adapters coordinate permissions, audio routes, interruptions, and background audio. Avoid competing microphone pipelines. The [source-build helper](tools/build-webrtc-source.sh) is a candidate tool, not evidence of a built or binary-equivalent WebRTC artifact. [Deployment operations](../../docs/operations/deployment.md#native-client-installation-and-connection) cover staging and install boundaries.

For read-only operator Xcode discovery over existing SSH, use the [Xcode MCP skill](../../.agents/skills/xcode-mcp-ssh/SKILL.md). It is development tooling and does not create a Cylo runtime or a Mac server.
