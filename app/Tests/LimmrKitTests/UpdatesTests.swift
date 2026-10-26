import CryptoKit
import Foundation
import Testing
@testable import LimmrKit

@Suite("U1 signed updates") struct UpdatesTests {
  private func feed(key: Curve25519.Signing.PrivateKey = .init(), archive: Data = Data("archive".utf8)) throws -> (UpdateFeed, Data, Data) {
    var feed = UpdateFeed(version: "0.3.0", build: 30, minMacOS: "26.0",
                          notesMarkdown: "A quieter Home.\n\nWorks with café notes and https://example.invalid/notes.",
                          url: URL(string: "https://example.invalid/Limmr.zip")!, size: Int64(archive.count),
                          sha256: SHA256.hash(data: archive).map { String(format: "%02x", $0) }.joined())
    feed.ed25519Signature = try key.signature(for: feed.canonicalBytes()).base64EncodedString()
    return (feed, try JSONEncoder().encode(feed), key.publicKey.rawRepresentation)
  }
  @Test func validSignedFeedAndRoundTrip() throws {
    let (expected, data, key) = try feed()
    let result = try UpdateFeed.verified(data, publicKey: key)
    #expect(result == expected)
    let object = try #require(JSONSerialization.jsonObject(with: result.canonicalBytes()) as? [String: Any])
    #expect(object["ed25519_signature"] == nil)
    #expect(object.count == 7)
    #expect(String(decoding: try result.canonicalBytes(), as: UTF8.self).contains("https://example.invalid/"))
    #expect(try result.canonicalBytes() == expected.canonicalBytes())
  }
  @Test func tamperedFeedAndWrongKeyAreRejected() throws {
    let (_, data, key) = try feed()
    var object = try #require(JSONSerialization.jsonObject(with: data) as? [String: Any])
    object["notes_markdown"] = "Replaced by an attacker"
    let tampered = try JSONSerialization.data(withJSONObject: object)
    #expect(throws: UpdateError.invalidSignature) { try UpdateFeed.verified(tampered, publicKey: key) }
    #expect(throws: UpdateError.invalidSignature) {
      try UpdateFeed.verified(data, publicKey: Curve25519.Signing.PrivateKey().publicKey.rawRepresentation)
    }
    object["extra"] = "ignored fields are forbidden"
    #expect(throws: UpdateError.invalidFeed) {
      try UpdateFeed.verified(JSONSerialization.data(withJSONObject: object), publicKey: key)
    }
    #expect(throws: (any Error).self) { try UpdateFeed.verified(Data("{}".utf8), publicKey: key) }
    #expect(throws: UpdateError.invalidFeed) { try UpdateFeed.verified(Data(repeating: 0, count: 262_145), publicKey: key) }
  }
  @Test func checksumAndSizeMustBothMatch() throws {
    let archive = Data("archive".utf8)
    let (feed, _, _) = try feed(archive: archive)
    let home = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
    try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: home) }
    let path = home.appending(path: "test.zip")
    try archive.write(to: path)
    try feed.verifyArchive(path)
    try Data("changed".utf8).write(to: path) // same length, different SHA
    #expect(throws: UpdateError.checksum) { try feed.verifyArchive(path) }
    try Data("too long archive".utf8).write(to: path)
    #expect(throws: UpdateError.checksum) { try feed.verifyArchive(path) }
    try Data().write(to: path)
    #expect(throws: UpdateError.checksum) { try feed.verifyArchive(path) }
  }
  @Test func numericVersionsAndDowngradeRefusal() throws {
    #expect(try UpdateVersion("0.10") > UpdateVersion("0.9.9"))
    #expect(try UpdateVersion("1.2") == UpdateVersion("1.2.0"))
    #expect(try UpdateVersion("1.2.1") > UpdateVersion("1.2"))
    for bad in ["", "1..2", "-1.2", "1.2-beta", "1.2.3.4.5", "١.٢", "9999999999999999999999"] {
      #expect(throws: (any Error).self) { try UpdateVersion(bad) }
    }
    let (feed, _, _) = try feed()
    try feed.requireUpgrade(from: "0.2.9", build: 29, macOS: "26.0")
    try feed.requireUpgrade(from: "0.3", build: 29, macOS: "27.0")
    for (version, build) in [("0.4", 29), ("0.3", 30), ("0.3", 31), ("0.2", 31)] {
      #expect(throws: UpdateError.downgrade) { try feed.requireUpgrade(from: version, build: build, macOS: "27") }
    }
    #expect(throws: UpdateError.incompatible) { try feed.requireUpgrade(from: "0.2", build: 29, macOS: "25.9") }
  }
  @Test func throttleAndModes() {
    let now = Date(timeIntervalSince1970: 1_000_000)
    #expect(UpdateSchedule.shouldCheck(mode: .automatically, lastAttempt: nil, now: now))
    #expect(UpdateSchedule.shouldCheck(mode: .ask, lastAttempt: now.addingTimeInterval(-86_400), now: now))
    #expect(!UpdateSchedule.shouldCheck(mode: .automatically, lastAttempt: now.addingTimeInterval(-86_399), now: now))
    #expect(!UpdateSchedule.shouldCheck(mode: .automatically, lastAttempt: now.addingTimeInterval(100), now: now))
    #expect(!UpdateSchedule.shouldCheck(mode: .off, lastAttempt: nil, now: now))
    #expect(!secureUpdateURL(URL(string: "http://example.invalid/feed")!))
    #expect(!secureUpdateURL(URL(string: "https://name:secret@example.invalid/feed")!))
    #expect(!secureUpdateURL(URL(string: "https://example.invalid/feed#fragment")!))
  }
  @Test func successfulTransaction() async throws {
    let installer = FakeUpdateInstaller(newEngineHealthy: true)
    try await UpdateTransaction.install(using: installer)
    #expect(await installer.events == ["swap", "register", "status:60", "relaunch", "resume"])
  }
  @Test func unhealthyNewEngineRollsBackAndReregistersOldEngine() async {
    let installer = FakeUpdateInstaller(newEngineHealthy: false)
    await #expect(throws: UpdateError.installFailed) { try await UpdateTransaction.install(using: installer) }
    #expect(await installer.events == ["swap", "register", "status:60", "restore", "register", "status:60", "relaunch", "resume"])
  }
  @Test func failedSwapDoesNotPretendItInstalledOrRestoreUnswappedBundle() async {
    let installer = FakeUpdateInstaller(newEngineHealthy: true, failSwap: true)
    await #expect(throws: UpdateError.unsafeToRestart) { try await UpdateTransaction.install(using: installer) }
    #expect(await installer.events == ["swap", "resume"])
  }
  @Test func registrationFailureAlsoRestoresOldBundle() async {
    let installer = FakeUpdateInstaller(newEngineHealthy: true, failRegistration: true)
    await #expect(throws: UpdateError.installFailed) { try await UpdateTransaction.install(using: installer) }
    #expect(await installer.events == ["swap", "register", "restore", "register", "status:60", "relaunch", "resume"])
  }
  @Test func rollbackFailureIsVisibleAndNeverLaunchesBrokenApp() async {
    let installer = FakeUpdateInstaller(newEngineHealthy: false, failRestore: true)
    await #expect(throws: UpdateError.unsafeBundle) { try await UpdateTransaction.install(using: installer) }
    #expect(await installer.events == ["swap", "register", "status:60", "restore", "resume"])
  }
}

private actor FakeUpdateInstaller: UpdateInstalling {
  private(set) var events: [String] = []
  let newEngineHealthy: Bool
  let failSwap: Bool
  let failRegistration: Bool
  let failRestore: Bool
  private var restored = false
  init(newEngineHealthy: Bool, failSwap: Bool = false, failRegistration: Bool = false, failRestore: Bool = false) {
    self.newEngineHealthy = newEngineHealthy; self.failSwap = failSwap
    self.failRegistration = failRegistration; self.failRestore = failRestore
  }
  func resume() { events.append("resume") }
  func swap() throws { events.append("swap"); if failSwap { throw UpdateError.unsafeToRestart } }
  func registerEngine() throws { events.append("register"); if failRegistration && !restored { throw UpdateError.unavailable } }
  func engineResponds(timeout: Duration) -> Bool {
    events.append("status:\(timeout.components.seconds)")
    return restored || newEngineHealthy
  }
  func restore() throws { events.append("restore"); if failRestore { throw UpdateError.unsafeBundle }; restored = true }
  func relaunch() { events.append("relaunch") }
}
