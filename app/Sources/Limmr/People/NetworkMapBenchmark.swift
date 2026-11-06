import AppKit
import SwiftUI
import LimmrKit

/// Explicit offline render benchmark. It measures CPU raster frames, not display/compositor FPS.
@MainActor enum NetworkMapBenchmark {
  static var completed = false
  static func run(scene: NetworkMapProjection, images: [String: Image], circles: [LimmrKit.Circle], size: CGSize, dark: Bool) {
    guard !completed, let index = ProcessInfo.processInfo.arguments.firstIndex(of: "--n1-map-benchmark"),
      ProcessInfo.processInfo.arguments.indices.contains(index + 1) else { return }
    completed = true
    var samples: [Double] = []
    var rendered = 0
    for frame in 0..<140 {
      let camera = NetworkMapCamera(viewport: size, world: scene.world, zoom: 1.1 + 0.5 * sin(Double(frame) / 20),
        pan: CGSize(width: sin(Double(frame) / 17) * 120, height: cos(Double(frame) / 19) * 60))
      let start = CACurrentMediaTime()
      let renderer = ImageRenderer(content: Canvas { context, _ in
        NetworkMapRenderer.draw(context: &context, scene: scene, camera: camera, images: images, circles: circles,
          dark: dark, active: nil, selected: nil)
      }.frame(width: size.width, height: size.height))
      renderer.scale = 2
      if renderer.cgImage != nil { rendered += 1 }
      if frame >= 20 { samples.append((CACurrentMediaTime() - start) * 1000) }
    }
    samples.sort()
    let lookupStart = CACurrentMediaTime()
    let preparedCount = NetworkMapLayoutCache.preparations
    let cached = NetworkMapLayoutCache.cached(scene.map)
    let lookupMS = (CACurrentMediaTime() - lookupStart) * 1000
    let values: [String: Any] = ["cached_reopen_ms": lookupMS, "cached_reopen_same_positions": cached?.positions == scene.positions, "layout_preparations": preparedCount, "people": scene.peopleCount, "cached_photos": images.count, "layout_preparation_ms": scene.preparationMilliseconds,
      "frames": samples.count, "successful_rasters": rendered, "scale": 2,
      "median_ms": samples[samples.count / 2], "p95_ms": samples[Int(Double(samples.count - 1) * 0.95)],
      "max_ms": samples.last!, "under_16_67_ms": samples.filter { $0 <= 16.67 }.count,
      "mode": "offscreen ImageRenderer, same Canvas renderer, scripted pan/zoom; excludes monitor/compositor and network"]
    let output = ProcessInfo.processInfo.arguments[index + 1]
    if let data = try? JSONSerialization.data(withJSONObject: values, options: [.prettyPrinted, .sortedKeys]) {
      try? data.write(to: URL(fileURLWithPath: output))
    }
  }
}
