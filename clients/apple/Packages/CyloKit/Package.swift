// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "CyloKit",
    platforms: [
        .macOS(.v14),
        .iOS(.v17)
    ],
    products: [
        .library(name: "CyloKit", targets: ["CyloKit"]),
        .library(name: "CyloUI", targets: ["CyloUI"])
    ],
    dependencies: [
        .package(url: "https://github.com/stasel/WebRTC.git", revision: "44f36788497f701bc8c0306de3a1c2511d56019d")
    ],
    targets: [
        .target(name: "CyloKit"),
        .target(name: "CyloUI", dependencies: ["CyloKit", .product(name: "WebRTC", package: "WebRTC")]),
        .testTarget(name: "CyloKitTests", dependencies: ["CyloKit"])
    ]
)
