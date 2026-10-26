import Foundation
import LimmrKit
import Testing
@testable import Limmr

@Suite("R3 startup progress") @MainActor struct R3Tests {
  @Test func eightSecondListenerIsNeverRestarted() async throws {
    let driver = R3Driver()
    driver.elapsed = 0
    let registration = HelperRegistration(driver: driver, pause: { _ in driver.elapsed += 0.25 })
    try await registration.register()
    #expect(driver.elapsed >= 8)
    #expect(driver.launches == 1 && driver.removals == 1)
    #expect(await driver.healthy())
  }
  @Test func alreadyRunningYoungProcessIsAdopted() async throws {
    let driver = R3Driver()
    driver.current = driver.candidate
    driver.elapsed = 1
    try await HelperRegistration(driver: driver, pause: { _ in driver.elapsed += 0.25 }).register()
    #expect(driver.elapsed >= 8)
    #expect(driver.launches == 0 && driver.removals == 0)
  }
  @Test func startingHealthMayProgressForMoreThanThirtySeconds() async throws {
    let driver = R3Driver()
    driver.readyAt = 65
    driver.reportsStages = true
    try await HelperRegistration(driver: driver, pause: { _ in driver.elapsed += 0.25 }).register()
    #expect(driver.elapsed >= 65)
    #expect(driver.launches == 1 && driver.removals == 1)
  }
  @Test(arguments: ["crash", "stall", "wrong-build"])
  func failedCandidateRestoresPreviousHelper(failure: String) async {
    let driver = R3Driver()
    driver.current = driver.previous
    driver.failure = failure
    do {
      try await HelperRegistration(driver: driver, pause: { _ in driver.elapsed += 0.25 }).register()
      Issue.record("Failed candidate accepted")
    } catch {}
    #expect(driver.current == driver.previous)
    #expect(driver.launches == 2 && driver.removals == 2)
    #expect(await driver.healthy())
  }
  @Test func spawnWindowImageIsNotAFailedUpdate() async throws {
    let driver = R3Driver()
    driver.current = driver.previous
    driver.execGap = 0.5
    try await HelperRegistration(driver: driver, pause: { _ in driver.elapsed += 0.25 }).register()
    #expect(driver.current == driver.candidate)
    #expect(driver.launches == 1 && driver.removals == 1)
  }
  @Test func rollbackSurvivesSpawnWindowImage() async {
    let driver = R3Driver()
    driver.current = driver.previous
    driver.failure = "crash"
    driver.execGap = 0.5
    do {
      try await HelperRegistration(driver: driver, pause: { _ in driver.elapsed += 0.25 }).register()
      Issue.record("Crashed candidate accepted")
    } catch let error as HelperUpdateFailure {
      #expect(!error.localizedDescription.contains("previous helper could not restart"))
    } catch {}
    #expect(driver.current == driver.previous)
    #expect(await driver.healthy())
  }
  @Test func storeKeepsStartingResponsePendingWithoutLoadFailure() async {
    let backend = R3Backend()
    let gate = TestGate()
    let store = AppStore(demo: false, snapshot: true, backend: backend,
      retryPause: { _ in await gate.enter() })
    let refresh = Task { await store.refresh() }
    await gate.waitUntilEntered()
    #expect(store.refreshing)
    #expect(store.status == nil)
    #expect(store.loadFailures.isEmpty)
    await backend.finishStartup()
    await gate.release()
    await refresh.value
    #expect(!store.refreshing && store.status != nil)
    #expect(store.loadFailures.isEmpty)
  }
  @Test func initialAppRecoveryWaitsWithoutRegistering() async {
    let service = R3EngineService()
    let startup = EngineStartup(pause: { _ in service.elapsed += 0.25 })
    await startup.recover(using: service)
    #expect(startup.phase == .ready)
    #expect(service.elapsed >= 8)
    #expect(service.registrations == 0)
  }
  @Test func cancellationDoesNotRollBackProgressingCandidate() async {
    let driver = R3Driver()
    driver.current = driver.previous
    do {
      try await HelperRegistration(driver: driver, pause: { _ in throw CancellationError() }).register()
      Issue.record("Cancellation ignored")
    } catch is CancellationError {} catch { Issue.record("Unexpected failure") }
    #expect(driver.current == driver.candidate)
    #expect(driver.launches == 1 && driver.removals == 1)
  }
  @Test func temporaryMissingHealthPreservesLastProgressDeadline() async throws {
    var elapsed: TimeInterval = 0
    let ready = try await HelperReadinessWait.ready(probe: {
      if elapsed >= 40 { return .ready }
      if elapsed >= 26 { return .starting(progress: nil, grace: 0) }
      return .starting(progress: elapsed < 20 ? "first:1" : "second:2")
    }, pause: { _ in elapsed += 0.25 })
    #expect(ready && elapsed == 40)
  }
  @Test func repeatedPollsDoNotExtendStartupDeadline() async {
    var probes = 0
    do {
      _ = try await HelperReadinessWait.ready(probe: {
        probes += 1
        return .starting(progress: "migration.messages:4")
      }, pause: { _ in })
      Issue.record("Stalled initialization accepted")
    } catch {}
    #expect(probes == 121)
  }
}

@MainActor private final class R3Driver: HelperRegistrationDriver {
  let candidate = HelperIdentity(sha256: "candidate", codeHash: "candidate", developerID: true)
  let previous = HelperIdentity(sha256: "previous", codeHash: "previous", developerID: false)
  var current: HelperIdentity?
  var elapsed: TimeInterval = 0
  var readyAt: TimeInterval = 8
  var reportsStages = false
  var failure = ""
  var execGap: TimeInterval = 0 // launchd's xpcproxy window before exec
  var launches = 0, removals = 0
  var registration = EngineRegistration.registered
  func healthy() async -> Bool { await readiness() == .ready }
  func readiness() async -> HelperReadiness {
    guard let current else { return .unavailable }
    if current == previous { return .ready }
    if failure == "crash", elapsed >= 2 { self.current = nil; return .unavailable }
    if failure != "stall", elapsed >= readyAt { return .ready }
    return .starting(progress: reportsStages ? "warmup-\(Int(elapsed / 20))" : "process-42")
  }
  func identity() async throws -> HelperIdentity { candidate }
  func retain(_ identity: HelperIdentity) async throws {}
  func job() async throws -> HelperJob? {
    guard let current else { return nil }
    let hash = elapsed < execGap ? "xpcproxy"
      : failure == "wrong-build" && current == candidate ? "unexpected" : current.codeHash
    return HelperJob(route: .userAgent, pid: current == previous ? 41 : 42, codeHash: hash,
      executable: nil, age: elapsed)
  }
  func prepare(_ identity: HelperIdentity, route: HelperRoute) async throws -> HelperLaunchPlan {
    .init(identity: identity, route: route, plist: nil)
  }
  func backup(_ job: HelperJob) async throws -> HelperLaunchPlan { .init(identity: previous, route: .userAgent, plist: nil) }
  func remove() async throws { removals += 1; current = nil }
  func launch(_ plan: HelperLaunchPlan) async throws { launches += 1; current = plan.identity; elapsed = 0 }
}

@MainActor private final class R3EngineService: EngineServiceManaging {
  var registration = EngineRegistration.registered
  var elapsed: TimeInterval = 0
  var registrations = 0
  func readiness() async -> HelperReadiness { elapsed >= 8 ? .ready : .starting(progress: "process-42") }
  func probe() async -> Bool { elapsed >= 8 }
  func needsUpdate() async throws -> Bool { false }
  func recordHealthyHelper() async throws {}
  func unregister() async throws { Issue.record("Starting helper unregistered") }
  func register() async throws { registrations += 1 }
}

private actor R3Backend: Backend {
  private var starting = true
  private let backend = DemoBackend()
  func finishStartup() { starting = false }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if starting { throw BackendError.starting(progress: "warmup.efforts:8") }
    return try await backend.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { AsyncThrowingStream { $0.finish() } }
}
