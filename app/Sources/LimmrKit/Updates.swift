import CryptoKit
import Foundation

public enum UpdateError: Error, Equatable, Sendable {
  case invalidFeed, invalidSignature, incompatible, downgrade, checksum, unsafeBundle
  case unavailable, tooLarge, engineNotReady, unsafeToRestart, installFailed
}

/// Numeric releases only (1.2 and 1.2.0 compare equally); no lexicographic version ordering.
public struct UpdateVersion: Comparable, Sendable {
  private let parts: [UInt64]
  public init(_ value: String) throws {
    let fields = value.split(separator: ".", omittingEmptySubsequences: false)
    guard (1...4).contains(fields.count), fields.allSatisfy({
      !$0.isEmpty && $0.allSatisfy({ $0.isASCII && $0.isNumber }) && UInt64($0) != nil
    }) else { throw UpdateError.invalidFeed }
    parts = fields.map { UInt64($0)! }
  }
  public static func == (lhs: Self, rhs: Self) -> Bool { !(lhs < rhs) && !(rhs < lhs) }
  public static func < (lhs: Self, rhs: Self) -> Bool {
    for index in 0..<max(lhs.parts.count, rhs.parts.count) {
      let a = index < lhs.parts.count ? lhs.parts[index] : 0
      let b = index < rhs.parts.count ? rhs.parts[index] : 0
      if a != b { return a < b }
    }
    return false
  }
}

/// Canonical form: UTF-8 JSON, sorted keys, no escaped slashes, no whitespace/newline,
/// these seven payload fields only, integral build/size. Signature is excluded.
public struct UpdateFeed: Codable, Equatable, Sendable {
  public let version: String
  public let build: Int
  public let minMacOS: String
  public let notesMarkdown: String
  public let url: URL
  public let size: Int64
  public let sha256: String
  public var ed25519Signature: String
  enum CodingKeys: String, CodingKey {
    case version, build, url, size, sha256
    case minMacOS = "min_macos", notesMarkdown = "notes_markdown"
    case ed25519Signature = "ed25519_signature"
  }
  public init(version: String, build: Int, minMacOS: String, notesMarkdown: String,
              url: URL, size: Int64, sha256: String, ed25519Signature: String = "") {
    self.version = version; self.build = build; self.minMacOS = minMacOS
    self.notesMarkdown = notesMarkdown; self.url = url; self.size = size
    self.sha256 = sha256; self.ed25519Signature = ed25519Signature
  }
  public func canonicalBytes() throws -> Data {
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
    let object = try JSONSerialization.jsonObject(with: encoder.encode(self)) as! [String: Any]
    return try JSONSerialization.data(withJSONObject: object.filter { $0.key != "ed25519_signature" },
                                      options: [.sortedKeys, .withoutEscapingSlashes])
  }
  public static func verified(_ data: Data, publicKey: Data) throws -> Self {
    guard data.count <= 262_144,
          let object = try JSONSerialization.jsonObject(with: data) as? [String: Any],
          Set(object.keys) == Set(["version", "build", "min_macos", "notes_markdown", "url", "size", "sha256", "ed25519_signature"])
    else { throw UpdateError.invalidFeed }
    let feed = try JSONDecoder().decode(Self.self, from: data)
    guard let signature = Data(base64Encoded: feed.ed25519Signature),
          let key = try? Curve25519.Signing.PublicKey(rawRepresentation: publicKey),
          key.isValidSignature(signature, for: try feed.canonicalBytes())
    else { throw UpdateError.invalidSignature }
    _ = try UpdateVersion(feed.version); _ = try UpdateVersion(feed.minMacOS)
    guard feed.build > 0, feed.size > 0, feed.size <= 1_073_741_824,
          feed.sha256.count == 64, feed.sha256.allSatisfy({ "0123456789abcdef".contains($0) }),
          secureUpdateURL(feed.url), feed.url.pathExtension == "zip"
    else { throw UpdateError.invalidFeed }
    return feed
  }
  public func requireUpgrade(from version: String, build currentBuild: Int, macOS: String) throws {
    let candidate = try UpdateVersion(self.version), current = try UpdateVersion(version)
    guard candidate > current || candidate == current && build > currentBuild else { throw UpdateError.downgrade }
    // Build numbers are globally monotonic, including across marketing versions.
    guard build > currentBuild else { throw UpdateError.downgrade }
    guard try UpdateVersion(macOS) >= UpdateVersion(minMacOS) else { throw UpdateError.incompatible }
  }
  public func verifyArchive(_ file: URL) throws {
    let handle = try FileHandle(forReadingFrom: file)
    defer { try? handle.close() }
    var hash = SHA256(); var count: Int64 = 0
    while let block = try handle.read(upToCount: 1_048_576), !block.isEmpty {
      count += Int64(block.count)
      guard count <= size else { throw UpdateError.checksum }
      hash.update(data: block)
    }
    guard count == size, hash.finalize().map({ String(format: "%02x", $0) }).joined() == sha256
    else { throw UpdateError.checksum }
  }
}

public func secureUpdateURL(_ url: URL) -> Bool {
  url.scheme == "https" && url.host != nil && url.user == nil && url.password == nil && url.fragment == nil
}

public enum UpdateMode: String, CaseIterable, Sendable {
  case automatically, ask, off
  public var title: String {
    switch self { case .automatically: "Automatically"; case .ask: "Ask me"; case .off: "Off" }
  }
}

public enum UpdateSchedule {
  public static func shouldCheck(mode: UpdateMode, lastAttempt: Date?, now: Date) -> Bool {
    mode != .off && (lastAttempt == nil || now.timeIntervalSince(lastAttempt!) >= 86_400)
  }
}

/// The OS adapter handles the filesystem, service registration and status probe. Kept injectable
/// so a broken new engine exercises exactly the same rollback path without running launchd.
public protocol UpdateInstalling: Sendable {
  func swap() async throws
  func resume() async
  func registerEngine() async throws
  func engineResponds(timeout: Duration) async -> Bool
  func restore() async throws
  func relaunch() async throws
}
public enum UpdateTransaction {
  public static func install(using installer: any UpdateInstalling, timeout: Duration = .seconds(60)) async throws {
    do {
      try await installer.swap()
      do {
        try await installer.registerEngine()
        guard await installer.engineResponds(timeout: timeout) else { throw UpdateError.engineNotReady }
        try await installer.relaunch()
      } catch {
        // Restore errors remain visible; never discard the only working bundle.
        try await installer.restore()
        try await installer.registerEngine()
        guard await installer.engineResponds(timeout: timeout) else { throw UpdateError.engineNotReady }
        try await installer.relaunch()
        throw UpdateError.installFailed
      }
      await installer.resume()
    } catch {
      // Covers prepare/refusal, stop/swap, registration, version check and rollback failures.
      await installer.resume()
      throw error
    }
  }
}
