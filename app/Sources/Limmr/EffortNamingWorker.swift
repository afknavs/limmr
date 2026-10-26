import CoreGraphics
import Foundation
import LimmrKit
import Observation

/// App-lifetime scheduling, independent of any window or Activity task.
@MainActor @Observable final class EffortNamingWorker {
  @MainActor struct Environment {
    var now: () -> Date = { Date() }
    var unavailableReason: () -> String? = { AppleBriefModel.unavailableReason }
    var paused: () -> Bool = {
      let process = ProcessInfo.processInfo
      let idle = [CGEventType.keyDown, .scrollWheel, .leftMouseDragged].map {
        CGEventSource.secondsSinceLastEventType(.combinedSessionState, eventType: $0)
      }.min() ?? 0
      return EffortNamingWorker.shouldPause(idleSeconds: idle,
        lowPower: process.isLowPowerModeEnabled, thermal: process.thermalState)
    }
    var sleep: (Duration) async throws -> Void = { try await Task.sleep(for: $0) }
  }
  static func shouldPause(idleSeconds: TimeInterval, lowPower: Bool,
                          thermal: ProcessInfo.ThermalState) -> Bool {
    idleSeconds < 3 || lowPower || (thermal == .serious || thermal == .critical)
  }
  private(set) var remaining = 0
  private(set) var pending = 0
  private(set) var notice: String?
  var enabled = false {
    didSet { if !enabled { cancelGeneration() } }
  }
  var ready = false {
    didSet { if !ready { cancelGeneration() } }
  }
  @ObservationIgnored private let backend: any Backend
  @ObservationIgnored private let model: any EffortNamingModel
  @ObservationIgnored private let nudgeWriter: OnDeviceNudgeWriter?
  @ObservationIgnored private let environment: Environment
  @ObservationIgnored let session: EffortNamingSession
  @ObservationIgnored private var task: Task<Void, Never>?
  @ObservationIgnored private var generation: Task<Void, Never>?
  @ObservationIgnored private var changed: () async -> Void
  @ObservationIgnored private var nudged: () async -> Void

  init(backend: any Backend, model: any EffortNamingModel,
       session: EffortNamingSession, environment: Environment,
       nudgeModel: (any TaskNudgeModel)? = nil, nudged: @escaping () async -> Void = {}, changed: @escaping () async -> Void) {
    self.backend = backend; self.model = model; self.session = session
    self.environment = environment; self.changed = changed; self.nudged = nudged
    self.nudgeWriter = nudgeModel.map { OnDeviceNudgeWriter(backend: backend, model: $0) }
  }

  func updateQueue(_ queue: EffortNamingResponse) {
    remaining = queue.total
    pending = queue.efforts.filter { !session.hasAttempted($0) }.count
  }

  func start() {
    guard task == nil else { return }
    let sleep = environment.sleep
    task = Task { [weak self] in
      while !Task.isCancelled {
        guard let delay = await self?.step() else { return }
        do { try await sleep(delay) } catch { return }
      }
    }
  }
  func stop() {
    task?.cancel(); task = nil
    cancelGeneration()
    remaining = 0; pending = 0; notice = nil
  }
  private func cancelGeneration() {
    generation?.cancel()
    pending = 0
    notice = nil
  }

  /// One bounded turn. An empty/attempted queue gets a cheap periodic check,
  /// never another generation until evidence changes. No timers belong to a view.
  @discardableResult func step() async -> Duration {
    guard ready else { pending = 0; notice = nil; return .seconds(5) }
    guard enabled else {
      pending = 0; notice = "Enable First briefs from This Mac in Settings → Briefs to name waiting work."
      return .seconds(5)
    }
    if let reason = environment.unavailableReason() {
      pending = 0; notice = reason; return .seconds(5)
    }
    guard !environment.paused() else {
      pending = 0; notice = "Naming pauses while you use this Mac, or to save power."
      return .seconds(2)
    }
    do {
      if let nudgeWriter {
        generation = Task { [weak self] in
          if (try? await nudgeWriter.writeNext(now: self?.environment.now() ?? Date())) == true {
            guard !Task.isCancelled else { return }
            await self?.nudged()
          }
        }
        await generation?.value
        generation = nil
        try Task.checkCancellation()
        guard enabled, ready else { return .seconds(5) }
      }
      let queue: EffortNamingResponse = try await backend.get("/v1/efforts/unnamed")
      try Task.checkCancellation()
      guard enabled, ready, environment.unavailableReason() == nil else { return .seconds(5) }
      updateQueue(queue)
      let candidates = queue.efforts.filter { !session.hasAttempted($0) }
      guard let candidate = candidates.first else {
        notice = remaining > 0 ? "Waiting work needs more evidence or a name from your AI." : nil
        return .seconds(30)
      }
      guard !environment.paused() else { pending = 0; return .seconds(2) }
      notice = nil
      let writer = OnDeviceEffortWriter(backend: backend, model: model, session: session,
                                        now: environment.now())
      generation = Task { try? await writer.name(candidate) }
      await generation?.value
      generation = nil
      try Task.checkCancellation()
      guard enabled, ready else { return .seconds(5) }
      // Re-read even after uncertain output/conflicts; the server owns the queue.
      let next: EffortNamingResponse = try await backend.get("/v1/efforts/unnamed")
      try Task.checkCancellation()
      updateQueue(next)
      if pending == 0 && remaining > 0 {
        notice = "Waiting work needs more evidence or a name from your AI."
      }
      await changed()
      return .seconds(8)
    } catch is CancellationError {
      return .seconds(30)
    } catch {
      pending = 0; notice = "Naming will check again when the helper is available."
      return .seconds(30)
    }
  }
}

extension AppStore {
  func syncEffortNaming() {
    guard !snapshot, !resettingEffortNaming else { return }
    let ready = status != nil && home.effortsReady == true
      && hasLoaded("/v1/settings/briefs")
    if effortNamer == nil, ready {
      effortNamer = makeEffortNamer()
      effortNamer?.start()
    }
    effortNamer?.enabled = briefSettings.thisMacEnabled
    effortNamer?.ready = ready
  }

  func makeEffortNamer(environment: EffortNamingWorker.Environment? = nil,
                       model: (any EffortNamingModel)? = nil) -> EffortNamingWorker {
    let source = backend
    let generation = backendGeneration
    let sampleEnvironment = EffortNamingWorker.Environment(unavailableReason: { nil }, paused: { false })
    return EffortNamingWorker(backend: source,
      model: model ?? effortNamingModel ?? (demo ? DemoEffortNamingModel() : AppleEffortNamingModel()),
      session: effortNamingSession, environment: environment ?? effortNamingEnvironment ?? (demo ? sampleEnvironment : .init()),
      nudgeModel: demo ? DemoTaskNudgeModel() : AppleTaskNudgeModel(), nudged: { [weak self] in
        guard let self, generation == self.backendGeneration else { return }
        await self.load("/v1/home", title: "Home") { (v: HomeResponse) in self.home = v }
      }) { [weak self] in
        guard let self, generation == self.backendGeneration else { return }
        self.effortRevision += 1
        await self.refreshHomeEfforts()
        guard generation == self.backendGeneration, !Task.isCancelled else { return }
        await self.load("/v1/home", title: "Home") { (v: HomeResponse) in self.home = v }
      }
  }
}
