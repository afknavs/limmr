import Foundation
import LimmrKit

// All launchd / Security effects sit behind this boundary. Tests never register a real job.
enum HelperRoute: Equatable { case serviceManagement, userAgent }
struct HelperIdentity: Equatable {
  let sha256: String
  let codeHash: String
  let developerID: Bool
  var alternateCodeHashes: Set<String> = []
  func matches(_ runningHash: String?) -> Bool {
    guard let runningHash else { return false }
    return runningHash == codeHash || alternateCodeHashes.contains(runningHash)
  }
}
struct HelperJob {
  let route: HelperRoute
  let pid: Int32?
  let codeHash: String?
  let executable: URL?
  var age: TimeInterval? = nil
}
struct HelperLaunchPlan {
  let identity: HelperIdentity
  let route: HelperRoute
  let plist: Data?
}

/// Only curated operation names and numeric OS codes cross the public log/UI boundary.
/// Raw localized errors may contain executable paths, launchctl environments or credentials.
struct HelperUpdateFailure: Error, LocalizedError {
  let reason: String
  var errorDescription: String? { reason }
  static func describing(_ error: Error) -> String {
    if let failure = error as? HelperUpdateFailure { return failure.reason }
    if error is CancellationError { return "The helper update was cancelled." }
    if let backend = error as? BackendError {
      switch backend {
      case .invalidResponse: return "The helper returned an incomplete response."
      case .http(let code): return "The helper returned an HTTP error (\(code))."
      default: return "The background helper command could not finish."
      }
    }
    if let update = error as? UpdateError {
      return update == .unsafeBundle ? "The installed app did not pass signature validation." : "The app update validation or installation failed (\(update))."
    }
    let os = error as NSError
    if os.domain == NSCocoaErrorDomain { return "A helper file could not be accessed (file error \(os.code))." }
    if os.domain == NSPOSIXErrorDomain { return "A helper operation failed (system error \(os.code))." }
    return "The helper operation did not complete (error \(os.code))."
  }
  @MainActor static func step<T>(_ operation: String, _ body: () async throws -> T) async throws -> T {
    do { return try await body() }
    catch is CancellationError { throw CancellationError() }
    catch { throw HelperUpdateFailure(reason: "\(operation) \(describing(error))") }
  }
}

@MainActor protocol HelperRegistrationDriver: AnyObject {
  var registration: EngineRegistration { get }
  func healthy() async -> Bool
  func readiness() async -> HelperReadiness
  func identity() async throws -> HelperIdentity
  func retain(_ identity: HelperIdentity) async throws
  func job() async throws -> HelperJob?
  /// Stage executable and plist, validate signature, and run only -h (before opening stores).
  func prepare(_ identity: HelperIdentity, route: HelperRoute) async throws -> HelperLaunchPlan
  /// A healthy old job must have a runnable, fingerprint-checked rollback before stopping it.
  func backup(_ job: HelperJob) async throws -> HelperLaunchPlan
  func remove() async throws
  func launch(_ plan: HelperLaunchPlan) async throws
}

extension HelperRegistrationDriver {
  func readiness() async -> HelperReadiness { await healthy() ? .ready : .unavailable }
}

@MainActor final class HelperRegistration {
  let driver: any HelperRegistrationDriver
  private let pause: (Duration) async throws -> Void
  private var busy = false
  init(driver: any HelperRegistrationDriver,
       pause: @escaping (Duration) async throws -> Void = { try await Task.sleep(for: $0) }) {
    self.driver = driver; self.pause = pause
  }

  func needsUpdate() async throws -> Bool {
    let desired = try await HelperUpdateFailure.step("Checking the bundled helper failed.") { try await driver.identity() }
    guard let job = try await driver.job(), let hash = job.codeHash else {
      // A healthy listener of unknown origin is never safe to stop or label as current.
      throw HelperUpdateFailure(reason: "The running helper could not be identified. Restart Limmr after checking the background helper.")
    }
    if desired.matches(hash) { try? await driver.retain(desired) }
    return !desired.matches(hash)
  }

  func register() async throws {
    guard !busy else { throw HelperUpdateFailure(reason: "Another helper update is already in progress. Try again shortly.") }
    busy = true
    defer { busy = false }
    let desired = try await HelperUpdateFailure.step("Checking the bundled helper failed.") { try await driver.identity() }
    let healthy = try await settledHealth()
    let old = try await HelperUpdateFailure.step("Inspecting the running helper failed.") { try await driver.job() }
    if healthy, desired.matches(old?.codeHash) {
      try? await driver.retain(desired)
      return
    } // adopt, including manual fallback; no receipt needed
    guard !healthy || old?.codeHash != nil else { throw HelperUpdateFailure(reason: "The running helper could not be identified safely.") }
    guard driver.registration != .needsApproval || old?.route == .userAgent else {
      throw HelperUpdateFailure(reason: "Allow Limmr in System Settings → Login Items, then try again.")
    }
    // Keep an existing user agent on that path, including Developer ID upgrades. Ad-hoc builds
    // always leave SMAppService: its LWCR can still pin an earlier helper after unregister/register.
    let route: HelperRoute = desired.developerID && old?.route != .userAgent ? .serviceManagement : .userAgent
    let candidate = try await HelperUpdateFailure.step("Preparing the new helper failed.") { try await driver.prepare(desired, route: route) }
    // Prepare the no-LWCR escape path BEFORE removing a working engine too.
    let fallback = route == .serviceManagement ? try await HelperUpdateFailure.step("Preparing the fallback helper failed.") { try await driver.prepare(desired, route: .userAgent) } : candidate
    // The original probe may have raced launchd starting a healthy process during preflight.
    let healthyBeforeRemoval = try await settledHealth()
    let latest = try await HelperUpdateFailure.step("Rechecking the running helper failed.") { try await driver.job() }
    if healthyBeforeRemoval, desired.matches(latest?.codeHash) { return }
    guard !healthyBeforeRemoval || latest?.codeHash != nil else { throw HelperUpdateFailure(reason: "The running helper changed or could not be identified during the update.") }
    let rollback: HelperLaunchPlan?
    if healthyBeforeRemoval, let latest { rollback = try await HelperUpdateFailure.step("Preparing the previous helper for recovery failed.") { try await driver.backup(latest) } } else { rollback = nil }
    try Task.checkCancellation()
    do {
      try await HelperUpdateFailure.step("Stopping the previous helper failed.") { try await driver.remove() } // exact job only
      do {
        try await HelperUpdateFailure.step("Starting the new helper failed.") { try await driver.launch(candidate) }
        if try await waitFor(desired) { return }
        throw HelperUpdateFailure(reason: "The new helper did not become healthy with the expected build.")
      } catch {
        if error is CancellationError { throw error }
        guard route == .serviceManagement, driver.registration != .needsApproval else { throw error }
        try await driver.remove()
        try await driver.launch(fallback)
        if try await waitFor(desired) { return }
        throw BackendError.invalidResponse
      }
    } catch {
      // Cancelling the wait must not kill a new helper that is still initializing.
      if error is CancellationError, let job = try? await driver.job(), job.pid != nil,
         desired.matches(job.codeHash) { throw error }
      // Rollback is independent of task cancellation. Preserve the prior helper if the candidate
      // cannot open the real stores/listener, or launchd refuses it after a successful preflight.
      if let rollback {
        // A failed unregister may have left the old process untouched. Never boot it out again.
        if await driver.healthy(), rollback.identity.matches(try await driver.job()?.codeHash) { throw error }
        try await driver.remove()
        try await driver.launch(rollback)
        // Unstructured task deliberately lets rollback finish even when the caller was cancelled.
        let restored = try await Task { @MainActor in try await self.waitFor(rollback.identity) }.value
        guard restored else { throw HelperUpdateFailure(reason: "The previous helper could not restart. Try again.") }
      }
      throw error
    }
  }

  private func settledHealth() async throws -> Bool {
    try await HelperReadinessWait.ready(probe: { await self.driver.readiness() }, pause: pause)
  }

  private func waitFor(_ identity: HelperIdentity) async throws -> Bool {
    var observedPID: Int32?
    return try await HelperReadinessWait.ready(probe: {
      let state = await self.driver.readiness()
      let job = try await self.driver.job()
      if let pid = job?.pid {
        // launchd spawns through xpcproxy before exec, so the first reads can show another image.
        // An unexpected image only fails once it persists, even if its listener answers health.
        if let hash = job?.codeHash, !identity.matches(hash) {
          return .starting(progress: "image-\(pid)-\(hash)", grace: 5)
        }
        let newlyObserved = observedPID != pid
        observedPID = pid
        if state == .ready, identity.matches(job?.codeHash) { return .ready }
        if case .starting(let progress, let grace) = state {
          if progress == nil && newlyObserved { return .starting(progress: "process-\(pid)", grace: grace) }
          return state
        }
        return .starting(progress: "process-\(pid)", grace: max(0, 30 - (job?.age ?? 0)))
      }
      if observedPID != nil { return .unavailable } // exited after spawning
      // launchd may accept bootstrap before assigning a PID; allow its spawn window.
      return .starting(progress: "spawn")
    }, pause: pause)
  }
}
