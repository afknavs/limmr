import Foundation
import Testing
@testable import Limmr
@testable import LimmrKit

@Suite("Q5 This Mac task nudges") @MainActor struct Q5NudgeTests {
  @Test func existingBackgroundWorkerEnrichesWithoutActivityOrEffortCandidates() async throws {
    let backend = NudgeBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    let environment = EffortNamingWorker.Environment(unavailableReason: { nil }, paused: { false })
    let worker = store.makeEffortNamer(environment: environment)
    worker.ready = true
    await worker.step()
    #expect(await backend.posts == 0)
    worker.enabled = true
    await worker.step()
    #expect(await backend.posts == 1)
    #expect(await backend.sent == 0)
    #expect(store.effortRevision == 0)
  }
  @Test func templateEnrichmentHasTwentySecondGateAndNoSend() async throws {
    let backend = NudgeBackend()
    let model = NudgeModel()
    let writer = OnDeviceNudgeWriter(backend: backend, model: model)
    let now = Date(timeIntervalSince1970: 1_000)
    #expect(try await writer.writeNext(now: now))
    #expect(model.calls == 1 && model.context.contains("syllabus"))
    #expect(await backend.posts == 1)
    #expect(try await !writer.writeNext(now: now.addingTimeInterval(19)))
    #expect(model.calls == 1)
    await backend.newTemplate()
    #expect(try await writer.writeNext(now: now.addingTimeInterval(20)))
    #expect(model.calls == 2)
    #expect(await backend.posts == 2)
    #expect(await backend.sent == 0)
  }
  @Test func aiAndUnknownProvenanceAreNeverGeneratedOrReplaced() async throws {
    for source in ["ai", "this-mac", ""] {
      let backend = NudgeBackend(source: source)
      let model = NudgeModel()
      let writer = OnDeviceNudgeWriter(backend: backend, model: model)
      #expect(try await !writer.writeNext(now: Date()))
      #expect(model.calls == 0)
      #expect(await backend.posts == 0)
    }
  }
  @Test func failureKeepsTemplateAndDoesNotSpin() async throws {
    let backend = NudgeBackend()
    let model = NudgeModel(); model.fails = true
    let writer = OnDeviceNudgeWriter(backend: backend, model: model)
    let now = Date()
    do { _ = try await writer.writeNext(now: now); Issue.record("Expected failure") } catch {}
    #expect(try await !writer.writeNext(now: now.addingTimeInterval(60)))
    #expect(model.calls == 1)
    #expect(await backend.posts == 0)
  }
  @Test func cancellationCannotPublishTheLateResult() async throws {
    let backend = NudgeBackend()
    let model = NudgeModel(); model.suspend = true
    let writer = OnDeviceNudgeWriter(backend: backend, model: model)
    let work = Task { try await writer.writeNext(now: Date()) }
    while model.continuation == nil { await Task.yield() }
    work.cancel(); model.continuation?.resume(); model.continuation = nil
    _ = try? await work.value
    #expect(await backend.posts == 0)
  }
  @Test func sampleDraftIsDeterministicAndAccessoryDownIsShown() async throws {
    let model = DemoTaskNudgeModel()
    let first = try await model.draft(context: "fixture")
    #expect(try await model.draft(context: "fixture") == first)
    let store = AppStore(demo: true, snapshot: true, backend: NudgeBackend())
    await store.refresh()
    #expect(await !store.testAccessoryCue())
    #expect(store.error?.contains("Accessory isn’t running") == true)
  }
}

@MainActor private final class NudgeModel: TaskNudgeModel {
  var calls = 0; var context = ""; var fails = false; var suspend = false
  var continuation: CheckedContinuation<Void, Never>?
  func draft(context: String) async throws -> String {
    calls += 1; self.context = context
    if suspend { await withCheckedContinuation { continuation = $0 } }
    if fails { throw BackendError.http(503) }
    return "Hi Maya, could you share the syllabus when you have a moment? No rush!"
  }
}
private actor NudgeBackend: Backend {
  let demo = DemoBackend()
  var source: String; var revision = 0
  var posts = 0; var sent = 0
  init(source: String = "template") { self.source = source }
  func newTemplate() { revision += 1; source = "template" }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if path == "/v1/efforts/unnamed" { return try Wire.data(EffortNamingResponse(efforts: [], total: 0)) }
    if path == "/v1/home" {
      var home = HomeResponse.empty
      var suggestion = DemoTasks.suggestions[1]
      suggestion.id += "-\(revision)"; suggestion.draftSource = source
      home.queued = [suggestion]
      return try Wire.data(home)
    }
    if path == "/v1/tasks/tsk_syllabus" {
      var task = DemoTasks.tasks[2]
      task.waiting?.nudgeSuggestionId = "task-nudge-\(revision)"
      return try Wire.data(task)
    }
    if path == "/v1/suggestions", method == "POST" {
      posts += 1; source = "this-mac"
      let data = try #require(body)
      let obj = try #require(JSONSerialization.jsonObject(with: data) as? [String: Any])
      #expect(obj["created_by"] as? String == "this-mac" && obj["task_id"] as? String == "tsk_syllabus")
      return try Wire.data(DemoTasks.tasks[2])
    }
    if path == "/v1/settings/accessory-cue/test" { return Data(#"{"ok":false,"running":false}"#.utf8) }
    if path.hasSuffix("/send") { sent += 1 }
    return try await demo.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { AsyncThrowingStream { $0.finish() } }
}
