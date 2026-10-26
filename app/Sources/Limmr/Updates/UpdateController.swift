import AppKit
import Foundation
import LimmrKit
import SwiftUI

@MainActor @Observable final class UpdateController {
  static let shared = UpdateController()
  var mode: UpdateMode = .automatically { didSet { persist() } }
  private(set) var available: UpdateFeed?
  private(set) var workspace: URL?
  private(set) var busy = false
  private(set) var message: String?
  var showingNotes = false
  private var lastAttempt: Date?
  @ObservationIgnored private var monitorTask: Task<Void, Never>?
  private let checker = UpdateChecker()
  private let bundle: Bundle
  private let settingsURL: URL
  private let sample: Bool

  init(bundle: Bundle = .main, sample: Bool = false) {
    self.bundle = bundle; self.sample = sample
    settingsURL = RuntimePaths().home.appending(path: "updates.plist")
    if !sample, let data = try? Data(contentsOf: settingsURL),
       let settings = try? PropertyListDecoder().decode(Preferences.self, from: data) {
      mode = UpdateMode(rawValue: settings.mode) ?? .automatically
      lastAttempt = settings.lastAttempt
      if let path = settings.workspace {
        let saved = URL(fileURLWithPath: path).standardizedFileURL
        let target = bundle.bundleURL.standardizedFileURL
        if saved.lastPathComponent.hasPrefix(".limmr-update-"),
           saved.deletingLastPathComponent() == target.deletingLastPathComponent(),
           saved.resolvingSymlinksInPath() == saved,
           let keyText = bundle.object(forInfoDictionaryKey: "LimmrUpdatePublicKey") as? String,
           let key = Data(base64Encoded: keyText),
           (try? UpdateSecurity.validateInstalled(target)) != nil,
           let data = try? Data(contentsOf: saved.appending(path: "appcast.json")),
           let feed = try? UpdateFeed.verified(data, publicKey: key),
           (try? feed.requireUpgrade(from: bundle.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "0",
                                     build: Int(bundle.object(forInfoDictionaryKey: "CFBundleVersion") as? String ?? "0") ?? 0,
                                     macOS: Self.osVersion)) != nil {
          // Re-verify the full archive and code again in the detached runner before swapping.
          workspace = saved; available = feed
        }
      }
    }
    if !sample, let outcome = try? String(contentsOf: UpdateOutcome.url, encoding: .utf8) {
      message = outcome
      try? FileManager.default.removeItem(at: UpdateOutcome.url)
    }
    if sample {
      workspace = FileManager.default.temporaryDirectory.appending(path: "limmr-update-preview")
      available = UpdateFeed(version: "0.3", build: 3, minMacOS: "26.0", notesMarkdown: "A calmer Home and more dependable background connections.",
                             url: URL(string: "https://example.invalid/Limmr.zip")!, size: 1, sha256: String(repeating: "0", count: 64))
    }
  }
  struct Preferences: Codable { let mode: String; let lastAttempt: Date?; var workspace: String? = nil }
  private func persist() {
    guard !sample else { return }
    do {
      try FileManager.default.createDirectory(at: settingsURL.deletingLastPathComponent(), withIntermediateDirectories: true,
                                             attributes: [.posixPermissions: 0o700])
      try PropertyListEncoder().encode(Preferences(mode: mode.rawValue, lastAttempt: lastAttempt, workspace: workspace?.path)).write(to: settingsURL, options: .atomic)
      try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: settingsURL.path)
    } catch { message = "Update preferences couldn’t be saved." }
  }
  var configured: Bool {
    guard let value = bundle.object(forInfoDictionaryKey: "LimmrUpdateFeed") as? String,
          let url = URL(string: value), secureUpdateURL(url),
          let keyText = bundle.object(forInfoDictionaryKey: "LimmrUpdatePublicKey") as? String,
          Data(base64Encoded: keyText)?.count == 32,
          (try? UpdateSecurity.validateInstalled(bundle.bundleURL)) != nil else { return false }
    return true
  }
  func start(store: AppStore) {
    guard !store.demo, !store.snapshot, monitorTask == nil else { return }
    // Outlive the main window: checks continue while only the menu bar app is open.
    monitorTask = Task { await self.run(store: store) }
  }
  private func run(store: AppStore) async {
    while !Task.isCancelled {
      if !store.demo, configured, UpdateSchedule.shouldCheck(mode: mode, lastAttempt: lastAttempt, now: Date()) {
        await check()
      }
      do { try await Task.sleep(for: .seconds(3600)) } catch { return }
    }
  }
  func check() async {
    guard !busy, !sample else { return }
    guard configured else { message = "Development build — install updates manually."; return }
    busy = true; message = nil
    lastAttempt = Date(); persist() // Record failures too: no automatic retry storm.
    defer { busy = false }
    do {
      let url = URL(string: bundle.object(forInfoDictionaryKey: "LimmrUpdateFeed") as! String)!
      let key = Data(base64Encoded: bundle.object(forInfoDictionaryKey: "LimmrUpdatePublicKey") as! String)!
      let feed = try await checker.fetch(feedURL: url, publicKey: key)
      do {
        try feed.requireUpgrade(from: bundle.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "0",
                                build: Int(bundle.object(forInfoDictionaryKey: "CFBundleVersion") as? String ?? "0") ?? 0,
                                macOS: Self.osVersion)
      } catch UpdateError.downgrade {
        discardStaging(); available = nil; message = "Limmr is up to date."; return
      }
      if available != feed { discardStaging(); available = feed }
      if mode == .automatically { try await stage(feed) }
    } catch { message = "The update couldn’t be verified or downloaded. Try again later." }
  }
  static var osVersion: String {
    let os = ProcessInfo.processInfo.operatingSystemVersion
    return "\(os.majorVersion).\(os.minorVersion).\(os.patchVersion)"
  }
  func download() async {
    guard !busy, let available, !sample else { return }
    busy = true; message = nil
    defer { busy = false }
    do { try await stage(available) }
    catch { message = "The update couldn’t be prepared. You can install the release manually." }
  }
  private func discardStaging() {
    if let workspace { try? FileManager.default.removeItem(at: workspace) }
    workspace = nil; persist()
  }
  private func stage(_ feed: UpdateFeed) async throws {
    if workspace != nil { return }
    let fm = FileManager.default
    let target = bundle.bundleURL.standardizedFileURL
    guard target.lastPathComponent == "Limmr.app", target == target.resolvingSymlinksInPath(),
          fm.isWritableFile(atPath: target.deletingLastPathComponent().path) else { throw UpdateError.unsafeBundle }
    let directory = target.deletingLastPathComponent().appending(path: ".limmr-update-\(UUID().uuidString)")
    try fm.createDirectory(at: directory, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
    do {
      let archive = directory.appending(path: "Limmr.zip")
      try await checker.download(feed, to: archive)
      // Archive is authenticated before extraction. Reject absolute and parent-traversal entries too.
      let names = try await UpdateCommand.run("/usr/bin/unzip", ["-Z1", archive.path])
      let paths = String(decoding: names, as: UTF8.self).split(separator: "\n")
      guard !paths.isEmpty, paths.allSatisfy({ path in
        !path.hasPrefix("/") && !path.contains("\\") && !path.split(separator: "/").contains("..") &&
        (path.hasPrefix("Limmr.app/") || path.hasPrefix("__MACOSX/"))
      }) else { throw UpdateError.unsafeBundle }
      let extracted = directory.appending(path: "extracted")
      _ = try await UpdateCommand.run("/usr/bin/ditto", ["-x", "-k", archive.path, extracted.path])
      let candidate = extracted.appending(path: "Limmr.app")
      try UpdateSecurity.prepare(candidate: candidate, feed: feed, installed: target)
      try fm.moveItem(at: candidate, to: directory.appending(path: "Previous.app"))
      try JSONEncoder().encode(feed).write(to: directory.appending(path: "appcast.json"), options: .atomic)
      workspace = directory; persist()
    } catch { try? fm.removeItem(at: directory); throw error }
  }
  func restart(store: AppStore) async {
    guard !busy, !store.busy, store.pending.isEmpty, store.home.queued.isEmpty,
          !store.writingMacBriefs, !sample, !store.demo, !store.snapshot, let workspace else {
      message = "Finish the current work and review queued messages before restarting."; return
    }
    busy = true
    defer { busy = false }
    do {
      // Older engines lack an atomic send-drain gate. Refuse installation, without quitting.
      struct Capabilities: Decodable, Sendable { let canPrepare: Bool }
      let data = try await store.backend.request("/v1/updates/capabilities", method: "GET", body: nil)
      let decoder = JSONDecoder(); decoder.keyDecodingStrategy = .convertFromSnakeCase
      guard try decoder.decode(Capabilities.self, from: data).canPrepare else { throw UpdateError.unsafeToRestart }
      let plan = UpdatePlan(installed: bundle.bundleURL.standardizedFileURL, workspace: workspace, parentPID: getpid())
      let planURL = workspace.appending(path: "plan.json")
      try JSONEncoder().encode(plan).write(to: planURL, options: .atomic)
      let runner = workspace.appending(path: "runner")
      if FileManager.default.fileExists(atPath: runner.path) { try FileManager.default.removeItem(at: runner) }
      try FileManager.default.copyItem(at: bundle.executableURL!, to: runner)
      let process = Process(); process.executableURL = runner
      process.arguments = ["--limmr-update-helper", planURL.path]
      process.standardInput = FileHandle.nullDevice; process.standardOutput = FileHandle.nullDevice
      process.standardError = FileHandle.nullDevice
      try process.run()
      NSApp.terminate(nil)
    } catch { message = "The background helper can’t pause sends safely for this update. Install manually after stopping Limmr." }
  }
}
