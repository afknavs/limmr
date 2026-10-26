import Foundation
import Testing
@testable import Limmr
@testable import LimmrKit

@Suite("Y1 deferred Activity navigation", .serialized) @MainActor struct Y1Tests {
  @Test func homeTaskWaitsForDestinationRegistration() async {
    let store = AppStore(demo: true, snapshot: true)
    store.openTask("tsk_study") // Same action as TasksTile on Home.
    #expect(store.page == .activity)
    #expect(store.activityPath.isEmpty)
    #expect(store.pendingActivityRoute == .task("tsk_study"))
    #expect(store.canGoBack)
    await store.activityDidAppear()
    #expect(store.activityPath == [.task("tsk_study")])
    #expect(store.pendingActivityRoute == nil)
    #expect(store.activityBackTitle == "Home")
    store.route(.back)
    #expect(!store.canGoBack)
    #expect(store.page == .overview) // Back returns where the task was opened.
    store.selectPage(.activity)
    await store.activityDidAppear()
    store.openTask("tsk_study")
    #expect(store.activityBackTitle == "Activity")
    store.route(.back)
    #expect(store.page == .activity && store.activityPath.isEmpty)
  }

  @Test func twentyHomeTaskBackHomeCyclesAndOtherEntryPoints() async throws {
    let store = AppStore(demo: true, snapshot: true)
    for _ in 0..<20 {
      store.selectPage(.overview)
      store.openTask("tsk_study")
      #expect(store.activityPath.isEmpty)
      await store.activityDidAppear()
      #expect(store.activityPath == [.task("tsk_study")])
      store.route(.back)
      #expect(!store.canGoBack)
      store.selectPage(.overview)
      #expect(store.activityPath.isEmpty && store.pendingActivityRoute == nil)
    }
    store.openEffort("effort-slides")
    #expect(store.activityPath.isEmpty)
    await store.activityDidAppear()
    #expect(store.activityPath == [.effort("effort-slides")])
    // An open from an already mounted Activity pushes immediately.
    store.openTask("tsk_study")
    #expect(store.activityPath == [.task("tsk_study")])
    #expect(store.pendingActivityRoute == nil)
    store.selectPage(.overview)
    let hits = try await HomeSearch.find("Pitch deck", backend: store.backend)
    let hit = try #require(hits.first { $0.kind == .effort })
    store.openHomeSearchHit(hit)
    #expect(store.activityPath.isEmpty && store.pendingActivityRoute == .effort(hit.target))
    await store.activityDidAppear()
    #expect(store.activityPath == [.effort(hit.target)])
    store.route(.section(3))
    #expect(store.page == .activity && !store.canGoBack)
    #expect(store.activityPath.isEmpty && store.pendingActivityRoute == nil)
  }

  @Test func abandonedOrReplacedPendingRoutesCannotResurface() async {
    let store = AppStore(demo: true, snapshot: true)
    store.openTask("tsk_study")
    store.openEffort("replacement")
    #expect(store.activityPath.isEmpty && store.pendingActivityRoute == .effort("replacement"))
    store.selectPage(.circles)
    await store.activityDidAppear()
    #expect(store.activityPath.isEmpty && store.pendingActivityRoute == nil)
    store.openTask("tsk_study")
    store.route(.back) // Back also cancels a push that hasn't mounted yet.
    await store.activityDidAppear()
    #expect(store.activityPath.isEmpty && !store.canGoBack)
    store.openTask("tsk_study")
    store.route(.search)
    await store.activityDidAppear()
    #expect(store.activityPath.isEmpty && store.pendingActivityRoute == nil)
    store.openTask("tsk_study")
    store.navigate(URL(string: "limmr://people/person-04")!)
    #expect(store.activityPath.isEmpty && store.pendingActivityRoute == nil)
  }
}
