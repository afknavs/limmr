import AppKit
import Foundation
import LimmrKit
import Testing
import SwiftUI
@testable import Limmr

@Suite("N1 real-data shapes", .serialized) @MainActor struct N1RealDataTests {
  @Test func onDeviceIsNotAConnectableAIAndUsesPlainSymbol() throws {
    let agent = try Wire.decode(Agent.self, from: Data(#"{"id":"this-mac","name":"This Mac","kind":"on_device","transport":"local","created_at":"2026-10-27T00:00:00Z","enabled":true}"#.utf8))
    #expect(AIIdentity.rows([agent], sync: nil).isEmpty)
    #expect(agent.symbol == "laptopcomputer")
    #expect(!ConnectKind.other.symbol.contains("sparkle"))
    var custom = agent; custom.id = "A"; custom.kind = "custom"
    #expect(!custom.symbol.contains("sparkle"))
  }
  @Test func exactDotShapeBeforeAndAfterOwnerLink() throws {
    let raw = #"{"agents":[{"id":"A","name":"Your dot","kind":"custom","transport":"remote","created_at":"2026-10-27T00:00:00Z","enabled":true},{"id":"B","name":"Pip","kind":"chatgpt","transport":"remote","created_at":"2026-10-27T00:00:00Z","enabled":true}]}"#
    var agents = try Wire.decode(AgentsResponse.self, from: Data(raw.utf8)).agents
    var sync = DotSyncStatus.demo("healthy")
    sync.peer.agentId = "A"; sync.peer.engineId = "E"; sync.peer.lastSyncAt = "2026-10-27T12:00:00Z"
    #expect(AIIdentity.rows(agents, sync: sync).map { AIIdentity.name($0, sync: sync) } == ["Your dot", "Pip"])
    #expect(AIIdentity.symbol(agents[0], sync: sync) == "desktopcomputer")
    #expect(AIIdentity.syncState(agents[0], sync: sync) == "Last synced 27 Oct")
    agents[1].runsOnPeer = "E"
    let rows = AIIdentity.rows(agents, sync: sync)
    #expect(rows.map { AIIdentity.name($0, sync: sync) } == ["Dot · Pip"])
    #expect(AIIdentity.syncState(rows[0], sync: sync) == "Last synced 27 Oct")
    sync.paused = true
    #expect(AIIdentity.syncState(rows[0], sync: sync) == "Sync paused")
    sync.paused = false; sync.peer.lastError = "Invented offline peer"
    #expect(AIIdentity.syncState(rows[0], sync: sync) == "Sync needs attention")
  }

  @Test func unsetCircleSymbolsAgreeAcrossSurfacesAndOwnerWins() {
    let cases = [("Family", "house"), ("Close people", "heart"), ("Northbridge University", "graduationcap"),
      ("Design Collective", "briefcase"), ("Charity fundraiser", "gift"), ("Build studio", "hammer"),
      ("Summer travel", "airplane"), ("Our flat", "key"), ("Other", "circle.grid.2x2"), ("You", "person.crop.circle")]
    for (name, symbol) in cases {
      var circle = DemoCircles.circles[0]
      circle.id = "invented"; circle.name = name; circle.kind = "custom"; circle.symbol = "circle.grid.2x2"; circle.icon = nil
      #expect(circle.displaySymbol == symbol)
      #expect(CirclePictures.chosen(circle).value == symbol)
      #expect(NetworkMapStyle.symbol(circle) == symbol)
      circle.icon = .init(kind: "symbol", value: "book")
      #expect(circle.displaySymbol == "book")
      circle.icon = nil; circle.symbol = "leaf"
      #expect(circle.displaySymbol == "leaf")
    }
    var family = DemoCircles.circles[0]; family.id = "family"; family.name = "Unusual name"; family.kind = "family"; family.symbol = ""; family.icon = nil
    #expect(family.displaySymbol == "house")
  }

  @Test func liveAndPreviewUseCurrentClockWhileFixturesStayFixed() {
    let live = AppStore(demo: false, snapshot: true, backend: EmptyBackend())
    let preview = AppStore(demo: true, snapshot: true, fixedClock: SnapshotRunner.usesFixtureClock(page: "preview-home"), backend: EmptyBackend())
    let threeDaysAgo = Date().addingTimeInterval(-3 * 86400 - 120).ISO8601Format()
    #expect(live.relativeTime(threeDaysAgo) == "3 d")
    #expect(preview.relativeTime(threeDaysAgo) == "3 d")
    #expect(abs(preview.presentationNow.timeIntervalSinceNow) < 2)
    #expect(SnapshotRunner.usesFixtureClock(page: "n1-look-home"))
    let fixed = AppStore(demo: true, snapshot: true, backend: EmptyBackend())
    let fixturePast = Timestamp.date(DemoData.now)!.addingTimeInterval(-2 * 86400).ISO8601Format()
    #expect(fixed.relativeTime(fixturePast) == "2 d")
  }

  @Test func homeFoldsPlainReadsAndCalendarSurvivesPendingOrAIOnlyHistory() throws {
    let store = AppStore(demo: true, snapshot: true)
    var read = DemoData.activity[0]; read.id = 8101; read.kind = "read"; read.tool = "list_messages"; read.count = 1; read.at = DemoData.now
    var summary = read; summary.id = 8102; summary.kind = "summary"; summary.tool = "save_summary"
    let source = [read, read, summary]
    let highlights = ActivityPresentation.tasks(ActivityPresentation.visible(source))
    #expect(highlights.count == 1 && highlights[0].steps.map(\.id) == [summary.id])
    store.allRecentActivity = [read]; store.home.recentActivity = [read]; store.history = nil
    let pending = try #require(store.homeRailHistory)
    #expect(N1ActivityWindow.days(history: pending, now: store.presentationNow).reduce(0) { $0 + N1ActivityWindow.count($1) } == 1)
    store.allRecentActivity = []; store.home.recentActivity = []
    store.history = StatsHistory(days: [HistoryDay(date: String(DemoData.now.prefix(10)), agentReads: 4)])
    #expect(store.homeRailHistory != nil)
    store.history = StatsHistory(days: [HistoryDay(date: "2020-01-01", messagesIn: 4)])
    #expect(store.homeRailHistory == nil)
  }

  @Test func purposeCardIsBoundedForLongCoverlessTask() {
    let store = AppStore(demo: true, snapshot: true)
    var task = TaskSummary(id: "long-coverless", title: String(repeating: "Prepare the university workshop and review the follow-up plan ", count: 5))
    task.circleIds = ["university"]; task.needsYouReason = String(repeating: "Waiting for the venue confirmation ", count: 20)
    task.lastActiveAt = DemoData.now; store.circles = DemoCircles.circles
    let host = NSHostingView(rootView: PurposeTaskCard(store: store, task: task).frame(width: 800))
    host.layoutSubtreeIfNeeded()
    #expect(host.fittingSize.height == 112)
    #expect(store.taskCircleName(task) == "University")
    #expect(store.purposeLastAction(task).contains("Waiting for the venue"))
  }

  @Test func emptyCircleHasNoTaskOrTopicSection() {
    let detail = CircleDetail(circle: DemoCircles.circles[0], chats: [], people: [], topics: [], brief: nil)
    #expect(!CirclePageSections.hasTasks([], id: detail.circle.id))
    #expect(detail.topics.isEmpty)
    var task = TaskSummary(id: "invented", title: "Prepare a class handout")
    task.circleIds = [detail.circle.id]
    #expect(CirclePageSections.hasTasks([task], id: detail.circle.id))
    #expect(!CirclePageSections.hasTasks([task], id: "another-circle"))
  }

  @Test func denseUniversityHubHasScreenSizedClearanceAndStableLayout() {
    let map = N1RealDataFixture.denseMap
    var layout = UnderstandingLayout()
    layout.updateNetwork(map: map, size: CGSize(width: 1600, height: 1000))
    #expect(layout.positions.count == 123)
    for hub in map.nodes.filter({ $0.kind == "circle" }) {
      let center = layout.positions[hub.id]!
      let clear = try! #require(layout.labels[hub.id])
      for person in map.nodes.filter({ $0.kind == "person" }) {
        let point = layout.positions[person.id]!
        #expect(!clear.contains(point))
        #expect(hypot(point.x - center.x, point.y - center.y) >= NetworkHubLabelGeometry.hubClearance)
      }
    }
    var reordered = UnderstandingLayout(); var reversed = map; reversed.nodes.reverse(); reversed.edges.reverse()
    reordered.updateNetwork(map: reversed, size: CGSize(width: 1600, height: 1000))
    #expect(layout.positions == reordered.positions)
  }

}
