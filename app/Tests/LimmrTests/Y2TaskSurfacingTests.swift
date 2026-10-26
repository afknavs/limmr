import Foundation
import LimmrKit
import Testing
@testable import Limmr

@Suite("Y2 in-place task actions", .serialized) @MainActor struct Y2TaskSurfacingTests {
  @Test func homeActionsReplaceRowsWithoutRefreshingAndDayRouteClearsDetail() async throws {
    let backend = Y2RecordingBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    let before = store.history?.days
    let reads = await backend.reads
    let kept = await store.surfaceTask("tsk_cv", action: .keep)
    #expect(kept?.status == "active")
    #expect(store.home.tasks.first { $0.id == "tsk_cv" }?.status == "active")
    _ = await store.surfaceTask("tsk_print", action: .dismiss)
    _ = await store.surfaceTask("tsk_appointment", action: .undo)
    #expect(!store.home.tasks.contains { ["tsk_print", "tsk_appointment"].contains($0.id) })
    #expect(store.history?.days == before && store.taskActions.isEmpty)
    #expect(await backend.reads == reads)
    let tasks = store.home.tasks
    #expect(await store.surfaceTask("missing-task", action: .keep) == nil)
    #expect(store.home.tasks == tasks && store.taskActions.isEmpty)
    #expect(store.taskActionErrors["missing-task"] != nil)
    store.openTask("tsk_study")
    store.openActivityDay("2026-10-21")
    #expect(store.page == .activity && store.activityDay == "2026-10-21")
    #expect(store.activityPath.isEmpty && store.pendingActivityRoute == nil)
    #expect(store.activityFilter == "all")
  }
}

private actor Y2RecordingBackend: Backend {
  var reads = 0
  private let demo = DemoBackend()
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if method == "GET" { reads += 1 }
    return try await demo.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}
