import Foundation
import Testing
@testable import Limmr
@testable import LimmrKit

@Suite("A6 naming journeys", .serialized) @MainActor struct A6FlowTests {
  @Test func demoQueueToLocalWriterToCreditedSearchAndHome() async throws {
    let backend = DemoBackend()
    let queue: EffortNamingResponse = try await backend.get("/v1/efforts/unnamed")
    #expect(queue.total == 1)
    let candidate = try #require(queue.efforts.first)
    #expect(candidate.snippets?.isEmpty == false)
    let before: EffortsResponse = try await backend.get("/v1/efforts")
    #expect(!before.efforts.contains { $0.id == candidate.id })
    try await OnDeviceEffortWriter(backend: backend, model: DemoEffortNamingModel(),
      session: EffortNamingSession()).name(candidate)
    let after: EffortDetailResponse = try await backend.get("/v1/efforts/\(candidate.id)")
    #expect(after.effort.title == "Festival budget")
    #expect(after.effort.createdBy == "This Mac")
    #expect(after.effort.messageRefs.count == candidate.messageCount)
    let remaining: EffortNamingResponse = try await backend.get("/v1/efforts/unnamed")
    #expect(remaining.total == 0)
    let hits = try await HomeSearch.find("Festival budget", backend: backend)
    #expect(hits.contains { $0.kind == .effort && $0.target == candidate.id })
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    #expect(store.home.circleUpdates.contains { $0.effortId == candidate.id && $0.title == after.effort.title })
    store.openEffort(candidate.id)
    #expect(store.activityPath.isEmpty && store.pendingActivityRoute == .effort(candidate.id))
    await store.activityDidAppear()
    #expect(store.activityPath == [.effort(candidate.id)])
    store.activityPath = [] // Native navigation-stack Back binding.
    #expect(store.activityPath.isEmpty && store.pendingActivityRoute == nil)
    store.openEffort(candidate.id)
    store.route(.back)
    #expect(store.page == .activity && store.activityPath.isEmpty && store.pendingActivityRoute == nil)
  }

  @Test func competingOwnerEditAndStaleRevisionWinOverLocalGeneration() async throws {
    let backend = DemoBackend()
    let queue: EffortNamingResponse = try await backend.get("/v1/efforts/unnamed")
    let candidate = try #require(queue.efforts.first)
    // A summary edit without a title still invalidates an old evidence digest.
    _ = try await backend.request("/v1/efforts/\(candidate.id)", method: "PATCH",
      body: Wire.data(["summary": "Owner is comparing the festival estimates."]))
    try await OnDeviceEffortWriter(backend: backend, model: DemoEffortNamingModel(),
      session: EffortNamingSession()).name(candidate)
    let stale: EffortDetailResponse = try await backend.get("/v1/efforts/\(candidate.id)")
    #expect(stale.effort.title.isEmpty && stale.effort.createdBy == "you")
    let fresh: EffortNamingResponse = try await backend.get("/v1/efforts/unnamed")
    #expect(fresh.efforts.first?.revision != candidate.revision)
    _ = try await backend.request("/v1/efforts/\(candidate.id)", method: "PATCH",
      body: Wire.data(["title": "Festival estimates"]))
    try await OnDeviceEffortWriter(backend: backend, model: DemoEffortNamingModel(),
      session: EffortNamingSession()).name(try #require(fresh.efforts.first))
    let owner: EffortDetailResponse = try await backend.get("/v1/efforts/\(candidate.id)")
    #expect(owner.effort.title == "Festival estimates" && owner.effort.createdBy == "you")
  }

  @Test func unnamedAccountHasHonestQuietStateAndDetailedHistory() async throws {
    let backend = DemoBackend(unnamedEffortsOnly: true)
    let efforts: EffortsResponse = try await backend.get("/v1/efforts")
    let queue: EffortNamingResponse = try await backend.get("/v1/efforts/unnamed")
    let history: ActivityPage = try await backend.get("/v1/activity")
    let home: HomeResponse = try await backend.get("/v1/home")
    #expect(efforts.efforts.isEmpty && queue.total == 1)
    #expect(home.circleUpdates.isEmpty && !history.items.isEmpty)
    #expect(EffortPresentation.emptyTitle(query: "", unnamed: queue.total) == "Work is waiting for names")
    #expect(EffortPresentation.emptyDetail(query: "", unnamed: queue.total).contains("Detailed log"))
    #expect(EffortPresentation.emptyTitle(query: "missing", unnamed: queue.total) == "No matching work")
    #expect(EffortPresentation.emptyTitle(query: "", unnamed: 0) == "No work here yet")
  }
}
