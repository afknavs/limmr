import Foundation
import Testing
@testable import Limmr
@testable import LimmrKit

@Suite("N1 background effort naming") @MainActor struct BackgroundEffortNamingTests {
  @Test func storeDrainsQueueWithActivityClosedAndKeepsCredit() async throws {
    let backend = BackgroundNamingBackend()
    let clock = NamingClock()
    let model = BackgroundNamingModel()
    var environment = clock.environment
    environment.sleep = { duration in
      try await Task.sleep(for: .milliseconds(1))
      clock.advance(duration)
    }
    let store = AppStore(demo: true, backend: backend,
      effortNamingEnvironment: environment, effortNamingModel: model)
    defer { store.effortNamer?.stop() }
    #expect(store.page == .overview)
    await store.refresh()
    #expect(store.effortNamer != nil)
    let firstWorker = store.effortNamer
    store.syncEffortNaming(); store.syncEffortNaming()
    #expect(store.effortNamer === firstWorker)
    await backend.waitForWrites(2)
    // Wait for the worker's final queue read and presentation refresh.
    for _ in 0..<1000 {
      if store.effortNamer?.remaining == 0 && store.effortRevision >= 2 { break }
      await Task.yield()
    }
    #expect(store.page == .overview)
    #expect(model.calls == 2)
    #expect(store.effortNamer?.remaining == 0)
    let efforts: EffortsResponse = try await backend.get("/v1/efforts")
    #expect(efforts.efforts.filter { $0.createdBy == "This Mac" }.count == 2)
  }

  @Test func storeWaitsForEffortsReadyBeforeStarting() async {
    let backend = BackgroundNamingBackend()
    await backend.setWarm(false)
    let clock = NamingClock()
    let model = BackgroundNamingModel()
    let store = AppStore(demo: true, backend: backend,
      effortNamingEnvironment: clock.environment, effortNamingModel: model)
    defer { store.effortNamer?.stop(); store.homeEffortsRetry?.cancel() }
    await store.refresh()
    #expect(store.home.effortsReady == false)
    #expect(store.effortNamer == nil)
    #expect(model.calls == 0)
    #expect(await backend.queueReads == 0)
    await backend.setWarm(true)
    await store.refresh()
    await backend.waitForWrites(1)
    #expect(store.effortNamer != nil)
    #expect(model.calls == 1)
  }

  @Test func inputPowerAndThermalPolicy() {
    #expect(EffortNamingWorker.shouldPause(idleSeconds: 1, lowPower: false, thermal: .nominal))
    #expect(EffortNamingWorker.shouldPause(idleSeconds: 20, lowPower: true, thermal: .nominal))
    for thermal: ProcessInfo.ThermalState in [.serious, .critical] {
      #expect(EffortNamingWorker.shouldPause(idleSeconds: 20, lowPower: false, thermal: thermal))
    }
    #expect(!EffortNamingWorker.shouldPause(idleSeconds: 3, lowPower: false, thermal: .nominal))
    #expect(!EffortNamingWorker.shouldPause(idleSeconds: 20, lowPower: false, thermal: .fair))
    #expect(EffortNamingWorker.shouldPause(idleSeconds: 1, lowPower: false, thermal: .fair))
    #expect(EffortNamingWorker.shouldPause(idleSeconds: 20, lowPower: true, thermal: .fair))
  }

  @Test func warmupSettingsAndAvailabilityGateAllWork() async {
    let backend = BackgroundNamingBackend()
    let clock = NamingClock()
    let model = BackgroundNamingModel()
    let worker = worker(backend, clock, model)
    worker.enabled = true
    await worker.step()
    #expect(await backend.queueReads == 0)
    worker.ready = true; worker.enabled = false
    await worker.step()
    #expect(await backend.queueReads == 0)
    clock.reason = "Fixture model is unavailable."
    worker.enabled = true
    await worker.step()
    #expect(worker.notice == clock.reason)
    #expect(await backend.queueReads == 0)
    #expect(model.calls == 0)
    clock.reason = nil
    await worker.step()
    #expect(model.calls == 1)
  }

  @Test func throttlePauseAndEmptyQueueResume() async throws {
    let backend = BackgroundNamingBackend()
    let clock = NamingClock()
    let model = BackgroundNamingModel()
    let worker = worker(backend, clock, model)
    worker.ready = true; worker.enabled = true
    clock.paused = true // Injected idle / low-power / thermal predicate.
    #expect(await worker.step() == .seconds(2))
    #expect(await backend.queueReads == 0)
    clock.paused = false
    #expect(await worker.step() == .seconds(8))
    #expect(model.calls == 1)
    clock.advance(.seconds(7))
    await worker.step()
    #expect(model.calls == 1)
    clock.advance(.seconds(1))
    clock.paused = true
    await worker.step()
    #expect(model.calls == 1)
    clock.paused = false
    await worker.step()
    #expect(model.calls == 2)
    #expect(worker.remaining == 0)
    #expect(await worker.step() == .seconds(30))
    #expect(worker.notice == nil)
    // A periodic check discovers new evidence without an Activity task or SSE.
    await backend.appendCandidate()
    clock.advance(.seconds(30))
    await worker.step()
    #expect(model.calls == 3)
  }

  @Test func failuresAndUncertaintyRetryOnlyOnChangedRevision() async {
    for fails in [false, true] {
      let backend = BackgroundNamingBackend()
      let clock = NamingClock()
      let model = BackgroundNamingModel()
      model.uncertain = true; model.fails = fails
      let worker = worker(backend, clock, model)
      worker.ready = true; worker.enabled = true
      await worker.step(); clock.advance(.seconds(8))
      await worker.step(); clock.advance(.seconds(30))
      #expect(await worker.step() == .seconds(30))
      #expect(model.calls == 2)
      #expect(worker.pending == 0)
      #expect(worker.notice?.contains("more evidence") == true)
      await backend.changeRevision()
      await worker.step()
      #expect(model.calls == 3)
      #expect(await backend.writes == 0)
    }
  }

  @Test func disabledSettingCancelsInFlightResult() async {
    let backend = BackgroundNamingBackend()
    let clock = NamingClock()
    let model = BackgroundNamingModel()
    model.suspend = true
    let worker = worker(backend, clock, model)
    worker.ready = true; worker.enabled = true
    let turn = Task { await worker.step() }
    await model.waitForStart()
    worker.enabled = false
    model.release()
    _ = await turn.value
    #expect(await backend.writes == 0)
    #expect(worker.pending == 0)
  }

  @Test func backendSwitchCancelsOldWorkerBeforeItCanWrite() async {
    let backend = BackgroundNamingBackend()
    let replacement = BackgroundNamingBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    let clock = NamingClock()
    let model = BackgroundNamingModel()
    model.suspend = true
    let worker = store.makeEffortNamer(environment: clock.environment, model: model)
    store.effortNamer = worker
    worker.ready = true; worker.enabled = true
    worker.start()
    await model.waitForStart()
    await store.useSampleData(false, realBackend: replacement)
    model.release()
    for _ in 0..<100 { await Task.yield() }
    #expect(await backend.writes == 0)
    #expect(await replacement.writes == 0)
    #expect(store.effortNamer == nil)
    #expect(store.effortRevision == 0)
  }

  private func worker(_ backend: BackgroundNamingBackend, _ clock: NamingClock,
                      _ model: BackgroundNamingModel) -> EffortNamingWorker {
    EffortNamingWorker(backend: backend, model: model, session: EffortNamingSession(),
                       environment: clock.environment, changed: {})
  }
}

@MainActor private final class NamingClock {
  var now = Date(timeIntervalSince1970: 1_000)
  var paused = false
  var reason: String?
  var environment: EffortNamingWorker.Environment {
    .init(now: { self.now }, unavailableReason: { self.reason }, paused: { self.paused })
  }
  func advance(_ duration: Duration) {
    now += Double(duration.components.seconds) + Double(duration.components.attoseconds) / 1e18
  }
}

@MainActor private final class BackgroundNamingModel: EffortNamingModel {
  var calls = 0
  var uncertain = false
  var fails = false
  var suspend = false
  private var started: CheckedContinuation<Void, Never>?
  private var suspended: CheckedContinuation<Void, Never>?
  func waitForStart() async {
    if calls > 0 { return }
    await withCheckedContinuation { started = $0 }
  }
  func release() { suspended?.resume(); suspended = nil }
  func name(context: String) async throws -> LocalEffortName {
    calls += 1
    if suspend {
      await withCheckedContinuation {
        suspended = $0
        started?.resume(); started = nil
      }
    }
    if fails { throw BackendError.unavailable("Fixture failure") }
    return uncertain ? .init(title: "", summary: "")
      : .init(title: "Festival budget", summary: "Noor asked for a budget review.")
  }
}

private actor BackgroundNamingBackend: Backend {
  private let demo = DemoBackend()
  private var candidates: [EffortNamingCandidate] = (1...2).map {
    .init(id: "fixture-\($0)", revision: "evidence-1", topTerms: ["festival", "budget"],
      artefactNames: [], snippets: ["Review the festival budget proposal"],
      messageCount: 1, chatCount: 1, participantCount: 1)
  }
  private var named: [Effort] = []
  private var warm = true
  func setWarm(_ value: Bool) { warm = value }
  private(set) var writes = 0
  private(set) var queueReads = 0
  private var waiter: (Int, CheckedContinuation<Void, Never>)?
  func waitForWrites(_ count: Int) async {
    if writes >= count { return }
    await withCheckedContinuation { waiter = (count, $0) }
  }
  func changeRevision() { if !candidates.isEmpty { candidates[0].revision += "-changed" } }
  func appendCandidate() {
    candidates.append(.init(id: "extra", revision: "new-evidence", topTerms: ["festival"],
      artefactNames: [], snippets: ["Review the festival budget proposal"],
      messageCount: 1, chatCount: 1, participantCount: 1))
  }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if path == "/v1/home" {
      var home: HomeResponse = try await demo.get(path)
      home.effortsReady = warm
      return try Wire.data(home)
    }
    if path == "/v1/settings/briefs" { return try Wire.data(BriefSettings(onDeviceEnabled: true)) }
    if path == "/v1/efforts/unnamed" {
      queueReads += 1
      return try Wire.data(EffortNamingResponse(efforts: candidates, total: candidates.count))
    }
    if path.components(separatedBy: "?")[0] == "/v1/efforts" {
      return try Wire.data(EffortsResponse(efforts: named))
    }
    if method == "PATCH", path.hasPrefix("/v1/efforts/"),
       let body, let object = try JSONSerialization.jsonObject(with: body) as? [String: Any] {
      let id = String(path.split(separator: "/").last ?? "")
      if let index = candidates.firstIndex(where: { $0.id == id }),
         candidates[index].revision == object["expected_revision"] as? String {
        candidates.remove(at: index)
        var effort = DemoEfforts.efforts[0]
        effort.id = id; effort.title = object["title"] as? String ?? ""
        effort.summary = object["summary"] as? String ?? ""; effort.createdBy = "This Mac"
        named.append(effort); writes += 1
      }
      if let (count, continuation) = waiter, writes >= count {
        waiter = nil; continuation.resume()
      }
      return Data("{}".utf8)
    }
    return try await demo.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}
