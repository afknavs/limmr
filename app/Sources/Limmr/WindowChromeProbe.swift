import AppKit

/// Inspect the real frame view, including ancestor visibility and hit testing.
/// Used by offline snapshots and mounted-window regression tests.
@MainActor enum WindowChromeProbe {
  static let buttons: [(String, NSWindow.ButtonType)] = [
    ("close", .closeButton), ("minimise", .miniaturizeButton), ("zoom", .zoomButton)
  ]

  static func failures(_ window: NSWindow) -> [String] {
    var failures: [String] = []
    let required: NSWindow.StyleMask = [.titled, .closable, .miniaturizable, .resizable]
    if !window.styleMask.contains(required) { failures.append("missing native window styles") }
    guard let frame = window.contentView?.superview else { return failures + ["missing frame view"] }
    func boundary(in view: NSView) -> NSView? {
      if view.identifier?.rawValue == "LimmrContentBoundary" { return view }
      return view.subviews.lazy.compactMap { boundary(in: $0) }.first
    }
    let contentTop = boundary(in: frame).map { $0.convert(.zero, to: frame).y }
    if contentTop == nil { failures.append("missing content boundary") }
    for (name, type) in buttons {
      guard let button = window.standardWindowButton(type) else {
        failures.append("\(name) missing"); continue
      }
      if button.isHiddenOrHasHiddenAncestor { failures.append("\(name) hidden") }
      let rect = button.convert(button.bounds, to: frame)
      if !frame.bounds.contains(rect) { failures.append("\(name) outside window") }
      if let contentTop, contentTop > rect.minY { failures.append("page extends beneath \(name)") }
      let hit = frame.hitTest(NSPoint(x: rect.midX, y: rect.midY))
      if hit !== button && hit?.isDescendant(of: button) != true { failures.append("\(name) covered") }
    }
    return failures
  }

  static func describe(_ window: NSWindow) -> String {
    "style=\(window.styleMask.rawValue), toolbar=\(window.toolbar?.isVisible.description ?? "nil"), "
      + "layout=\(window.contentLayoutRect), failures=\(failures(window))"
  }
}
