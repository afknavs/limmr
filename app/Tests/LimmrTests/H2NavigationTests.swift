import Foundation
import Testing
@testable import Limmr
@testable import LimmrKit

@Suite("H2 Home navigation") struct H2NavigationTests {
  @MainActor @Test func eachSearchKindOpensItsOwnDestination() async throws {
    let store = AppStore(demo: true, snapshot: true)
    let hits = try await HomeSearch.find("Maya", backend: store.backend)
    let person = try #require(hits.first { $0.kind == .person })
    store.openHomeSearchHit(person)
    #expect(store.page == .circles && store.selectedPerson == person.target)
    #expect(store.circlePath == [.person(person.target)])
    let chat = try #require(hits.first { $0.kind == .chat })
    store.openHomeSearchHit(chat)
    #expect(store.page == .circles && store.circlePath == [.chat(chat.target, chat.title)])
    let work = try #require(hits.first { $0.kind == .effort })
    store.openHomeSearchHit(work)
    #expect(store.page == .activity && store.pendingActivityRoute == .effort(work.target))
    #expect(store.circlePath.isEmpty)
    let detail: EffortDetailResponse = try await store.backend.get("/v1/efforts/\(work.target)")
    #expect(detail.effort.id == work.target)
    store.selectPage(.overview)
    #expect(store.activityPath.isEmpty && store.pendingActivityRoute == nil)
  }
  @Test func snapshotStatesCoverPinnedSurfacedEvenOddAndEmptyTriggers() async throws {
    for odd in [false, true] {
      for empty in [false, true] {
        let backend = H2SnapshotBackend(odd: odd, emptyTriggers: empty)
        let home: HomeResponse = try await backend.get("/v1/home")
        let circles: CirclesResponse = try await backend.get("/v1/circles")
        let efforts: EffortsResponse = try await backend.get("/v1/efforts")
        let tiles = HomeTiles.tiles(circles: circles.circles, home: home, efforts: efforts.efforts, now: Timestamp.date(DemoData.now)!)
        #expect(tiles.count == 4)
        #expect(tiles.filter(\.pinned).count == 2)
        #expect(home.watches.count == (empty ? 0 : 3))
      }
    }
  }
}
