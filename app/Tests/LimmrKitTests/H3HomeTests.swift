import Foundation
import Testing
@testable import LimmrKit

@Suite("H3 server-driven Home") struct H3HomeTests {
  let now = Timestamp.date(DemoData.now)!
  func item(_ id: String, reason: String, count: Int = 0, pinned: Bool = false) -> CircleToday {
    var circle = DemoCircles.details[1].circle
    circle.id = id; circle.name = id; circle.pinned = pinned
    return .init(circle: circle, brief: nil, newMessages: count, openAsks: 0, upcoming: 0,
      pinned: pinned, whyShown: reason)
  }
  @Test func liveRegressionServerReasonsSurfaceWithoutAnyNamedEfforts() {
    var home = HomeResponse.empty
    home.circles = [item("busy", reason: "busy", count: 20), item("ask", reason: "open_ask"),
      item("event", reason: "upcoming"), item("quiet-busy", reason: "busy", count: 2)]
    let tiles = HomeTiles.tiles(circles: [], home: home, efforts: [], now: now)
    #expect(tiles.map(\.id) == ["ask", "event", "busy", "quiet-busy"])
    #expect(tiles.allSatisfy { $0.rectangles.isEmpty })
    #expect(tiles.first?.reason == "on you")
    #expect(tiles.last?.newMessages == 2)
  }
  @Test func orderCapAndOtherCirclesPreserveUnshownDestinations() {
    var home = HomeResponse.empty
    home.circles = [item("pinned", reason: "busy", pinned: true), item("ask", reason: "open_ask"),
      item("event", reason: "upcoming"), item("changed", reason: "busy"),
      item("busy-8", reason: "busy", count: 8), item("busy-90", reason: "busy", count: 90),
      item("busy-2", reason: "busy", count: 2)]
    home.circleUpdates = [.init(circle: .init(id: "changed", name: "Changed"), effortId: "changed",
      title: "New venue", status: "news", line: "A venue was confirmed", lastAt: DemoData.now)]
    let tiles = HomeTiles.tiles(circles: [], home: home, efforts: [], now: now)
    #expect(tiles.map(\.id) == ["pinned", "ask", "event", "changed", "busy-90", "busy-8"])
    #expect(HomeTiles.otherCircles(circles: home.circles.map(\.circle), tiles: tiles).map(\.id) == ["busy-2"])
    #expect(!HomeTiles.needsOtherTile(count: tiles.count))
    for count in [0, 1, 3, 5] { #expect(HomeTiles.needsOtherTile(count: count)) }
    for count in [2, 4, 6] { #expect(!HomeTiles.needsOtherTile(count: count)) }
    #expect(HomeTiles.otherCircles(circles: home.circles.map(\.circle), tiles: []).count == 7)
  }
  @Test func mixedRectanglesPrioritizeAsksAndEventsAndDoNotAcknowledgeUnshownEfforts() throws {
    var home = HomeResponse.empty
    var today = item("one", reason: "open_ask")
    var brief = try #require(DemoCircles.details[1].brief)
    brief.summary = "A room and date are being agreed."
    brief.openAsksForYou = ["Please confirm the room"]
    brief.upcoming = [.init(date: "2026-10-22", text: "Workshop"), .init(date: "2026-01-01", text: "Past event")]
    today.brief = brief; home.circles = [today]
    home.needsReply = [.init(id: "ask", circleId: "one", circle: "One", text: "Please confirm the room", from: "Maya Rao")]
    var mine = DemoEfforts.efforts[0]; mine.circleIds = ["one"]; mine.status = "on_you"
    let unread = CircleEffortUpdate(circle: .init(id: "one", name: "One"), effortId: "unshown",
      title: "Budget changed", status: "moving", line: "Updated estimate", lastAt: DemoData.now)
    home.circleUpdates = [unread]
    let tiles = HomeTiles.tiles(circles: [], home: home, efforts: [mine], now: now)
    let tile = try #require(tiles.first)
    #expect(tile.rectangles.count == 3)
    #expect(tile.rectangles.map(\.id) == ["effort-" + mine.id, "ask-ask", "event-2026-10-22Workshop"])
    #expect(tile.updates.map(\.effortId) == [mine.id])
    #expect(HomeTiles.acknowledgedUpdates(tiles: tiles, unread: [unread]).isEmpty)
    #expect(tile.brief == brief.summary)
  }
  @Test func activitySlotPrefersWaitingThenRepliesAndNeverCreatesEmptyTriggers() {
    var home = HomeResponse.empty
    home.watches = DemoHomeTriggers.watches
    #expect(HomePresentation.activitySlot(home) == .history)
    #expect(!HomePresentation.mainSections(home).contains("triggers"))
    home.needsReply = [.init(id: "ask", circleId: "one", circle: "One", text: "Can you check?")]
    #expect(HomePresentation.activitySlot(home) == .replies)
    #expect(!HomePresentation.mainSections(home).contains("needs_reply"))
    home.waiting = [.init(id: "watch", agent: "Codex", kind: "watch", sentence: "When Maya replies", where: "Maya", since: DemoData.now, newHits: 0)]
    #expect(HomePresentation.activitySlot(home) == .triggers)
    #expect(HomePresentation.mainSections(home).contains("needs_reply"))
    #expect(Array(HomePresentation.mainSections(home).prefix(3)) == ["greeting", "search", "dashboard"])
  }
  @Test func fullWidthHistoryUsesReadableCalendarCellsAndLabelsItsPeriod() {
    for (width, weeks) in [(282.0, 15), (442.0, 25), (542.0, 31), (950.0, 52)] {
      let geometry = HomePresentation.heatmapGeometry(contentWidth: width, columns: 52)
      #expect(geometry.visible == weeks && (14...16).contains(geometry.side))
    }
    #expect(HomePresentation.heatmapPeriod(weeks: 52) == "Past 12 months")
    #expect(HomePresentation.heatmapPeriod(weeks: 26) == "Past 6 months")
    #expect(HomePresentation.heatmapPeriod(weeks: 42) == "Past 42 weeks")
    let cells = HomePresentation.stripCells(.init(days: [.init(date: "2026-10-19", messagesIn: 8)]), weeks: 52)
    #expect(cells.count == 364 && cells[51 * 7]?.messagesIn == 8)
    #expect(cells.suffix(6).allSatisfy { $0 == nil })
  }
  @Test func fullDashboardPlacesCountsBesideNaturalHeatmapWithoutUnusedWidth() {
    for width in [840.0, 852.0] {
      let layout = HomePresentation.fullDashboardGeometry(contentWidth: width)
      #expect(layout.horizontal && layout.heatmap == width - 144)
      #expect(layout.counts == 120)
      #expect(layout.heatmap + 24 + layout.counts == width)
      #expect(HomePresentation.heatmapGeometry(contentWidth: layout.heatmap, columns: 52).visible >= 40)
    }
    let narrow = HomePresentation.fullDashboardGeometry(contentWidth: 640)
    #expect(narrow.horizontal && narrow.counts == 120)
    #expect(narrow.heatmap + 24 + narrow.counts == 640)
    #expect(!HomePresentation.fullDashboardGeometry(contentWidth: 500).horizontal)
  }
  @Test func pairingKeepsPriorityLeadersAndMatchesHeavyAndBriefOnlyCards() {
    func tile(_ id: String, pinned: Bool, reason: String, rectangles: Int) -> HomeCircleTile {
      HomeCircleTile(circle: item(id, reason: reason, pinned: pinned).circle, pinned: pinned,
        reason: reason, brief: "A short circle brief.", newMessages: 1,
        rectangles: (0..<rectangles).map { .ask(id: "ask-\($0)", text: "Can you confirm?", from: "Maya") })
    }
    let pinHeavy = tile("pin-heavy", pinned: true, reason: "on you", rectangles: 3)
    let pinBrief = tile("pin-brief", pinned: true, reason: "Busy", rectangles: 0)
    let askHeavy = tile("ask-heavy", pinned: false, reason: "on you", rectangles: 3)
    let busyBrief = tile("busy-brief", pinned: false, reason: "Busy", rectangles: 0)
    // The review regression: never stretch Atlas's brief to University's three asks.
    let paired = HomeTiles.pairedTiles([pinHeavy, pinBrief, askHeavy, busyBrief])
    #expect(paired.map(\.id) == ["pin-heavy", "ask-heavy", "pin-brief", "busy-brief"])
    #expect(paired[0].pinned && paired[2].pinned)
    #expect(paired.map { $0.rectangles.isEmpty } == [false, false, true, true])
    let pinHeavy2 = tile("pin-heavy-2", pinned: true, reason: "on you", rectangles: 3)
    let pinBrief2 = tile("pin-brief-2", pinned: true, reason: "Busy", rectangles: 0)
    let sameBand = HomeTiles.pairedTiles([pinHeavy, pinBrief, pinHeavy2, pinBrief2, askHeavy, busyBrief])
    #expect(sameBand.map(\.id) == ["pin-heavy", "pin-heavy-2", "pin-brief", "pin-brief-2", "ask-heavy", "busy-brief"])
    #expect(HomeTiles.pairedTiles([]).isEmpty)
    #expect(HomeTiles.pairedTiles([pinBrief]).map(\.id) == ["pin-brief"])
    #expect(HomeTiles.pairedTiles([pinHeavy, pinBrief, askHeavy]).map(\.id) == ["pin-heavy", "ask-heavy", "pin-brief"])
    for natural in [65.0, 120, 200, 290] {
      let allocated = HomeTiles.rowTileHeight(natural: natural, row: 290)
      #expect(allocated >= natural && allocated <= 290)
      #expect(16 + allocated - natural <= 40)
    }
  }

}
