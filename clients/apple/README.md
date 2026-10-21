# Apple clients

Cylo ships a native SwiftUI application for macOS and a shared SwiftUI application for iPhone and iPad. Both clients use the server as the authority for tasks, permissions, events, receipts, and memory versions. Local snapshots are visibly marked stale or offline, and pending commands remain journaled until the server returns their durable receipt.

## WebRTC dependency

`Packages/CyloKit/Package.swift` pins the community `stasel/WebRTC` binary distribution at source revision `44f36788497f701bc8c0306de3a1c2511d56019d` (release M155 / tag 155.0.0). Its package manifest declares the `WebRTC-M155.xcframework.zip` artifact checksum `cb9adacf2d6bbf598d1d9e3e6c16d8fb97ca987b5b697746891828f9dc068ffe`. The upstream project states that its framework binaries are compiled from the official WebRTC source without modifications; this repository consumes that verified release artifact and does not reproduce the upstream source build.

The concrete adapter creates a microphone audio track, receives remote audio, negotiates the Space3044 `oai-events` DataChannel, exchanges non-trickle SDP through Cylo's authenticated voice-session API, and waits for ICE and DataChannel readiness before displaying an active session. Voice captions are drafts until the user confirms them. The client closes local media before requesting hangup; if upstream termination cannot be verified, the server reports and keeps the account pool quarantined.

`tools/build-webrtc-source.sh` is the reproducible source-build path. It pins candidate WebRTC M155 source branch head (`3b7ef6826ea6cb4c89c3972b1f14fcb3ed0fa469`) and `depot_tools` revision (`c84f5f5081e89eda268785a04a9e8a9a1987724f`), builds iOS device/simulator and macOS arm64/x86_64 frameworks, combines the architecture slices, and validates the resulting XCFramework. The source revision is selected by milestone, but binary equivalence to the consumed community XCFramework has not been proven. The script restricts generated files to `/Users/zhuwei/terminal/cylo-apple-build/`. Xcode/SDK compatibility may still prevent this source revision from building with a newer toolchain; the script has not been executed as part of current build verification.

## Build evidence and limits

The package tests and native macOS app build can run on a Mac with Xcode 16.2 and the pinned Swift package. The iOS app can be compiled directly against the iOS SDK with SwiftPM when Xcode does not offer an iOS platform destination. A successful compile does not verify microphone permissions, real-device audio routing, gateway credentials, network traversal, background/locked-device behavior, or live account-pool recovery. Those require an authorized configured gateway and physical-device acceptance.
