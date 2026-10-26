// swift-tools-version: 6.2
import PackageDescription

let package = Package(
    name: "Limmr",
    platforms: [.macOS(.v26)],
    products: [.library(name: "LimmrKit", targets: ["LimmrKit"]), .executable(name: "Limmr", targets: ["Limmr"])],
    targets: [
        .target(name: "LimmrKit"),
        .executableTarget(name: "Limmr", dependencies: ["LimmrKit"]),
        .testTarget(name: "LimmrKitTests", dependencies: ["LimmrKit"], exclude: ["Fixtures"]),
        .testTarget(name: "LimmrTests", dependencies: ["Limmr", "LimmrKit"])
    ]
)
