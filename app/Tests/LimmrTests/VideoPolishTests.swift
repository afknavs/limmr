import AppKit
import ImageIO
import LimmrKit
import Testing
@testable import Limmr

private actor VideoPreviewBackend: Backend {
  let path: String
  let thumbnail: Data
  var requests: [String] = []
  init(path: String, thumbnail: Data) { self.path = path; self.thumbnail = thumbnail }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    requests.append(path)
    guard method == "GET", path.contains("/preview") || path.contains("/cover/thumbnail") else { throw BackendError.http(404) }
    return try JSONSerialization.data(withJSONObject: ["path": self.path, "thumbnail": thumbnail.base64EncodedString()])
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { .init { $0.finish() } }
}

@Suite("Video polish", .serialized) @MainActor struct VideoPolishTests {
  private func image(_ width: Int, _ height: Int, alpha: Bool = false) throws -> CGImage {
    let context = try #require(CGContext(data: nil, width: width, height: height, bitsPerComponent: 8, bytesPerRow: 0,
      space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: (alpha ? CGImageAlphaInfo.premultipliedLast : .noneSkipLast).rawValue))
    context.setFillColor(CGColor(red: 0.2, green: 0.5, blue: 0.8, alpha: alpha ? 0.35 : 1))
    context.fill(CGRect(x: 0, y: 0, width: width, height: height))
    return try #require(context.makeImage())
  }
  private func encoded(_ image: CGImage, type: String = "public.jpeg") throws -> Data {
    let data = NSMutableData()
    let output = try #require(CGImageDestinationCreateWithData(data, type as CFString, 1, nil))
    CGImageDestinationAddImage(output, image, nil)
    #expect(CGImageDestinationFinalize(output))
    return data as Data
  }
  private func dimensions(_ url: URL) throws -> (Int, Int) {
    let source = try #require(CGImageSourceCreateWithURL(url as CFURL, nil))
    let image = try #require(CGImageSourceCreateImageAtIndex(source, 0, nil))
    return (image.width, image.height)
  }
  private func directory() throws -> URL {
    let url = FileManager.default.temporaryDirectory.appendingPathComponent("video-polish-" + UUID().uuidString)
    try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
    return url
  }

  @Test func coverPipelineUses1280AndIgnoresOldDiskCaches() async throws {
    let root = try directory(); defer { try? FileManager.default.removeItem(at: root) }
    let original = root.appendingPathComponent("invented-photo.jpg")
    try encoded(image(2560, 1280)).write(to: original)
    let stamp = try original.resourceValues(forKeys: [.contentModificationDateKey, .fileSizeKey])
    let oldKey = MediaPreviewCache.key(original.path + ":\(stamp.contentModificationDate?.timeIntervalSince1970 ?? 0):\(stamp.fileSize ?? 0)")
    try encoded(image(640, 320), type: "public.png").write(to: root.appendingPathComponent(oldKey + ".png"))
    let cache = MediaPreviewCache(root: root)
    let preview = try #require(await cache.file(original))
    #expect(try dimensions(preview) == (1280, 640))
    #expect(preview.lastPathComponent.hasPrefix("v2-1280-"))
    let raster = try #require(await cache.raster(preview, pixels: MediaPreviewCache.coverPixels))
    #expect(raster.image.width == 1280 && raster.image.height == 640)
    let restored = try #require(await MediaPreviewCache(root: root).raster(preview, pixels: MediaPreviewCache.coverPixels))
    #expect(restored.image.width == 1280)
    for pixels in [48, 96] { #expect((await cache.raster(preview, pixels: pixels))?.image.width == pixels) }
    #expect(ThumbnailImage().pixels == 1280)
    #expect(MediaPreviewCache.decodedByteLimit == 64 * 1024 * 1024)
    let small = root.appendingPathComponent("small.jpg")
    try encoded(image(80, 100)).write(to: small)
    #expect(try dimensions(#require(await cache.file(small))) == (80, 100))
  }

  @Test func diskPreviewsAndRastersUseJPEGForPhotosAndPNGForAlpha() async throws {
    for alpha in [false, true] {
      let root = try directory(); defer { try? FileManager.default.removeItem(at: root) }
      let original = root.appendingPathComponent(alpha ? "transparent.png" : "photo.jpg")
      try encoded(image(1600, 1200, alpha: alpha), type: alpha ? "public.png" : "public.jpeg").write(to: original)
      let cache = MediaPreviewCache(root: root)
      let preview = try #require(await cache.file(original))
      #expect(await cache.raster(preview, pixels: MediaPreviewCache.coverPixels) != nil)
      let disk = try FileManager.default.contentsOfDirectory(at: root, includingPropertiesForKeys: nil)
        .filter { $0.lastPathComponent.hasPrefix("v2-1280-") }
      #expect(disk.count == 2)
      for url in disk {
        let source = try #require(CGImageSourceCreateWithURL(url as CFURL, nil))
        #expect(CGImageSourceGetType(source) as String? == (alpha ? "public.png" : "public.jpeg"))
        #expect(NSImage(contentsOf: url) != nil) // Content sniffing works despite the opaque .png cache suffix.
        if alpha {
          let decoded = try #require(CGImageSourceCreateImageAtIndex(source, 0, nil))
          #expect([CGImageAlphaInfo.premultipliedLast, .premultipliedFirst, .last, .first].contains(decoded.alphaInfo))
        }
      }
    }
  }

  @Test func bothPreviewRoutesPreferFileAndFallBackOnlyWithoutFile() async throws {
    let root = try directory(); defer { try? FileManager.default.removeItem(at: root) }
    let original = root.appendingPathComponent("invented-full.jpg")
    try encoded(image(2400, 1600)).write(to: original)
    let tiny = try encoded(image(96, 64))
    let item = WorkspaceItem(id: UUID().uuidString, kind: "file", title: "Invented photo.jpg")
    for hasFile in [true, false] {
      let backend = VideoPreviewBackend(path: hasFile ? original.path : "", thumbnail: tiny)
      let source = TaskAssetSource(backend: backend, taskID: "invented-task", item: item)
      let asset = try #require(await source.preview())
      #expect(try dimensions(asset).0 == (hasFile ? 1280 : 96))
      var task = TaskSummary(id: "invented-task", title: "Invented task")
      task.coverCandidate = .init(thumbnailUrl: "/v1/tasks/invented-task/cover/thumbnail?revision=1")
      let cover = try #require(await N1TaskPreview.load(backend: backend, task: task))
      #expect(try dimensions(cover).0 == (hasFile ? 1280 : 96))
      let store = AppStore(demo: true, snapshot: true, backend: backend)
      let display = try #require(await store.taskCovers.load(task, backend: backend, generation: 0))
      #expect(display.raster.image.width == (hasFile ? 1280 : 96))
      #expect(await backend.requests.allSatisfy { !$0.contains("download") })
    }
  }

  @Test func tenLargeCoversRemainDecoded() async throws {
    let root = try directory(); defer { try? FileManager.default.removeItem(at: root) }
    let original = root.appendingPathComponent("invented-square.jpg")
    try encoded(image(1280, 1280)).write(to: original)
    let backend = VideoPreviewBackend(path: original.path, thumbnail: Data())
    let cache = TaskCoverCache()
    var tasks: [TaskSummary] = []
    for index in 0..<10 {
      var task = TaskSummary(id: "invented-\(index)", title: "Invented task \(index)")
      task.coverCandidate = .init(thumbnailUrl: "/v1/tasks/\(task.id)/cover/thumbnail")
      tasks.append(task)
      #expect(await cache.load(task, backend: backend, generation: 0) != nil)
    }
    #expect(tasks.allSatisfy { cache.cached($0, generation: 0)?.raster.image.width == 1280 })
  }

  @Test func mapAvatarIsSharpAtTwoTimesDisplayScale() throws {
    let avatar = try #require(NetworkAvatarThumbnail.decode(encoded(image(640, 640))))
    #expect(avatar.width == 96 && avatar.height == 96)
  }
}
