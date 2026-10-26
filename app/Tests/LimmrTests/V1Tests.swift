import Foundation
import LimmrKit
import Testing
@testable import Limmr

@Suite("V1 helper builds and integration names") @MainActor struct V1Tests {
  @Test func sameBuildLeavesHealthyJobAlone() async {
    let service = VersionedEngine()
    let startup = EngineStartup(pause: { _ in })
    await startup.recover(using: service)
    #expect(startup.phase == .ready)
    #expect(service.events == ["probe", "compare"])
  }

  @Test func changedBuildUpdatesOnceAndRecordsOnlyAfterHealth() async {
    let service = VersionedEngine()
    service.changed = true
    var logs: [String] = []
    let startup = EngineStartup(pause: { _ in }, log: { logs.append($0) })
    service.onRemoval = { #expect(startup.phase == .updating) }
    await startup.recover(using: service)
    #expect(startup.phase == .ready)
    #expect(service.events == ["probe", "compare", "bootout", "register", "probe", "record"])
    #expect(logs.count == 2)
    await startup.recover(using: service)
    #expect(startup.phase == .ready)
    #expect(service.events.filter { $0 == "bootout" }.count == 1)
    service.onRemoval = {}
    // A later outage can recover; an old failed-attempt latch must not poison Try again.
    service.healthy = false
    await startup.recover(using: service)
    #expect(startup.phase == .ready)
    #expect(service.events.filter { $0 == "bootout" }.count == 2)
  }

  @Test func missingProcessStartsWithoutAnUpdateRestart() async {
    let service = VersionedEngine()
    service.healthy = false
    service.registration = .absent
    let startup = EngineStartup(pause: { _ in })
    await startup.recover(using: service)
    #expect(startup.phase == .newInstall)
    #expect(service.events == ["probe"])
    await startup.recover(using: service, allowInstall: true)
    #expect(startup.phase == .ready)
    #expect(service.events == ["probe", "probe", "register", "probe", "record"])
  }

  @Test(arguments: ["bootout", "register", "health", "record"])
  func failedUpdateNeverAcceptsOldHealthAndCanRetry(failure: String) async {
    let service = VersionedEngine()
    service.changed = true
    service.failure = failure
    let startup = EngineStartup(pause: { _ in })
    await startup.recover(using: service)
    #expect(startup.phase == .failed)
    #expect(!service.recorded)
    // Simulate the OLD helper still being healthy after a failed restart.
    service.healthy = true
    await startup.recover(using: service)
    #expect(startup.phase == .failed)
    service.failure = ""
    await startup.recover(using: service)
    #expect(startup.phase == .ready)
    let removals = service.events.filter { $0 == "bootout" }.count
    await startup.recover(using: service)
    #expect(service.events.filter { $0 == "bootout" }.count == removals)
  }

  @Test func receiptDetectsSameMarketingVersionButDifferentBytes() throws {
    let root = FileManager.default.temporaryDirectory.appending(path: "limmr-v1-\(UUID().uuidString)")
    try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: root) }
    let build = EngineBuildReceipt(helper: root.appending(path: "helper"), receipt: root.appending(path: "home/receipt"))
    #expect(throws: (any Error).self) { try build.needsUpdate() }
    try Data("abc".utf8).write(to: build.helper)
    try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: build.helper.path)
    #expect(try build.fingerprint() == "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")
    #expect(try build.needsUpdate()) // pre-V1: no receipt
    try build.record(build.fingerprint())
    #expect(try !build.needsUpdate())
    try Data("new helper, same version".utf8).write(to: build.helper)
    #expect(try build.needsUpdate())
  }

  @Test func renameTrimsPatchesAndUpdatesListAndSignature() async throws {
    let backend = RenameBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    store.agents = DemoData.agents
    store.selectedAgent = store.agents[0].id
    let editor = AgentNameEditor()
    editor.begin(store.agents[0].name)
    editor.draft = "  Studio AI\n"
    await editor.save { try await store.renameAgent(store.agents[0].id, name: $0) }
    #expect(!editor.editing && !editor.saving && editor.error == nil)
    #expect(await backend.calls == ["PATCH /v1/agents/agent-1"])
    #expect(await backend.body == ["name": "Studio AI"])
    #expect(store.agents[0].name == "Studio AI")
    #expect(store.currentAgent?.name == "Studio AI")
  }

  @Test func escapeDiscardsWithoutSavingAndInvalidNamesStayInline() async throws {
    let editor = AgentNameEditor()
    editor.begin("Studio AI")
    editor.draft = "Discard this"
    editor.cancel()
    await editor.save { _ in Issue.record("Cancelled name sent") }
    #expect(!editor.editing && editor.draft.isEmpty)
    for invalid in [" \n ", String(repeating: "a", count: 41)] {
      editor.begin("Studio AI")
      editor.draft = invalid
      await editor.save { _ in Issue.record("Invalid name sent") }
      #expect(editor.editing && editor.error != nil && !editor.saving)
    }
    #expect(try AgentName.normalized(String(repeating: "a", count: 40)).count == 40)
    #expect(try AgentName.normalized(" X ") == "X")
  }

  @Test(arguments: [400, 404]) func renameRouteErrorsPreserveNameAndDraft(code: Int) async {
    let backend = RenameBackend(code: code)
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    store.agents = DemoData.agents
    let old = store.agents[0]
    let editor = AgentNameEditor()
    editor.begin(old.name)
    editor.draft = "Studio AI"
    await editor.save { try await store.renameAgent(old.id, name: $0) }
    #expect(editor.editing && !editor.saving && editor.error != nil)
    #expect(editor.draft == "Studio AI")
    #expect(store.agents[0] == old)
  }

  @Test func demoRenamePersistsAndUsesValidation() async throws {
    let backend = DemoBackend()
    _ = try await backend.request("/v1/agents/agent-1", method: "PATCH", body: Wire.data(["name": " Studio AI "]))
    let read: Agent = try await backend.get("/v1/agents/agent-1")
    #expect(read.name == "Studio AI")
    do {
      _ = try await backend.request("/v1/agents/agent-1", method: "PATCH", body: Wire.data(["name": " "]))
      Issue.record("Demo accepted blank name")
    } catch BackendError.http(400) {} catch { Issue.record("Unexpected error") }
  }
}

@MainActor private final class VersionedEngine: EngineServiceManaging {
  var registration = EngineRegistration.registered
  var changed = false
  var healthy = true
  var recorded = false
  var failure = ""
  var events: [String] = []
  var onRemoval: () -> Void = {}
  func probe() async -> Bool { events.append("probe"); return healthy }
  func needsUpdate() async throws -> Bool { events.append("compare"); return changed }
  func unregister() async throws {
    events.append("bootout"); onRemoval()
    if failure == "bootout" { throw BackendError.invalidResponse }
    registration = .absent; healthy = false
  }
  func register() async throws {
    if registration != .absent { try await unregister() }
    events.append("register")
    if failure == "register" { throw BackendError.invalidResponse }
    registration = .registered; healthy = failure != "health"
  }
  func recordHealthyHelper() async throws {
    events.append("record")
    if failure == "record" { throw BackendError.invalidResponse }
    recorded = true; changed = false
  }
}

private actor RenameBackend: Backend {
  let code: Int
  var calls: [String] = []
  var body: [String: String]?
  init(code: Int = 204) { self.code = code }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    calls.append("\(method) \(path)")
    self.body = try body.map { try Wire.decode([String: String].self, from: $0) }
    if code >= 400 { throw BackendError.http(code) }
    return Data()
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { .init { $0.finish() } }
}
