import Foundation
import LimmrKit
import Testing
@testable import Limmr

@Suite("A5 quiet unlinked history") struct A5LoadingTests {
  @MainActor @Test func unchangedHistoryReturnsAfterRelinking() async {
    let backend = A5HistoryBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    let days = store.history?.days
    #expect(days?.isEmpty == false)
    await backend.setLinked(false)
    await store.refresh()
    #expect(store.history == nil)
    #expect(store.refreshError == nil)
    await backend.setLinked(true)
    await store.refresh()
    #expect(store.history?.days == days)
    #expect(store.hasLoaded("/v1/stats/history"))
  }
}

private actor A5HistoryBackend: Backend {
  private let base = DemoBackend()
  private var linked = true
  func setLinked(_ value: Bool) { linked = value }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if path == "/v1/status" {
      var status: Status = try await base.get(path)
      status.linked = linked; status.connected = linked
      return try Wire.data(status)
    }
    return try await base.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}
