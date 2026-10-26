import Darwin
import Foundation
import LimmrKit
import Security

/// Never infer trust from a bundle's claimed Team ID in Info.plist.
enum UpdateSecurity {
  static func code(_ url: URL) throws -> SecStaticCode {
    var code: SecStaticCode?
    guard SecStaticCodeCreateWithPath(url as CFURL, [], &code) == errSecSuccess, let code else {
      throw UpdateError.unsafeBundle
    }
    return code
  }
  static func team(_ url: URL) throws -> String? {
    var info: CFDictionary?
    guard SecCodeCopySigningInformation(try code(url), SecCSFlags(rawValue: kSecCSSigningInformation), &info) == errSecSuccess,
          let values = info as? [String: Any] else { throw UpdateError.unsafeBundle }
    return values[kSecCodeInfoTeamIdentifier as String] as? String
  }
  private static let validationFlags = SecCSFlags(rawValue:
    kSecCSStrictValidate | kSecCSCheckAllArchitectures | kSecCSCheckNestedCode)

  static func validateInstalled(_ url: URL) throws {
    guard SecStaticCodeCheckValidity(try code(url), validationFlags, nil) == errSecSuccess else {
      throw UpdateError.unsafeBundle
    }
    // Helpers are executable code, not just sealed resources. Require their own intact signature.
    let helper = url.appending(path: "Contents/Helpers/limmr-engine")
    guard SecStaticCodeCheckValidity(try code(helper), validationFlags, nil) == errSecSuccess else {
      throw UpdateError.unsafeBundle
    }
  }

  /// A certificate-signed installation must never move to an ad-hoc or different-team release.
  static func requiresDesignatedRequirement(installedTeam: String?, candidateTeam: String?) throws -> Bool {
    guard let installedTeam, !installedTeam.isEmpty else { return false }
    guard candidateTeam == installedTeam else { throw UpdateError.unsafeBundle }
    return true
  }

  static func verify(candidate: URL, installed: URL) throws {
    try validateInstalled(installed)
    try validateInstalled(candidate)
    guard let current = Bundle(url: installed), let next = Bundle(url: candidate),
          let identifier = current.bundleIdentifier, next.bundleIdentifier == identifier
    else { throw UpdateError.unsafeBundle }
    if try requiresDesignatedRequirement(installedTeam: team(installed), candidateTeam: team(candidate)) {
      var requirement: SecRequirement?
      guard SecCodeCopyDesignatedRequirement(try code(installed), [], &requirement) == errSecSuccess,
            let requirement,
            SecStaticCodeCheckValidity(try code(candidate), validationFlags, requirement) == errSecSuccess
      else { throw UpdateError.unsafeBundle }
    } else {
      guard let keyText = current.object(forInfoDictionaryKey: "LimmrUpdatePublicKey") as? String,
            let key = Data(base64Encoded: keyText), key.count == 32,
            let candidateText = next.object(forInfoDictionaryKey: "LimmrUpdatePublicKey") as? String,
            Data(base64Encoded: candidateText) == key else { throw UpdateError.unsafeBundle }
    }
  }

  /// Only authenticated, fully verified staged code may lose its download quarantine.
  static func prepare(candidate: URL, feed: UpdateFeed, installed: URL) throws {
    try verifyMetadata(candidate: candidate, feed: feed, installed: installed)
    var traversalFailed = false
    guard let entries = FileManager.default.enumerator(at: candidate, includingPropertiesForKeys: nil,
      errorHandler: { _, _ in traversalFailed = true; return false }) else { throw UpdateError.unsafeBundle }
    for case let entry as URL in entries { try removeQuarantine(entry) }
    guard !traversalFailed else { throw UpdateError.unsafeBundle }
    try removeQuarantine(candidate)
  }
  private static func removeQuarantine(_ url: URL) throws {
    // Never follow symlinks out of the staged bundle. Missing attributes are normal.
    guard removexattr(url.path, "com.apple.quarantine", XATTR_NOFOLLOW) == 0 || errno == ENOATTR
    else { throw UpdateError.installFailed }
  }
  static func verifyMetadata(candidate: URL, feed: UpdateFeed, installed: URL) throws {
    guard candidate.standardizedFileURL == candidate.resolvingSymlinksInPath(),
          let bundle = Bundle(url: candidate), let current = Bundle(url: installed),
          bundle.bundleIdentifier == current.bundleIdentifier,
          bundle.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String == feed.version,
          bundle.object(forInfoDictionaryKey: "CFBundleVersion") as? String == String(feed.build),
          bundle.object(forInfoDictionaryKey: "LSMinimumSystemVersion") as? String == feed.minMacOS,
          FileManager.default.isExecutableFile(atPath: candidate.appending(path: "Contents/Helpers/limmr-engine").path)
    else { throw UpdateError.unsafeBundle }
    try verify(candidate: candidate, installed: installed)
  }
}

/// Fixed tools and argument arrays; no interpolation into a shell or inheritance of stdin.
enum UpdateCommand {
  static func run(_ executable: String, _ arguments: [String]) async throws -> Data {
    try await Task.detached {
      let process = Process()
      process.executableURL = URL(fileURLWithPath: executable); process.arguments = arguments
      process.standardInput = FileHandle.nullDevice; process.standardError = FileHandle.nullDevice
      let pipe = Pipe(); process.standardOutput = pipe
      try process.run()
      let output = pipe.fileHandleForReading.readDataToEndOfFile()
      await process.waitForExit()
      guard process.terminationStatus == 0 else { throw UpdateError.installFailed }
      return output
    }.value
  }
}
