import Foundation
import Testing
@testable import LimmrKit

@Suite struct TaskProtocolTests {
  @Test func openAndAttentionExcludeSuggestionsAndKeepQuiet() {
    let active = TaskSummary(id: "fair", title: "Science fair", needsYou: true)
    let waiting = TaskSummary(id: "reply", title: "Waiting for Mira", status: "waiting")
    let quiet = TaskSummary(id: "revision", title: "Revision plan", status: "quiet")
    let suggested = TaskSummary(id: "guide", title: "Print the guide", status: "suggested", needsYou: true)
    let ended = TaskSummary(id: "done", title: "Finished review", status: "done")
    let tasks = [active, waiting, quiet, suggested, ended]
    let open = TaskListPresentation.open(tasks)
    #expect(open.map(\.id) == ["fair", "reply", "revision"])
    #expect(open.filter(\.needsYou).count == 1)
    #expect(Set(TaskListPresentation.active(tasks).map(\.id)) == ["fair", "reply"])
    #expect(TaskPresentation.suggested(tasks).map(\.id) == ["guide"])
    #expect(TaskListPresentation.history(tasks).map(\.id) == ["done"])
  }
}
