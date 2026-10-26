import Foundation
import Testing
import SwiftUI
@testable import Limmr
@testable import LimmrKit

@Suite("K1 keyboard and spoken presentation") struct K1Tests {
  @MainActor @Test func sectionShortcutsResetDetailAndKeepExistingSidebar() {
    let store = AppStore(demo: true, snapshot: true)
    #expect(Page.allCases.map(\.rawValue) == ["Home", "People", "Activity"])
    for (number, page) in [(1, Page.overview), (2, .circles), (3, .activity)] {
      store.circlePath = [.circle("work"), .person("maya")]
      store.activityPath = [.effort("draft")]
      store.route(.section(number))
      #expect(store.page == page)
      #expect(store.circlePath.isEmpty && store.activityPath.isEmpty && store.pendingActivityRoute == nil)
    }
    store.route(.section(4))
    #expect(store.showSettings)
    store.showSettings = false
    store.route(.settings)
    #expect(store.showSettings)
    store.route(.section(9))
    #expect(store.page == .activity)
  }
  @MainActor @Test func findReturnsToSearchForEachPageAndDoesNotInterruptSetup() {
    let store = AppStore(demo: true, snapshot: true)
    for page in Page.allCases {
      store.page = page
      if page == .circles { store.circlePath = [.person("maya")] }
      if page == .activity { store.activityPath = [.effort("draft")] }
      let before = store.searchRequest
      store.route(.search)
      #expect(store.searchRequest == before + 1)
      #expect(store.page == page)
      #expect(!store.canGoBack)
    }
    store.showSetup = true
    let before = store.searchRequest
    store.route(.search)
    #expect(store.searchRequest == before)
  }
  @MainActor @Test func backPopsOneLevelAndEscapeSignalsLocalPresentations() {
    let store = AppStore(demo: true, snapshot: true)
    store.page = .circles
    store.circlePath = [.circle("work"), .person("maya")]
    #expect(store.canGoBack)
    store.route(.back)
    #expect(store.circlePath == [.circle("work")])
    store.route(.back); store.route(.back)
    #expect(!store.canGoBack)
    store.openEffort("draft")
    store.route(.back)
    #expect(store.activityPath.isEmpty && store.pendingActivityRoute == nil)
    store.showProposalReview = true; store.showConnect = true; store.showRemote = true
    store.connectKind = .codex
    store.route(.escape)
    #expect(store.escapeRequest == 1)
    #expect(!store.showProposalReview && !store.showConnect && !store.showRemote)
    #expect(store.connectKind == nil)
    store.route(.moveRow(-1)); store.route(.openRow)
    #expect(store.rowRequest == 1 && store.rowStep == -1 && store.rowOpenRequest == 1)
  }
  @Test func rowSelectionHandlesBoundariesAndReplacedResults() {
    var selected = KeyboardSelection()
    selected.move(1, in: [])
    #expect(selected.id == nil)
    selected.move(1, in: ["one", "two", "three"])
    #expect(selected.id == "one")
    selected.move(1, in: ["one", "two", "three"])
    #expect(selected.id == "two")
    selected.move(50, in: ["one", "two", "three"])
    #expect(selected.id == "three")
    selected.move(-50, in: ["one", "two", "three"])
    #expect(selected.id == "one")
    #expect(selected.current(in: ["replacement"]) == nil)
    selected.move(-1, in: ["new", "last"])
    #expect(selected.id == "last")
  }
  @MainActor @Test func selectedSearchResultOpensCorrectDestination() async throws {
    let store = AppStore(demo: true, snapshot: true)
    let hits = try await HomeSearch.find("Maya", backend: store.backend)
    var selection = KeyboardSelection()
    for hit in hits {
      selection.move(1, in: hits.map(\.id))
      let current = try #require(hits.first { $0.id == selection.current(in: hits.map(\.id)) })
      #expect(current == hit)
      store.openHomeSearchHit(current)
      switch current.kind {
      case .person: #expect(store.circlePath == [.person(current.target)])
      case .chat: #expect(store.circlePath == [.chat(current.target, current.title)])
      case .effort: #expect(store.pendingActivityRoute == .effort(current.target))
      }
    }
    #expect(hits.count > 1)
  }
  @Test func spokenCountsAndHeatmapUseUnitsAndActualBusiestDay() {
    #expect(Spoken.count(43, "Messages") == "43 messages")
    #expect(Spoken.count(6, "New") == "6 new messages")
    #expect(Spoken.sidebarValue(.overview, selected: .overview, queued: 3) == "Selected. 3 queued drafts")
    #expect(Spoken.sidebarValue(.circles, selected: .overview, queued: 3) == "Unselected")
    let days = [HistoryDay(date: "2026-10-12", messagesIn: 3, agentReads: 12),
                HistoryDay(date: "2026-10-13", messagesIn: 40, agentSends: 2)]
    #expect(Spoken.heatmap(days, agents: false) == "43 messages in the displayed history. Busiest day 2026-10-13.")
    #expect(Spoken.heatmap(days, agents: true) == "14 agent actions in the displayed history. Busiest day 2026-10-12.")
    #expect(Spoken.heatmap([], agents: false) == "0 messages in the displayed history")
  }
  @Test func spokenMapAndEffortMatchDemoContent() async throws {
    let backend = DemoBackend()
    let map: CircleMap = try await backend.get("/v1/map")
    #expect(Spoken.map(map).contains("people"))
    #expect(Spoken.map(map).contains("circles"))
    for node in map.nodes { #expect(Spoken.node(node) == "\(node.title), \(node.kind)") }
    let response: EffortsResponse = try await backend.get("/v1/efforts")
    let effort = try #require(response.efforts.first)
    #expect(Spoken.effort(effort) == "\(effort.title). \(effort.summary)")
  }
  @Test func increasedContrastPromotesMetadataInk() {
    var environment = EnvironmentValues()
    environment.snapshotIncreasedContrast = true
    #expect(AccessibleSecondary().resolve(in: environment) == Color.primary)
    #expect(AccessibleTertiary().resolve(in: environment) == Color.primary)
    environment.snapshotIncreasedContrast = false
    // The native Increase Contrast environment remains authoritative too.
    if environment.colorSchemeContrast == .standard {
      #expect(AccessibleSecondary().resolve(in: environment) == Color.secondary)
      #expect(AccessibleTertiary().resolve(in: environment) == Color.secondary.opacity(0.5))
    }
  }

}
