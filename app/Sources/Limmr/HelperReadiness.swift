import Foundation

/// Readiness is separate from connection state and the running image's identity.
enum HelperReadiness: Equatable {
  case ready
  case starting(progress: String?, grace: TimeInterval = 30)
  case unavailable
}

/// Only a changed stage/counter or a newly observed PID counts as progress.
/// Polls alone never extend the deadline. Account for probe time as well as sleeps.
@MainActor enum HelperReadinessWait {
  static func ready(probe: () async throws -> HelperReadiness,
                    pause: (Duration) async throws -> Void) async throws -> Bool {
    let started = ProcessInfo.processInfo.systemUptime
    var elapsed: TimeInterval = 0
    var deadline: TimeInterval = 30
    var previous: String?
    var observedStarting = false
    while true {
      try Task.checkCancellation()
      let state = try await probe()
      elapsed = max(elapsed, ProcessInfo.processInfo.systemUptime - started)
      switch state {
      case .ready: return true
      case .unavailable: return false
      case .starting(let progress, let grace):
        if !observedStarting || (progress != nil && progress != previous) {
          deadline = elapsed + min(30, max(0, grace))
        }
        observedStarting = true
        if let progress { previous = progress }
        guard elapsed < deadline else {
          throw HelperUpdateFailure(reason: "The helper made no startup progress for 30 seconds. Try again.")
        }
      }
      try await pause(.milliseconds(250))
      elapsed += 0.25
    }
  }
}
