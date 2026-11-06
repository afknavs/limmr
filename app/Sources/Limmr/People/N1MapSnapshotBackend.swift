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
  /// Raster avatar fixtures exercise the authenticated backend + ImageIO thumbnail path.
  static func make() -> [Data] {
    (0..<12).compactMap { i in
      let image = NSImage(size: NSSize(width: 160, height: 160))
      image.lockFocus()
      let backgrounds: [UInt32] = [0xCEDDD7, 0xDBD6EB, 0xE4D6C9, 0xC9DBE9]
      func color(_ value: UInt32) -> NSColor {
        NSColor(calibratedRed: Double((value >> 16) & 255) / 255, green: Double((value >> 8) & 255) / 255, blue: Double(value & 255) / 255, alpha: 1)
      }
      color(backgrounds[i % 4]).setFill(); NSBezierPath(rect: NSRect(x: 0, y: 0, width: 160, height: 160)).fill()
      color([0x446678, 0x806181, 0x567B65][i % 3]).setFill()
      NSBezierPath(ovalIn: NSRect(x: 24, y: -36, width: 112, height: 104)).fill()
      color([0xE4B28E, 0xB77753, 0x8F583F, 0xF1C8A7][i % 4]).setFill()
      NSBezierPath(roundedRect: NSRect(x: 68, y: 48, width: 24, height: 35), xRadius: 10, yRadius: 10).fill()
      color([0x342D2A, 0x684D37, 0x252B32][i % 3]).setFill()
      NSBezierPath(ovalIn: NSRect(x: 41, y: 56, width: 78, height: 92)).fill()
      color([0xE4B28E, 0xB77753, 0x8F583F, 0xF1C8A7][i % 4]).setFill()
      NSBezierPath(ovalIn: NSRect(x: 48, y: 63, width: 64, height: 73)).fill()
      color([0x342D2A, 0x684D37, 0x252B32][i % 3]).setFill()
      NSBezierPath(ovalIn: NSRect(x: 45, y: 116, width: 72, height: 28)).fill()
      for x in [66, 93] { NSBezierPath(ovalIn: NSRect(x: x, y: 100, width: 4, height: 5)).fill() }
      NSColor(calibratedWhite: 0.25, alpha: 0.8).setStroke()
      let smile = NSBezierPath(); smile.move(to: NSPoint(x: 71, y: 83)); smile.curve(to: NSPoint(x: 91, y: 83), controlPoint1: NSPoint(x: 76, y: 77), controlPoint2: NSPoint(x: 86, y: 77)); smile.lineWidth = 2; smile.stroke()
      image.unlockFocus()
      guard let tiff = image.tiffRepresentation, let bitmap = NSBitmapImageRep(data: tiff) else { return nil }
      return bitmap.representation(using: .png, properties: [:])
    }
  }
}
