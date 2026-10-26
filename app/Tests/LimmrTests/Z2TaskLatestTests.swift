import Foundation
import LimmrKit
import Testing
@testable import Limmr

@MainActor private final class FakeTaskSummaryModel: TaskSummaryModel {
  var available = true
  var calls = 0
  var result: String? = "Maya needs feedback on her deck. You will send notes before Friday."
  var duringGeneration: (() async -> Void)?
  func summarize(_ context: String) async throws -> String? {
    calls += 1
    #expect(context.contains("Maya Rao"))
    #expect(context.contains("Latest messages, newest first"))
    await duringGeneration?()
    return result
  }
}
private actor TaskSummaryTestBackend: Backend {
  var task: TrackedTask
  var patches = 0
  init(_ task: TrackedTask) { self.task = task }
  func setSummary(_ text: String) { task.summary = .init(text: text, updatedBy: "you") }
  func newerMessage() { task.latest[0].messageId = "newer"; task.latest[0].timestamp = "2026-10-17T12:00:00Z" }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    guard path == TaskClient.path(task.id) else { throw BackendError.http(404) }
    if method == "PATCH", let body {
      patches += 1
      struct Patch: Decodable { var summary: TaskSummaryText?; var summaryAttempt: TaskSummaryAttempt? }
      let patch = try Wire.decode(Patch.self, from: body)
      if let attempt = patch.summaryAttempt {
        guard task.summary.text.isEmpty, task.latest.first?.attempt == attempt else { throw BackendError.http(409) }
        if patch.summary == nil {
          guard task.summaryAttempt != attempt else { throw BackendError.http(409) }
          task.summaryAttempt = attempt
        } else {
          guard task.summaryAttempt == attempt else { throw BackendError.http(409) }
          task.summary = patch.summary!
        }
      }
    }
    return try Wire.data(task)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { AsyncThrowingStream { $0.finish() } }
}

@MainActor struct Z2TaskLatestTests {
  private func fixture() async throws -> TrackedTask {
    try await Z2SnapshotBackend(minimal: false, taskLatest: true).get("/v1/tasks/tsk_cv")
  }
  @Test func latestFallbackUsesTheRenderedPresentationAndSummaryPrecedence() async throws {
    var task = try await fixture()
    #expect(task.summary.text.isEmpty && task.effortId == nil && task.suggestion?.effortIds.isEmpty == true)
    #expect(TaskContextPresentation.latest(task, efforts: []).count == 3)
    #expect(TaskContextPresentation.latest(task, efforts: []).last?.text == "Document")
    var effort = DemoEfforts.efforts[0]; effort.summary = "The deck needs review."
    #expect(TaskContextPresentation.latest(task, efforts: [effort]).isEmpty)
    #expect(TaskContextPresentation.summary(task, efforts: [effort]).text == effort.summary)
    task.summary = .init(text: "Owner summary")
    #expect(TaskContextPresentation.latest(task, efforts: []).isEmpty)
    #expect(TaskContextPresentation.summary(task, efforts: [effort]).text == "Owner summary")
  }
  @Test func writerOnlyRunsForAnEmptySummaryWithMessagesAndAnAvailableModel() async throws {
    let task = try await fixture()
    for kind in ["task summary", "effort summary", "unavailable", "no messages"] {
      var value = task; let model = FakeTaskSummaryModel(); var efforts: [Effort] = []
      if kind == "task summary" { value.summary = .init(text: "Already written") }
      if kind == "effort summary" { var effort = DemoEfforts.efforts[0]; effort.summary = "Already written"; efforts = [effort] }
      if kind == "unavailable" { model.available = false }
      if kind == "no messages" { value.latest = [] }
      let backend = TaskSummaryTestBackend(value)
      let result = try await OnDeviceTaskSummaryWriter(client: .init(backend: backend), model: model).write(task: value, efforts: efforts)
      #expect(result == nil && model.calls == 0)
      #expect(await backend.patches == 0)
    }
    let model = FakeTaskSummaryModel(); let backend = TaskSummaryTestBackend(task)
    let workspace = TaskWorkspace(id: task.id, backend: backend, summaryModel: model)
    await workspace.load(); await workspace.load()
    #expect(model.calls == 1)
    #expect(workspace.task?.summary.updatedBy == "This Mac")
    #expect(workspace.task?.summary.text == model.result)
    #expect(await backend.patches == 2)
  }
  @Test func failedResultDoesNotRetryUntilANewerMessageEvenAcrossWorkspaces() async throws {
    let task = try await fixture(); let backend = TaskSummaryTestBackend(task); let model = FakeTaskSummaryModel()
    model.result = nil
    await TaskWorkspace(id: task.id, backend: backend, summaryModel: model).load()
    await TaskWorkspace(id: task.id, backend: backend, summaryModel: model).load()
    #expect(model.calls == 1)
    await backend.newerMessage()
    await TaskWorkspace(id: task.id, backend: backend, summaryModel: model).load()
    #expect(model.calls == 2)
  }
  @Test func writerCannotOverwriteAnOwnerSummaryThatArrivesDuringGeneration() async throws {
    let task = try await fixture(); let backend = TaskSummaryTestBackend(task); let model = FakeTaskSummaryModel()
    model.duringGeneration = { await backend.setSummary("Owner wrote this while the model ran.") }
    let workspace = TaskWorkspace(id: task.id, backend: backend, summaryModel: model)
    await workspace.load()
    let saved: TrackedTask = try await backend.get(TaskClient.path(task.id))
    #expect(saved.summary.updatedBy == "you")
    #expect(model.calls == 1 && workspace.error == nil)
  }
  @Test func demoAndSnapshotNeverUseTheRealModelButAllowExplicitFakes() async throws {
    #expect(TaskSummaryWriterPolicy.model(demo: true, snapshot: false) == nil)
    #expect(TaskSummaryWriterPolicy.model(demo: false, snapshot: true) == nil)
    let task = try await fixture(); let backend = TaskSummaryTestBackend(task)
    await TaskWorkspace(id: task.id, backend: backend).load()
    #expect(await backend.patches == 0)
    let fake = FakeTaskSummaryModel()
    await TaskWorkspace(id: task.id, backend: backend, summaryModel: fake).load()
    #expect(fake.calls == 1)
  }
  @Test func summaryPatchLetsTheEngineStampItsDate() throws {
    let body = try Wire.data(TaskPatch(summary: .init(text: "Review the deck. Send notes.", updatedBy: "This Mac")))
    let object = try #require(JSONSerialization.jsonObject(with: body) as? [String: Any])
    let summary = try #require(object["summary"] as? [String: String])
    #expect(summary["updated_at"] == nil)
    #expect(summary["updated_by"] == "This Mac")
  }
  @Test func latestDecodesAbsentAndNullArrays() throws {
    for json in [#"{"id":"empty"}"#, #"{"id":"empty","latest":null}"#] {
      #expect(try Wire.decode(TrackedTask.self, from: Data(json.utf8)).latest.isEmpty)
    }
  }
}
