import LimmrKit
import Observation
import OSLog
import SwiftUI

enum EngineRegistration { case absent, registered, needsApproval }

@MainActor protocol EngineServiceManaging {
  var registration: EngineRegistration { get }
  func probe() async -> Bool
  func readiness() async -> HelperReadiness
  func needsUpdate() async throws -> Bool
  func recordHealthyHelper() async throws
  func unregister() async throws
  func register() async throws
  func restart() async throws
}

extension EngineServiceManaging {
  func restart() async throws { throw HelperUpdateFailure(reason: "Limmr’s background engine couldn’t restart. Try again.") }
  func readiness() async -> HelperReadiness { await probe() ? .ready : .unavailable }
}

/// Registration succeeding doesn't mean launchd accepted the new helper's constraints.
/// Registration owns preflight, replacement and rollback; readiness requires authenticated health.
@MainActor @Observable final class EngineStartup {
  enum Phase: Equatable { case starting, busy, stalled, restarting, updating, ready, newInstall, failed }
  private(set) var phase: Phase = .starting
  private(set) var progressMessage: String?
  private(set) var failureMessage: String?
  private var running = false
  private let log: (String) -> Void
  private let logError: (String) -> Void
  private let pause: @MainActor (Duration) async throws -> Void
  init(pause: @escaping @MainActor (Duration) async throws -> Void = { try await Task.sleep(for: $0) },
       logError: @escaping (String) -> Void = { Logger(subsystem: "dev.limmr", category: "EngineStartup").error("\($0, privacy: .public)") },
       log: @escaping (String) -> Void = { Logger(subsystem: "dev.limmr", category: "EngineStartup").info("\($0, privacy: .public)") }) {
    self.pause = pause
    self.log = log
    self.logError = logError
  }
  func recover(using service: any EngineServiceManaging, allowInstall: Bool = false, restartIfStalled: Bool = false) async {
    guard !running else { return }
    running = true
    defer { running = false }
    let wasStalled = phase == .stalled
    phase = .starting
    failureMessage = nil
    progressMessage = nil
    if wasStalled && restartIfStalled {
      if await updateBusyEngineIfNeeded(service) { return }
      await restartExisting(service)
      return
    }
    let registration = service.registration
    let initialReadiness = await service.readiness()
    if initialReadiness == .busy, await updateBusyEngineIfNeeded(service) { return }
    var healthy: Bool
    do { healthy = try await settledHealth(service, initial: initialReadiness) }
    catch is EngineReadinessStalled {
      if await updateBusyEngineIfNeeded(service) { return }
      if restartIfStalled { await restartExisting(service) } else { offerRestart() }
      return
    }
    catch { fail(error); return }
    // One slow 0.75 s answer at launch must not send a running engine down the restart path.
    if !healthy && registration != .absent {
      for _ in 0..<2 where !healthy {
        try? await pause(.milliseconds(500))
        do { healthy = try await settledHealth(service) }
        catch is EngineReadinessStalled {
          if await updateBusyEngineIfNeeded(service) { return }
          if restartIfStalled { await restartExisting(service) } else { offerRestart() }
          return
        }
        catch { fail(error); return }
      }
    }
    if healthy {
      do {
        if try await service.needsUpdate() {
          phase = .updating
          log("The running background engine differs from the bundled build; preparing a safe update.")
          try Task.checkCancellation()
          try await service.register()
          for _ in 0..<16 {
            try await pause(.milliseconds(250))
            if try await settledHealth(service) {
              try await service.recordHealthyHelper()
              phase = .ready
              log("Engine update is healthy.")
              return
            }
          }
          throw HelperUpdateFailure(reason: "Limmr’s new background engine did not become ready.")
        }
        phase = .ready
      } catch {
        let reason = HelperUpdateFailure.describing(error)
        let retained = await service.probe()
        let previous = retained ? (try? await service.needsUpdate()) == true : false
        failureMessage = "The engine update couldn’t finish. " + reason
          + (previous ? " The previous engine is still running." :
             retained ? " Limmr’s background engine is still running." : " Limmr’s background engine is not responding. Try again.")
        logError(failureMessage!)
        phase = .failed
      }
      return
    }
    if registration == .absent && !allowInstall { phase = .newInstall; return }
    if registration == .needsApproval {
      failureMessage = "Allow Limmr in System Settings → Login Items, then try again."
      phase = .failed; return
    }
    phase = .restarting
    for attempt in 0..<3 {
      do {
        try Task.checkCancellation()
        try await service.register()
        // Give a freshly bootstrapped process time to open its stores and listener.
        for _ in 0..<4 {
          try await pause(.milliseconds(250))
          if try await settledHealth(service) {
            try await service.recordHealthyHelper()
            phase = .ready
            return
          }
        }
      } catch is CancellationError { phase = .failed; return }
      catch {
        failureMessage = HelperUpdateFailure.describing(error)
        logError("Starting Limmr’s background engine failed. " + failureMessage!)
      }
      if attempt < 2 {
        do { try await pause(.milliseconds(250 * (1 << attempt))) }
        catch { phase = .failed; return }
      }
    }
    phase = .failed
  }
  private func updateBusyEngineIfNeeded(_ service: any EngineServiceManaging) async -> Bool {
    guard (try? await service.needsUpdate()) == true else { return false }
    phase = .updating
    do {
      try await service.register()
      guard try await settledHealth(service) else { throw EngineReadinessStalled() }
      try await service.recordHealthyHelper()
      phase = .ready
    } catch { fail(error) }
    return true
  }
  private func offerRestart() {
    phase = .stalled
    progressMessage = nil
    failureMessage = EngineReadinessStalled().localizedDescription
  }
  private func restartExisting(_ service: any EngineServiceManaging) async {
    phase = .restarting
    progressMessage = "Restarting Limmr’s background engine…"
    do {
      try Task.checkCancellation()
      try await service.restart()
      progressMessage = "Waiting for Limmr’s background engine to respond…"
      // launchd may return before the new process opens its listener. Poll health
      // through that gap; never use registration/replacement as a restart fallback.
      let ready = try await HelperReadinessWait.ready(probe: {
        let state = await service.readiness()
        return state == .unavailable ? .starting(progress: nil) : state
      }, pause: pause)
      guard ready else { throw EngineReadinessStalled() }
      phase = .ready
      progressMessage = nil
      failureMessage = nil
    } catch is CancellationError { phase = .failed; progressMessage = nil }
    catch {
      offerRestart()
      failureMessage = "Limmr’s background engine couldn’t restart. " + HelperUpdateFailure.describing(error)
    }
  }
  private func settledHealth(_ service: any EngineServiceManaging, initial: HelperReadiness? = nil) async throws -> Bool {
    // The fast update decision already made this probe. Consume its result once
    // instead of probing a healthy or missing engine twice.
    var pending = initial
    return try await HelperReadinessWait.ready(probe: {
      if let first = pending { pending = nil; return first }
      return await service.readiness()
    }, pause: pause, onBusy: { self.phase = .busy })
  }
  private func fail(_ error: Error) {
    failureMessage = HelperUpdateFailure.describing(error)
    logError(failureMessage!)
    phase = .failed
  }
}

struct EngineStartupView: View {
  let phase: EngineStartup.Phase
  var failureMessage: String? = nil
  var progressMessage: String? = nil
  var retry: () -> Void
  private var state: EngineSurfaceState {
    switch phase {
    case .busy: .busy
    case .stalled: .stalled
    case .updating: .updating
    case .restarting: .restarting
    case .failed: .notRunning
    default: .starting
    }
  }
  var body: some View {
    VStack(alignment: .leading, spacing: 12) {
      HStack(spacing: 16) {
        if state.inProgress { ProgressView().controlSize(.small).accessibilityLabel(state.title) }
        VStack(alignment: .leading, spacing: 4) {
          Text(state.title).font(.headline)
          Text(progressMessage ?? state.detail).font(.callout).foregroundStyle(AccessibleSecondary())
        }
        Spacer(minLength: 0)
        Button(phase == .stalled ? "Restart Limmr" : phase == .failed ? "Try again" : "Check again", action: retry)
          .buttonStyle(NativeGlassButtonStyle())
      }
      if [.failed, .stalled].contains(phase), let failureMessage {
        Text(failureMessage).font(.caption).foregroundStyle(AccessibleSecondary())
          .fixedSize(horizontal: false, vertical: true)
      }
    }
    .padding(20).frame(maxWidth: 640)
    .background(.primary.opacity(0.035), in: RoundedRectangle(cornerRadius: 14))
    .padding(24)
    .frame(maxWidth: .infinity, maxHeight: .infinity)
    .background(Color(nsColor: .windowBackgroundColor))
  }
}
