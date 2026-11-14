import Foundation
import LimmrKit
import Testing
@testable import Limmr

@Suite("Duplicate suggestion keep", .serialized) @MainActor struct TaskDuplicateTests {
  @Test func keepUpdatesExistingHomeCardWithoutAnotherRead() async throws {
    let backend = DuplicateKeepBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    let reads = await backend.reads
    let count = store.home.tasks.count
    #expect(store.home.tasks.contains { $0.id == "tsk_study" })
    let kept = await store.surfaceTask("tsk_cv", action: .keep)
    #expect(kept?.id == "tsk_study")
    #expect(store.home.tasks.count == count)
    #expect(store.home.tasks.filter { $0.id == "tsk_study" }.count == 1)
    #expect(await backend.reads == reads)
    #expect(store.taskActionErrors["tsk_cv"] == nil)
  }

  @Test func statusActionAfterKeepUsesReturnedTaskAndUndoSnapshot() async throws {
    let backend = DuplicateKeepBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    await store.quietTask("tsk_cv", status: "done")
    #expect(await backend.patches == ["/v1/tasks/tsk_study"])
    #expect(store.taskQuietUndo?.id == "tsk_study")
    #expect(store.taskQuietUndo?.status == "active")
    #expect(store.taskActionErrors["tsk_cv"] == nil)
  }
}

private actor DuplicateKeepBackend: Backend {
  var reads = 0
  var patches: [String] = []
  private let demo = DemoBackend()
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if method == "GET" { reads += 1 }
    if method == "PATCH" { patches.append(path) }
    if path == "/v1/tasks/tsk_cv/keep", method == "POST" {
      return try await demo.request("/v1/tasks/tsk_study", method: "GET", body: nil)
    }
    return try await demo.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}
