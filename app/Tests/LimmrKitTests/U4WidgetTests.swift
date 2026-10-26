import Foundation
import Testing

@testable import LimmrKit

@Suite("U4 widgets") struct U4WidgetTests {
  @Test func historyDecodesAndPeriodsChangeValues() throws {
    let history = try Wire.decode(
      StatsHistory.self,
      from: Data(
        #"{"days":[{"date":"2026-10-17","messages_in":7,"messages_out":2,"agent_sends":1,"agent_reads":3},{"date":"2026-10-18","messages_in":1,"messages_out":0,"agent_sends":0,"agent_reads":1,"agents":{"ai":{"reads":1,"sends":0}}}]}"#
          .utf8))
    #expect(history.period(1).reduce(0) { $0 + $1.messages } == 1)
    #expect(history.period(7).reduce(0) { $0 + $1.messages } == 10)
    let demo = DemoData.history()
    let totals = [1, 7, 30].map { days in demo.period(days).reduce(0) { $0 + $1.messages } }
    #expect(Set(totals).count == 3)
    #expect(history.days.last?.agents?["ai"]?.reads == 1)
  }
  @Test func heatmapAlignsMondayAndPadsWithoutInventedDays() {
    let h = StatsHistory(days: [
      .init(date: "2026-10-17", messagesIn: 1), .init(date: "2026-10-18", messagesIn: 4),
    ])
    #expect(h.heatmap.count == 7)
    #expect(h.heatmap.prefix(5).allSatisfy { $0 == nil })
    #expect(h.heatmap[5]?.messages == 1 && h.heatmap[6]?.messages == 4)
    #expect(StatsHistory().heatmap.isEmpty)
    #expect(StatsHistory.intensity(0, maximum: 4) == 0)
    #expect(StatsHistory.intensity(1, maximum: 4) < StatsHistory.intensity(4, maximum: 4))
  }
  private func row(
    _ id: Int64, _ at: String, chat: String = "one", agent: String = "ai", kind: String = "sent",
    tool: String = "send_message"
  ) -> Activity {
    .init(
      id: id, at: at, agentId: agent, tool: tool, kind: kind, chatJids: [chat], messageIds: [],
      count: 1)
  }
  @Test func tasksMergeRelatedActionsAndSplitAgentScopeTimeAndDay() {
    let rows = [
      row(1, "2026-10-17T10:00:00Z", kind: "read", tool: "list_messages"),
      row(2, "2026-10-17T10:02:00Z", kind: "media", tool: "upload_attachment"),
      row(3, "2026-10-17T10:03:00Z"), row(4, "2026-10-17T10:04:00Z", chat: "two"),
      row(5, "2026-10-17T10:05:00Z", agent: "other"), row(6, "2026-10-17T11:00:00Z"),
      row(7, "2026-10-18T00:00:00Z"),
    ]
    let tasks = ActivityPresentation.tasks(rows + [rows[0]])
    #expect(tasks.count == 5)
    #expect(tasks.map(\.id) == ActivityPresentation.tasks(rows.reversed()).map(\.id))
    let combined = tasks.first { $0.id == 1 }!
    #expect(combined.steps.map(\.id) == [1, 2, 3])
    #expect(
      combined.title(agent: "Studio AI", chatName: { _ in "Design Club" })
        == "Studio AI · Design Club: 1 message sent, 1 file uploaded, 1 message read")
  }
  @Test func circleUpdatesHideQuietUnpinnedEvenWithAnUpcomingDate() {
    var item = DemoCircles.home(details: DemoCircles.details, suggestions: [], proposals: []).today[0]
    item.circle.pinned = false; item.pinned = false; item.newMessages = 0; item.openAsks = 0; item.upcoming = 1
    #expect(HomePresentation.circles([item]).isEmpty)
    item.circle.pinned = true
    #expect(HomePresentation.circles([item]).count == 1)
    item.circle.pinned = false; item.newMessages = 1
    #expect(HomePresentation.circles([item]).count == 1)
  }
  @Test func iconsAreOptionalAndRoundTrip() throws {
    var person = try Wire.decode(
      Person.self,
      from: Data(#"{"id":"fixture","name":"Fixture","icon":{"kind":"emoji","value":"🎓"}}"#.utf8))
    #expect(person.icon?.value == "🎓")
    person = try Wire.decode(Person.self, from: Wire.data(person))
    #expect(person.icon?.kind == "emoji")
    let old = try Wire.decode(Person.self, from: Data(#"{"id":"fixture","name":"Fixture"}"#.utf8))
    #expect(old.icon == nil)
  }
}
