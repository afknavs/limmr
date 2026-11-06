import AppKit
import ImageIO
import Testing
import LimmrKit
@testable import Limmr

@Suite("N1 speed regressions", .serialized) @MainActor struct N1SpeedRegressionTests {
  @Test func thumbnailsAreDisplaySizedAndSurviveNewCacheInstance() async throws {
    let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: directory) }
    let image = NSImage(size: NSSize(width: 4000, height: 3000), flipped: false) { r in NSColor.systemBlue.setFill(); r.fill(); return true }
    let data = try #require(image.tiffRepresentation)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    let source = directory.appendingPathComponent("invented-poster.tiff")
    try data.write(to: source)
    let cache = MediaPreviewCache(root: directory)
    let start = ContinuousClock.now
    let first = try #require(await cache.raster(source, pixels: 320))
    #expect(first.image.width == 320 && first.image.height == 240)
    let cold = start.duration(to: .now)
    let warm = ContinuousClock.now
    for _ in 0..<40 { _ = await cache.raster(source, pixels: 320) }
    print("N1 thumbnail cold \(cold), 40 warm \(warm.duration(to: .now))")
    let restored = try #require(await MediaPreviewCache(root: directory).raster(source, pixels: 320))
    #expect(restored.image.width == 320)
    #expect((await cache.raster(source, pixels: 96))?.image.width == 96)
  }
  @Test func embeddedPreviewArrivesBeforeLocalSharpeningAndRechecksVisibility() async throws {
    let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }
    func pixels(_ width: Int, _ height: Int) throws -> Data {
      let image = NSImage(size: NSSize(width: width, height: height), flipped: false) { r in NSColor.systemBlue.setFill(); r.fill(); return true }
      return try #require(image.tiffRepresentation)
    }
    let local = directory.appendingPathComponent("invented-photo.tiff")
    try pixels(1200, 900).write(to: local)
    let backend = N1PreviewBackend(path: local.path, thumbnail: try pixels(64, 48))
    let item = WorkspaceItem(id: "invented-image-" + UUID().uuidString, kind: "file", title: "Workshop poster.jpg")
    let source = TaskAssetSource(backend: backend, taskID: "invented-task", item: item)
    let recorder = N1PreviewRecorder()
    await source.progressivePreview { recorder.urls.append($0) }
    #expect(recorder.urls.count == 2)
    if recorder.urls.count == 2 {
      #expect((await MediaPreviewCache.shared.raster(recorder.urls[0], pixels: 640))?.image.width == 64)
      #expect((await MediaPreviewCache.shared.raster(recorder.urls[1], pixels: 640))?.image.width == 640)
    }
    await backend.hide()
    #expect(await source.preview() == nil) // Cached pixels cannot bypass the engine gate.
  }
  @Test func concurrentReadsShareTransportAndLastCancellationReleasesIt() async throws {
    let backend = N1SlowBackend()
    let shared = CoalescingBackend(backend: backend)
    try await withThrowingTaskGroup(of: Data.self) { group in
      for _ in 0..<20 { group.addTask { try await shared.request("/v1/tasks/invented", method: "GET", body: nil) } }
      for try await _ in group {}
    }
    #expect(await backend.calls == 1)
    _ = try await shared.request("/v1/tasks/invented", method: "GET", body: nil)
    #expect(await backend.calls == 2) // A later read rechecks permissions, no TTL.
    let cancelled = Task { try await shared.request("/v1/tasks/offscreen", method: "GET", body: nil) }
    try await Task.sleep(for: .milliseconds(10)); cancelled.cancel()
    do { _ = try await cancelled.value; Issue.record("Offscreen result was applied") } catch {}
    #expect(await backend.cancelled == 1)
  }
  @Test func liveAppendSkipsEmptyReadsAndKeepsPagedRowsAndFilters() async throws {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    var row = try #require(store.activity.first)
    row.id = 9000; row.kind = "read"; row.tool = "list_messages"; row.count = 0; row.messageIds = []
    // SSEEvent's memberwise initializer is internal to LimmrKit; exercise the parser.
    func emit(_ row: Activity) throws {
      var parser = SSEParser()
      _ = parser.consume("event: activity")
      _ = parser.consume("data: " + String(decoding: try Wire.data(row), as: UTF8.self))
      let parsed = parser.consume("")
      store.handleActivityEvent(try #require(parsed))
    }
    let before = store.activity
    try emit(row); #expect(store.activity == before)
    row.count = 2; try emit(row); try emit(row)
    #expect(store.activity.filter { $0.id == 9000 }.count == 1)
    #expect(store.activity.count == before.count + 1)
    store.activityQuery = "agent=other&quiet=true"; row.id = 9001
    try emit(row); #expect(!store.activity.contains { $0.id == 9001 })
    #expect(ActivityPresentation.detailed([row, row]).count == 1)
  }
}

private actor N1SlowBackend: Backend {
  var calls = 0
  var cancelled = 0
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    calls += 1
    do { try await Task.sleep(for: .milliseconds(80)) } catch { cancelled += 1; throw error }
    return Data("{}".utf8)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { AsyncThrowingStream { $0.finish() } }
}

@MainActor private final class N1PreviewRecorder { var urls: [URL] = [] }
private actor N1PreviewBackend: Backend {
  let path: String
  let thumbnail: Data
  var hidden = false
  init(path: String, thumbnail: Data) { self.path = path; self.thumbnail = thumbnail }
  func hide() { hidden = true }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    guard !hidden, method == "GET", path.hasSuffix("/preview") else { throw BackendError.http(404) }
    return try JSONSerialization.data(withJSONObject: ["path": self.path, "thumbnail": thumbnail.base64EncodedString()])
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { AsyncThrowingStream { $0.finish() } }
}
