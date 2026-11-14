import AppKit
import LimmrKit

/// In-memory illustrated portrait images, deliberately invented; no filesystem or network source.
actor N1MapSnapshotBackend: Backend {
  let samples = DemoBackend()
  let dense: Bool
  let portraits: [Data]
  init(dense: Bool, portraits: [Data]) { self.dense = dense; self.portraits = portraits }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if method == "GET", path.hasPrefix("/v1/map") {
      let base: CircleMap = try await samples.get("/v1/map")
      return try Wire.data(dense ? DemoPeopleUnderstanding.network400(base) : Self.network(base))
    }
    if method == "GET", path.hasSuffix("/avatar"), let id = path.split(separator: "/").dropLast().last {
      let hash = MapSceneMotion.stableHash(String(id))
      guard hash % 5 != 0, !portraits.isEmpty else { throw BackendError.http(404) }
      return portraits[Int(hash % UInt64(portraits.count))]
    }
    return try await samples.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { AsyncThrowingStream { $0.finish() } }
  static func network(_ base: CircleMap) -> CircleMap {
    var map = base
    map.edges.append(CircleMapEdge(source: "person-04", target: "person-05", kind: "collaborates", state: "confirmed"))
    return map
  }
}

@MainActor enum N1MapPortraits {
  /// Fixtures carry no invented faces: a person without a real photo shows initials or the silhouette,
  /// exactly as in the app.
  static func make() -> [Data] { [] }
  /// A flat raster for tests of the authenticated backend + ImageIO thumbnail path.
  static func tile() -> Data? {
    let image = NSImage(size: NSSize(width: 160, height: 160))
    image.lockFocus()
    NSColor(calibratedRed: 0.86, green: 0.84, blue: 0.80, alpha: 1).setFill()
    NSBezierPath(rect: NSRect(x: 0, y: 0, width: 160, height: 160)).fill()
    image.unlockFocus()
    guard let tiff = image.tiffRepresentation, let bitmap = NSBitmapImageRep(data: tiff) else { return nil }
    return bitmap.representation(using: .png, properties: [:])
  }
}
