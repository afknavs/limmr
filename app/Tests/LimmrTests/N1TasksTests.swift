import AppKit
@testable import LimmrKit
import SwiftUI
import Testing
@testable import Limmr

@Suite("N1 tasks", .serialized) @MainActor struct N1TasksTests {
  @Test func agentCreatedUncircledTaskAppearsOnMountedHomeAndAllTasks() async throws {
    _ = NSApplication.shared
    let backend = N1TaskBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    let host = NSHostingView(rootView: RootView(store: store).frame(width: 1100, height: 800))
    host.frame = NSRect(x: 0, y: 0, width: 1100, height: 800)
    host.layoutSubtreeIfNeeded()
    #expect(!store.home.tasks.contains { $0.id == "tsk_agent" })
    let watcher = Task { await store.watchActivity() }
    defer { watcher.cancel() }
    await backend.createAgentTask()
    // Wait for the real activity-stream debounce, with Home still mounted.
    for _ in 0..<100 {
      if store.home.tasks.contains(where: { $0.id == "tsk_agent" }) { break }
      try await Task.sleep(for: .milliseconds(100))
    }
    host.layoutSubtreeIfNeeded()
    #expect(TaskPresentation.open(store.home.tasks).contains { $0.id == "tsk_agent" })
    #expect(store.home.tasks.first { $0.id == "tsk_agent" }?.circleIds.isEmpty == true)
    store.selectPage(.tasks)
    await store.refreshAllTasks()
    host.layoutSubtreeIfNeeded()
    #expect(TaskListPresentation.active(store.allTasks).contains { $0.id == "tsk_agent" })
    #expect(TaskListPresentation.search(store.allTasks, title: "exam").map(\.id) == ["tsk_agent"])
    store.openTask("tsk_agent")
    #expect(store.page == .tasks && store.navigationPath.wrappedValue == [.activity(.task("tsk_agent"))])
    store.route(.back)
    #expect(store.page == .tasks && store.navigationPath.wrappedValue.isEmpty)
  }
  @Test func circleGroupsAndNewestClosedDate() {
    var older = TaskSummary(id: "old", title: "Old task", status: "done")
    older.closedAt = "2026-10-20T12:00:00Z"; older.lastActiveAt = "2026-10-30T12:00:00Z"; older.circleIds = ["study"]
    var newer = TaskSummary(id: "new", title: "New task", status: "dropped")
    newer.closedAt = "2026-10-25T12:00:00Z"; newer.circleIds = ["study"]
    let orphan = TaskSummary(id: "none", title: "No circle")
    #expect(TaskListPresentation.history([older,newer]).map(\.id) == ["new","old"])
    #expect(TaskListPresentation.inCircle([older,newer,orphan], id: "study").count == 2)
    #expect(TaskListPresentation.noCircle([older,newer,orphan]).map(\.id) == ["none"])
  }
  @Test func sectionsSearchAndOrdering() {
    var active = TaskSummary(id: "a", title: "Exam guide")
    active.lastActiveAt = DemoData.now
    let quiet = TaskSummary(id: "q", title: "Old guide", status: "quiet")
    let done = TaskSummary(id: "d", title: "Printed guide", status: "done")
    let tasks = [done, quiet, active]
    #expect(TaskListPresentation.active(tasks).map(\.id) == ["a"])
    #expect(TaskListPresentation.quiet(tasks).map(\.id) == ["q"])
    #expect(TaskListPresentation.history(tasks).map(\.id) == ["d"])
    #expect(TaskListPresentation.search(tasks, title: "  EXAM ").map(\.id) == ["a"])
  }
}

private actor N1TaskBackend: Backend {
  let demo = DemoBackend()
  nonisolated let stream = AsyncThrowingStream<SSEEvent, Error>.makeStream()
  var task: TaskSummary?
  func createAgentTask() {
    var value = TaskSummary(id: "tsk_agent", title: "Exam preparation", createdBy: .init(agentId: "agent-fixture", name: "Example AI"))
    value.lastActiveAt = "2026-10-31T12:00:00Z"
    value.createdAt = value.lastActiveAt
    task = value
    stream.continuation.yield(SSEEvent(event: "activity", data: "{}", id: nil))
  }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if path == "/v1/home" {
      var home: HomeResponse = try await demo.get(path)
      if let task { home.tasks.insert(task, at: 0) }
      return try Wire.data(home)
    }
    if path == "/v1/tasks?status=all&limit=500" { return try Wire.data(TasksResponse(tasks: task.map { [$0] } ?? [])) }
    return try await demo.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { stream.stream }
}
