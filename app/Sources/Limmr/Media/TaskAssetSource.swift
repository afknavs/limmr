import Foundation
import ImageIO
import LimmrKit

struct TaskAssetSource: Sendable {
  let backend: any Backend
  let taskID: String
  let item: WorkspaceItem
  var path: String { TaskClient.path(taskID) + "/items/" + item.id }
  struct Cached: Decodable, Sendable { var path: String; var thumbnail: Data? }
  func cached() async throws -> Cached { try await backend.get(path + "/preview") }
  func file(download: Bool) async throws -> URL? {
    let original: String
    let cached = try await cached() // Recheck visibility even when a thumbnail is on disk.
    if download && cached.path.isEmpty {
      struct Download: Decodable, Sendable { var path: String }
      original = try Wire.decode(Download.self, from: await backend.request(path + "/download", method: "POST", body: nil)).path
    } else { original = cached.path }
    guard !original.isEmpty else { return nil }
    return try Self.readableFile(path: original, taskID: taskID, item: item)
  }
  // Preserve a real extension so Quick Look can select its PDF/document provider.
  static func readableFile(path: String, taskID: String, item: WorkspaceItem) throws -> URL {
    let directory = FileManager.default.temporaryDirectory.appendingPathComponent("limmr-task-files")
      .appendingPathComponent(MediaPreviewCache.key(taskID + item.id), isDirectory: true)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
    var name = (item.title as NSString).lastPathComponent
    if name.isEmpty || name == "." || name == ".." { name = "Attachment" }
    if (name as NSString).pathExtension.isEmpty {
      name += ["image": ".jpg", "video": ".mp4", "document": ".pdf", "audio": ".m4a"][item.mediaType ?? ""] ?? ""
    }
    let output = directory.appendingPathComponent(name)
    if FileManager.default.fileExists(atPath: output.path) { return output }
    try FileManager.default.copyItem(at: URL(fileURLWithPath: path), to: output)
    try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: output.path)
    return output
  }
  func coverPreview() async -> URL? { await preview() }
  /// Recheck source visibility before consulting any thumbnail cache. Embedded first,
  /// then a sharper local image; never trigger a WhatsApp download for a tile.
  func progressivePreview(_ show: @MainActor @Sendable (URL) -> Void) async {
    guard let cached = try? await cached(), !Task.isCancelled else { return }
    if let data = cached.thumbnail,
       let url = await MediaPreviewCache.shared.embedded(data, key: taskID + item.id), !Task.isCancelled {
      _ = await MediaPreviewCache.shared.raster(url, pixels: MediaPreviewCache.coverPixels)
      if !Task.isCancelled { await show(url) }
    }
    guard !Task.isCancelled, !cached.path.isEmpty,
      let url = await MediaPreviewCache.shared.file(URL(fileURLWithPath: cached.path)), !Task.isCancelled else { return }
    _ = await MediaPreviewCache.shared.raster(url, pixels: MediaPreviewCache.coverPixels)
    if !Task.isCancelled { await show(url) }
  }
  func preview() async -> URL? {
    guard let cached = try? await cached(), !Task.isCancelled else { return nil }
    if !cached.path.isEmpty, let url = await MediaPreviewCache.shared.file(URL(fileURLWithPath: cached.path)) { return url }
    if let data = cached.thumbnail { return await MediaPreviewCache.shared.embedded(data, key: taskID + item.id) }
    return nil
  }
}
