// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "CyloKit",
    platforms: [
        .macOS(.v13),
        .iOS(.v16)
    ],
    products: [
        .library(name: "CyloKit", targets: ["CyloKit"])
    ],
    targets: [
        .target(name: "CyloKit"),
        .testTarget(name: "CyloKitTests", dependencies: ["CyloKit"])
    ]
)
