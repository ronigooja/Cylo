// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "ZoeKit",
    defaultLocalization: "en",
    platforms: [
        .macOS(.v14),
        .iOS(.v17)
    ],
    products: [
        .library(name: "ZoeKit", targets: ["ZoeKit"]),
        .library(name: "ZoeUI", targets: ["ZoeUI"])
    ],
    dependencies: [
        .package(url: "https://github.com/stasel/WebRTC.git", revision: "44f36788497f701bc8c0306de3a1c2511d56019d")
    ],
    targets: [
        .target(name: "ZoeKit"),
        .target(name: "ZoeUI", dependencies: ["ZoeKit", .product(name: "WebRTC", package: "WebRTC")], resources: [.process("Resources")]),
        .testTarget(name: "ZoeKitTests", dependencies: ["ZoeKit", "ZoeUI"])
    ]
)
