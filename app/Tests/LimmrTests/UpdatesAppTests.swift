import Foundation
import Testing
@testable import Limmr
@testable import LimmrKit

@Suite("U1 app installation safety") struct UpdatesAppTests {
  @Test @MainActor func developmentBuildCannotEnableAutomaticUpdating() {
    let controller = UpdateController()
    #expect(!controller.configured)
  }
  @Test func anUnsupportedOrBusyEngineNeverSwapsTheBundle() async {
    for response in [Data(#"{"paused":false,"active_sends":1}"#.utf8), Data(#"{"paused":true,"active_sends":1}"#.utf8), Data("{}".utf8)] {
      let backend = UpdateGateBackend(response: response)
      // Nonexistent paths also ensure a regression cannot alter a real app/service.
      let plan = UpdatePlan(installed: URL(fileURLWithPath: "/nonexistent/Limmr.app"),
                            workspace: URL(fileURLWithPath: "/nonexistent/.limmr-update-test"), parentPID: 0)
      let installer = BundleUpdateInstaller(plan: plan, backend: backend, expectedVersion: "0.3.0")
      await #expect(throws: (any Error).self) { try await UpdateTransaction.install(using: installer, timeout: .milliseconds(10)) }
      #expect(!(await installer.exchanged))
      #expect(await backend.calls == ["POST /v1/updates/prepare", "POST /v1/updates/resume"])
    }
  }
  @Test func statusWaitHonorsItsDeadlineWithoutLaunchingAnything() async {
    let backend = UpdateGateBackend(response: Data("{}".utf8))
    let plan = UpdatePlan(installed: URL(fileURLWithPath: "/nonexistent/Limmr.app"),
                          workspace: URL(fileURLWithPath: "/nonexistent/.limmr-update-test"), parentPID: 0)
    let installer = BundleUpdateInstaller(plan: plan, backend: backend, expectedVersion: "0.3.0")
    let start = ContinuousClock.now
    #expect(!(await installer.engineResponds(timeout: .milliseconds(10))))
    #expect(start.duration(to: .now) < .seconds(1))
    #expect(await backend.calls == ["GET /v1/status"])
  }
  @Test func completeInstallerUsesExpectedVersionAndResumes() async throws {
    let fixture = InstallerFixture()
    let installer = makeInstaller(fixture)
    try await UpdateTransaction.install(using: installer, timeout: .milliseconds(20))
    #expect(await installer.exchanged)
    #expect(await fixture.events == ["prepare", "status:0.1.0", "unregister", "status:offline",
                                     "exchange", "register", "status:0.3.0", "relaunch", "resume"])
  }
  @Test func oldAuthenticatedEngineAfterSwapRollsBack() async {
    let fixture = InstallerFixture(failure: .wrongVersion)
    let installer = makeInstaller(fixture)
    await #expect(throws: UpdateError.installFailed) {
      try await UpdateTransaction.install(using: installer, timeout: .milliseconds(20))
    }
    #expect(!(await installer.exchanged))
    #expect(await fixture.events == ["prepare", "status:0.1.0", "unregister", "status:offline",
      "exchange", "register", "status:0.1.0", "unregister", "exchange", "register",
      "status:0.1.0", "relaunch", "resume"])
  }
  @Test func missingEngineVersionNeverPassesHandshake() async {
    let fixture = InstallerFixture(failure: .missingVersion)
    let installer = makeInstaller(fixture)
    #expect(!(await installer.engineResponds(timeout: .milliseconds(20))))
  }
  @Test func everyInstallerFailureAttemptsResume() async {
    for failure in [InstallerFixture.Failure.prepare, .refused, .drainTimeout, .exchange, .register, .restore] {
      let fixture = InstallerFixture(failure: failure)
      let installer = makeInstaller(fixture)
      await #expect(throws: (any Error).self) {
        try await UpdateTransaction.install(using: installer, timeout: .milliseconds(20))
      }
      let events = await fixture.events
      #expect(events.last == "resume")
      if [.prepare, .refused, .drainTimeout].contains(failure) {
        #expect(events == ["prepare", "resume"])
        #expect(!(await installer.exchanged))
      }
      if failure == .exchange {
        // Original registration is repaired even though the atomic exchange failed.
        #expect(events.suffix(3) == ["exchange", "register", "resume"])
      }
      if failure == .restore { #expect(!events.contains("relaunch")) }
    }
  }
  private func makeInstaller(_ fixture: InstallerFixture) -> BundleUpdateInstaller {
    let plan = UpdatePlan(installed: URL(fileURLWithPath: "/nonexistent/Limmr.app"),
                          workspace: URL(fileURLWithPath: "/nonexistent/.limmr-update-test"), parentPID: 0)
    return BundleUpdateInstaller(plan: plan, backend: fixture, expectedVersion: "0.3.0", system: fixture)
  }
  @Test func aPlistClaimingATeamDoesNotEstablishCodeTrust() throws {
    let root = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
    let app = root.appending(path: "Limmr.app")
    try FileManager.default.createDirectory(at: app.appending(path: "Contents"), withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: root) }
    let plist: [String: Any] = ["CFBundleIdentifier": "dev.limmr.app", "TeamIdentifier": "FAKE123456"]
    try PropertyListSerialization.data(fromPropertyList: plist, format: .xml, options: 0)
      .write(to: app.appending(path: "Contents/Info.plist"))
    #expect(throws: (any Error).self) { try UpdateSecurity.verify(candidate: app, installed: app) }
  }
}
private actor UpdateGateBackend: Backend {
  let response: Data
  private(set) var calls: [String] = []
  init(response: Data) { self.response = response }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    calls.append("\(method) \(path)")
    return response
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}

/// All filesystem/service effects and authenticated responses share this fake state.
private actor InstallerFixture: Backend, UpdateSystem {
  enum Failure { case prepare, refused, drainTimeout, exchange, register, restore, wrongVersion, missingVersion }
  let failure: Failure?
  private(set) var events: [String] = []
  private var running = true
  private var swapped = false
  init(failure: Failure? = nil) { self.failure = failure }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if path == "/v1/updates/prepare" {
      events.append("prepare")
      if failure == .prepare { throw UpdateError.unavailable }
      if failure == .refused { return Data(#"{"paused":false,"active_sends":0}"#.utf8) }
      if failure == .drainTimeout { return Data(#"{"paused":false,"active_sends":1}"#.utf8) }
      return Data(#"{"paused":true,"active_sends":0}"#.utf8)
    }
    if path == "/v1/updates/resume" { events.append("resume"); return Data(#"{"paused":false}"#.utf8) }
    guard path == "/v1/status" else { throw UpdateError.unavailable }
    guard running else { events.append("status:offline"); throw UpdateError.unavailable }
    let version: String? = failure == .missingVersion ? nil :
      (swapped && failure != .wrongVersion && failure != .restore ? "0.3.0" : "0.1.0")
    events.append("status:\(version ?? "missing")")
    return try Wire.data(Status(engineVersion: version, linked: true, connected: true, accountName: "Example",
                                pendingApprovals: 0, agents: [], health: "ok"))
  }
  func unregister(_ installed: URL) { events.append("unregister"); running = false }
  func register(_ installed: URL) throws {
    events.append("register")
    if swapped && failure == .register { throw UpdateError.unavailable }
    running = true
  }
  func exchange(_ installed: URL, _ candidate: URL) throws {
    events.append("exchange")
    if failure == .exchange || swapped && failure == .restore { throw UpdateError.installFailed }
    swapped.toggle()
  }
  func relaunch(_ installed: URL) { events.append("relaunch") }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}
