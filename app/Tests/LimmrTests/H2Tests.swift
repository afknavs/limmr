import Foundation
import LimmrKit
import Testing
@testable import Limmr

@Suite("H2 staged helper updates", .serialized) @MainActor struct H2Tests {
  @Test(arguments: [false, true]) func healthyStagedUserAgentSwitchesToNewBundle(staleApproval: Bool) async throws {
    let root = FileManager.default.temporaryDirectory.appending(path: "limmr-h2-\(UUID().uuidString)")
    defer { try? FileManager.default.removeItem(at: root) }
    let bundle = root.appending(path: "New.app/Contents/Helpers/limmr-engine")
    let oldBundle = root.appending(path: "Old.app/Contents/Helpers/limmr-engine")
    for (path, name) in [(oldBundle, "previous"), (bundle, "next")] {
      try FileManager.default.createDirectory(at: path.deletingLastPathComponent(), withIntermediateDirectories: true)
      try FileManager.default.copyItem(at: URL(fileURLWithPath: "/usr/bin/true"), to: path)
      _ = try await UpdateCommand.run("/usr/bin/codesign", ["--force", "--sign", "-", "--identifier", "dev.limmr.h2.\(name)", path.path])
    }
    let home = root.appending(path: "Application Support/Limmr")
    let fallback = root.appending(path: "LaunchAgents/dev.limmr.engine.plist")
    let system = SystemActions(demo: true)
    let oldFiles = LaunchdHelperDriver(system: system, helper: oldBundle, home: home, fallback: fallback)
    let previous = try await oldFiles.identity()
    let oldPlan = try await oldFiles.prepare(previous, route: .userAgent)
    try FileManager.default.createDirectory(at: fallback.deletingLastPathComponent(), withIntermediateDirectories: true)
    try #require(oldPlan.plist).write(to: fallback)
    let oldPlist = try #require(oldPlan.plist)
    let properties = try #require(PropertyListSerialization.propertyList(from: oldPlist, format: nil) as? [String: Any])
    let oldPath = try #require((properties["ProgramArguments"] as? [String])?.first)
    try FileManager.default.removeItem(at: oldBundle.deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent())
    let files = LaunchdHelperDriver(system: system, helper: bundle, home: home, fallback: fallback)
    let driver = H2FileDriver(files: files, previous: .init(route: .userAgent, pid: 42, codeHash: previous.codeHash, executable: URL(fileURLWithPath: oldPath)))
    driver.registration = staleApproval ? .needsApproval : .registered
    let registration = HelperRegistration(driver: driver, pause: { _ in })
    #expect(try await registration.needsUpdate())
    try await registration.register()
    #expect(driver.events == ["prepare", "backup", "remove", "launch"])
    #expect(driver.current?.codeHash == (try await files.identity()).codeHash)
    let events = driver.events
    try await registration.register()
    #expect(driver.events == events)
  }
  @Test func concretePreflightErrorSurvivesStartupAndRetryClearsIt() async {
    let service = H2StartupService()
    var errors: [String] = []
    let startup = EngineStartup(pause: { _ in }, logError: { errors.append($0) })
    await startup.recover(using: service)
    #expect(startup.phase == .failed)
    #expect(startup.failureMessage?.contains("exit status 23") == true)
    #expect(startup.failureMessage?.contains("previous engine is still running") == true)
    #expect(errors == [startup.failureMessage!])
    service.failure = nil
    await startup.recover(using: service)
    #expect(startup.phase == .ready && startup.failureMessage == nil)
  }

  @Test func unknownSystemErrorsNeverExposePathsOrCredentials() {
    let error = NSError(domain: NSCocoaErrorDomain, code: 513,
      userInfo: [NSLocalizedDescriptionKey: "private credential value", NSFilePathErrorKey: "/Users/example/person/secrets"])
    let reason = HelperUpdateFailure.describing(error)
    #expect(reason.contains("513"))
    #expect(!reason.contains("private") && !reason.contains("credential") && !reason.contains("/Users"))
  }

  @Test func actualHelpFailureIncludesExitStatus() async throws {
    let root = FileManager.default.temporaryDirectory.appending(path: "limmr-h2-help-\(UUID().uuidString)")
    try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: root) }
    let executable = root.appending(path: "fake-helper")
    try Data("#!/bin/sh\nexit 23\n".utf8).write(to: executable)
    try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: executable.path)
    do { try await LaunchdHelperDriver.preflight(executable); Issue.record("Accepted a failed help check") }
    catch { #expect(HelperUpdateFailure.describing(error).contains("exit status 23")) }
  }

}

@MainActor private final class H2FileDriver: HelperRegistrationDriver {
  let files: LaunchdHelperDriver
  var current: HelperJob?
  var events: [String] = []
  var registration = EngineRegistration.registered
  init(files: LaunchdHelperDriver, previous: HelperJob) { self.files = files; current = previous }
  func healthy() async -> Bool { current?.pid != nil }
  func identity() async throws -> HelperIdentity { try await files.identity() }
  func retain(_ identity: HelperIdentity) async throws { try await files.retain(identity) }
  func job() async throws -> HelperJob? { current }
  func prepare(_ identity: HelperIdentity, route: HelperRoute) async throws -> HelperLaunchPlan {
    events.append("prepare"); return try await files.prepare(identity, route: route)
  }
  func backup(_ job: HelperJob) async throws -> HelperLaunchPlan {
    events.append("backup"); return try await files.backup(job)
  }
  func remove() async throws { events.append("remove"); current = nil }
  func launch(_ plan: HelperLaunchPlan) async throws {
    events.append("launch")
    let plist = try #require(plan.plist)
    let properties = try #require(PropertyListSerialization.propertyList(from: plist, format: nil) as? [String: Any])
    let path = try #require((properties["ProgramArguments"] as? [String])?.first)
    current = .init(route: plan.route, pid: 43, codeHash: plan.identity.codeHash, executable: URL(fileURLWithPath: path))
  }
}

@MainActor private final class H2StartupService: EngineServiceManaging {
  var registration = EngineRegistration.needsApproval // stale SM status; user job owns the engine
  var failure: (any Error)? = HelperUpdateFailure(reason: "Preparing the new helper failed. The helper help check failed (exit status 23, exit).")
  var changed = true
  func probe() async -> Bool { true }
  func needsUpdate() async throws -> Bool { changed }
  func recordHealthyHelper() async throws {}
  func unregister() async throws { Issue.record("Startup must leave replacement to registration") }
  func register() async throws { if let failure { throw failure }; changed = false }
}
