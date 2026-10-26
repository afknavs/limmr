import AppKit
import Foundation
import SwiftUI
import Testing
@testable import Limmr
@testable import LimmrKit

@Suite("H3 invented Home fixtures", .serialized) @MainActor struct H3HomeViewTests {
  @Test func serverOnlyCaseSurvivesRefreshAndShowsFourCirclesWithNoWork() async throws {
    let backend = H3SnapshotBackend(scenario: "server")
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    for _ in 0..<2 {
      await store.refresh()
      await store.refreshHomeEfforts()
      let tiles = HomeTiles.tiles(circles: store.circles, home: store.home,
        efforts: store.homeEfforts, now: Timestamp.date(DemoData.now)!)
      #expect(tiles.count == 4 && store.homeEfforts.isEmpty)
      #expect(tiles.contains { $0.reason?.hasPrefix("Event ") == true })
      #expect(tiles.contains { $0.reason == "on you" })
      #expect(tiles.filter { $0.reason == "Busy" }.count == 2)
      #expect(HomePresentation.activitySlot(store.home) == .history)
    }
  }
  @Test func snapshotCasesReachEverySlotAndOddOtherCircleState() async throws {
    for (scenario, slot) in [("demo", HomePresentation.ActivitySlot.triggers),
      ("replies", .replies), ("history", .history), ("odd", .history), ("empty", .history)] {
      let backend = H3SnapshotBackend(scenario: scenario)
      let home: HomeResponse = try await backend.get("/v1/home")
      #expect(HomePresentation.activitySlot(home) == slot)
      #expect(HomePresentation.mainSections(home).contains("triggers") == (slot == .triggers))
      let circles: CirclesResponse = try await backend.get("/v1/circles")
      let efforts: EffortsResponse = try await backend.get("/v1/efforts")
      let tiles = HomeTiles.tiles(circles: circles.circles, home: home,
        efforts: efforts.efforts, now: Timestamp.date(DemoData.now)!)
      if scenario == "odd" { #expect(tiles.count == 3 && HomeTiles.needsOtherTile(count: tiles.count)) }
      if scenario == "empty" { #expect(tiles.isEmpty && HomeTiles.needsOtherTile(count: 0)) }
      let history: StatsHistory = try await backend.get("/v1/stats/history")
      #expect(history.days.count == 364)
    }
  }
  @Test func allCirclesClearsPreviousPeopleFiltersAndNavigation() {
    let defaults = UserDefaults.standard
    let previous = defaults.object(forKey: "peopleTab")
    defer {
      if let previous { defaults.set(previous, forKey: "peopleTab") }
      else { defaults.removeObject(forKey: "peopleTab") }
    }
    defaults.set("People", forKey: "peopleTab")
    let store = AppStore(demo: true, snapshot: true)
    store.circlePath = [.person("sample")]
    store.mapSelection.person = "sample"
    store.openAllHomeCircles()
    #expect(store.page == .circles && store.circlePath.isEmpty)
    #expect(store.mapSelection == MapSelection())
    #expect(defaults.string(forKey: "peopleTab") == "Circles")
  }
}
