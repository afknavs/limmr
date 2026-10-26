#!/usr/bin/env swift
import CryptoKit
import Foundation
import Security

do {
  let env = ProcessInfo.processInfo.environment
  guard CommandLine.arguments.count == 4,
        let build = Int(env["LIMMR_BUILD_NUMBER"] ?? ""), build > 0,
        let version = env["LIMMR_VERSION"], let download = env["LIMMR_DOWNLOAD_URL"],
        let notesFile = env["LIMMR_RELEASE_NOTES"],
        let expectedKey = Data(base64Encoded: env["LIMMR_UPDATE_PUBLIC_KEY"] ?? "")
  else { throw NSError(domain: "feed", code: 1) }
  let keyData: Data
  if let encoded = env["LIMMR_UPDATE_PRIVATE_KEY"] {
    guard let raw = Data(base64Encoded: encoded) else { throw NSError(domain: "feed", code: 2) }
    keyData = raw
  } else if let path = env["LIMMR_UPDATE_PRIVATE_KEY_FILE"] {
    let attributes = try FileManager.default.attributesOfItem(atPath: path)
    guard (attributes[.posixPermissions] as? NSNumber)?.intValue == 0o600 else { throw NSError(domain: "feed", code: 3) }
    keyData = try Data(contentsOf: URL(fileURLWithPath: path))
  } else {
    var result: CFTypeRef?
    let query: [String: Any] = [
      kSecClass as String: kSecClassGenericPassword,
      kSecAttrService as String: env["LIMMR_UPDATE_KEYCHAIN_SERVICE"] ?? "dev.limmr.release.ed25519",
      kSecAttrAccount as String: "signing", kSecReturnData as String: true,
      kSecMatchLimit as String: kSecMatchLimitOne,
    ]
    guard SecItemCopyMatching(query as CFDictionary, &result) == errSecSuccess, let raw = result as? Data
    else { throw NSError(domain: "feed", code: 4) }
    keyData = raw
  }
  let key = try Curve25519.Signing.PrivateKey(rawRepresentation: keyData)
  guard key.publicKey.rawRepresentation == expectedKey else { throw NSError(domain: "feed", code: 5) }
  let archive = URL(fileURLWithPath: CommandLine.arguments[1])
  let file = try FileHandle(forReadingFrom: archive)
  var hash = SHA256(); var size: Int64 = 0
  while let data = try file.read(upToCount: 1_048_576), !data.isEmpty { hash.update(data: data); size += Int64(data.count) }
  try file.close()
  guard size > 0, size <= 1_073_741_824,
        version.split(separator: ".", omittingEmptySubsequences: false).allSatisfy({ UInt64($0) != nil })
  else { throw NSError(domain: "feed", code: 6) }
  var payload: [String: Any] = [
    "version": version, "build": build, "min_macos": "26.0",
    "notes_markdown": try String(contentsOfFile: notesFile, encoding: .utf8),
    "url": download, "size": size,
    "sha256": hash.finalize().map { String(format: "%02x", $0) }.joined(),
  ]
  let canonical = try JSONSerialization.data(withJSONObject: payload, options: [.sortedKeys, .withoutEscapingSlashes])
  let signature = try key.signature(for: canonical)
  payload["ed25519_signature"] = signature.base64EncodedString()
  // Third argument contains the exact signed payload, useful for an independent release audit.
  try canonical.write(to: URL(fileURLWithPath: CommandLine.arguments[3]), options: .atomic)
  let output = try JSONSerialization.data(withJSONObject: payload, options: [.sortedKeys, .withoutEscapingSlashes])
  guard output.count <= 262_144 else { throw NSError(domain: "feed", code: 7) }
  try output.write(to: URL(fileURLWithPath: CommandLine.arguments[2]), options: .atomic)
} catch {
  fputs("Feed signing failed. Check release settings and access to the signing key.\n", stderr)
  exit(1)
}
