# Apple clients

Cylo ships a native SwiftUI application for macOS and a shared SwiftUI application for iPhone and iPad. Both clients use the server as the authority for tasks, permissions, events, receipts, and memory versions. Local snapshots are visibly marked stale or offline, and pending commands remain journaled until the server returns their durable receipt.

## WebRTC dependency

`Packages/CyloKit/Package.swift` pins the community `stasel/WebRTC` binary distribution at source revision `44f36788497f701bc8c0306de3a1c2511d56019d` (release M155 / tag 155.0.0). Its package manifest declares the `WebRTC-M155.xcframework.zip` artifact checksum `cb9adacf2d6bbf598d1d9e3e6c16d8fb97ca987b5b697746891828f9dc068ffe`. The upstream project states that its framework binaries are compiled from the official WebRTC source without modifications; this repository consumes that verified release artifact and does not reproduce the upstream source build.

The concrete adapter creates a microphone audio track, receives remote audio, negotiates the Space3044 `oai-events` DataChannel, exchanges non-trickle SDP through Cylo's authenticated voice-session API, and waits for ICE and DataChannel readiness before displaying an active session. Voice captions are drafts until the user confirms them. The client closes local media before requesting hangup; if upstream termination cannot be verified, the server reports and keeps the account pool quarantined.

## Build evidence and limits

The package tests and native macOS app build can run on a Mac with Xcode 16.2 and the pinned Swift package. The iOS app can be compiled directly against the iOS SDK with SwiftPM when Xcode does not offer an iOS platform destination. A successful compile does not verify microphone permissions, real-device audio routing, gateway credentials, network traversal, background/locked-device behavior, or live account-pool recovery. Those require an authorized configured gateway and physical-device acceptance.
