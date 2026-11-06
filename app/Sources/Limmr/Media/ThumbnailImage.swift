import ImageIO
import SwiftUI
import Foundation

/// A view task owns each visible request; leaving the viewport cancels its result.
/// ImageIO decoding and disk I/O stay on MediaPreviewCache's actor, never in body.
struct ThumbnailImage: View {
  var url: URL? = nil
  var data: Data? = nil
  var identity = ""
  var pixels = MediaPreviewCache.coverPixels
  @State private var raster: PreviewRaster?
  init(url: URL? = nil, data: Data? = nil, identity: String = "", pixels: Int = MediaPreviewCache.coverPixels) {
    self.url = url; self.data = data; self.identity = identity; self.pixels = pixels
    _raster = State(initialValue: url.flatMap { ThumbnailDisplayMemory.get($0, pixels: pixels) })
  }
  private var requestID: String { (url?.path ?? identity) + ":\(pixels):\(data?.count ?? 0)" }
  var body: some View {
    Group {
      if let raster { Image(decorative: raster.image, scale: 2).resizable() }
      else { Color.clear }
    }.task(id: requestID) {
      let value: PreviewRaster?
      if let url { value = await MediaPreviewCache.shared.raster(url, pixels: pixels) }
      else if let data { value = await MediaPreviewCache.shared.raster(data, id: identity, pixels: pixels) }
      else { value = nil }
      guard !Task.isCancelled else { return }
      if let url, let value { ThumbnailDisplayMemory.put(value, url: url, pixels: pixels) }
      raster = value
    }
  }
}

struct PreviewRaster: @unchecked Sendable {
  let image: CGImage // Immutable, fully decoded by ImageIO before crossing actors.
}

/// Synchronous display lookup only; ImageIO and filesystem work stay on the actor.
@MainActor private enum ThumbnailDisplayMemory {
  final class Box { let value: PreviewRaster; init(_ value: PreviewRaster) { self.value = value } }
  static let values: NSCache<NSString, Box> = { let cache = NSCache<NSString, Box>(); cache.totalCostLimit = MediaPreviewCache.decodedByteLimit; return cache }()
  static func get(_ url: URL, pixels: Int) -> PreviewRaster? { values.object(forKey: (url.absoluteString + ":\(pixels)") as NSString)?.value }
  static func put(_ value: PreviewRaster, url: URL, pixels: Int) {
    values.setObject(Box(value), forKey: (url.absoluteString + ":\(pixels)") as NSString, cost: value.image.bytesPerRow * value.image.height)
  }
}
