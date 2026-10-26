import Foundation
import Testing

@testable import Limmr
@testable import LimmrKit

private actor RetryingCirclesBackend: Backend {
  let demo = DemoBackend()
  var remaining: Int
  var attempts = 0
  init(failures: Int) { remaining = failures }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if path == "/v1/circles" {
      attempts += 1
      if remaining > 0 {
        remaining -= 1
        throw BackendError.http(503)
      }
    }
    return try await demo.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}
@Suite("U3 app state") struct U3Tests {
  @MainActor @Test func circleWarningNeedsARealFailureAfterRetry() async {
    let transient = RetryingCirclesBackend(failures: 1)
    let store = AppStore(demo: true, snapshot: true, backend: transient)
    #expect(store.refreshError == nil)
    await store.refresh()
    #expect(await transient.attempts == 2)
    #expect(store.loadFailures["/v1/circles"] == nil)
    #expect(!store.circles.isEmpty)
    let failed = AppStore(demo: true, snapshot: true, backend: RetryingCirclesBackend(failures: 5))
    await failed.refresh()
    #expect(failed.loadFailures["/v1/circles"] != nil)
    await failed.refresh()
    #expect(failed.loadFailures["/v1/circles"] == nil)
  }
  @MainActor @Test func dashboardAndDigestsRefreshAndClearAcrossBackends() async {
    let store = AppStore(demo: true, snapshot: true)
    #expect(store.activityFilter == "sends")
    await store.refresh()
    #expect(store.dashboard?.today.sends == 2)
    #expect(!store.digests.digests.isEmpty && store.digestError == nil)
    #expect(!store.circlePreviews.isEmpty)
    #expect(HomePresentation.mainSections(store.home).contains("dashboard"))
    await store.useSampleData(false, realBackend: EmptyBackend())
    #expect(store.dashboard == nil && store.digests.digests.isEmpty && store.circlePreviews.isEmpty)
  }
  @MainActor @Test func calmMapKeepsCirclesAndCapsPeopleWithNeutralHairlines() async throws {
    let map: CircleMap = try await DemoBackend().get("/v1/map")
    let calm = UnderstandingPresentation.calmMap(map)
    #expect(calm.nodes.filter { $0.kind == "person" }.count <= 36)
    #expect(
      calm.nodes.filter { $0.kind == "circle" }.count
        == map.nodes.filter { $0.kind == "circle" }.count)
    #expect(
      calm.edges.allSatisfy {
        $0.style.tone == "neutral" && $0.style.width <= 0.6 && $0.style.opacity < 0.5
      })
    var layout = UnderstandingLayout()
    layout.update(map: calm, size: CGSize(width: 760, height: 240))
    #expect(calm.nodes.allSatisfy { layout.positions[$0.id] != nil })
    for page in [
      "home-waiting-0", "home-waiting-1", "home-waiting-4", "activity-sends", "activity-reads",
      "people", "agent-detail",
    ] {
      #expect(SnapshotRunner.pages.contains(page))
    }
  }
}
