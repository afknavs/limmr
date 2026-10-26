import Foundation
import LimmrKit
import Testing
@testable import Limmr

@Suite("P2 helper recovery and pairing") @MainActor struct P2Tests {
  @Test func healthyRegisteredJobIsNeverTouched() async {
    let service = FakeEngineService(healthy: true)
    let startup = EngineStartup(pause: { _ in })
    await startup.recover(using: service)
    #expect(startup.phase == .ready)
    #expect(service.events == ["status", "probe"])
  }
  @Test func firstInstallIsDistinctFromUnreachableRegisteredHelper() async {
    let service = FakeEngineService(registered: false)
    let startup = EngineStartup(pause: { _ in })
    await startup.recover(using: service)
    #expect(startup.phase == .newInstall)
    #expect(!service.events.contains("register"))
    service.healthyAfterRegister = true
    await startup.recover(using: service, allowInstall: true)
    #expect(startup.phase == .ready)
    #expect(service.events.filter { $0 == "register" }.count == 1)
  }
  @Test func updateFailureAwaitsUnregisterAndRetriesWithBackoff() async {
    let service = FakeEngineService()
    service.failRegistrations = 1
    service.healthyAfterRegister = true
    var phases: [EngineStartup.Phase] = []
    var waits: [Duration] = []
    let startup = EngineStartup(pause: { waits.append($0) })
    service.onUnregister = { phases.append(startup.phase) }
    await startup.recover(using: service)
    #expect(startup.phase == .ready)
    #expect(phases == [.restarting])
    #expect(service.events.filter { ["unregister-begin", "unregister-end", "register"].contains($0) }
      == ["unregister-begin", "unregister-end", "register", "register"])
    // Two launch re-probes before treating a registered engine as down, then registration backoff.
    #expect(waits == [.milliseconds(500), .milliseconds(500), .milliseconds(250), .milliseconds(250)])
  }
  @Test func spawnFailureIsNotMistakenForSuccessfulRegistration() async {
    let service = FakeEngineService()
    var waits: [Duration] = []
    let startup = EngineStartup(pause: { waits.append($0) })
    await startup.recover(using: service)
    #expect(startup.phase == .failed)
    #expect(service.events.filter { $0 == "register" }.count == 3)
    #expect(service.events.filter { $0 == "unregister-end" }.count == 3)
    #expect(waits.count == 16)  // includes the two launch re-probes
    #expect(waits.contains(.milliseconds(500)))
    service.healthyAfterRegister = true
    await startup.recover(using: service)
    #expect(startup.phase == .ready)
  }
  @Test func failedRemovalNeverRegistersOverExistingJob() async {
    let service = FakeEngineService()
    service.failUnregister = true
    let startup = EngineStartup(pause: { _ in })
    await startup.recover(using: service)
    #expect(startup.phase == .failed)
    #expect(!service.events.contains("register"))
    #expect(service.events.filter { $0 == "unregister-begin" }.count == 3)
  }
  @Test func needsApprovalDoesNotLoop() async {
    let service = FakeEngineService()
    service.needsApproval = true
    let startup = EngineStartup(pause: { _ in })
    await startup.recover(using: service)
    #expect(startup.phase == .failed)
    #expect(!service.events.contains("register"))
  }
  @Test func pairingPollOpensClosedWindowWithoutActivityOrRefresh() async throws {
    let store = AppStore(demo: false, backend: EmptyBackend())
    var state: RemoteState = try await DemoBackend().get("/v1/remote")
    let pending = state.pendingCodes
    state.pendingCodes = []
    let review = RemotePairingReview(isActive: { false }, notify: { _, _ in }, clearNotifications: { _ in })
    var opens = 0, polls = 0
    var virtualTime: Duration = .zero
    review.openMainWindow = { opens += 1 }
    await store.monitorRemotePairings(review: review, fetch: {
      polls += 1
      if polls == 2 { state.pendingCodes = pending }
      return state
    }, pause: { duration in
      if polls == 3 { throw CancellationError() }
      virtualTime += duration
    })
    #expect(polls == 3)
    #expect(opens == 1)
    #expect(virtualTime == .seconds(2))
    #expect(store.connectKind == .chatgpt)
    #expect(store.showSettings)
    #expect(store.status == nil && store.agents.isEmpty)
    #expect(review.selectedCode == pending.first?.code)
  }
  @Test func pairingQueuesBehindExistingSheetThenRechecksSamePayload() async throws {
    let store = AppStore(demo: false, backend: EmptyBackend())
    store.remote = try await DemoBackend().get("/v1/remote")
    store.showRemote = true
    let review = RemotePairingReview(isActive: { true }, notify: { _, _ in }, clearNotifications: { _ in })
    var opens = 0
    review.openMainWindow = { opens += 1 }
    review.update(store: store)
    #expect(store.connectKind == nil && opens == 0)
    store.showRemote = false
    review.update(store: store)
    #expect(store.connectKind == .chatgpt && opens == 1)
    store.connectKind = nil
    review.update(store: store)
    #expect(store.connectKind == nil && opens == 1)
  }
  @Test func sampleAndSnapshotPairingHaveNoWindowEffects() async throws {
    let store = AppStore(demo: true, snapshot: true)
    store.remote = try await DemoBackend().get("/v1/remote")
    let review = RemotePairingReview(isActive: { false }, notify: { _, _ in Issue.record("sample notification") }, clearNotifications: { _ in })
    review.openMainWindow = { Issue.record("sample window opened") }
    review.update(store: store)
    await store.monitorRemotePairings(fetch: { Issue.record("snapshot polled"); return store.remote! })
    #expect(store.connectKind == nil)
  }
}

@MainActor private final class FakeEngineService: EngineServiceManaging {
  var registered: Bool
  var healthy: Bool
  var healthyAfterRegister = false
  var needsApproval = false
  var failRegistrations = 0
  var failUnregister = false
  var events: [String] = []
  var onUnregister: () -> Void = {}
  init(registered: Bool = true, healthy: Bool = false) { self.registered = registered; self.healthy = healthy }
  var registration: EngineRegistration {
    events.append("status")
    return needsApproval ? .needsApproval : registered ? .registered : .absent
  }
  func probe() async -> Bool { events.append("probe"); return healthy }
  func needsUpdate() async throws -> Bool { false }
  func recordHealthyHelper() async throws {}
  func unregister() async throws {
    events.append("unregister-begin")
    onUnregister()
    await Task.yield()
    if failUnregister { throw BackendError.invalidResponse }
    registered = false
    events.append("unregister-end")
  }
  func register() async throws {
    if registered { try await unregister() }
    events.append("register")
    if failRegistrations > 0 { failRegistrations -= 1; throw BackendError.invalidResponse }
    registered = true
    healthy = healthyAfterRegister
  }
}
