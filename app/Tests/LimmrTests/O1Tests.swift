import Foundation
import Testing
@testable import Limmr
@testable import LimmrKit

@Suite("O1 first run and menu bar", .serialized) @MainActor struct O1Tests {
  @Test func progressingStartupWaitsQuietlyBeyondThirtySeconds() async throws {
    let backend = O1StartingBackend(readyAt: 65, progressing: true)
    _ = try await CalmLoadRetry.request(backend, path: "/v1/status", pause: { _ in })
    #expect(await backend.calls == 66)
  }

  @Test func stalledStartupStopsRetrying() async {
    let backend = O1StartingBackend(readyAt: 100, progressing: false)
    do {
      _ = try await CalmLoadRetry.request(backend, path: "/v1/status", pause: { _ in })
      Issue.record("Stalled startup was accepted")
    } catch {}
    #expect(await backend.calls == 31)
  }

  @Test func firstRunCanRecoverAndWalkBack() {
    var flow = SetupFlow()
    flow.advance()
    flow.helperFailed()
    #expect(flow.step == .link && flow.helperError != nil)
    flow.back()
    #expect(flow.step == .welcome)
    flow.advance()
    flow.helperStarted()
    flow.advance()
    #expect(flow.step == .link) // An unlinked account cannot advance.
    flow.advance(linkReady: true)
    #expect(flow.step == .syncing)
    flow.advance()
    flow.advance()
    flow.advance()
    #expect(flow.step == .done && flow.helperError == nil)
    flow.back()
    #expect(flow.step == .connect)
  }

  @Test func emptyAccountAndLinkActionDoNotInventEntities() async {
    let store = AppStore(demo: false, snapshot: true, backend: EmptyBackend())
    await store.refresh()
    await store.refreshHomeEfforts()
    #expect(store.people.isEmpty && store.chats.isEmpty && store.circles.isEmpty)
    #expect(store.homeEfforts.isEmpty && store.home.watches.isEmpty)
    #expect(store.menuWaitingCount == 0 && store.menuWatchCount == 0)
    #expect(store.loadFailures.isEmpty && store.refreshError == nil && store.digestError == nil)
    #expect(store.engineSurfaceState == .loggedOut)
    await store.connectionAction(.loggedOut)
    #expect(store.showSetup && store.setupStep == 1)
    let linked = AppStore(demo: false, snapshot: true, backend: O1EmptyAccountBackend())
    await linked.refresh()
    await linked.refreshHomeEfforts()
    #expect(linked.engineSurfaceState == .connected && linked.people.isEmpty && linked.chats.isEmpty)
    #expect(linked.circles.isEmpty && linked.homeEfforts.isEmpty && linked.home.watches.isEmpty)
    #expect(linked.loadFailures.isEmpty && linked.digestError == nil)
    #expect(linked.dashboard?.today.messages == 0 && linked.history?.days.isEmpty == true)
  }

  @Test func statusUsesLatestEvidenceAndDoesNotGuessPhoneFailure() async {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    #expect(store.engineSurfaceState == .connected)
    store.checks = [.init(id: "phone", title: "Phone", state: "warn",
                         detail: "No recent phone connection evidence.", fixId: nil)]
    #expect(store.engineSurfaceState == .connected)
    store.status?.connected = false
    #expect(store.engineSurfaceState == .offline)
    store.checks[0].detail = "Phone unreachable"
    #expect(store.engineSurfaceState == .phoneUnreachable)
    store.status?.linked = false
    #expect(store.engineSurfaceState == .loggedOut)
    store.loadFailures["/v1/status"] = "Connection unavailable"
    #expect(store.engineSurfaceState == .notRunning)
  }

  @Test func menuCountsExcludeResolvedApprovalsAndInactiveWatches() async {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    #expect(store.menuWaitingCount == store.pending.count + store.home.waiting.count)
    let original = store.menuWatchCount
    #expect(original > 0)
    store.home.watches[0].status = "paused"
    #expect(store.menuWatchCount == original - 1)
    store.home.watches[0].status = "active"
    store.home.watches[0].expiresAt = "2000-01-01T00:00:00Z"
    #expect(store.menuWatchCount == original - 1)
  }

  @Test func linkStreamRecoversQuietlyAndRefreshesReadyAccount() async {
    let backend = O1LinkBackend(interruptions: 1)
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.linkEvents()
    #expect(store.link.state == "ready")
    #expect(store.status?.linked == true)
    #expect(backend.attempts == 2)
    #expect(store.error == nil && store.refreshError == nil)
  }

  @Test func endedLinkStreamHasBoundedRetryAndActionableFailure() async {
    let backend = O1LinkBackend(interruptions: 10)
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.linkEvents()
    #expect(backend.attempts == 3)
    #expect(store.link.state == "error" && store.link.reason?.contains("Try linking again") == true)
  }

  @Test func cancelledLinkStreamDoesNotShowPairingFailure() async {
    let store = AppStore(demo: true, snapshot: true, backend: O1LinkBackend(interruptions: 10))
    let task = Task { await store.linkEvents() }
    task.cancel()
    await task.value
    #expect(store.link.state != "error")
  }
}

// Only invented events and an in-memory backend. No listener, device, or helper registration.
private final class O1LinkBackend: Backend, @unchecked Sendable {
  private let lock = NSLock()
  private var count = 0
  let interruptions: Int
  let demo = DemoBackend()
  init(interruptions: Int) { self.interruptions = interruptions }
  var attempts: Int { lock.withLock { count } }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    try await demo.request(path, method: method, body: body)
  }
  func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    let attempt = lock.withLock { count += 1; return count }
    return AsyncThrowingStream { stream in
      if attempt > interruptions {
        stream.yield(.init(event: "message", data: "{\"state\":\"syncing\",\"chats\":0,\"messages\":0}"))
        stream.yield(.init(event: "message", data: "{\"state\":\"ready\"}"))
      }
      stream.finish()
    }
  }
}

private actor O1StartingBackend: Backend {
  var calls = 0
  let readyAt: Int
  let progressing: Bool
  init(readyAt: Int, progressing: Bool) { self.readyAt = readyAt; self.progressing = progressing }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    calls += 1
    if calls <= readyAt { throw BackendError.starting(progress: "warmup:\(progressing ? calls / 20 : 0)") }
    return Data("{}".utf8)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { AsyncThrowingStream { $0.finish() } }
}
