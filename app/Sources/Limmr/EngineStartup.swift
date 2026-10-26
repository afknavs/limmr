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
}

extension EngineServiceManaging {
  func readiness() async -> HelperReadiness { await probe() ? .ready : .unavailable }
}

/// Registration succeeding doesn't mean launchd accepted the new helper's constraints.
/// Registration owns preflight, replacement and rollback; readiness requires authenticated health.
@MainActor @Observable final class EngineStartup {
  enum Phase: Equatable { case starting, restarting, updating, ready, newInstall, failed }
  private(set) var phase: Phase = .starting
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
  func recover(using service: any EngineServiceManaging, allowInstall: Bool = false) async {
    guard !running else { return }
    running = true
    defer { running = false }
    phase = .starting
    failureMessage = nil
    let registration = service.registration
    var healthy: Bool
    do { healthy = try await settledHealth(service) }
    catch { fail(error); return }
    // One slow 0.75 s answer at launch must not send a running engine down the restart path.
    if !healthy && registration != .absent {
      for _ in 0..<2 where !healthy {
        try? await pause(.milliseconds(500))
        do { healthy = try await settledHealth(service) }
        catch { fail(error); return }
      }
    }
    if healthy {
      do {
        if try await service.needsUpdate() {
          phase = .updating
          log("Running helper differs from the bundled engine; preparing a safe update.")
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
          throw HelperUpdateFailure(reason: "The new helper did not become healthy after registration.")
        }
        phase = .ready
      } catch {
        let reason = HelperUpdateFailure.describing(error)
        let retained = await service.probe()
        let previous = retained ? (try? await service.needsUpdate()) == true : false
        failureMessage = "The engine update couldn’t finish. " + reason
          + (previous ? " The previous engine is still running." :
             retained ? " The background helper is still running." : " The background helper is not responding. Try again.")
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
        logError("Starting the background helper failed. " + failureMessage!)
      }
      if attempt < 2 {
        do { try await pause(.milliseconds(250 * (1 << attempt))) }
        catch { phase = .failed; return }
      }
    }
    phase = .failed
  }
  private func settledHealth(_ service: any EngineServiceManaging) async throws -> Bool {
    try await HelperReadinessWait.ready(probe: { await service.readiness() }, pause: pause)
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
  var retry: () -> Void
  private var state: EngineSurfaceState {
    switch phase {
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
          Text(state.detail).font(.callout).foregroundStyle(AccessibleSecondary())
        }
        Spacer(minLength: 0)
        Button(phase == .failed ? "Try again" : "Check again", action: retry)
          .buttonStyle(NativeGlassButtonStyle())
      }
      if phase == .failed, let failureMessage {
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
