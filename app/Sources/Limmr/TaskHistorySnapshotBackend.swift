import Foundation
import LimmrKit

/// Invented protocol-demotion history, with the sample app's fixed clock.
struct TaskHistorySnapshotBackend: Backend {
  let base = DemoBackend()
  static var task: TrackedTask {
    var value = TrackedTask(id: "tsk_protocol_review", title: "Review the Design Club poster")
    value.goal = "Review the poster layout before the next club meeting."
    value.status = "suggested"; value.origin = "limmr"
    value.chats = [EffortEntity(id: DemoData.personJid(3), name: "Maya Rao")]
    value.people = [EffortEntity(id: DemoData.personId(3), name: "Maya Rao")]
    value.circleIds = ["university"]
    value.createdAt = "2026-10-12T12:00:00Z"; value.updatedAt = DemoData.now
    value.summary = TaskSummaryText(text: "Maya shared the draft poster. Review the heading and meeting details.", updatedAt: DemoData.now, updatedBy: "This Mac")
    value.suggestion = TaskSuggestion(signals: ["The poster conversation may still need a review"], detectedAt: DemoData.now)
    value.history = [
      TaskHistoryEvent(at: DemoData.now, kind: "rule_protocol_demote", text: "Moved back to suggestions: Limmr made this, you never asked for it",
        changeId: "chg_demo_protocol", undoUntil: "2026-11-16T12:00:00Z", reason: "Limmr made this, you never asked for it"),
      TaskHistoryEvent(at: "2026-10-12T12:00:00Z", kind: "suggested", text: "Limmr spotted this work")
    ]
    return value
  }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if method == "GET", path == TaskClient.path(Self.task.id) { return try Wire.data(Self.task) }
    return try await base.request(path, method: method, body: body)
  }
  func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { .init { $0.finish() } }
}
