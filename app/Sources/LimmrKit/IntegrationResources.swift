import Foundation
import CryptoKit

public struct IntegrationResources: Sendable {
  public let root: URL
  public init(root: URL) { self.root = root }
  public static func locate(bundleResources: URL?, repository: URL?, sample: Bool) -> Self? {
    let candidates = sample ? [repository?.appending(path: "integrations"), bundleResources?.appending(path: "integrations")]
      : [bundleResources?.appending(path: "integrations")]
    return candidates.compactMap { $0 }.first { FileManager.default.fileExists(atPath: $0.path) }.map { Self(root: $0) }
  }
  public func skill(kind: String) -> URL {
    root.appending(path: kind == "codex" ? "codex/skills/limmr" : "claude-code/skills/limmr")
  }
  public func instructions() throws -> String {
    try String(contentsOf: root.appending(path: "chatgpt/instructions.md"), encoding: .utf8)
  }
}
/// Explicit destinations make installation testable without touching any user configuration.
/// Existing folders are never overwritten; Remove refuses to delete a modified installation.
public struct SkillInstaller {
  public struct Receipt: Codable, Equatable, Sendable {
    public var files: [String: String]
  }
  private static let receiptName = ".limmr-install.json"
  public init() {}
  private func inventory(_ folder: URL) throws -> [String: String] {
    let fm = FileManager.default
    var result: [String: String] = [:]
    func visit(_ directory: URL, prefix: String) throws {
      guard try directory.resourceValues(forKeys: [.isSymbolicLinkKey]).isSymbolicLink != true else {
        throw BackendError.unavailable("A skill cannot contain symbolic links.")
      }
      for file in try fm.contentsOfDirectory(at: directory, includingPropertiesForKeys: [.isSymbolicLinkKey, .isRegularFileKey, .isDirectoryKey]) {
        let values = try file.resourceValues(forKeys: [.isSymbolicLinkKey, .isRegularFileKey, .isDirectoryKey])
        guard values.isSymbolicLink != true else { throw BackendError.unavailable("A skill cannot contain symbolic links.") }
        let name = prefix + file.lastPathComponent
        if values.isDirectory == true { try visit(file, prefix: name + "/") }
        else if values.isRegularFile == true && name != Self.receiptName {
          result[name] = SHA256.hash(data: try Data(contentsOf: file)).map { String(format: "%02x", $0) }.joined()
        }
      }
    }
    try visit(folder, prefix: "")
    return result
  }
  public func install(source: URL, destination: URL) throws -> Receipt {
    let fm = FileManager.default
    guard !fm.fileExists(atPath: destination.path) else {
      throw BackendError.unavailable("A Limmr skill already exists here. Remove it before adding another copy.")
    }
    let files = try inventory(source)
    guard files["SKILL.md"] != nil else { throw BackendError.unavailable("The bundled Limmr skill is missing.") }
    try fm.createDirectory(at: destination.deletingLastPathComponent(), withIntermediateDirectories: true)
    try fm.copyItem(at: source, to: destination)
    do {
      let receipt = Receipt(files: files)
      try Wire.data(receipt).write(to: destination.appending(path: Self.receiptName), options: .atomic)
      return receipt
    } catch { try? fm.removeItem(at: destination); throw error }
  }
  public func receipt(destination: URL) throws -> Receipt {
    try Wire.decode(Receipt.self, from: Data(contentsOf: destination.appending(path: Self.receiptName)))
  }
  public func remove(destination: URL) throws {
    let receipt = try receipt(destination: destination)
    guard try inventory(destination) == receipt.files else {
      throw BackendError.unavailable("This skill has changed since Limmr added it. Remove it yourself to keep your edits safe.")
    }
    try FileManager.default.removeItem(at: destination)
  }
}
