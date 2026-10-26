import Foundation
import Testing
@testable import LimmrKit

@Suite("A9 explicit skill installation") struct IntegrationResourcesTests {
  @Test func copyReadBackRemoveAndProtectExistingEdits() throws {
    let fm = FileManager.default
    let temp = fm.temporaryDirectory.appending(path: UUID().uuidString)
    try fm.createDirectory(at: temp, withIntermediateDirectories: true)
    defer { try? fm.removeItem(at: temp) }
    let source = temp.appending(path: "bundled")
    try fm.createDirectory(at: source, withIntermediateDirectories: true)
    let text = Data("---\nname: limmr\n---\nUse Limmr.".utf8)
    try text.write(to: source.appending(path: "SKILL.md"))
    let destination = temp.appending(path: "test-home/skills/limmr")
    let installer = SkillInstaller()
    let receipt = try installer.install(source: source, destination: destination)
    #expect(receipt.files.keys.sorted() == ["SKILL.md"])
    #expect(try Data(contentsOf: destination.appending(path: "SKILL.md")) == text)
    #expect(throws: (any Error).self) { try installer.install(source: source, destination: destination) }
    try Data("owner edits".utf8).write(to: destination.appending(path: "SKILL.md"))
    #expect(throws: (any Error).self) { try installer.remove(destination: destination) }
    #expect(fm.fileExists(atPath: destination.path))
    try text.write(to: destination.appending(path: "SKILL.md"))
    try installer.remove(destination: destination)
    #expect(!fm.fileExists(atPath: destination.path))
  }
  @Test func sampleResourceResolutionAndBundledInstructions() throws {
    let root = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
    let resources = try #require(IntegrationResources.locate(bundleResources: nil, repository: root, sample: true))
    #expect(try resources.instructions().contains("Limmr"))
    #expect(FileManager.default.fileExists(atPath: resources.skill(kind: "codex").appending(path: "SKILL.md").path))
    #expect(FileManager.default.fileExists(atPath: resources.skill(kind: "claude_code").appending(path: "SKILL.md").path))
    #expect(IntegrationResources.locate(bundleResources: nil, repository: root, sample: false) == nil)
  }
}
