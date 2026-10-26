import Foundation
import Testing
@testable import LimmrKit

@Suite("Y2 readable history") struct Y2HomeTests {
  @Test func calendarCellsGrowWithWidthAndMonthLabelsStayAligned() {
    let small = HomePresentation.heatmapGeometry(contentWidth: 356, columns: 20)
    let large = HomePresentation.heatmapGeometry(contentWidth: 394, columns: 20)
    #expect(small.visible == 20 && large.visible == 20)
    #expect(large.side > small.side && (14...16).contains(small.side))
    let history = StatsHistory(days: (1...31).map { .init(date: String(format: "2026-10-%02d", $0), messagesIn: 1) })
    let labels = HomePresentation.monthLabels(cells: HomePresentation.stripCells(history, weeks: 6), side: 15)
    #expect(labels.count == 1 && labels[0].title == "Oct")
    #expect(labels[0].x == 36 + Double(labels[0].column) * 17)
    #expect(HomePresentation.heatmapGeometry(contentWidth: .nan, columns: 52).visible == 0)
  }
  @Test func tooltipContainsDateMessagesAndEachAI() {
    let day = HistoryDay(date: "2026-10-21", messagesIn: 48, agentSends: 3, agentReads: 5,
      agents: [DemoData.agents[0].id: .init(reads: 5, sends: 3)])
    let label = HomePresentation.dayLabel(day, agents: DemoData.agents)
    #expect(label.hasPrefix("Wed 21 Oct · 48 messages · 3 AI sends"))
    #expect(label.contains("5 AI reads") && label.contains(DemoData.agents[0].name + ": 5 reads, 3 sends"))
  }
  @Test func dayFilterUsesLocalCalendarBoundariesAndClears() {
    let zone = TimeZone(secondsFromGMT: 19800)!
    #expect(ActivityDayFilter.matches("2026-10-20T20:00:00Z", day: "2026-10-21", timeZone: zone))
    #expect(!ActivityDayFilter.matches("2026-10-21T20:00:00Z", day: "2026-10-21", timeZone: zone))
    #expect(ActivityDayFilter.matches("anything", day: nil))
    #expect(!ActivityDayFilter.matches("bad timestamp", day: "2026-10-21"))
  }
  @Test func demoActionsReadBackKeepDismissAndUndo() async throws {
    let backend = DemoBackend(); let client = TaskClient(backend: backend)
    let home: HomeResponse = try await backend.get("/v1/home")
    #expect(home.tasks.suffix(2).allSatisfy { $0.status == "suggested" })
    #expect(TaskPresentation.suggested(home.tasks).count == 2)
    let kept = try await client.surface("tsk_cv", action: .keep)
    #expect(kept.status == "active" && kept.origin == "limmr")
    let dismissed = try await client.surface("tsk_print", action: .dismiss)
    #expect(dismissed.status == "dropped" && dismissed.closeReason == "not_a_task")
    let now = Timestamp.date(DemoData.now)!
    let auto = try await client.get("tsk_appointment")
    #expect(TaskPresentation.canUndo(auto.compact(now: now), now: now))
    #expect(!TaskPresentation.canUndo(auto.compact(now: now), now: now.addingTimeInterval(86400)))
    let undone = try await client.surface(auto.id, action: .undo)
    #expect(undone.status == "dropped" && undone.undoChangeId == nil)
    let after: HomeResponse = try await backend.get("/v1/home")
    #expect(TaskPresentation.suggested(after.tasks).isEmpty && after.tasks.contains { $0.id == kept.id })
    #expect(!after.tasks.contains { $0.id == dismissed.id || $0.id == undone.id })
  }
}
