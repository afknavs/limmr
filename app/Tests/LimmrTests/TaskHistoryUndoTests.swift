import Foundation
import LimmrKit
import Testing
@testable import Limmr

@Suite("Task history undo", .serialized) @MainActor struct TaskHistoryUndoViewTests {
  @Test func undoPostsRefreshesTaskAndMarksOriginalLineUndone() async throws {
    let backend = HistoryUndoBackend()
    let workspace = TaskWorkspace(id: "demo", backend: backend)
    await workspace.load()
    let event = try #require(workspace.task?.history.first)
    #expect(workspace.task?.status == "suggested")
    #expect(event.canUndo(now: Timestamp.date(DemoData.now)!))
    await workspace.undoChange(event, now: Timestamp.date(DemoData.now)!)
    #expect(await backend.calls == ["GET /v1/tasks/demo", "POST /v1/tasks/demo/changes/chg_demo/undo", "GET /v1/tasks/demo"])
    #expect(workspace.task?.status == "active")
    #expect(workspace.task?.history.first?.undone == true)
    #expect(workspace.task?.history.first?.canUndo(now: Timestamp.date(DemoData.now)!) == false)
    #expect(workspace.historyErrors.isEmpty && !workspace.saving)
    await workspace.undoChange(try #require(workspace.task?.history.first), now: Timestamp.date(DemoData.now)!)
    #expect(await backend.calls.count == 3)
  }

  @Test func undoErrorStaysOnTheLineAndLeavesTaskAlone() async throws {
    let backend = HistoryUndoBackend(fail: true)
    let workspace = TaskWorkspace(id: "demo", backend: backend)
    await workspace.load()
    let before = try #require(workspace.task)
    await workspace.undoChange(try #require(before.history.first), now: Timestamp.date(DemoData.now)!)
    #expect(workspace.task == before)
    #expect(workspace.historyErrors["chg_demo"] == "The operation conflicts with the current state.")
    #expect(workspace.error == nil && !workspace.saving)
  }

  @Test(arguments: [TaskSurfacingAction.keep, .dismiss])
  func openedSuggestionCanBeKeptOrDismissed(_ action: TaskSurfacingAction) async throws {
    let workspace = TaskWorkspace(id: "tsk_cv", backend: DemoBackend())
    await workspace.load()
    #expect(workspace.task?.status == "suggested")
    await workspace.resolveSuggestion(action)
    #expect(workspace.task?.status == (action == .keep ? "active" : "dropped"))
    #expect(workspace.error == nil && !workspace.saving)
  }
}

private actor HistoryUndoBackend: Backend {
  var calls: [String] = []
  var fail: Bool
  var value: TrackedTask
  init(fail: Bool = false) {
    self.fail = fail
    value = TrackedTask(id: "demo", title: "Review poster", status: "suggested", origin: "limmr", suggestion: .init(detectedAt: DemoData.now))
    value.history = [.init(at: DemoData.now, kind: "rule_protocol_demote", text: "Moved back to suggestions: Limmr made this, you never asked for it", changeId: "chg_demo", undoUntil: "2026-11-16T12:00:00Z", reason: "Limmr made this, you never asked for it")]
  }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    calls.append(method + " " + path)
    if method == "POST" {
      if fail { throw BackendError.unavailable("The operation conflicts with the current state.") }
      value.status = "active"; value.origin = "owner"; value.suggestion = nil
      value.history[0].undone = true
    }
    return try Wire.data(value)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { .init { $0.finish() } }
}
