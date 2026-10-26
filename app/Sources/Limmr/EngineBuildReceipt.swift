import CryptoKit
import Foundation

/// SHA-256 labels immutable staged helpers and records successful installs for diagnostics.
/// Startup compares the running kernel code hash; this disk receipt is not process identity.
struct EngineBuildReceipt: Sendable {
  let helper: URL
  let receipt: URL

  func fingerprint() throws -> String {
    guard FileManager.default.isExecutableFile(atPath: helper.path) else {
      throw CocoaError(.fileNoSuchFile)
    }
    let file = try FileHandle(forReadingFrom: helper)
    defer { try? file.close() }
    var hash = SHA256()
    while let bytes = try file.read(upToCount: 1_048_576), !bytes.isEmpty { hash.update(data: bytes) }
    return hash.finalize().map { String(format: "%02x", $0) }.joined()
  }

  func needsUpdate() throws -> Bool {
    let current = try fingerprint()
    return (try? String(contentsOf: receipt, encoding: .utf8)) != current
  }

  func record(_ fingerprint: String) throws {
    try FileManager.default.createDirectory(at: receipt.deletingLastPathComponent(), withIntermediateDirectories: true)
    try Data(fingerprint.utf8).write(to: receipt, options: .atomic)
  }
}
