import SwiftUI

/// Only semantic system styles; circle colour is owned by CircleIdentityStyle.
enum LimmrTheme {
  static let canvas = Color(nsColor: .windowBackgroundColor)
  static let surface = Color(nsColor: .controlBackgroundColor)
  static let surface2 = Color.primary.opacity(0.04)
  static let hairline = Color(nsColor: .separatorColor)
  static let ink = Color.primary
  static let ink2 = Color.secondary
  static let accent = Color.accentColor
  static let needsYou = Color.orange
  enum TypeScale {
    static let page = Font.largeTitle.bold()
    static let section = Font.title3.weight(.semibold)
    static let card = Font.headline
    static let body = Font.body
    static let meta = Font.caption
  }
  static let cardRadius: CGFloat = 10, tileRadius: CGFloat = 12
}
extension View {
  func n1PageSurface() -> some View {
    background { Color(nsColor: .windowBackgroundColor).ignoresSafeArea(edges: .top) }
      .toolbarBackgroundVisibility(.automatic, for: .windowToolbar)
  }
  func n1Card() -> some View {
    background(LimmrTheme.surface, in: RoundedRectangle(cornerRadius: LimmrTheme.cardRadius))
      .overlay(RoundedRectangle(cornerRadius: LimmrTheme.cardRadius).stroke(LimmrTheme.hairline, lineWidth: 0.5))
  }
  func coverLabel() -> some View { modifier(CoverLabel()) }
}
private struct CoverLabel: ViewModifier {
  @Environment(\.accessibilityReduceTransparency) private var reduceTransparency
  @Environment(\.snapshotReduceTransparency) private var snapshotReduceTransparency
  func body(content: Content) -> some View {
    if reduceTransparency || snapshotReduceTransparency {
      content.background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 8))
    } else {
      content.glassEffect(.regular, in: .rect(cornerRadius: 8))
    }
  }
}

private struct SnapshotReduceTransparencyKey: EnvironmentKey { static let defaultValue = false }
extension EnvironmentValues {
  var snapshotReduceTransparency: Bool {
    get { self[SnapshotReduceTransparencyKey.self] }
    set { self[SnapshotReduceTransparencyKey.self] = newValue }
  }
}
