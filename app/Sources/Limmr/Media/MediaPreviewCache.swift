import Foundation
import CryptoKit
import ImageIO
import QuickLookThumbnailing
import AVFoundation
import os

struct LinkPreview: Codable, Sendable {
  var title: String
  var image: URL?
  var icon: URL?
}

private final class PreviewRedirects: NSObject, URLSessionTaskDelegate, Sendable {
  func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
                  newRequest request: URLRequest, completionHandler: @escaping (URLRequest?) -> Void) {
    guard let url = request.url, MediaPreviewCache.safeRemote(url), task.countOfBytesReceived < 4_194_304 else { completionHandler(nil); return }
    var clean = request; clean.setValue(nil, forHTTPHeaderField: "Cookie"); clean.setValue(nil, forHTTPHeaderField: "Referer")
    completionHandler(clean)
  }
}

/// Shared by task assets, Home covers and People. Only small raster thumbnails
/// enter the cache. Original files stay lazy and keep the engine's privacy gate.
actor MediaPreviewCache {
  static let shared = MediaPreviewCache()
  nonisolated static let coverPixels = 1280
  nonisolated static let decodedByteLimit = 64 * 1024 * 1024
  private let root: URL
  private let session: URLSession
  private var rasters: [String: PreviewRaster] = [:]
  private var rasterOrder: [String] = []
  private var rasterBytes = 0
  private struct PendingFile { let task: Task<URL?, Never>; var waiters: Set<UUID> }
  private var files: [String: PendingFile] = [:]
  private var links: [String: Task<LinkPreview, Never>] = [:]
  init(root: URL? = nil) {
    let home = ProcessInfo.processInfo.environment["LIMMR_HOME"].map { URL(fileURLWithPath: $0) }
      ?? FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("Library/Application Support/Limmr")
    self.root = root ?? home.appendingPathComponent("previews", isDirectory: true)
    let config = URLSessionConfiguration.ephemeral
    config.httpShouldSetCookies = false; config.httpCookieStorage = nil; config.urlCredentialStorage = nil
    config.urlCache = nil; config.timeoutIntervalForRequest = 5; config.timeoutIntervalForResource = 8
    config.httpAdditionalHeaders = ["User-Agent": "Limmr-Preview/1.0", "Accept": "text/html,image/*;q=0.8"]
    session = URLSession(configuration: config, delegate: PreviewRedirects(), delegateQueue: nil)
  }
  nonisolated static func key(_ value: String) -> String { SHA256.hash(data: Data(value.utf8)).map { String(format: "%02x", $0) }.joined() }
  private nonisolated static func dataKey(_ value: Data) -> String {
    SHA256.hash(data: value).map { String(format: "%02x", $0) }.joined()
  }
  nonisolated static func safeRemote(_ url: URL) -> Bool {
    guard ["http", "https"].contains(url.scheme?.lowercased() ?? ""), let host = url.host?.lowercased(), url.user == nil, url.password == nil else { return false }
    // Never let a chat link probe loopback, local devices or cloud metadata.
    return host.contains(".") && host != "localhost" && !host.hasSuffix(".local") && !host.hasSuffix(".localhost")
      && !host.contains(":") && host.range(of: #"^\d+\.\d+\.\d+\.\d+$"#, options: .regularExpression) == nil
  }
  private func output(_ key: String, ext: String = "png") -> URL {
    try? FileManager.default.createDirectory(at: root, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
    return root.appendingPathComponent("v2-1280-" + key).appendingPathExtension(ext)
  }
  func embedded(_ data: Data, key: String) -> URL? {
    guard !data.isEmpty, data.count <= 262_144 else { return nil }
    let target = output(Self.key("embedded:" + key + ":" + Self.dataKey(data)))
    if FileManager.default.fileExists(atPath: target.path) { return target }
    guard let source = CGImageSourceCreateWithData(data as CFData, nil), let image = Self.smallImage(source) else { return nil }
    return Self.write(image, to: target) ? target : nil
  }
  func file(_ url: URL) async -> URL? {
    guard !Task.isCancelled else { return nil }
    let key = url.path, waiter = UUID()
    if files[key] != nil { files[key]?.waiters.insert(waiter) }
    else { files[key] = PendingFile(task: Task { await self.makeFile(url) }, waiters: [waiter]) }
    let work = files[key]!.task
    return await withTaskCancellationHandler {
      defer { releaseFile(key, waiter: waiter) }
      let value = await work.value
      return Task.isCancelled ? nil : value
    } onCancel: { Task { await self.releaseFile(key, waiter: waiter) } }
  }
  private func releaseFile(_ key: String, waiter: UUID) {
    guard files[key]?.waiters.remove(waiter) != nil else { return }
    if files[key]?.waiters.isEmpty == true { files.removeValue(forKey: key)?.task.cancel() }
  }
  private func makeFile(_ url: URL) async -> URL? {
    guard url.isFileURL else { return nil }
    let stamp = (try? url.resourceValues(forKeys: [.contentModificationDateKey, .fileSizeKey]))
    let target = output(Self.key(url.path + ":\(stamp?.contentModificationDate?.timeIntervalSince1970 ?? 0):\(stamp?.fileSize ?? 0)"))
    if FileManager.default.fileExists(atPath: target.path) { return target }
    if let source = CGImageSourceCreateWithURL(url as CFURL, nil), let image = Self.smallImage(source) {
      return Self.write(image, to: target) ? target : nil
    }
    let ext = url.pathExtension.lowercased()
    if ["mp4", "mov", "m4v", "webm"].contains(ext) {
      let generator = AVAssetImageGenerator(asset: AVURLAsset(url: url)); generator.appliesPreferredTrackTransform = true
      generator.maximumSize = CGSize(width: 640, height: 640)
      if let frame = try? await generator.image(at: .zero).image { return Self.write(frame, to: target) ? target : nil }
    }
    guard let raster = await Self.quickLook(url), !Task.isCancelled else { return nil }
    return Self.write(raster.image, to: target) ? target : nil
  }
  private nonisolated static func quickLook(_ url: URL) async -> PreviewRaster? {
    let request = PreviewQLRequest(url)
    return await withTaskCancellationHandler {
      guard let representation = try? await QLThumbnailGenerator.shared.generateBestRepresentation(for: request.value) else { return nil }
      return PreviewRaster(image: representation.cgImage)
    } onCancel: { QLThumbnailGenerator.shared.cancel(request.value) }
  }
  private nonisolated static func smallImage(_ source: CGImageSource) -> CGImage? {
    CGImageSourceCreateThumbnailAtIndex(source, 0, [kCGImageSourceCreateThumbnailFromImageAlways: true,
      kCGImageSourceCreateThumbnailWithTransform: true, kCGImageSourceShouldCacheImmediately: true, kCGImageSourceThumbnailMaxPixelSize: coverPixels] as CFDictionary)
  }
  private nonisolated static func write(_ image: CGImage, to url: URL) -> Bool {
    let opaque = [.none, .noneSkipFirst, .noneSkipLast].contains(image.alphaInfo)
    let temp = url.deletingLastPathComponent().appendingPathComponent(UUID().uuidString + (opaque ? ".jpg" : ".png"))
    defer { try? FileManager.default.removeItem(at: temp) }
    // Cache URLs are opaque identifiers. ImageIO and NSImage sniff the encoded
    // content; Quick Look receives the original file with its real extension.
    guard let destination = CGImageDestinationCreateWithURL(temp as CFURL, (opaque ? "public.jpeg" : "public.png") as CFString, 1, nil) else { return false }
    CGImageDestinationAddImage(destination, image, opaque ? [kCGImageDestinationLossyCompressionQuality: 0.9] as CFDictionary : nil)
    guard CGImageDestinationFinalize(destination) else { return false }
    do { try FileManager.default.moveItem(at: temp, to: url); try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path); return true }
    catch { try? FileManager.default.removeItem(at: temp); return FileManager.default.fileExists(atPath: url.path) }
  }
  /// Display-size decoded memory cache (64 MiB) backed by small private disk rasters.
  /// Source stamps/content hashes prevent a replacement image reusing stale pixels.
  func raster(_ url: URL, pixels: Int) -> PreviewRaster? {
    guard url.isFileURL, !Task.isCancelled else { return nil }
    let stamp = try? url.resourceValues(forKeys: [.contentModificationDateKey, .fileSizeKey])
    let id = "file:" + url.path + ":\(stamp?.contentModificationDate?.timeIntervalSince1970 ?? 0):\(stamp?.fileSize ?? 0)"
    return decoded(id: id, pixels: pixels) { CGImageSourceCreateWithURL(url as CFURL, [kCGImageSourceShouldCache: false] as CFDictionary) }
  }
  func raster(_ data: Data, id: String, pixels: Int) -> PreviewRaster? {
    guard !data.isEmpty, data.count <= 64 * 1024 * 1024, !Task.isCancelled else { return nil }
    return decoded(id: id + ":" + Self.dataKey(data), pixels: pixels) {
      CGImageSourceCreateWithData(data as CFData, [kCGImageSourceShouldCache: false] as CFDictionary)
    }
  }
  private func decoded(id: String, pixels: Int, source: () -> CGImageSource?) -> PreviewRaster? {
    let size = min(Self.coverPixels, max(16, pixels))
    let key = Self.key(id + ":\(size)")
    if let cached = rasters[key] { return cached }
    let target = output("raster-" + key)
    let disk = CGImageSourceCreateWithURL(target as CFURL, [kCGImageSourceShouldCache: false] as CFDictionary)
    guard let input = disk ?? source() else { return nil }
    os_signpost(.begin, log: SpeedTrace.log, name: "Thumbnail decode")
    defer { os_signpost(.end, log: SpeedTrace.log, name: "Thumbnail decode") }
    guard let image = CGImageSourceCreateThumbnailAtIndex(input, 0, [
      kCGImageSourceCreateThumbnailFromImageAlways: true, kCGImageSourceCreateThumbnailWithTransform: true,
      kCGImageSourceShouldCacheImmediately: true, kCGImageSourceThumbnailMaxPixelSize: size] as CFDictionary) else { return nil }
    if disk == nil { _ = Self.write(image, to: target) }
    let value = PreviewRaster(image: image)
    let cost = image.bytesPerRow * image.height
    while rasterBytes + cost > Self.decodedByteLimit, !rasterOrder.isEmpty {
      let oldest = rasterOrder.removeFirst()
      if let old = rasters.removeValue(forKey: oldest) { rasterBytes -= old.image.bytesPerRow * old.image.height }
    }
    rasters[key] = value; rasterOrder.append(key); rasterBytes += cost
    return value
  }
  func link(_ url: URL) async -> LinkPreview {
    let key = Self.key(url.absoluteString)
    let record = output(key, ext: "json")
    if let data = try? Data(contentsOf: record), let cached = try? JSONDecoder().decode(LinkPreview.self, from: data) { return cached }
    if let work = links[key] { return await work.value }
    let work = Task { await self.fetchLink(url, key: key) }; links[key] = work
    let value = await work.value; links[key] = nil
    if let data = try? JSONEncoder().encode(value) { try? data.write(to: record, options: .atomic) }
    return value
  }
  private func bytes(_ url: URL, cap: Int, image: Bool) async -> Data? {
    guard Self.safeRemote(url) else { return nil }
    do {
      let (stream, response) = try await session.bytes(from: url)
      defer { stream.task.cancel() }
      guard let http = response as? HTTPURLResponse, (200..<300).contains(http.statusCode), response.expectedContentLength <= cap,
        let final = response.url, Self.safeRemote(final),
        image ? (response.mimeType?.hasPrefix("image/") ?? false) : ["text/html", "application/xhtml+xml"].contains(response.mimeType ?? "") else { return nil }
      var data = Data(); for try await byte in stream { if data.count >= cap { return nil }; data.append(byte) }
      return data
    } catch { return nil }
  }
  private func remoteImage(_ raw: String?, base: URL, key: String) async -> URL? {
    guard let raw, let url = URL(string: Self.unescape(raw), relativeTo: base)?.absoluteURL,
      let data = await bytes(url, cap: 4_194_304, image: true), let source = CGImageSourceCreateWithData(data as CFData, nil),
      let image = Self.smallImage(source) else { return nil }
    let target = output(key); return Self.write(image, to: target) ? target : nil
  }
  private func fetchLink(_ url: URL, key: String) async -> LinkPreview {
    var value = LinkPreview(title: url.host ?? "Link")
    guard let data = await bytes(url, cap: 524_288, image: false), let html = String(data: data, encoding: .utf8) else { return value }
    let metadata = Self.metadata(html)
    value.title = String(Self.unescape(metadata["og:title"] ?? metadata["title"] ?? value.title).prefix(160))
    value.image = await remoteImage(metadata["og:image"], base: url, key: key + "-image")
    value.icon = await remoteImage(metadata["icon"] ?? "/favicon.ico", base: url, key: key + "-icon")
    return value
  }
  nonisolated static func metadata(_ html: String) -> [String: String] {
    var result: [String: String] = [:]
    let tags = (try? NSRegularExpression(pattern: #"<(?:meta|link)\b[^>]{0,4096}>"#, options: .caseInsensitive))?.matches(in: html, range: NSRange(html.startIndex..., in: html)) ?? []
    let attrs = try? NSRegularExpression(pattern: #"([\w:-]+)\s*=\s*["']([^"']*)["']"#)
    for match in tags {
      guard let r = Range(match.range, in: html) else { continue }; let tag = String(html[r]); var attributes: [String: String] = [:]
      for a in attrs?.matches(in: tag, range: NSRange(tag.startIndex..., in: tag)) ?? [] {
        if let k = Range(a.range(at: 1), in: tag), let v = Range(a.range(at: 2), in: tag) { attributes[String(tag[k]).lowercased()] = String(tag[v]) }
      }
      if let key = attributes["property"] ?? attributes["name"], let value = attributes["content"] { result[key.lowercased()] = value }
      if (attributes["rel"] ?? "").lowercased().contains("icon") { result["icon"] = attributes["href"] }
    }
    if let regex = try? NSRegularExpression(pattern: #"<title[^>]*>([^<]{0,500})</title>"#, options: .caseInsensitive),
       let match = regex.firstMatch(in: html, range: NSRange(html.startIndex..., in: html)), let r = Range(match.range(at: 1), in: html) { result["title"] = String(html[r]) }
    return result
  }
  nonisolated static func unescape(_ text: String) -> String { text.replacingOccurrences(of: "&amp;", with: "&").replacingOccurrences(of: "&quot;", with: "\"").replacingOccurrences(of: "&#39;", with: "'") }
}

// Quick Look requests are immutable after construction; its cancellation API is
// explicitly designed to be called while generation is in flight.
private final class PreviewQLRequest: @unchecked Sendable {
  let value: QLThumbnailGenerator.Request
  init(_ url: URL) {
    value = QLThumbnailGenerator.Request(fileAt: url, size: CGSize(width: 640, height: 480), scale: 1, representationTypes: .thumbnail)
  }
}
