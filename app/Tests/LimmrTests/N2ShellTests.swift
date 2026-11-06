import AppKit
import SwiftUI
import Testing
import LimmrKit
@testable import Limmr

@Suite("N2 shell", .serialized) @MainActor struct N2ShellTests {
  @Test func retainedOldHistoryStillRendersCalendar() {
    let store = AppStore(demo: true, snapshot: true)
    store.history = StatsHistory(days: [HistoryDay(date: "2020-01-01", messagesIn: 4)])
    #expect(store.homeActivityHistory != nil)
  }
  @Test func everySidebarRowAndBack() async {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    for page in Page.allCases {
      store.selectSidebar("page:" + page.rawValue)
      #expect(store.page == page)
      #expect(store.sidebarDestination == "page:" + page.rawValue)
      #expect(store.circlePath.isEmpty && store.activityPath.isEmpty)
    }
    for circle in store.circles {
      store.selectSidebar("page:Home")
      let stale = store.navigationPath
      store.selectSidebar("circle:" + circle.id)
      stale.wrappedValue = []
      // Simulate the new stack's initial empty path write before its task.
      store.navigationPath.wrappedValue = []
      #expect(store.sidebarDestination == "circle:" + circle.id)
      await store.sidebarCircleDidAppear()
      #expect(store.navigationPath.wrappedValue == [.people(.circle(circle.id))])
      #expect(store.sidebarDestination == "circle:" + circle.id)
      #expect(store.peopleBackTitle == "People")
      store.openTask("tsk_study"); await store.activityDidAppear()
      store.route(.back)
      #expect(store.circlePath == [.circle(circle.id)])
      store.route(.back)
      #expect(store.page == .circles && store.circlePath.isEmpty)
      #expect(store.sidebarDestination == "page:People")
    }
    for agent in AIIdentity.rows(store.agents, sync: store.connectionSync) {
      store.selectSidebar("ai:" + agent.id)
      #expect(store.settingsTab == .integrations && store.selectedAgent == agent.id)
      #expect(store.showSettings)
    }
    store.selectSidebar("circle:university")
    store.selectSidebar("page:Tasks")
    await store.sidebarCircleDidAppear()
    #expect(store.page == .tasks && store.circlePath.isEmpty)
  }
}

private actor N2CoverBackend: Backend {
  let base = DemoBackend()
  let tasks: [TaskSummary]
  let thumbnail: Data
  var reads: [String: Int] = [:]
  init(tasks: [TaskSummary], thumbnail: Data) { self.tasks = tasks; self.thumbnail = thumbnail }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    reads[path, default: 0] += 1
    if path.hasPrefix("/v1/tasks?") { return try Wire.data(TasksResponse(tasks: tasks)) }
    if path.contains("/cover/thumbnail") {
      try await Task.sleep(for: .milliseconds(25))
      return try JSONSerialization.data(withJSONObject: ["path": "", "thumbnail": thumbnail.base64EncodedString()])
    }
    if path == "/v1/home" {
      var value: HomeResponse = try await base.get(path); value.tasks = Array(tasks.prefix(6)); return try Wire.data(value)
    }
    if path == "/v1/status" {
      let data = try await base.request(path, method: method, body: body)
      var value = try JSONSerialization.jsonObject(with: data) as! [String: Any]
      value["linked"] = false
      return try JSONSerialization.data(withJSONObject: value)
    }
    return try await base.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { .init { $0.finish() } }
}
extension N2ShellTests {
  @Test func startupWarmsTenRecentCoversAndReopensSynchronously() async throws {
    let image = NSImage(size: .init(width: 640, height: 800), flipped: false) { rect in NSColor.systemGreen.setFill(); rect.fill(); return true }
    let tiff = try #require(image.tiffRepresentation)
    let bitmap = try #require(NSBitmapImageRep(data: tiff))
    let data = try #require(bitmap.representation(using: .png, properties: [:]))
    let tasks = (0..<12).map { index in
      var task = TaskSummary(id: "warm-\(index)", title: "Invented task \(index)")
      task.lastActiveAt = String(format: "2026-10-%02dT12:00:00Z", 31 - index)
      task.coverCandidate = .init(itemId: "photo", thumbnailUrl: "/v1/tasks/\(task.id)/cover/thumbnail?revision=1")
      return task
    }
    let backend = N2CoverBackend(tasks: tasks, thumbnail: data)
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    let startup = ContinuousClock.now
    await store.refresh()
    print("N2 startup including 10 cover warms \(startup.duration(to: .now))")
    #expect(store.history != nil) // History survives a linked=false preview.
    for task in tasks.prefix(10) { #expect(store.taskCovers.cached(task, generation: store.backendGeneration) != nil) }
    #expect(store.taskCovers.cached(tasks[10], generation: store.backendGeneration) == nil)
    let reads = await backend.reads
    #expect(reads.keys.filter { $0.contains("/cover/thumbnail") }.count == 10)
    #expect(!reads.keys.contains { $0.contains("/items") || $0 == "/v1/tasks/warm-0" })
    let firstPixels = ContinuousClock.now
    _ = try #require(store.taskCovers.cached(tasks[0], generation: store.backendGeneration))
    print("N2 first appearance cached pixels \(firstPixels.duration(to: .now))")
    for (name, view) in [("Home", AnyView(HomeView(store: store))), ("Tasks", AnyView(AllTasksView(store: store)))] {
      for visit in 1...2 {
        let host = NSHostingView(rootView: view); host.sizingOptions = []; host.frame = .init(x: 0, y: 0, width: 1280, height: 800)
        let start = ContinuousClock.now; host.layoutSubtreeIfNeeded()
        print("N2 \(name) visit \(visit) first layout \(start.duration(to: .now))")
      }
    }
    let workspace = store.workspace("tsk_study")
    await workspace.load()
    let reopen = ContinuousClock.now
    #expect(store.workspace("tsk_study") === workspace)
    await workspace.load()
    #expect(workspace.task != nil)
    print("N2 task workspace cached reopen \(reopen.duration(to: .now))")
    #expect(await backend.reads["/v1/tasks/tsk_study"] == 1)
    var changed = tasks[0]; changed.coverCandidate?.thumbnailUrl += "-changed"
    #expect(store.taskCovers.cached(changed, generation: store.backendGeneration) == nil)
    #expect(store.taskCovers.cached(tasks[0], generation: store.backendGeneration + 1) == nil)
    // A disappearing view releases its waiter, while the app-owned warm completes.
    let waiter = Task { await store.taskCovers.load(tasks[11], backend: backend, generation: store.backendGeneration) }
    await Task.yield(); waiter.cancel(); _ = await waiter.value
    #expect(store.taskCovers.cached(tasks[11], generation: store.backendGeneration) != nil)
    store.taskCovers.reset(generation: store.backendGeneration + 1)
    #expect(await store.taskCovers.load(tasks[0], backend: backend, generation: store.backendGeneration) == nil)
    #expect(store.taskCovers.cached(tasks[0], generation: store.backendGeneration) == nil)
  }
}
