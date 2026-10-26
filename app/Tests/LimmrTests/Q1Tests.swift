import Foundation
import LimmrKit
import Testing
@testable import Limmr

@Suite("Q1 safe remote verification") @MainActor struct Q1Tests {
  @Test(arguments: [false, true])
  func timeoutRestoresExactSnapshot(wasEnabled: Bool) async throws {
    let backend = Q1Backend(enabled: wasEnabled, url: "https://previous.example")
    let wizard = RemoteWizardTransaction(backend: backend)
    let before = try await wizard.snapshot()
    #expect(await wizard.run(before: before, url: "https://wizard.example") { false } == .restored)
    #expect(try await wizard.snapshot().config == before.config)
    #expect(await backend.writeCount() == 2)
  }
  @Test(arguments: ["failure", "cancel"])
  func thrownFailureRestoresSnapshot(reason: String) async throws {
    let backend = Q1Backend(enabled: true, url: "https://previous.example")
    let wizard = RemoteWizardTransaction(backend: backend)
    let before = try await wizard.snapshot()
    let result = await wizard.run(before: before, url: "https://wizard.example") {
      if reason == "cancel" { throw CancellationError() }
      throw BackendError.invalidResponse
    }
    #expect(result == .restored)
    #expect(try await wizard.snapshot().config == before.config)
  }
  @Test(arguments: ["url", "enabled"])
  func concurrentChangesAreLeftAlone(change: String) async throws {
    let backend = Q1Backend(enabled: false, url: "https://previous.example")
    let wizard = RemoteWizardTransaction(backend: backend)
    let before = try await wizard.snapshot()
    let expected = RemoteWizardConfig(enabled: change == "url", publicUrl: change == "url" ? "https://other.example" : "https://wizard.example")
    let result = await wizard.run(before: before, url: "https://wizard.example") {
      await backend.change(expected)
      return false
    }
    #expect(result == .changed)
    #expect(try await wizard.snapshot().config == expected)
    #expect(await backend.writeCount() == 1)
  }
  @Test(arguments: [false, true])
  func approvedClientPreventsRollbackUntilExplicitTurnOff(alreadyApproved: Bool) async throws {
    let backend = Q1Backend(enabled: false, url: "")
    let wizard = RemoteWizardTransaction(backend: backend)
    if alreadyApproved { await backend.approve() }
    let before = try await wizard.snapshot()
    let result = await wizard.run(before: before, url: "https://wizard.example") {
      if !alreadyApproved { await backend.approve() }
      return false
    }
    #expect(result == .clients)
    #expect(try await wizard.snapshot().enabled)
    #expect(await backend.writeCount() == 1)
    try await wizard.turnOff()
    let after = try await wizard.snapshot()
    #expect(!after.enabled && after.publicUrl == "https://wizard.example")
    #expect(after.clients.count == 1)
  }
  @Test(arguments: ["change", "approve"])
  func raceBetweenReadAndRollbackIsRejected(race: String) async throws {
    let backend = Q1Backend(enabled: false, url: "")
    let wizard = RemoteWizardTransaction(backend: backend)
    let before = try await wizard.snapshot()
    let result = await wizard.run(before: before, url: "https://wizard.example") {
      await backend.raceOnRestore(race)
      return false
    }
    #expect(result == (race == "change" ? .changed : .clients))
    let after = try await wizard.snapshot()
    #expect(after.enabled)
    #expect(after.publicUrl == (race == "change" ? "https://other.example" : "https://wizard.example"))
  }
  @Test func staleSnapshotCannotOverwriteNewerSetup() async throws {
    let backend = Q1Backend(enabled: false, url: "")
    let wizard = RemoteWizardTransaction(backend: backend)
    let before = try await wizard.snapshot()
    await backend.change(.init(enabled: true, publicUrl: "https://other.example"))
    let result = await wizard.run(before: before, url: "https://wizard.example") {
      Issue.record("Verification ran after a stale write")
      return true
    }
    #expect(result == .changed)
    #expect(try await wizard.snapshot().publicUrl == "https://other.example")
  }
  @Test func sheetCancellationAwaitsIndependentRestore() async throws {
    let backend = Q1Backend(enabled: true, url: "https://previous.example")
    let wizard = RemoteWizardTransaction(backend: backend)
    let before = try await wizard.snapshot()
    let gate = TestGate()
    let task = Task {
      await wizard.run(before: before, url: "https://wizard.example") {
        await gate.enter()
        try Task.checkCancellation()
        return true
      }
    }
    await gate.waitUntilEntered()
    task.cancel()
    await gate.release()
    #expect(await task.value == .restored)
    #expect(try await wizard.snapshot().config == before.config)
  }
  @Test func successKeepsIntendedConfiguration() async throws {
    let backend = Q1Backend(enabled: false, url: "")
    let wizard = RemoteWizardTransaction(backend: backend)
    let before = try await wizard.snapshot()
    #expect(await wizard.run(before: before, url: "https://wizard.example") { true } == .ready)
    #expect(try await wizard.snapshot().enabled)
    #expect(await backend.writeCount() == 1)
  }
  @Test func warnsBeforeChangingAddressWithTokenHolders() throws {
    let status = try Wire.decode(RemoteWizardStatus.self, from: Data(#"{"enabled":true,"public_url":"https://old.example","clients":[{"name":"Example AI"}],"clients_with_tokens":1,"clients_on_other_url":0}"#.utf8))
    #expect(status.needsAddressWarning(target: "https://new.example"))
    #expect(status.needsAddressWarning(target: nil))
    #expect(!status.needsAddressWarning(target: "https://old.example"))
    let other = try Wire.decode(RemoteWizardStatus.self, from: Data(#"{"enabled":true,"public_url":"https://old.example","clients":[],"clients_with_tokens":0,"clients_on_other_url":1}"#.utf8))
    #expect(other.needsAddressWarning(target: "https://new.example"))
  }
  @Test func cleanupFailureDoesNotRetryUnconditionally() async throws {
    let backend = Q1Backend(enabled: false, url: "")
    let wizard = RemoteWizardTransaction(backend: backend)
    let before = try await wizard.snapshot()
    let result = await wizard.run(before: before, url: "https://wizard.example") {
      await backend.failRestore()
      return false
    }
    #expect(result == .unknown)
    #expect(try await wizard.snapshot().enabled)
    #expect(await backend.writeCount() == 2)
  }
}

private actor Q1Backend: Backend {
  private var config: RemoteWizardConfig
  private var clients: [String] = []
  private var writes = 0
  private var race = ""
  private var fail = false
  init(enabled: Bool, url: String) { config = .init(enabled: enabled, publicUrl: url) }
  func change(_ config: RemoteWizardConfig) { self.config = config }
  func approve() { clients.append("Example AI") }
  func raceOnRestore(_ value: String) { race = value }
  func failRestore() { fail = true }
  func writeCount() -> Int { writes }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    try Task.checkCancellation()
    #expect(path == "/v1/remote")
    if method == "POST" {
      writes += 1
      let write = try Wire.decode(Q1Write.self, from: body!)
      if write.requireNoClients {
        if race == "change" { config = .init(enabled: true, publicUrl: "https://other.example") }
        if race == "approve" { approve() }
        if fail { throw BackendError.invalidResponse }
      }
      guard config == write.onlyIf, !write.requireNoClients || clients.isEmpty else { throw BackendError.invalidResponse }
      config = .init(enabled: write.enabled, publicUrl: write.publicUrl)
    }
    struct Status: Encodable {
      var enabled: Bool
      var publicUrl: String
      var clients: [[String: String]]
    }
    return try Wire.data(Status(enabled: config.enabled, publicUrl: config.publicUrl, clients: clients.map { ["name": $0] }))
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { AsyncThrowingStream { $0.finish() } }
}
private struct Q1Write: Decodable {
  var enabled: Bool
  var publicUrl: String
  var onlyIf: RemoteWizardConfig
  var requireNoClients: Bool
}
