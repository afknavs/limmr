import Foundation
import LimmrKit
import Testing
@testable import Limmr

@Suite("Busy engine") @MainActor struct SyncBusyTests {
  @Test func tenSecondBusyEngineRecoversWithoutReplacement() async {
    let service = BusyEngineService()
    service.readyAt = 10
    let startup = EngineStartup(pause: { service.elapse($0) })
    await startup.recover(using: service, allowInstall: true, restartIfStalled: true)
    #expect(startup.phase == .ready)
    #expect(service.elapsed == 10 && service.restarts == 0)
    #expect(service.registrations == 0 && service.removals == 0)
  }
  @Test func thirtySecondsWithoutProgressOffersRestart() async {
    let service = BusyEngineService()
    let startup = EngineStartup(pause: { service.elapse($0) })
    await startup.recover(using: service)
    #expect(startup.phase == .stalled && service.elapsed == 30)
    #expect(service.restarts == 0 && service.registrations == 0 && service.removals == 0)
    #expect(startup.failureMessage?.contains("30 seconds") == true)
    #expect(EngineSurfaceState.stalled.action == "Restart engine")
    // The offered button restarts immediately, rather than waiting another 30 s.
    await startup.recover(using: service, allowInstall: true, restartIfStalled: true)
    #expect(startup.phase == .ready && startup.progressMessage == nil)
    #expect(service.elapsed == 34 && service.restarts == 1)
    #expect(service.registrations == 0 && service.removals == 0)
  }
  @Test func startActionRestartsStuckEngineAndWaitsThroughListenerGap() async {
    let service = BusyEngineService()
    var progress: [String] = []
    let startup = EngineStartup(pause: { service.elapse($0) })
    service.onProbe = {
      if let message = startup.progressMessage { progress.append(message) }
    }
    service.onRestart = {
      #expect(startup.phase == .restarting)
      #expect(startup.progressMessage == "Restarting Limmr’s background engine…")
    }
    await startup.recover(using: service, allowInstall: true, restartIfStalled: true)
    #expect(startup.phase == .ready && service.restarts == 1)
    #expect(progress.contains("Waiting for Limmr’s background engine to respond…"))
    #expect(service.registrations == 0 && service.removals == 0)
  }
  @Test(arguments: [false, true]) func failedRestartNeverReplacesEngine(commandFails: Bool) async {
    let service = BusyEngineService()
    service.restartFails = commandFails
    service.recoversAfterRestart = false
    let startup = EngineStartup(pause: { service.elapse($0) })
    await startup.recover(using: service, allowInstall: true, restartIfStalled: true)
    #expect(startup.phase == .stalled && service.restarts == 1)
    #expect(service.registrations == 0 && service.removals == 0)
    #expect(startup.failureMessage?.contains("couldn’t restart") == true)
  }
  @Test func kickstartUsesOnlyTheExistingJob() async throws {
    var commands: [[String]] = []
    try await LaunchdHelperDriver.restartJob(uid: 502, run: { executable, arguments in
      commands.append([executable] + arguments)
      return ""
    })
    #expect(commands == [["/bin/launchctl", "kickstart", "-k", "gui/502/dev.limmr.engine"]])
  }
  @Test func outdatedBusyEngineUpdatesWithoutRestartingOldBuild() async {
    let service = BusyEngineService()
    service.outdated = true
    let startup = EngineStartup(pause: { service.elapse($0) })
    await startup.recover(using: service)
    #expect(startup.phase == .ready)
    #expect(service.registrations == 1 && service.restarts == 0)
  }
  @Test func busyIsCancellable() async {
    do {
      _ = try await HelperReadinessWait.ready(probe: { .busy }, pause: { _ in throw CancellationError() })
      Issue.record("Cancellation ignored")
    } catch is CancellationError {} catch { Issue.record("Unexpected error") }
  }
}

@MainActor private final class BusyEngineService: EngineServiceManaging {
  var registration = EngineRegistration.registered
  var elapsed: TimeInterval = 0
  var readyAt: TimeInterval = .infinity
  var restartedAt: TimeInterval?
  var restarts = 0, registrations = 0, removals = 0
  var outdated = false
  var restartFails = false, recoversAfterRestart = true
  var onProbe: () -> Void = {}, onRestart: () -> Void = {}
  func elapse(_ duration: Duration) {
    elapsed += Double(duration.components.seconds) + Double(duration.components.attoseconds) / 1e18
  }
  func readiness() async -> HelperReadiness {
    onProbe()
    if let restartedAt {
      if elapsed - restartedAt < 2 { return .unavailable }
      if recoversAfterRestart && elapsed - restartedAt >= 4 { return .ready }
    }
    return elapsed >= readyAt ? .ready : .busy
  }
  func probe() async -> Bool { await readiness() == .ready }
  func needsUpdate() async throws -> Bool { outdated }
  func recordHealthyHelper() async throws {}
  func unregister() async throws { removals += 1 }
  func register() async throws { registrations += 1; if outdated { outdated = false; readyAt = elapsed } }
  func restart() async throws {
    onRestart()
    restarts += 1
    if restartFails { throw BackendError.invalidResponse }
    restartedAt = elapsed
  }
}
