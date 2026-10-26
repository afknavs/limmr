import Darwin
import Foundation
import ServiceManagement
import LimmrKit
import Testing
@testable import Limmr

@Suite("H1 helper registration") @MainActor struct H1Tests {
  @Test func kernelFingerprintMatchesOwnSignedExecutable() throws {
    var buffer = [CChar](repeating: 0, count: 4 * Int(MAXPATHLEN))
    #expect(proc_pidpath(getpid(), &buffer, UInt32(buffer.count)) > 0)
    let path = String(decoding: buffer.prefix { $0 != 0 }.map { UInt8(bitPattern: $0) }, as: UTF8.self)
    let disk = try HelperCodeSignature.identity(URL(fileURLWithPath: path))
    #expect(disk.matches(HelperCodeSignature.runningHash(getpid())))
  }

  @Test func replacingDiskImageDoesNotChangeRunningFingerprint() async throws {
    let root = FileManager.default.temporaryDirectory.appending(path: "limmr-h1-image-\(UUID().uuidString)")
    try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: root) }
    let image = root.appending(path: "helper"), next = root.appending(path: "replacement")
    for (url, identifier) in [(image, "dev.limmr.test.previous"), (next, "dev.limmr.test.next")] {
      try FileManager.default.copyItem(at: URL(fileURLWithPath: "/bin/cat"), to: url)
      _ = try await UpdateCommand.run("/usr/bin/codesign", ["--force", "--sign", "-", "--identifier", identifier, url.path])
    }
    let original = try HelperCodeSignature.identity(image)
    let process = Process(), input = Pipe()
    process.executableURL = image; process.standardInput = input
    process.standardOutput = FileHandle.nullDevice; process.standardError = FileHandle.nullDevice
    try process.run()
    defer { process.terminate(); try? input.fileHandleForWriting.close(); process.waitUntilExit() }
    // The code signature is readable only once the child has exec'd; give it a moment.
    for _ in 0..<50 where HelperCodeSignature.runningHash(process.processIdentifier) == nil { try await Task.sleep(for: .milliseconds(20)) }
    #expect(original.matches(HelperCodeSignature.runningHash(process.processIdentifier)))
    try FileManager.default.moveItem(at: image, to: root.appending(path: "previous-image"))
    try FileManager.default.moveItem(at: next, to: image)
    #expect(try !HelperCodeSignature.identity(image).matches(HelperCodeSignature.runningHash(process.processIdentifier)))
    #expect(original.matches(HelperCodeSignature.runningHash(process.processIdentifier)))
  }

  @Test func preflightHasHomeAndOnlyRequestsHelp() async throws {
    let root = FileManager.default.temporaryDirectory.appending(path: "limmr-h1-preflight-\(UUID().uuidString)")
    try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: root) }
    let executable = root.appending(path: "fake-engine")
    try Data("#!/bin/sh\n[ \"$1\" = -h ] && [ -n \"$HOME\" ] && [ -n \"$LIMMR_HOME\" ]\n".utf8).write(to: executable)
    try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: executable.path)
    try await LaunchdHelperDriver.preflight(executable)
  }

  @Test(arguments: [SMAppService.Status.enabled, .notFound, .notRegistered])
  func unregisterCleansBothFormsAndIsIdempotent(status: SMAppService.Status) async throws {
    let root = FileManager.default.temporaryDirectory.appending(path: "limmr-h1-\(UUID().uuidString)")
    try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: root) }
    let plist = root.appending(path: "dev.limmr.engine.plist")
    try Data("synthetic fallback".utf8).write(to: plist)
    var present = true
    var events: [String] = []
    for _ in 0..<2 {
      try await HelperLaunchCleanup.remove(status: status,
        unregister: { events.append("unregister") }, jobExists: { present },
        bootout: { events.append("bootout"); present = false }, fallback: plist)
    }
    #expect(!FileManager.default.fileExists(atPath: plist.path))
    #expect(events.filter { $0 == "bootout" }.count == 1)
    #expect(events.filter { $0 == "unregister" }.count == (status == .notRegistered ? 0 : 2))
  }

  @Test func staleNotFoundRecordDoesNotBlockFallbackCleanup() async throws {
    var removed = false
    try await HelperLaunchCleanup.remove(status: .notFound,
      unregister: { throw BackendError.invalidResponse }, jobExists: { true },
      bootout: { removed = true }, fallback: URL(fileURLWithPath: "/nonexistent/limmr-h1.plist"), pause: { _ in })
    #expect(removed)
  }

  @Test func staleApprovalRecordIsLeftAloneAndUserAgentIsRemoved() async throws {
    var removed = false, unregistered = false
    try await HelperLaunchCleanup.remove(status: .requiresApproval,
      unregister: { unregistered = true; throw NSError(domain: "SMAppServiceErrorDomain", code: 144) }, jobExists: { true },
      bootout: { removed = true }, fallback: URL(fileURLWithPath: "/nonexistent/limmr-h1.plist"), pause: { _ in })
    #expect(removed && !unregistered)
  }

  @Test func liveUserAgentNeverTouchesTheLoginItemRecord() async throws {
    var removed = false, unregistered = false
    try await HelperLaunchCleanup.remove(status: .enabled,
      unregister: { unregistered = true; throw NSError(domain: "SMAppServiceErrorDomain", code: 144) }, jobExists: { true },
      bootout: { removed = true }, fallback: URL(fileURLWithPath: "/nonexistent/limmr-h1.plist"), liveIsUserAgent: { true }, pause: { _ in })
    #expect(removed && !unregistered)
  }

  @Test func bootstrapClearsAResubmittedLoginItemAndRetries() async throws {
    var tries = 0, bootouts = 0
    try await HelperLaunchCleanup.bootstrap(run: { tries += 1; if tries == 1 { throw BackendError.invalidResponse } },
      staleServiceJob: { true }, bootout: { bootouts += 1 }, pause: { _ in })
    #expect(tries == 2 && bootouts == 1)
  }

  @Test func bootstrapNeverBootsOutAUserAgentItDidNotExpect() async throws {
    var bootouts = 0, tries = 0
    await #expect(throws: (any Error).self) {
      try await HelperLaunchCleanup.bootstrap(run: { tries += 1; throw BackendError.invalidResponse },
        staleServiceJob: { false }, bootout: { bootouts += 1 }, pause: { _ in })
    }
    #expect(bootouts == 0 && tries == 3)
  }

  @Test func bootstrapRetriesWhileTheOldUserAgentIsStillExiting() async throws {
    var tries = 0
    try await HelperLaunchCleanup.bootstrap(run: { tries += 1; if tries < 3 { throw BackendError.invalidResponse } },
      staleServiceJob: { false }, bootout: {}, pause: { _ in })
    #expect(tries == 3)
  }

  @Test func removeWaitsForLaunchdToReleaseTheLabel() async throws {
    var checks = 0, waits = 0
    try await HelperLaunchCleanup.remove(status: .notRegistered, unregister: {}, jobExists: { checks += 1; return checks < 4 },
      bootout: {}, fallback: URL(fileURLWithPath: "/nonexistent/limmr-h1.plist"), pause: { _ in waits += 1 })
    #expect(waits == 2)
  }

  @Test func bootstrapGivesUpAfterThreeTries() async throws {
    var tries = 0
    await #expect(throws: (any Error).self) {
      try await HelperLaunchCleanup.bootstrap(run: { tries += 1; throw BackendError.invalidResponse },
        staleServiceJob: { true }, bootout: {}, pause: { _ in })
    }
    #expect(tries == 3)
  }

  @Test func enabledRecordUnregisterFailureStillStopsTheUpdate() async throws {
    var removed = false
    await #expect(throws: (any Error).self) {
      try await HelperLaunchCleanup.remove(status: .enabled,
        unregister: { throw NSError(domain: "SMAppServiceErrorDomain", code: 144) }, jobExists: { true },
        bootout: { removed = true }, fallback: URL(fileURLWithPath: "/nonexistent/limmr-h1.plist"), pause: { _ in })
    }
    #expect(!removed)
  }

  @Test func adHocSkipsSMAndCleansStaleLWCR() async throws {
    let driver = HelperDriverFake()
    driver.current = .init(route: .serviceManagement, pid: nil, codeHash: nil, executable: nil)
    try await HelperRegistration(driver: driver, pause: { _ in }).register()
    #expect(driver.events == ["prepare-user", "remove", "launch-user"])
    #expect(driver.alive && driver.current?.route == .userAgent)
  }

  @Test(arguments: [false, true]) func SMRefusalOrEXCONFIGFallsBack(registerThrows: Bool) async throws {
    let driver = HelperDriverFake(developerID: true)
    driver.refuseSM = registerThrows
    driver.smSpawnFails = !registerThrows // register returned success, but job has no PID / health
    try await HelperRegistration(driver: driver, pause: { _ in }).register()
    #expect(driver.events == ["prepare-sm", "prepare-user", "remove", "launch-sm", "remove", "launch-user"])
    #expect(driver.current?.route == .userAgent && driver.alive)
  }

  @Test func existingFallbackStartsThroughFallback() async throws {
    let driver = HelperDriverFake(developerID: true)
    driver.current = .init(route: .userAgent, pid: nil, codeHash: nil, executable: nil)
    try await HelperRegistration(driver: driver, pause: { _ in }).register()
    #expect(driver.events == ["prepare-user", "remove", "launch-user"])
  }

  @Test func healthyManualFallbackAdoptedWithoutReceiptOrRegistration() async throws {
    let driver = HelperDriverFake()
    driver.installHealthy(driver.desired, route: .userAgent)
    let helper = HelperRegistration(driver: driver, pause: { _ in })
    #expect(try await !helper.needsUpdate())
    for _ in 0..<3 { try await helper.register() }
    #expect(driver.events.isEmpty)
  }

  @Test(arguments: [HelperRoute.userAgent, .serviceManagement]) func oldHelperUsesItsExistingRoute(route: HelperRoute) async throws {
    let driver = HelperDriverFake(developerID: route == .serviceManagement)
    driver.installHealthy(driver.previous, route: route)
    let helper = HelperRegistration(driver: driver, pause: { _ in })
    #expect(try await helper.needsUpdate())
    try await helper.register()
    #expect(driver.current?.codeHash == driver.desired.codeHash)
    #expect(driver.current?.route == route)
    #expect(driver.events.first == (route == .userAgent ? "prepare-user" : "prepare-sm"))
    #expect(driver.events.firstIndex(of: "backup")! < driver.events.firstIndex(of: "remove")!)
    driver.events = []
    try await helper.register()
    #expect(driver.events.isEmpty) // Try again / reopened window adopts the same process
  }

  @Test(arguments: ["prepare", "backup"]) func preflightFailureNeverStopsHealthyJob(failure: String) async {
    let driver = HelperDriverFake()
    driver.installHealthy(driver.previous, route: .userAgent)
    driver.failure = failure
    let helper = HelperRegistration(driver: driver, pause: { _ in })
    for _ in 0..<2 {
      do { try await helper.register(); Issue.record("Expected safe failure") } catch {}
    }
    #expect(driver.alive && driver.current?.codeHash == driver.previous.codeHash)
    #expect(!driver.events.contains("remove"))
  }

  @Test func helperThatRecoversDuringPreflightIsAdopted() async throws {
    let driver = HelperDriverFake()
    driver.recoverDuringPrepare = true
    try await HelperRegistration(driver: driver, pause: { _ in }).register()
    #expect(driver.alive && !driver.events.contains("remove"))
    #expect(!driver.events.contains("launch-user"))
  }

  @Test func failedRemovalLeavesHealthyEngineAlone() async {
    let driver = HelperDriverFake()
    driver.installHealthy(driver.previous, route: .userAgent)
    driver.failure = "remove"
    let helper = HelperRegistration(driver: driver, pause: { _ in })
    for _ in 0..<2 {
      do { try await helper.register(); Issue.record("Expected removal failure") } catch {}
    }
    #expect(driver.alive && driver.current?.codeHash == driver.previous.codeHash)
    #expect(driver.events.filter { $0 == "remove" }.count == 2)
    #expect(!driver.events.contains("launch-user"))
  }

  @Test func failedNewHelperRestoresPreviousAndRetryCanRecover() async throws {
    let driver = HelperDriverFake()
    driver.installHealthy(driver.previous, route: .userAgent)
    driver.failure = "new-health"
    let helper = HelperRegistration(driver: driver, pause: { _ in })
    do { try await helper.register(); Issue.record("Accepted failed candidate") } catch {}
    #expect(driver.alive && driver.current?.codeHash == driver.previous.codeHash)
    #expect(driver.events.suffix(2) == ["remove", "launch-user"])
    driver.failure = ""
    try await helper.register()
    #expect(driver.alive && driver.current?.codeHash == driver.desired.codeHash)
    let events = driver.events
    try await helper.register()
    #expect(driver.events == events)
  }

  @Test func healthyUnknownProcessNeverStopped() async {
    let driver = HelperDriverFake()
    driver.alive = true
    let helper = HelperRegistration(driver: driver, pause: { _ in })
    do { _ = try await helper.needsUpdate(); Issue.record("Unknown identity accepted") } catch {}
    do { try await helper.register(); Issue.record("Unknown process replaced") } catch {}
    #expect(driver.events.isEmpty && driver.alive)
  }

  @Test func approvalRefusalDoesNotBypassUserConsent() async {
    let driver = HelperDriverFake(developerID: true)
    driver.approvalOnSM = true
    do { try await HelperRegistration(driver: driver, pause: { _ in }).register(); Issue.record("Approval bypassed") } catch {}
    #expect(!driver.events.contains("launch-user"))
  }

  @Test func missingHelperOrBadSignatureNeverStopsHealthyJob() async {
    let driver = HelperDriverFake()
    driver.installHealthy(driver.previous, route: .userAgent)
    driver.failure = "identity"
    do { try await HelperRegistration(driver: driver, pause: { _ in }).register(); Issue.record("Bad helper accepted") } catch {}
    #expect(driver.alive && driver.events.isEmpty)
  }

  @Test func cancellationBeforeRemovalPreservesEngine() async {
    let driver = HelperDriverFake()
    driver.installHealthy(driver.previous, route: .userAgent)
    let task = Task { @MainActor in
      withUnsafeCurrentTask { $0?.cancel() }
      do { try await HelperRegistration(driver: driver, pause: { _ in }).register(); Issue.record("Cancellation ignored") } catch {}
    }
    await task.value
    #expect(driver.alive && !driver.events.contains("remove"))
  }
}

@MainActor private final class HelperDriverFake: HelperRegistrationDriver {
  var desired: HelperIdentity
  let previous = HelperIdentity(sha256: "previous", codeHash: "old-kernel-hash", developerID: false)
  var current: HelperJob?
  var alive = false
  var events: [String] = []
  var failure = ""
  var recoverDuringPrepare = false
  var refuseSM = false
  var smSpawnFails = false
  var approvalOnSM = false
  var registration = EngineRegistration.registered
  init(developerID: Bool = false) { desired = .init(sha256: "candidate", codeHash: "new-kernel-hash", developerID: developerID) }
  func healthy() async -> Bool { alive }
  func identity() async throws -> HelperIdentity {
    if failure == "identity" { throw BackendError.invalidResponse }
    return desired
  }
  func retain(_ identity: HelperIdentity) async throws {}
  func job() async throws -> HelperJob? { current }
  func prepare(_ identity: HelperIdentity, route: HelperRoute) async throws -> HelperLaunchPlan {
    events.append(route == .userAgent ? "prepare-user" : "prepare-sm")
    if failure == "prepare" { throw BackendError.invalidResponse }
    if recoverDuringPrepare { installHealthy(desired, route: .userAgent) }
    return .init(identity: identity, route: route, plist: nil)
  }
  func backup(_ job: HelperJob) async throws -> HelperLaunchPlan {
    events.append("backup")
    if failure == "backup" { throw BackendError.invalidResponse }
    return .init(identity: previous, route: .userAgent, plist: nil)
  }
  func remove() async throws {
    events.append("remove")
    if failure == "remove" { throw BackendError.invalidResponse }
    current = nil; alive = false
  }
  func launch(_ plan: HelperLaunchPlan) async throws {
    events.append(plan.route == .userAgent ? "launch-user" : "launch-sm")
    if plan.route == .serviceManagement {
      if approvalOnSM { registration = .needsApproval; throw BackendError.invalidResponse }
      if refuseSM { throw BackendError.invalidResponse }
      if smSpawnFails {
        current = .init(route: .serviceManagement, pid: nil, codeHash: nil, executable: nil)
        return
      }
    }
    installHealthy(plan.identity, route: plan.route)
    if failure == "new-health" && plan.identity == desired { alive = false }
  }
  func installHealthy(_ identity: HelperIdentity, route: HelperRoute) {
    current = .init(route: route, pid: 42, codeHash: identity.codeHash, executable: URL(fileURLWithPath: "/synthetic/helper"))
    alive = true
  }
}
