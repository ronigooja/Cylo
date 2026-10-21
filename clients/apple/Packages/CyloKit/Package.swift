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
    targets: [
        .target(name: "CyloKit"),
        .target(name: "CyloUI", dependencies: ["CyloKit"]),
        .testTarget(name: "CyloKitTests", dependencies: ["CyloKit"])
    ]
)
