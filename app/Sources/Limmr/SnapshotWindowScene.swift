import AppKit
import SwiftUI

/// --window-chrome uses WindowGroup itself: a hand-made NSWindow does not
/// exercise SwiftUI's scene-level toolbar/title-bar visibility updates.
@MainActor @Observable final class SnapshotWindowScene {
  static let shared = SnapshotWindowScene()
  var content: AnyView?
  var contentSize = NSSize(width: 900, height: 568)
  weak var window: NSWindow?

  static func capture(content: AnyView, size: NSSize, path: String, assertChrome: Bool) async {
    shared.content = content
    for _ in 0..<100 where shared.window == nil {
      try? await Task.sleep(for: .milliseconds(50))
    }
    guard let window = shared.window else {
      fputs("SwiftUI snapshot scene did not create its window.\n", stderr); exit(1)
    }
    // A scene content minimum lets the real window exceed the physical display.
    // Otherwise AppKit silently caps tall capture windows to the screen height.
    let titlebarHeight = window.frame.height - window.contentLayoutRect.height
    shared.contentSize = NSSize(width: size.width, height: size.height - titlebarHeight)
    window.setFrameOrigin(NSPoint(x: -10000, y: -10000))
    window.setFrame(NSRect(origin: window.frame.origin, size: size), display: false)
    window.orderBack(nil)
    try? await Task.sleep(for: .seconds(3))
    window.contentView?.layoutSubtreeIfNeeded()
    window.displayIfNeeded()
    print("Window chrome: " + describe(window, size: size))
    do {
      let url = URL(fileURLWithPath: path)
      try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
      try N1NativeSnapshot.write(window: window, to: path, size: size)
    } catch {
      fputs("Native window capture failed: \(error.localizedDescription)\n", stderr); exit(1)
    }
    let failures = WindowChromeProbe.failures(window)
    window.orderOut(nil)
    exit(assertChrome && (!failures.isEmpty || window.frame.size != size) ? 1 : 0)
  }

  private static func describe(_ window: NSWindow, size: NSSize) -> String {
    "requested=\(size), actual=\(window.frame.size), " + WindowChromeProbe.describe(window)
  }
}

struct SnapshotWindowSceneView: View {
  var body: some View {
    Group {
      if let content = SnapshotWindowScene.shared.content { content }
      else { Color.clear }
    }.frame(minWidth: SnapshotWindowScene.shared.contentSize.width,
      minHeight: SnapshotWindowScene.shared.contentSize.height)
      .background { SnapshotWindowReader() }
  }
}

private struct SnapshotWindowReader: NSViewRepresentable {
  final class WindowView: NSView {
    override func viewDidMoveToWindow() {
      super.viewDidMoveToWindow()
      guard let window else { return }
      SnapshotWindowScene.shared.window = window
      window.setFrameOrigin(NSPoint(x: -10000, y: -10000))
    }
  }
  func makeNSView(context: Context) -> WindowView { WindowView() }
  func updateNSView(_ view: WindowView, context: Context) {}
}
