import AppKit
import Foundation
import LimmrKit
import Testing
@testable import Limmr

private actor N2LiveCoverBackend: Backend {
  let base = DemoBackend()
  var unpaired = false
  var revision = 1
  let thumbnail: Data
  var paths: [String] = []
  init(thumbnail: Data) { self.thumbnail = thumbnail }
  func unpairAndDownload() { unpaired = true; revision = 2 }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    paths.append(path)
    if path == "/v1/sync", unpaired {
      // Exact old-engine shape: cleared peer and a stale sending summary.
      return Data(#"{"paired":false,"paused":false,"peer":{},"lease":{"holder":"","this_mac":true},"items_in":0,"items_out":0,"merge_reviews":[],"sending_summary":"Sending is paused…"}"#.utf8)
    }
    if path == "/v1/agents" {
      var dot = DemoData.agents[1]
      dot.id = "dot-credential"; dot.name = "Your dot"; dot.kind = "custom"; dot.transport = "remote"
      dot.permissions = .defaults(remote: true); dot.permissions?.sync = !unpaired
      return try Wire.data(["agents": [DemoData.agents[0], dot]])
    }
    if path.hasPrefix("/v1/tasks?") {
      var card = TaskSummary(id: "cover-live", title: "Invented live cover")
      card.coverCandidate = .init(mediaId: "photo", chatJid: "15550000001@s.whatsapp.net", thumbnailUrl: "/v1/tasks/cover-live/cover/thumbnail?revision=\(revision)")
      return try Wire.data(TasksResponse(tasks: [card]))
    }
    if path.contains("/cover/thumbnail") {
      let pixels: Any = revision == 1 ? NSNull() : thumbnail.base64EncodedString()
      return try JSONSerialization.data(withJSONObject: ["path": "", "thumbnail": pixels])
    }
    return try await base.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { .init { $0.finish() } }
}

@Suite("N2 pixel covers and unpairing", .serialized) @MainActor struct N2CoversTests {
  @Test func unpairAndDownloadedCoverUpdateSameStoreWithoutRestart() async throws {
    let image = NSImage(size: .init(width: 32, height: 40), flipped: false) { rect in NSColor.gray.setFill(); rect.fill(); return true }
    let bitmap = try #require(NSBitmapImageRep(data: image.tiffRepresentation!))
    let data = try #require(bitmap.representation(using: .png, properties: [:]))
    let backend = N2LiveCoverBackend(thumbnail: data)
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refreshVisualState()
    let response: TasksResponse = try await backend.get("/v1/tasks?status=all&sort=recent&limit=10")
    store.allTasks = response.tasks; store.home.tasks = response.tasks
    #expect(AIIdentity.rows(store.agents, sync: store.connectionSync).contains { $0.id == "dot-credential" })
    #expect(store.taskCovers.cached(response.tasks[0], generation: store.backendGeneration) == nil)
    await backend.unpairAndDownload()
    await store.refreshVisualState()
    let status = try #require(store.connectionSync)
    #expect(!status.paired && status.peer.engineId.isEmpty && status.peer.agentId == nil)
    #expect(status.connectionTitle == "Not paired" && status.connectionAction == "Pair…")
    #expect(AIIdentity.rows(store.agents, sync: status).map(\.id) == [DemoData.agents[0].id])
    #expect(store.allTasks[0].coverCandidate?.thumbnailUrl.hasSuffix("revision=2") == true)
    #expect(store.home.tasks[0].coverCandidate == store.allTasks[0].coverCandidate)
    #expect(store.taskCovers.cached(store.allTasks[0], generation: store.backendGeneration) != nil)
    #expect(await backend.paths.allSatisfy { !$0.contains("/items") && $0 != "/v1/tasks/cover-live" })
    var ordinary = store.agents[1]; ordinary.id = "ordinary"; ordinary.name = "Research AI"
    #expect(AIIdentity.rows([ordinary], sync: status).map(\.id) == ["ordinary"])
  }
}
