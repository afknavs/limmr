import Foundation
import Testing
@testable import Limmr
@testable import LimmrKit

@Suite("A3 responsive Home and calm first reads") @MainActor struct A3ResponsiveTests {
  @Test func commonWindowGeometryUsesEveryMainColumnPoint() {
    for window in [760.0, 900, 1024, 1194, 1280, 1440, 1600] {
      // Exercise every permitted sidebar width, not just the ideal 180 pt.
      for sidebar in [165.0, 180, 220] {
        let detail = window - sidebar - 1
        let recent = HomePresentation.usesRecentColumn(windowWidth: window, contentWidth: detail)
        #expect(recent == (window >= 1150))
        let main = detail - (recent ? 332 : 0) - 56
        #expect(!HomePresentation.stacksDashboard(mainWidth: main))
        let dashboard = HomePresentation.dashboardWidth(mainWidth: main)
        let triggers = main - dashboard - 16
        #expect(dashboard >= 190 && dashboard <= HomePresentation.dashboardContentWidth + 32)
        #expect(triggers >= 200)
        #expect(dashboard + 16 + triggers == main)
        let heatmap = HomePresentation.heatmapGeometry(contentWidth: dashboard - 32, columns: 26)
        #expect(heatmap.visible > 0 && heatmap.visible <= 26 && (14...16).contains(heatmap.side))
        let occupied = 36 + Double(heatmap.visible) * (heatmap.side + 2) - 2
        #expect(occupied <= dashboard - 32 + 0.001)
        let search = HomePresentation.searchWidth(mainWidth: main)
        #expect(search == main)
      }
    }
    for main in [420.0, 480, 520, 650] {
      #expect(!HomePresentation.stacksDashboard(mainWidth: main))
      #expect(HomePresentation.dashboardWidth(mainWidth: main) + 16 < main)
    }
    #expect(HomePresentation.stacksDashboard(mainWidth: 419))
    #expect(HomePresentation.dashboardWidth(mainWidth: 419) == 419)
  }

  @Test func pendingAndKnownEmptyAreDifferent() async {
    let store = AppStore(demo: true, snapshot: true, backend: EmptyBackend())
    for path in ["/v1/status", "/v1/home", "/v1/circles", "/v1/people", "/v1/activity", "/v1/stats/history"] {
      #expect(store.isLoading(path) && !store.hasLoaded(path))
    }
    await store.refresh()
    #expect(store.hasLoaded("/v1/home") && !store.isLoading("/v1/home"))
    #expect(store.home.integrations.isEmpty)
    #expect(store.hasLoaded("/v1/circles") && store.circles.isEmpty)
    #expect(store.hasLoaded("/v1/activity") && store.activity.isEmpty)
  }

  @Test func transientHomeAndHistoryRecoverWithoutFalseFailures() async {
    let backend = A3ReadBackend(failing: ["/v1/home": 2, "/v1/stats/history": 3])
    let waits = A3Waits()
    let store = AppStore(demo: true, snapshot: true, backend: backend,
      retryPause: { await waits.append($0) })
    await store.refresh()
    #expect(store.hasLoaded("/v1/home") && store.history != nil)
    #expect(store.loadFailures.isEmpty && store.refreshError == nil)
    #expect(await backend.count("/v1/home") == 3)
    #expect(await backend.count("/v1/stats/history") == 4)
    #expect(await waits.values == [.seconds(1), .seconds(2), .seconds(1), .seconds(2), .seconds(4)])
  }

  @Test func retryStaysPendingAndOldBackendCannotRestoreDemoData() async {
    let gate = TestGate()
    let store = AppStore(demo: true, snapshot: true,
      backend: A3ReadBackend(failing: ["/v1/home": 1]),
      retryPause: { _ in await gate.enter() })
    let refresh = Task { await store.refresh() }
    await gate.waitUntilEntered()
    #expect(store.status?.linked == true)
    #expect(store.isLoading("/v1/home") && !store.hasLoaded("/v1/home"))
    #expect(store.refreshError == nil && store.loadFailures.isEmpty)
    await store.useSampleData(false, realBackend: EmptyBackend())
    await gate.release()
    await refresh.value
    #expect(!store.demo && store.home.integrations.isEmpty && store.circles.isEmpty)
    #expect(store.hasLoaded("/v1/home") && !store.isLoading("/v1/home"))
  }

  @Test func peopleAndActivityReadsShareTheQuietRetryPolicy() async throws {
    let backend = A3ReadBackend(failing: ["/v1/people": 1, "/v1/activity": 2, "/v1/efforts": 2])
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    let work: EffortsResponse = try await store.read("/v1/efforts?q=")
    #expect(!store.people.isEmpty && !store.activity.isEmpty && !work.efforts.isEmpty)
    #expect(await backend.count("/v1/people") == 2)
    #expect(await backend.count("/v1/activity") == 3)
    #expect(await backend.count("/v1/efforts") == 3)
    #expect(store.refreshError == nil)
  }

  @Test func exhaustedRetryShowsFailureAndPreservesLastGoodData() async {
    let backend = A3ReadBackend()
    let waits = A3Waits()
    let store = AppStore(demo: true, snapshot: true, backend: backend,
      retryPause: { await waits.append($0) })
    await store.refresh()
    let old = store.history?.days
    await backend.fail("/v1/stats/history", count: 5)
    await store.refreshHistory()
    #expect(store.history?.days == old)
    #expect(store.hasLoaded("/v1/stats/history") && !store.isLoading("/v1/stats/history"))
    #expect(store.loadFailures["/v1/stats/history?days=364"] != nil)
    #expect(await waits.values == CalmLoadRetry.delays)
    #expect(CalmLoadRetry.delays.reduce(Duration.zero, +) == .seconds(15))
    await store.refreshHistory()
    #expect(store.loadFailures["/v1/stats/history?days=364"] == nil)
    #expect(store.refreshError == nil)
  }

  @Test func definiteFailureDoesNotWaitAndCancellationIsNotAnError() async {
    let definite = A3ReadBackend(failing: ["/v1/home": 1], status: 403)
    let waits = A3Waits()
    let store = AppStore(demo: true, snapshot: true, backend: definite,
      retryPause: { await waits.append($0) })
    await store.refresh()
    #expect(!store.isLoading("/v1/home") && !store.hasLoaded("/v1/home"))
    #expect(store.loadFailures["/v1/home"] != nil)
    #expect(await definite.count("/v1/home") == 1)
    #expect(await waits.values.isEmpty)
    let cancelled = AppStore(demo: true, snapshot: true,
      backend: A3ReadBackend(failing: ["/v1/stats/history": 5]),
      retryPause: { _ in throw CancellationError() })
    await cancelled.refreshHistory()
    #expect(cancelled.loadFailures.isEmpty && cancelled.history == nil)
  }

  @Test func helperOutageUsesOneRetryWindowAndModeSwitchClearsKnowledge() async {
    let backend = A3ReadBackend(failing: ["/v1/status": 5])
    let waits = A3Waits()
    let store = AppStore(demo: true, snapshot: true, backend: backend,
      retryPause: { await waits.append($0) })
    await store.refresh()
    #expect(await backend.count("/v1/status") == 5)
    #expect(await backend.count("/v1/home") == 0)
    #expect(await waits.values == CalmLoadRetry.delays)
    #expect(store.refreshError != nil && store.isLoading("/v1/home"))
    await store.refresh()
    #expect(store.hasLoaded("/v1/home"))
    await store.useSampleData(false, realBackend: EmptyBackend())
    #expect(store.home.integrations.isEmpty && store.history == nil)
    #expect(store.hasLoaded("/v1/home") && !store.isLoading("/v1/home"))
  }
}

private actor A3Waits {
  var values: [Duration] = []
  func append(_ value: Duration) { values.append(value) }
}
private actor A3ReadBackend: Backend {
  let demo = DemoBackend()
  var failing: [String: Int]
  var calls: [String: Int] = [:]
  let status: Int
  init(failing: [String: Int] = [:], status: Int = 503) {
    self.failing = failing; self.status = status
  }
  func fail(_ route: String, count: Int) { failing[route] = count }
  func count(_ route: String) -> Int { calls[route, default: 0] }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    #expect(method == "GET" && body == nil)
    let route = URLComponents(string: path)?.path ?? path
    calls[route, default: 0] += 1
    if failing[route, default: 0] > 0 {
      failing[route, default: 0] -= 1
      throw BackendError.http(status)
    }
    return try await demo.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}
