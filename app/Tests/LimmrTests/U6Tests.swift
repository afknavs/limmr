import Testing

@testable import Limmr
@testable import LimmrKit

@Suite("U6 Home snapshot fixtures") struct U6Tests {
  @MainActor @Test func fixtureIsOptInAndBothSnapshotVariantsUseTheSameHome() async {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    let original = store.home.watches
    HomeQuietSnapshot.prepare(store: store, arguments: [])
    #expect(store.home.watches == original)
    HomeQuietSnapshot.prepare(store: store, arguments: ["--u6-watches"])
    #expect(store.home.watches == DemoHomeTriggers.watches)
    #expect(store.home.watches.count == 3)
    #expect(HomePresentation.stripCells(store.history!).count == 182)
    HomeQuietSnapshot.prepare(store: store, arguments: ["--u6-empty"])
    #expect(store.home.watches.isEmpty)
    #expect(!HomePresentation.mainSections(store.home).contains("triggers"))
  }
}
