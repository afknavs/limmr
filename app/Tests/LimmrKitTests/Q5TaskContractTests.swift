import Foundation
import Testing
@testable import LimmrKit

@Suite struct Q5TaskContractTests {
  private func fixture<T: Decodable>(_ name: String, _ type: T.Type) throws -> T {
    let path = URL(filePath: #filePath).deletingLastPathComponent().appending(path: "Fixtures/tasks/\(name).json")
    return try Wire.decode(type, from: Data(contentsOf: path))
  }
  @Test func realEngineResponsesDecode() throws {
    let detail = try fixture("detail", TrackedTask.self)
    #expect(detail.id == "tsk_demo_study" && detail.title == "Maya's study plan")
    #expect(detail.progress == .init(done: 1, total: 2))
    #expect(detail.checkIn?.rule == "weekly" && detail.checkIn?.note == "Learning progress")
    #expect(detail.summary.text == "Maya has started revision.")
    #expect(detail.items.contains { $0.kind == "note" && $0.text == "Review memory exercises" })
    #expect(detail.createdBy.agentId == "agent-2" && detail.needsYou)
    let waiting = try fixture("waiting", TrackedTask.self)
    #expect(waiting.waiting?.on?.name == "Maya Rao" && waiting.waiting?.nudgeState == "queued")
    #expect(waiting.waiting?.nudgeSuggestionId?.contains("tsk_demo_waiting") == true)
    let short = try fixture("short", TrackedTask.self)
    #expect(short.kind == "short" && short.lifespanDays == 14)
    let list = try fixture("list", TasksResponse.self)
    let home = try fixture("home", HomeResponse.self)
    #expect(list.tasks.count == 4 && home.tasks == list.tasks)
    #expect(home.tasks.contains { $0.waiting?.days == 4 && $0.needsYou })
    struct Suggestions: Decodable { var suggestions: [CircleSuggestion] }
    let suggestions = try fixture("suggestion", Suggestions.self).suggestions
    #expect(suggestions.count == 2 && suggestions == home.queued)
    #expect(Set(suggestions.map(\.kind)) == ["check_in", "nudge"])
    #expect(suggestions.first { $0.kind == "nudge" }?.draftSource == "template")
    #expect(suggestions.first { $0.kind == "check_in" }?.draftSource == "ai")
    let settings = try fixture("settings", AppSettings.self)
    #expect(!settings.accessoryCue.enabled && settings.accessoryCue.quietHours?.timezone == "Asia/Kolkata")
    let encoded = try #require(JSONSerialization.jsonObject(with: Wire.data(settings)) as? [String: Any])
    let cue = try #require(encoded["accessory_cue"] as? [String: Any])
    #expect((cue["quiet_hours"] as? [String: Any])?["timezone"] as? String == "Asia/Kolkata")
    let feed = try fixture("feed", TaskFeedResponse.self)
    #expect(feed.tasks.count == 4)
    #expect(feed.tasks.contains { $0.events.contains { $0.kind == "nudge_queued" } })
    let effort = try fixture("effort", EffortDetailResponse.self)
    #expect(effort.taskId == "tsk_demo_linked")
  }
  @Test func cardLabelIncludesAttentionProgressAndSchedule() {
    let task = DemoTasks.tasks[0].compact(now: Timestamp.date(DemoData.now)!)
    let label = TaskPresentation.accessibilityLabel(task)
    #expect(label.contains("Open Noor") && label.contains("Needs you") && label.contains("2 of 4 steps") && label.contains("Next check-in"))
    #expect(TaskPresentation.accessibilityLabel(DemoTasks.tasks[2].compact(now: Timestamp.date(DemoData.now)!)).contains("Waiting on Maya"))
  }
}
