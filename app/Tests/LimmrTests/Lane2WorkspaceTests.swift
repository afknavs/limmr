import Foundation
import LimmrKit
import Testing
@testable import Limmr

@Suite("Lane 2 task loading", .serialized) @MainActor struct Lane2WorkspaceTests {
  @Test func navigationReusesWorkspaceAndDefersEffortReads() async throws {
    let backend = Lane2TaskBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    var task = DemoTasks.tasks[0]
    task.lastActiveAt = DemoData.now
    store.home.tasks = [task.compact(now: Timestamp.date(DemoData.now)!)]
    let first = store.workspace(task.id)
    #expect(first.task?.title == task.title) // The card supplies an immediate first frame.
    let clock = ContinuousClock()
    let cold = clock.now
    await first.load()
    let coldDuration = cold.duration(to: clock.now)
    let warm = clock.now
    for _ in 0..<100 { let next = store.workspace(task.id); #expect(next === first); await next.load() }
    let warmDuration = warm.duration(to: clock.now)
    #expect(await backend.taskReads == 1)
    #expect(await backend.effortReads == 0)
    print("lane2 task cold: \(coldDuration); 100 cached reopenings: \(warmDuration)")
    await first.loadDetails()
    #expect(await backend.effortReads == 1)
  }
  @Test func workspaceCacheIsBoundedAndMutationsRefresh() async throws {
    let backend = Lane2TaskBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    for i in 0..<60 { _ = store.workspace("synthetic-\(i)") }
    #expect(store.taskWorkspaceCache.count == 32)
    let model = store.workspace("tsk_study")
    await model.load()
    await model.change { try await model.client.patch(model.id, .init(status: "done")) }
    #expect(await backend.taskReads == 2)
    #expect(model.task?.status == "done")
  }
  @Test func dueCheckInsReachHomeWithoutActivityEvents() async throws {
    let backend = Lane2TaskBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refreshTaskCheckIns()
    #expect(store.home.queued.count == 1 && store.home.queued[0].taskId == "tsk_study")
    await store.refreshTaskCheckIns()
    #expect(store.home.queued.count == 1)
  }
  @Test func homeCardsDecodeDatesAndCircles() throws {
    let value = try Wire.decode(TaskSummary.self, from: Data(#"{"id":"a","created_at":"2026-10-28T09:00:00Z","last_active_at":"2026-10-28T10:00:00Z","circle_ids":["synthetic-circle"]}"#.utf8))
    #expect(value.createdAt != nil && value.lastActiveAt != nil && value.circleIds == ["synthetic-circle"])
  }
}

private actor Lane2TaskBackend: Backend {
  var taskReads = 0
  var effortReads = 0
  var task = DemoTasks.tasks[0]
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if path == "/v1/suggestions" { return Data(#"{"suggestions":[{"id":"due-review","kind":"check_in","task_id":"tsk_study","circle_id":"university","chat_jid":"15550100004@s.whatsapp.net","draft_text":"Review synthetic study progress","created_by":"agent-2","expires_at":"","created_at":"2026-10-28T09:00:00Z"}]}"#.utf8) }
    if path.hasPrefix("/v1/efforts/") { effortReads += 1; return try Wire.data(EffortDetailResponse(effort: DemoEfforts.efforts[0], timeline: [])) }
    if path.hasPrefix("/v1/tasks/") {
      if method == "GET" { taskReads += 1; try await Task.sleep(for: .milliseconds(30)) }
      if method == "PATCH" { task.status = "done" }
      return try Wire.data(task)
    }
    throw BackendError.http(404)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { .init { $0.finish() } }
}
