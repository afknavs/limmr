import AppKit
import SwiftUI

/// Navigation destinations contribute their own toolbar preferences. Apply this
/// inside each stack as well as at the window root so a push cannot restore chrome.
extension View {
  /// Window chrome belongs to the shell. Hiding windowToolbar on a destination
  /// also hides the standard button container in a SwiftUI-managed window.
  func mainWindowChrome() -> some View {
    toolbarVisibility(.visible, for: .windowToolbar)
      .toolbar(removing: .title)
  }

  func pageSurface() -> some View {
    background { AppBackground().ignoresSafeArea(edges: .top) }
      .toolbarBackgroundVisibility(.hidden, for: .windowToolbar)
  }
}

/// A zero-sized marker at the top of the shell's safe content area lets the
/// regression probe check that page layout stays below the native controls.
struct MainWindowContentBoundary: NSViewRepresentable {
  func makeNSView(context: Context) -> NSView {
    let view = NSView()
    view.identifier = NSUserInterfaceItemIdentifier("LimmrContentBoundary")
    return view
  }
  func updateNSView(_ view: NSView, context: Context) {}
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
