import Foundation
import Testing

@testable import LimmrKit

@Suite("U5 Home real-data presentation") struct U5HomeTests {
  @Test func changesStayAtTheBottomAndDisappearWhenEmpty() throws {
    var home = DemoCircles.home(details: DemoCircles.details, suggestions: [], proposals: [])
    home.circleChanges = [
      try Wire.decode(
        CircleAutoChange.self,
        from: Data(
          #"{"id":"placement","circle_id":"studio","name":"Studio","agent":"Limmr","action":"placed","at":"2026-10-19T12:00:00Z","count":40}"#
            .utf8))
    ]
    let sections = HomePresentation.mainSections(home)
    #expect(sections.first == "greeting")
    #expect(sections.last == "changes")
    home.needsReply = []
    #expect(HomePresentation.mainSections(home).contains("changes"))
    home.circleChanges = []
    #expect(!HomePresentation.mainSections(home).contains("changes"))
    home.circleChangesMore = 4
    #expect(HomePresentation.mainSections(home).contains("changes"))
    #expect(!HomePresentation.mainSections(.empty).contains("changes"))
  }

  private func row(
    _ id: Int64, minute: Int, tool: String, kind: String = "read", count: Int = 0,
    chat: String = "studio"
  ) -> Activity {
    .init(
      id: id, at: "2026-10-19T10:\(String(format: "%02d", minute)):00Z", agentId: "example-ai",
      tool: tool, kind: kind, chatJids: [chat], messageIds: [], count: count)
  }
  @Test func recentUsesActivityTasksWithoutEmptyReadsOrDirectoryScans() {
    let noise = [
      row(1, minute: 0, tool: "list_messages"), row(2, minute: 1, tool: "search_messages"),
      row(3, minute: 2, tool: "list_chats", count: 200),
      row(4, minute: 3, tool: "list_groups", count: 40),
      row(5, minute: 4, tool: "get_person", kind: "people_read", count: 1),
    ]
    #expect(ActivityPresentation.tasks(noise).isEmpty)
    let work = [
      row(6, minute: 5, tool: "list_messages", count: 8),
      row(7, minute: 6, tool: "search_messages", kind: "search", count: 4),
      row(8, minute: 7, tool: "send_draft", kind: "sent", count: 1),
      row(9, minute: 8, tool: "list_messages", count: 2, chat: "garden"),
    ]
    let tasks = ActivityPresentation.tasks(work + noise + work)
    #expect(tasks.map(\.id) == [9, 6])
    #expect(tasks.last?.steps.map(\.id) == [6, 7, 8])
    #expect(
      tasks.last?.title(agent: "Studio AI", chatName: { $0 })
        == "Studio AI · studio: 1 message sent, 12 messages read")
    #expect(tasks.map(\.id) == ActivityPresentation.tasks((work + noise).reversed()).map(\.id))
    // Failed actions remain visible even when no messages were read.
    #expect(
      ActivityPresentation.tasks([row(10, minute: 9, tool: "list_messages", kind: "failed")]).count
        == 1)
  }

  @Test func selfCircleRequiresEitherPinFlagEvenWhenBusyOrRenamed() throws {
    var item = try #require(
      DemoCircles.home(details: DemoCircles.details, suggestions: [], proposals: [])
        .today.first { $0.circle.kind == "self" })
    item.newMessages = 120
    item.openAsks = 8
    item.circle.name = "Personal notes"
    item.pinned = false
    item.circle.pinned = false
    #expect(HomePresentation.circles([item]).isEmpty)
    item.pinned = true
    #expect(HomePresentation.circles([item]).count == 1)
    item.pinned = false
    item.circle.pinned = true
    #expect(HomePresentation.circles([item]).count == 1)
    item.circle.kind = "other"
    item.circle.name = "You"
    item.circle.pinned = false
    #expect(HomePresentation.circles([item]).count == 1)
  }

  @Test func greetingUsesLocalBandsIncludingMidnightAndBoundaries() {
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = TimeZone(identifier: "Asia/Kolkata")!
    for hour in 0..<24 {
      let date = calendar.date(
        from: DateComponents(year: 2026, month: 10, day: 19, hour: hour, minute: 50))!
      let expected =
        hour < 5 || hour >= 22
        ? "Today"
        : hour < 12
          ? "Good morning"
          : hour < 17 ? "Good afternoon" : "Good evening"
      #expect(HomePresentation.greeting(at: date, calendar: calendar) == expected)
    }
  }

  @Test func footerUsesOwnerZoneAndEventAgeIncludingDSTAndMissingEvents() {
    let now = Timestamp.date("2026-10-19T10:00:00Z")!
    let zone = TimeZone(identifier: "Asia/Kolkata")!
    let prefix = zone.abbreviation(for: now)! + " · "
    #expect(
      HomePresentation.dashboardFooter(lastSyncAt: "2026-10-19T09:59:40Z", now: now, timeZone: zone)
        == prefix + "Last sync just now")
    #expect(
      HomePresentation.dashboardFooter(lastSyncAt: "2026-10-19T09:44:00Z", now: now, timeZone: zone)
        == prefix + "Last sync 16 min ago")
    for missing in [nil, "invalid"] as [String?] {
      #expect(
        HomePresentation.dashboardFooter(lastSyncAt: missing, now: now, timeZone: zone) == prefix
          + "Waiting for sync")
    }
    let pacific = TimeZone(identifier: "America/Los_Angeles")!
    #expect(
      HomePresentation.dashboardFooter(lastSyncAt: nil, now: now, timeZone: pacific).hasPrefix(
        "PDT · "))
    #expect(
      HomePresentation.dashboardFooter(
        lastSyncAt: nil, now: Timestamp.date("2026-12-19T10:00:00Z")!, timeZone: pacific
      ).hasPrefix("PST · "))
  }

  @Test func heatmapUsesQuietStripGeometry() {
    for width in [280.0, 448, 672, 704, 760, 1200] {
      for columns in [20, 26, 52, 53] {
        let (side, visible) = HomePresentation.heatmapGeometry(
          contentWidth: width, columns: columns)
        #expect((14...16).contains(side))
        #expect(visible == min(columns, min(52, Int((width - 34) / 16))))
        #expect(36 + side * Double(visible) + Double(visible - 1) * 2 <= width + 0.001)
      }
    }
    #expect(HomePresentation.heatmapGeometry(contentWidth: 704, columns: 0).visible == 0)
  }
}
