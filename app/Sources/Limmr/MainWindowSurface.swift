import AppKit
import SwiftUI

/// Navigation destinations contribute their own toolbar preferences. Apply this
/// inside each stack as well as at the window root so a push cannot restore chrome.
extension View {
  func pageSurface() -> some View {
    background { AppBackground().ignoresSafeArea(edges: .top) }
      .toolbarBackgroundVisibility(.hidden, for: .windowToolbar)
  }
}

@MainActor enum MainWindowSurface {
  static func apply(to window: NSWindow) {
    window.titlebarAppearsTransparent = true
    window.titlebarSeparatorStyle = .none
  }
}

/// Configure the actual SwiftUI window, without replacing its toolbar, delegate,
/// standard buttons, or native title-bar dragging behavior.
struct MainWindowSurfaceReader: NSViewRepresentable {
  final class WindowView: NSView {
    override func viewDidMoveToWindow() {
      super.viewDidMoveToWindow()
      if let window { MainWindowSurface.apply(to: window) }
    }
  }

  func makeNSView(context: Context) -> WindowView { WindowView() }
  func updateNSView(_ view: WindowView, context: Context) {
    if let window = view.window { MainWindowSurface.apply(to: window) }
  }
}
