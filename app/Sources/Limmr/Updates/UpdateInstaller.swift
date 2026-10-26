import AppKit
import Darwin
import Foundation
import LimmrKit
import ServiceManagement

struct UpdatePlan: Codable, Sendable {
  let installed: URL
  let workspace: URL
  let parentPID: Int32
  var candidate: URL { workspace.appending(path: "Previous.app") }
}

/// Injectable OS boundary: contract tests never launch a bundle, touch launchd or swap files.
protocol UpdateSystem: Sendable {
  func unregister(_ installed: URL) async throws
  func register(_ installed: URL) async throws
  func exchange(_ installed: URL, _ candidate: URL) async throws
  func relaunch(_ installed: URL) async throws
}
struct SystemUpdateOperations: UpdateSystem {
  func unregister(_ installed: URL) async throws {
    _ = try await UpdateCommand.run(installed.appending(path: "Contents/MacOS/Limmr").path,
                                    ["--limmr-unregister-engine"])
  }
  func register(_ installed: URL) async throws {
    _ = try await UpdateCommand.run(installed.appending(path: "Contents/MacOS/Limmr").path,
                                    ["--limmr-repair-engine"])
  }
  func exchange(_ installed: URL, _ candidate: URL) throws {
    guard renameatx_np(AT_FDCWD, installed.path, AT_FDCWD, candidate.path, UInt32(RENAME_SWAP)) == 0
    else { throw UpdateError.installFailed }
  }
  func relaunch(_ installed: URL) async throws {
    _ = try await UpdateCommand.run("/usr/bin/open", ["-n", installed.path])
  }
}

actor BundleUpdateInstaller: UpdateInstalling {
  let plan: UpdatePlan
  let backend: any Backend
  let system: any UpdateSystem
  private var expectedVersion: String
  private var previousVersion: String?
  private(set) var exchanged = false
  init(plan: UpdatePlan, backend: any Backend, expectedVersion: String,
       system: any UpdateSystem = SystemUpdateOperations()) {
    self.plan = plan; self.backend = backend; self.expectedVersion = expectedVersion; self.system = system
  }
  // Called only after verifying the signed feed, never from untrusted plan metadata.
  func expectVersion(_ version: String) { expectedVersion = version }

  func swap() async throws {
    struct Pause: Decodable, Sendable { let paused: Bool; let activeSends: Int }
    var registrationTouched = false
    do {
      guard !expectedVersion.isEmpty else { throw UpdateError.unsafeToRestart }
      let data = try await backend.request("/v1/updates/prepare", method: "POST", body: nil)
      let decoder = JSONDecoder(); decoder.keyDecodingStrategy = .convertFromSnakeCase
      let pause = try decoder.decode(Pause.self, from: data)
      guard pause.paused && pause.activeSends == 0 else { throw UpdateError.unsafeToRestart }
      let oldStatus = try await backend.get("/v1/status", as: Status.self)
      guard let version = oldStatus.engineVersion, !version.isEmpty else { throw UpdateError.unsafeToRestart }
      previousVersion = version
      registrationTouched = true
      try await system.unregister(plan.installed)
      var stopped = false
      for _ in 0..<20 {
        if (try? await backend.get("/v1/status", as: Status.self)) == nil { stopped = true; break }
        try await Task.sleep(for: .milliseconds(250))
      }
      guard stopped else { throw UpdateError.unsafeToRestart }
      try await exchange()
    } catch {
      // Unregister may have succeeded before the filesystem failed. Repair the original.
      if registrationTouched { try? await registerEngine() }
      throw error
    }
  }
  private func exchange() async throws {
    try await system.exchange(plan.installed, plan.candidate)
    exchanged.toggle()
  }
  func registerEngine() async throws { try await system.register(plan.installed) }
  func resume() async { try? await backend.post("/v1/updates/resume") }
  func engineResponds(timeout: Duration) async -> Bool {
    let backend = self.backend
    let expected = expectedVersion
    guard !expected.isEmpty else { return false }
    return await withTaskGroup(of: Bool.self) { group in
      group.addTask {
        while !Task.isCancelled {
          // Authentication alone could be an old helper still serving the port.
          if let status = try? await backend.get("/v1/status", as: Status.self),
             status.engineVersion == expected { return true }
          do { try await Task.sleep(for: .milliseconds(500)) } catch { return false }
        }
        return false
      }
      group.addTask { try? await Task.sleep(for: timeout); return false }
      let result = await group.next() ?? false
      group.cancelAll()
      return result
    }
  }
  func restore() async throws {
    try await system.unregister(plan.installed)
    try await exchange()
    expectedVersion = previousVersion ?? ""
    UpdateOutcome.write("The update couldn’t start. Limmr restored the previous version.")
  }
  func relaunch() async throws { try await system.relaunch(plan.installed) }
}

@MainActor enum UpdateEntrypoint {
  /// A detached copy of this executable survives replacing/quitting the app. Engine registration
  /// runs INSIDE the newly installed bundle so SMAppService resolves its bundled plist correctly.
  static func handle(_ arguments: [String]) -> Bool {
    guard let mode = arguments.dropFirst().first, ["--limmr-update-helper", "--limmr-repair-engine", "--limmr-unregister-engine"].contains(mode) else { return false }
    NSApplication.shared.setActivationPolicy(.prohibited)
    Task {
      do {
        if mode == "--limmr-update-helper" {
          guard arguments.count == 3 else { throw UpdateError.invalidFeed }
          try await install(planURL: URL(fileURLWithPath: arguments[2]))
        } else {
          try UpdateSecurity.validateInstalled(Bundle.main.bundleURL)
          let actions = SystemActions(demo: false)
          if mode == "--limmr-repair-engine" { try await actions.startEngine() }
          else { try await actions.unregister() }
        }
        exit(0)
      } catch {
        // Only curated helper reasons and numeric OS codes; never raw localized errors.
        fputs("Limmr update could not finish. " + HelperUpdateFailure.describing(error) + "\n", stderr)
        exit(1)
      }
    }
    return true
  }

  private static func install(planURL: URL) async throws {
    let plan = try JSONDecoder().decode(UpdatePlan.self, from: Data(contentsOf: planURL))
    let fm = FileManager.default
    guard plan.workspace.deletingLastPathComponent().standardizedFileURL == plan.installed.deletingLastPathComponent().standardizedFileURL,
          plan.workspace.lastPathComponent.hasPrefix(".limmr-update-"),
          plan.installed.pathExtension == "app", plan.installed.lastPathComponent == "Limmr.app",
          planURL.standardizedFileURL == plan.workspace.appending(path: "plan.json").standardizedFileURL,
          plan.installed.resolvingSymlinksInPath() == plan.installed.standardizedFileURL,
          plan.workspace.resolvingSymlinksInPath() == plan.workspace.standardizedFileURL,
          let bundle = Bundle(url: plan.installed),
          let keyText = bundle.object(forInfoDictionaryKey: "LimmrUpdatePublicKey") as? String,
          let key = Data(base64Encoded: keyText)
    else { throw UpdateError.unsafeBundle }
    let configuration = URLSessionConfiguration.ephemeral
    // The engine bounds prepare at 10 seconds; transport must allow that drain.
    configuration.timeoutIntervalForRequest = 15; configuration.timeoutIntervalForResource = 15
    let backend = APIClient(credentialFile: RuntimePaths().home.appending(path: "control-token"),
                            session: URLSession(configuration: configuration))
    let installer = BundleUpdateInstaller(plan: plan, backend: backend, expectedVersion: "")
    do {
      let feed = try UpdateFeed.verified(Data(contentsOf: plan.workspace.appending(path: "appcast.json")), publicKey: key)
      try feed.requireUpgrade(from: bundle.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "0",
                              build: Int(bundle.object(forInfoDictionaryKey: "CFBundleVersion") as? String ?? "0") ?? 0,
                              macOS: UpdateController.osVersion)
      try feed.verifyArchive(plan.workspace.appending(path: "Limmr.zip"))
      try UpdateSecurity.prepare(candidate: plan.candidate, feed: feed, installed: plan.installed)
      // Do not swap underneath a user who cancelled app termination.
      for _ in 0..<100 {
        if kill(plan.parentPID, 0) != 0 { break }
        try await Task.sleep(for: .milliseconds(300))
      }
      guard kill(plan.parentPID, 0) != 0 else { throw UpdateError.unsafeToRestart }
      await installer.expectVersion(feed.version)
      try await UpdateTransaction.install(using: installer)
    } catch {
      // A validation/prepare failure must reopen the old app after its parent exits.
      // A failed rollback leaves both bundles intact and requires manual recovery.
      if await installer.exchanged {
        UpdateOutcome.write("The update needs manual recovery. The previous app is retained in its backup folder.")
      } else {
        UpdateOutcome.write("The update couldn’t start. Limmr kept the previous version.")
      }
      if !(await installer.exchanged), kill(plan.parentPID, 0) != 0 {
        try? await installer.relaunch()
      }
      throw error
    }
    // Keep Previous.app for manual recovery; never delete the only rollback copy.
    try? fm.removeItem(at: plan.workspace.appending(path: "Limmr.zip"))
    try? fm.removeItem(at: plan.workspace.appending(path: "runner"))
  }
}

/// A plain, nonsecret outcome survives quitting the old UI. No raw system errors are persisted.
enum UpdateOutcome {
  static var url: URL { RuntimePaths().home.appending(path: "update-result") }
  static func write(_ message: String) {
    try? Data(message.utf8).write(to: url, options: .atomic)
    try? FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
  }
}
