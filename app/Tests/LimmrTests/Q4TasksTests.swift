import Foundation
import LimmrKit
import Testing
@testable import Limmr

@Suite("Q4 task actions", .serialized) @MainActor struct Q4TasksTests {
  @Test func sendEditAndSkipUseTheExistingSendGateAndTaskSkipRoute() async throws {
    let backend = Q4RecordingBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    let checkIn = try #require(store.home.queued.first { $0.kind == "check_in" })
    await store.decideSuggestion(checkIn, send: true, text: "Exact edited update.\nSecond line.", variation: 0)
    let send = try #require(await backend.mutations.first { $0.path == "/v1/suggestions/task-check-in/send" })
    #expect(send.method == "POST")
    let body = try #require(send.body)
    let request = try Wire.decode(SuggestionSendRequest.self, from: body)
    #expect(request.text == "Exact edited update.\nSecond line." && request.variation == 0)
    let study = try await TaskClient(backend: backend).get("tsk_study")
    #expect(study.updatesSent.last?.textPreview == request.text && !study.needsYou)
    // Repeated clicks must not create a second delivery attempt.
    await store.decideSuggestion(checkIn, send: true)
    #expect(await backend.mutations.filter { $0.path.hasSuffix("/send") }.count == 1)
    let nudge = try #require(store.home.queued.first { $0.kind == "nudge" })
    await store.skipTaskSuggestion(nudge)
    #expect(await backend.mutations.contains { $0.path == "/v1/tasks/tsk_syllabus/nudge/skip" && $0.method == "POST" })
    #expect(!store.home.queued.contains { $0.id == nudge.id })
  }
  @Test func skipCheckInDismissesWithoutSendingAndNavigationUsesActivityStack() async throws {
    let backend = Q4RecordingBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    let item = try #require(store.home.queued.first { $0.kind == "check_in" })
    await store.skipTaskSuggestion(item)
    #expect(await backend.mutations.contains { $0.path == "/v1/suggestions/task-check-in/dismiss" })
    #expect(await backend.mutations.allSatisfy { !$0.path.hasSuffix("/send") })
    store.openTask("tsk_study")
    #expect(store.page == .activity && store.activityPath.isEmpty && store.pendingActivityRoute == .task("tsk_study") && store.canGoBack)
    store.route(.back)
    #expect(store.activityPath.isEmpty)
  }
  @Test func accessoryDefaultsOffSavesThroughSettingsAndTestsThroughControlOnly() async throws {
    let backend = Q4RecordingBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    #expect(!store.settings.accessoryCue.enabled)
    store.editAccessoryCue { $0.enabled = true; $0.color = "#64a8d1" }
    await store.waitForSettingsSave()
    let settings: AppSettings = try await backend.get("/v1/settings")
    #expect(settings.accessoryCue.enabled && settings.accessoryCue.color == "#64a8d1")
    #expect(await store.testAccessoryCue())
    #expect(await backend.mutations.contains { $0.path == "/v1/settings" && $0.method == "PUT" })
    #expect(await backend.mutations.contains { $0.path == "/v1/settings/accessory-cue/test" && $0.method == "POST" })
    #expect(await backend.mutations.allSatisfy { $0.path.hasPrefix("/v1/") })
  }
  @Test func taskClientUsesContractMethodsAndReadbackAndKeepsEditsOnFailure() async throws {
    let backend = Q4RecordingBackend()
    let model = TaskWorkspace(id: "tsk_study", backend: backend)
    await model.load()
    await model.change { try await model.client.step(model.id, step: "s3", done: true) }
    #expect(model.task?.progress.done == 3)
    #expect(await backend.mutations.contains { $0.path == "/v1/tasks/tsk_study/checklist/s3" && $0.method == "PATCH" })
    await backend.failNext()
    await model.change { try await model.client.patch(model.id, .init(status: "done")) }
    #expect(model.error != nil && model.task?.status == "active" && !model.saving)
    let task = try await TaskClient(backend: backend).create(.init(effort: DemoEfforts.efforts[2]))
    #expect(task.effortId == DemoEfforts.efforts[2].id)
    #expect(await backend.mutations.contains { $0.path == "/v1/tasks" && $0.method == "POST" })
  }
}

private actor Q4RecordingBackend: Backend {
  struct Mutation: Sendable { var path: String; var method: String; var body: Data? }
  var mutations: [Mutation] = []
  private var fail = false
  private let demo = DemoBackend()
  func failNext() { fail = true }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if method != "GET" {
      mutations.append(.init(path: path, method: method, body: body))
      if fail { fail = false; throw BackendError.http(503) }
    }
    return try await demo.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { AsyncThrowingStream { $0.finish() } }
}
