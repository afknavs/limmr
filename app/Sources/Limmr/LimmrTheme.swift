import SwiftUI

/// Paper: all appearance-dependent brand values live here.
enum LimmrTheme {
  static func pair(_ light: UInt32, _ dark: UInt32) -> Color {
    Color(nsColor: NSColor(name: nil) { appearance in
      let hex = appearance.bestMatch(from: [.aqua, .darkAqua]) == .darkAqua ? dark : light
      return NSColor(srgbRed: Double((hex >> 16) & 255) / 255,
        green: Double((hex >> 8) & 255) / 255, blue: Double(hex & 255) / 255, alpha: 1)
    })
  }
  static let canvas = pair(0xEFEBE4, 0x141413)
  static let panel = pair(0xFDFCFA, 0x201F1D)
  static let surface = pair(0xF5F2EC, 0x292725)
  static let surface2 = pair(0xE9E5DD, 0x353330)
  static let ink = pair(0x141413, 0xF4EFE7)
  static let ink2 = pair(0x5E5B56, 0xA6A29A)
  static let ink3 = pair(0x918D86, 0x77736D)
  static let onPrimary = pair(0xFCFAF6, 0x141413)
  static let brand = pair(0xF4461F, 0xF4461F)
  static let needsYou = pair(0xC2370F, 0xFF7B57)
  static let needBackground = alpha(0xF4461F, 0.10, 0xF4461F, 0.18)
  static let info = pair(0x0074E2, 0x6D9CFE)
  static let infoText = pair(0x0067CE, 0xA1CAFF)
  static let infoBackground = alpha(0x0074E2, 0.10, 0x88BCFF, 0.16)
  static let green = pair(0x198100, 0x64AF4C)
  static let navOn = alpha(0x141413, 0.07, 0xF4EFE7, 0.09)
  static let hairline = alpha(0x141413, 0.09, 0xF4EFE7, 0.10)
  static let line2 = alpha(0x141413, 0.17, 0xF4EFE7, 0.18)
  static let accent = ink
  private static func alpha(_ light: UInt32, _ la: Double, _ dark: UInt32, _ da: Double) -> Color {
    Color(nsColor: NSColor(name: nil) { appearance in
      let isDark = appearance.bestMatch(from: [.aqua, .darkAqua]) == .darkAqua
      let hex = isDark ? dark : light
      return NSColor(srgbRed: Double((hex >> 16) & 255) / 255,
        green: Double((hex >> 8) & 255) / 255, blue: Double(hex & 255) / 255, alpha: isDark ? da : la)
    })
  }
  static func font(_ size: CGFloat, _ weight: Font.Weight = .regular) -> Font {
    let name = weight == .bold ? "Figtree-Bold" : weight == .semibold ? "Figtree-SemiBold" : weight == .medium ? "Figtree-Medium" : "Figtree-Regular"
    return NSFont(name: name, size: size) == nil ? .system(size: size, weight: weight) : .custom(name, fixedSize: size)
  }
  enum TypeScale {
    static var page: Font { font(24, .semibold) }
    static var section: Font { font(14, .semibold) }
    static var card: Font { font(17, .semibold) }
    static var body: Font { font(14) }
    static var meta: Font { font(12) }
  }
  static let cardRadius: CGFloat = 16, tileRadius: CGFloat = 16
}
extension View {
  func n1PageSurface() -> some View {
    background { LimmrTheme.panel.ignoresSafeArea(edges: .top) }
      .toolbarBackgroundVisibility(.automatic, for: .windowToolbar)
  }
  func n1Card() -> some View {
    background(LimmrTheme.surface, in: RoundedRectangle(cornerRadius: LimmrTheme.cardRadius))

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
