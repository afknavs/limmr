import AppKit
import Foundation
import LimmrKit
import Testing
@testable import Limmr

@Suite("N1 identity", .serialized) @MainActor struct N1LookTests {
  @Test func paletteIsStableAndOwnerChoiceRoundTrips() async throws {
    let backend = DemoBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    let circle = try #require(store.circles.first)
    #expect(CircleHue.stable(id: circle.id, icon: nil) == CircleHue.stable(id: circle.id, icon: nil))
    await store.setCircleHue(circle, hue: .purple)
    #expect(store.circles.first { $0.id == circle.id }?.hue == .purple)
    _ = await store.setIcon(bucket: "circles", id: circle.id, icon: .init(kind: "symbol", value: "book"))
    let saved = try #require(store.circles.first { $0.id == circle.id }?.icon)
    #expect(saved.hue == "purple" && saved.value == "book")
    let read = try Wire.decode(EntityIcon.self, from: Wire.data(saved))
    #expect(read == saved)
  }
  @Test func presenceBoundaryAndDotCredentialMerge() throws {
    var sync = DotSyncStatus.demo("healthy")
    var ai = DemoData.agents[0]
    ai.name = "Pip"; ai.runsOnPeer = sync.peer.engineId
    var credential = DemoData.agents[1]
    sync.peer.agentId = credential.id
    credential.name = "Your dot"
    #expect(AIIdentity.rows([ai, credential], sync: sync).map(\.id) == [ai.id])
    #expect(AIIdentity.name(ai, sync: sync) == "Dot · Pip")
    let now = Timestamp.date(DemoData.now)!
    ai.lastSeenAt = now.addingTimeInterval(-900).ISO8601Format()
    #expect(AIIdentity.isPresent(ai, now: now))
    ai.lastSeenAt = now.addingTimeInterval(-901).ISO8601Format()
    #expect(!AIIdentity.isPresent(ai, now: now))
    ai.enabled = false; ai.lastSeenAt = DemoData.now
    #expect(!AIIdentity.isPresent(ai, now: now))
    sync.paired = false
    #expect(AIIdentity.rows([ai, credential], sync: sync).count == 2)
    #expect(AIIdentity.name(ai, sync: sync) == "Pip")
  }
  @Test func ownerLinkPersistsThroughRefresh() async throws {
    let store = AppStore(demo: true, snapshot: true, backend: DemoBackend(dotState: "healthy"))
    await store.refresh()
    let agent = try #require(store.agents.first)
    await store.linkAIToDot(agent, peer: "demo-dot")
    #expect(store.agents.first { $0.id == agent.id }?.runsOnPeer == "demo-dot")
    await store.linkAIToDot(agent, peer: "")
    #expect(store.agents.first { $0.id == agent.id }?.runsOnPeer == nil)
  }
  @Test func overviewNeedsYouFirstAndCanonicalCircleMatchesEverySurface() async throws {
    let store = AppStore(demo: true, snapshot: true, backend: DemoBackend())
    await store.refresh(); await store.refreshAllTasks()
    store.home.recentLines = [.init(text: "Codex added a practice set", targetId: "tsk_study", at: DemoData.now, by: "Codex")]
    let rows = store.overviewRows
    #expect(rows.first?.needsYou == true)
    if let firstMoved = rows.firstIndex(where: { !$0.needsYou }) { #expect(rows.dropFirst(firstMoved).allSatisfy { !$0.needsYou }) }
    for task in store.allTasks {
      let circles = store.taskCircles(task)
      #expect(circles.map(\.id) == store.circles.filter { TaskListPresentation.inCircle([task], id: $0.id).count == 1 }.map(\.id))
      if !circles.isEmpty { #expect(store.taskCircleName(task) != "No circle") }
    }
    var stale = TaskSummary(id: "stale", title: "Archived circle task")
    stale.circleIds = ["not-in-index"]
    #expect(store.taskCircleName(stale) == "Circle")
    #expect(!TaskListPresentation.noCircle([stale]).contains { $0.id == stale.id })
    let syllabus = try #require(store.allTasks.first { $0.id == "tsk_syllabus" })
    #expect(syllabus.circleIds == ["university"])
    #expect(store.taskCircleName(syllabus) == "University")
  }
  @Test func activityIsExactlyTwelveWeeksAndExcludesFutureDays() {
    let now = Timestamp.date("2026-11-01T18:00:00Z")!
    let history = StatsHistory(days: [
      HistoryDay(date: "2026-11-01", messagesIn: 10),
      HistoryDay(date: "2026-11-02", messagesIn: 100),
      HistoryDay(date: "2025-01-01", messagesIn: 100)
    ])
    let days = N1ActivityWindow.days(history: history, now: now)
    #expect(days.count == 84 && Set(days.map(\.id)).count == 84)
    #expect(days.last?.date == "2026-11-01")
    #expect(days.reduce(0) { $0 + $1.messages } == 10)
    #expect(Set((1...100).map { N1ActivityWindow.opacity($0, maximum: 100) }) == Set([0.25, 0.5, 0.75, 1]))
  }
  @Test func tableSortsByTitleProgressCircleAndDate() {
    var a = TaskSummary(id: "a", title: "Zebra", progress: .init(done: 1, total: 4))
    var b = TaskSummary(id: "b", title: "Apple", progress: .init(done: 3, total: 4))
    a.lastActiveAt = "2026-10-31T00:00:00Z"; b.lastActiveAt = "2026-10-30T00:00:00Z"
    let rows = [N1TaskTableRecord(task: a, circle: "A"), N1TaskTableRecord(task: b, circle: "Z")]
    #expect(rows.sorted(using: KeyPathComparator(\.title)).map(\.id) == ["b", "a"])
    #expect(rows.sorted(using: KeyPathComparator(\.progress)).map(\.id) == ["a", "b"])
    #expect(rows.sorted(using: KeyPathComparator(\.circle)).map(\.id) == ["a", "b"])
    #expect(rows.sorted(using: KeyPathComparator(\.moved, order: .reverse)).map(\.id) == ["a", "b"])
  }
  @Test func cachedThumbnailWorksWithoutAnOriginalOrDownload() async throws {
    let image = NSImage(size: .init(width: 32, height: 40), flipped: false) { rect in
      NSColor.gray.setFill(); rect.fill(); return true
    }
    let bitmap = try #require(NSBitmapImageRep(data: image.tiffRepresentation!))
    let data = try #require(bitmap.representation(using: .png, properties: [:]))
    let backend = N1ThumbnailBackend(thumbnail: data)
    var task = TaskSummary(id: "thumbnail-test-" + UUID().uuidString, title: "Invented task")
    task.cover = WorkspaceItem(id: "cover", kind: "file", title: "cover.png", addedBy: "You", addedAt: DemoData.now)
    let preview = await N1TaskPreview.load(backend: backend, task: task)
    #expect(preview != nil)
    #expect(await backend.paths == ["/v1/tasks/" + task.id + "/items/cover/preview"])
  }
  @Test func olderHomePayloadKeepsStringCompatibility() throws {
    let old = try Wire.decode(HomeResponse.self, from: Data(#"{"recently":["1 task changed."],"today":[],"suggestions":[],"proposals":[],"integrations":[]}"#.utf8))
    #expect(old.recentLines.isEmpty && old.recently == ["1 task changed."])
  }
}

private actor N1ThumbnailBackend: Backend {
  let thumbnail: Data
  var paths: [String] = []
  init(thumbnail: Data) { self.thumbnail = thumbnail }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    paths.append(path)
    struct Preview: Encodable { let path: String; let thumbnail: Data }
    return try Wire.data(Preview(path: "/does-not-exist/invented-original.png", thumbnail: thumbnail))
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { .init { $0.finish() } }
}
