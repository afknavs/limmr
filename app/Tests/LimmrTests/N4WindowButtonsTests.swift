import Foundation
import Testing

@Suite("N4 real SwiftUI window buttons", .serialized) struct N4WindowButtonsTests {
  @Test(arguments: ["n4-home", "n4-tasks", "n4-people", "n4-activity", "n4-task"], ["900x600", "1200x800", "1700x1050"])
  func nativeButtonsRemainVisibleAndHitTestable(page: String, size: String) throws {
    let app = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
      .deletingLastPathComponent().deletingLastPathComponent()
    let home = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: home) }
    let process = Process()
    process.executableURL = app.appendingPathComponent(".build/debug/Limmr")
    process.arguments = ["--snapshot", home.appendingPathComponent("window.png").path,
      "--page", page, "--size", size, "--window-chrome", "--assert-window-chrome"]
    var environment = ProcessInfo.processInfo.environment
    environment["LIMMR_HOME"] = home.path
    process.environment = environment
    process.standardInput = FileHandle.nullDevice
    try process.run()
    let timeout = DispatchWorkItem { if process.isRunning { process.terminate() } }
    DispatchQueue.global().asyncAfter(deadline: .now() + 25, execute: timeout)
    process.waitUntilExit()
    timeout.cancel()
    #expect(process.terminationStatus == 0, "\(page) must retain native styles and visible, uncovered buttons")
  }
}
