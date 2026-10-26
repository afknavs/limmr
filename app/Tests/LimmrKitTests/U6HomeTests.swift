import Foundation
import Testing

@testable import LimmrKit

@Suite("U6 quiet Home") struct U6HomeTests {
  let now = Timestamp.date(DemoData.now)!

  @Test func stripGrowsWithinReadableBoundsWithoutOverflow() {
    #expect(HomePresentation.dashboardContentWidth == 450)
    #expect(HomePresentation.heatmapWeeks == 26)
    #expect(HomePresentation.heatmapGap == 2)
    for width in stride(from: 0.0, through: 1500, by: 1) {
      let (side, weeks) = HomePresentation.heatmapGeometry(contentWidth: width, columns: 53)
      #expect(weeks >= 0 && weeks <= 52)
      if weeks > 0 {
        #expect((14...16).contains(side))
        #expect(36 + Double(weeks) * side + Double(weeks - 1) * 2 <= width + 0.001)
      }
    }
    #expect(HomePresentation.heatmapGeometry(contentWidth: 450, columns: 53).visible == 26)
    #expect(HomePresentation.heatmapGeometry(contentWidth: 449, columns: 53).visible == 25)
  }

  @Test func stripShowsExactly26CalendarWeeksWithGapsAndPartialFinalWeek() throws {
    // Monday is the first cell of the final week. Missing records are not shifted into other dates.
    let history = StatsHistory(days: [
      .init(date: "2025-01-01", messagesIn: 999),
      .init(date: "2026-10-19", messagesIn: 7),
      .init(date: "2026-10-17", messagesIn: 3),
    ])
    let cells = HomePresentation.stripCells(history)
    #expect(cells.count == 26 * 7)
    #expect(cells[25 * 7]?.date == "2026-10-19")
    #expect(cells[24 * 7 + 5]?.date == "2026-10-17")
    #expect(cells.suffix(6).allSatisfy { $0 == nil })
    #expect(cells.compactMap { $0 }.count == 2)
    #expect(HomePresentation.stripCells(StatsHistory()).isEmpty)
  }

  @Test func replyKeywordAndDeadlineUsePlainWordingAndDifferentStates() {
    let watches = DemoHomeTriggers.watches
    let reply = HomeTriggerPresentation(watches[0], now: now)
    #expect(reply.condition == "When Maya replies about the design")
    #expect(reply.action == "then: ask Codex to help")
    #expect(reply.state == .armed)
    #expect(reply.timing == "armed 2 d · checked 4 min ago")
    let keyword = HomeTriggerPresentation(watches[1], now: now)
    #expect(keyword.condition == "When anyone mentions the workshop in Design Club")
    #expect(keyword.action == "then: notify you")
    #expect(keyword.state == .due && keyword.timing == "fired 2 min ago")
    let deadline = HomeTriggerPresentation(watches[2], now: now)
    #expect(deadline.condition == "When the design review is due")
    #expect(deadline.action == "then: prepare a draft for review")
    #expect(deadline.state == .armed && deadline.timing == "fires in 2 d")
    let soon = HomeTriggerPresentation(watches[2], now: now.addingTimeInterval(172800 - 1800))
    #expect(soon.state == .due && soon.timing == "fires in 30 min")
    let due = HomeTriggerPresentation(watches[2], now: now.addingTimeInterval(172800))
    #expect(due.state == .due && due.timing == "due now")
  }

  @Test func integrationLineUsesTheSelectedPeriodAndRealAgentNames() throws {
    let agents = Array(DemoData.agents.prefix(2))
    let first = try #require(agents.first)
    let history = StatsHistory(days: [
      .init(date: "2026-10-16", agents: [first.id: HistoryAgent(reads: 40, sends: 2)]),
      .init(date: "2026-10-17", agents: [first.id: HistoryAgent(reads: 3, sends: 1)]),
    ])
    #expect(
      HomePresentation.integrationCounts(agents: agents, days: history.period(1))
        == "\(first.name) 4 · \(agents[1].name) 0")
    #expect(
      HomePresentation.integrationCounts(agents: agents, days: history.period(7))
        == "\(first.name) 46 · \(agents[1].name) 0")
  }

  @Test func olderContractNeverClaimsAnExpiryOrHitIsTheLastCheck() throws {
    let watch = try Wire.decode(
      WatchSummary.self,
      from: Data(
        #"{"id":"old","kind":"watch","title":"Exam","goal":"Tell me when the date changes","where":"Design Club","action":"notify","status":"active","created_by":"Codex","created_at":"2026-10-15T12:00:00Z","expires_at":"2026-10-19T12:00:00Z","new_hits":2,"latest":{"at":"2026-10-16T12:00:00Z","chat":"Design Club","sender":"Maya","text":"Exam news"},"future":true}"#
          .utf8))
    let row = HomeTriggerPresentation(watch, now: now)
    #expect(row.state == .armed)
    #expect(row.timing == "armed 2 d")
    #expect(!row.timing.contains("checked") && !row.timing.contains("fires"))
    #expect(watch.nextFireAt == nil && watch.lastCheckedAt == nil)
    let roundTrip = try Wire.decode(WatchSummary.self, from: Wire.data(DemoHomeTriggers.watches[2]))
    #expect(roundTrip.nextFireAt == DemoHomeTriggers.watches[2].nextFireAt)
  }

  @Test func onlyActiveUnexpiredWatchesAreArmedAndEmptyHasNoHomeTriggerCard() {
    var watches = DemoHomeTriggers.watches
    watches[0].status = "paused"
    watches[1].status = "stopped"
    watches[2].expiresAt = "2026-10-16T00:00:00Z"
    #expect(HomeTriggerPresentation.armedWatches(watches, now: now).isEmpty)
    #expect(!HomePresentation.mainSections(.empty).contains("triggers"))
    #expect(HomeTriggerPresentation.emptyExample.hasPrefix("Example: When "))
    #expect(HomeTriggerPresentation.emptyExample.contains("then: notify you"))
    #expect(
      HomeTriggerPresentation.emptyHint == "Ask any agent: tell me when ... Limmr watches and acts."
    )
  }

  @Test func changesUseTodaysActualPlacementCountAndStayAtBottom() {
    var home = HomeResponse.empty
    home.circleChanges = [
      .init(
        id: "one", circleId: "studio", name: "Studio", agent: "Limmr", action: "placed",
        at: DemoData.now, count: 40),
      .init(
        id: "two", circleId: "club", name: "Club", agent: "Limmr", action: "placed",
        at: DemoData.now, count: 63),
      .init(
        id: "old", circleId: "club", name: "Club", agent: "Limmr", action: "placed",
        at: "2026-10-14T12:00:00Z", count: 99),
    ]
    #expect(
      HomePresentation.changesLine(home, now: now) == "Limmr placed 103 people into circles today")
    #expect(HomePresentation.mainSections(home).last == "changes")
    home.circleChangesMore = 1
    #expect(HomePresentation.changesLine(home, now: now).contains("at least 103"))
  }
}
