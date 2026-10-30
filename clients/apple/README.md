# Apple clients

Native SwiftUI targets share [ZoeKit](Packages/ZoeKit/Package.swift) for API/domain state, synchronization, credentials, and voice protocols. Use the [product requirements](../../docs/product.md#3-client-requirements) for connection and interaction behavior, [architecture](../../docs/architecture.md#identity-clients-and-notices) for authority, [deployment operations](../../docs/operations/deployment.md#native-client-installation-and-connection) for Mac staging/install paths, and [evidence register](../../docs/acceptance/evidence.md) for build/install results and remaining device gates.

## Build and platform tools

Use the [Apple workflow](../../.github/workflows/apple.yml) for Swift tests and app builds; unsigned archives are produced only by its manual release option. See [CI verification](../../docs/development/verification.md) for scope selection and artifact commands. The [Xcode project](Zoe.xcodeproj/project.pbxproj) defines app targets and [package manifest](Packages/ZoeKit/Package.swift) defines dependency pins.

The manual release disk image includes `Sign-Zoe.command` ([source](tools/Sign-Zoe.command)). Double-click it on a Mac to re-sign the app with a code-signing identity from that Mac's Keychain and install it to `/Applications/Zoe.app`; pass `--no-install` to save a signed copy on the Desktop instead. A stable signing identity lets Keychain approvals persist across builds. The re-signed copy carries no entitlements, so push notifications stay unavailable until a provisioning profile is added. The private key never leaves the Mac.

WebRTC owns capture/playback; platform audio/session/lifecycle adapters coordinate permissions, audio routes, interruptions, and background audio. Avoid competing microphone pipelines. The [source-build helper](tools/build-webrtc-source.sh) is a candidate tool, not evidence of a built or binary-equivalent WebRTC artifact. [Deployment operations](../../docs/operations/deployment.md#native-client-installation-and-connection) cover staging and install boundaries.

For read-only operator Xcode discovery over existing SSH, use the [Xcode MCP skill](../../.agents/skills/xcode-mcp-ssh/SKILL.md). It is development tooling and does not create a Zoe runtime or a Mac server.
