import Darwin
import Foundation
import Testing
@testable import Limmr
@testable import LimmrKit

@Suite("U2 self-signed update trust") struct UpdatesSecurityTests {
  @Test func intactAdHocCandidateIsAccepted() async throws {
    let fixture = try await SignedUpdateFixture()
    defer { fixture.remove() }
    #expect(try UpdateSecurity.team(fixture.installed) == nil)
    try UpdateSecurity.verifyMetadata(candidate: fixture.candidate, feed: fixture.feed, installed: fixture.installed)
    let controller = await UpdateController(bundle: try #require(Bundle(url: fixture.installed)))
    #expect(await controller.configured)
  }

  @Test(arguments: ["Contents/MacOS/Limmr", "Contents/Helpers/limmr-engine"])
  func tamperedAdHocCodeIsRejected(path: String) async throws {
    let fixture = try await SignedUpdateFixture()
    defer { fixture.remove() }
    let file = try FileHandle(forWritingTo: fixture.candidate.appending(path: path))
    try file.seekToEnd(); try file.write(contentsOf: Data("tampered".utf8)); try file.close()
    #expect(throws: UpdateError.unsafeBundle) {
      try UpdateSecurity.verifyMetadata(candidate: fixture.candidate, feed: fixture.feed, installed: fixture.installed)
    }
  }

  @Test(arguments: [false, true]) func unsignedBundleOrHelperIsRejected(helperOnly: Bool) async throws {
    let fixture = try await SignedUpdateFixture()
    defer { fixture.remove() }
    if helperOnly {
      _ = try await UpdateCommand.run("/usr/bin/codesign", ["--remove-signature", fixture.candidate.appending(path: "Contents/Helpers/limmr-engine").path])
    } else {
      _ = try await UpdateCommand.run("/usr/bin/codesign", ["--remove-signature", fixture.candidate.path])
    }
    #expect(throws: UpdateError.unsafeBundle) {
      try UpdateSecurity.verifyMetadata(candidate: fixture.candidate, feed: fixture.feed, installed: fixture.installed)
    }
  }

  @Test func changedFeedKeyIsRejectedEvenWithValidSignature() async throws {
    let fixture = try await SignedUpdateFixture(candidateKey: Data(repeating: 2, count: 32))
    defer { fixture.remove() }
    try UpdateSecurity.validateInstalled(fixture.candidate)
    #expect(throws: UpdateError.unsafeBundle) {
      try UpdateSecurity.verifyMetadata(candidate: fixture.candidate, feed: fixture.feed, installed: fixture.installed)
    }
  }

  @Test func developerIDInstalledRejectsAdHocCandidate() async throws {
    let fixture = try await SignedUpdateFixture()
    defer { fixture.remove() }
    // No real certificate/key is accessed. Use the production team-policy guard with a synthetic
    // installed Developer ID team and the actual candidate's Security.framework signing identity.
    #expect(throws: UpdateError.unsafeBundle) {
      try UpdateSecurity.requiresDesignatedRequirement(installedTeam: "TESTTEAM01", candidateTeam: UpdateSecurity.team(fixture.candidate))
    }
    #expect(try UpdateSecurity.requiresDesignatedRequirement(installedTeam: "TESTTEAM01", candidateTeam: "TESTTEAM01"))
    #expect(throws: UpdateError.unsafeBundle) {
      try UpdateSecurity.requiresDesignatedRequirement(installedTeam: "TESTTEAM01", candidateTeam: "OTHERTEAM1")
    }
  }

  @Test(arguments: ["dev.limmr.other", "dev.limmr.app"])
  func bundleIdentityAndFeedMetadataMustMatch(identifier: String) async throws {
    let fixture = try await SignedUpdateFixture(candidateIdentifier: identifier, candidateVersion: "9.0.0")
    defer { fixture.remove() }
    #expect(throws: UpdateError.unsafeBundle) {
      try UpdateSecurity.verifyMetadata(candidate: fixture.candidate, feed: fixture.feed, installed: fixture.installed)
    }
    if identifier != "dev.limmr.app" {
      #expect(throws: UpdateError.unsafeBundle) {
        try UpdateSecurity.verify(candidate: fixture.candidate, installed: fixture.installed)
      }
    }
  }

  @Test func quarantineIsRemovedOnlyAfterVerification() async throws {
    let fixture = try await SignedUpdateFixture()
    defer { fixture.remove() }
    let helper = fixture.candidate.appending(path: "Contents/Helpers/limmr-engine")
    for url in [fixture.candidate, helper] { try Self.quarantine(url) }
    try UpdateSecurity.prepare(candidate: fixture.candidate, feed: fixture.feed, installed: fixture.installed)
    for url in [fixture.candidate, helper] { #expect(!Self.isQuarantined(url)) }
    // Idempotent when a prepared update is re-verified by the detached runner.
    try UpdateSecurity.prepare(candidate: fixture.candidate, feed: fixture.feed, installed: fixture.installed)
  }

  @Test func failedVerificationKeepsQuarantine() async throws {
    let fixture = try await SignedUpdateFixture(candidateKey: Data(repeating: 2, count: 32))
    defer { fixture.remove() }
    try Self.quarantine(fixture.candidate)
    #expect(throws: UpdateError.unsafeBundle) {
      try UpdateSecurity.prepare(candidate: fixture.candidate, feed: fixture.feed, installed: fixture.installed)
    }
    #expect(Self.isQuarantined(fixture.candidate))
  }

  private static func quarantine(_ url: URL) throws {
    let value = Array("0081;00000000;Limmr;".utf8)
    let result = value.withUnsafeBytes { setxattr(url.path, "com.apple.quarantine", $0.baseAddress, $0.count, 0, XATTR_NOFOLLOW) }
    guard result == 0 else { throw UpdateError.installFailed }
  }
  private static func isQuarantined(_ url: URL) -> Bool {
    getxattr(url.path, "com.apple.quarantine", nil, 0, 0, XATTR_NOFOLLOW) >= 0
  }
}

private struct SignedUpdateFixture {
  let root: URL
  var installed: URL { root.appending(path: "installed/Limmr.app") }
  var candidate: URL { root.appending(path: "staged/Limmr.app") }
  let feed = UpdateFeed(version: "0.3.0", build: 30, minMacOS: "26.0", notesMarkdown: "Synthetic update",
                        url: URL(string: "https://example.invalid/Limmr.zip")!, size: 1, sha256: String(repeating: "0", count: 64))

  init(candidateKey: Data = Data(repeating: 1, count: 32), candidateIdentifier: String = "dev.limmr.app",
       candidateVersion: String = "0.3.0") async throws {
    root = FileManager.default.temporaryDirectory.appending(path: "limmr-signature-\(UUID().uuidString)")
    do {
      try await makeBundle(installed, key: Data(repeating: 1, count: 32), identifier: "dev.limmr.app", version: "0.2.0", build: "20")
      try await makeBundle(candidate, key: candidateKey, identifier: candidateIdentifier, version: candidateVersion, build: "30")
    } catch { remove(); throw error }
  }
  func remove() { try? FileManager.default.removeItem(at: root) }
  private func makeBundle(_ url: URL, key: Data, identifier: String, version: String, build: String) async throws {
    let fm = FileManager.default
    for path in ["Contents/MacOS", "Contents/Helpers"] {
      try fm.createDirectory(at: url.appending(path: path), withIntermediateDirectories: true)
    }
    let plist: [String: Any] = ["CFBundleIdentifier": identifier, "CFBundleExecutable": "Limmr", "CFBundlePackageType": "APPL",
      "CFBundleShortVersionString": version, "CFBundleVersion": build, "LSMinimumSystemVersion": "26.0",
      "LimmrUpdatePublicKey": key.base64EncodedString(), "LimmrUpdateFeed": "https://example.invalid/appcast.json"]
    try PropertyListSerialization.data(fromPropertyList: plist, format: .xml, options: 0).write(to: url.appending(path: "Contents/Info.plist"))
    for path in ["Contents/Helpers/limmr-engine", "Contents/MacOS/Limmr"] {
      let target = url.appending(path: path)
      try fm.copyItem(at: URL(fileURLWithPath: "/usr/bin/true"), to: target)
      _ = try await UpdateCommand.run("/usr/bin/codesign", ["--force", "--sign", "-", "--options", "runtime", target.path])
    }
    _ = try await UpdateCommand.run("/usr/bin/codesign", ["--force", "--sign", "-", "--options", "runtime", url.path])
  }
}
