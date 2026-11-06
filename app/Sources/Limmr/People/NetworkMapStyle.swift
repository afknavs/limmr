import SwiftUI
import LimmrKit

/// Shared system hues, including the owner's persisted choice.
enum NetworkMapStyle {
  static func hue(_ id: String, circle: LimmrKit.Circle?) -> CircleHue {
    CircleHue.stable(id: id, icon: circle?.icon)
  }
  static func fill(_ id: String, circle: LimmrKit.Circle?) -> Color {
    hue(id, circle: circle).color.opacity(0.16)
  }
  static func ink(_ id: String, circle: LimmrKit.Circle?) -> Color {
    hue(id, circle: circle).color
  }
  // Avatar initials retain the same deterministic pastel as PersonAvatar.
  static func color(_ hex: UInt32) -> Color {
    Color(red: Double((hex >> 16) & 255) / 255, green: Double((hex >> 8) & 255) / 255, blue: Double(hex & 255) / 255)
  }
  static func symbol(_ circle: LimmrKit.Circle?) -> String {
    if let icon = circle?.icon, icon.kind == "symbol" { return icon.value }
    return circle?.displaySymbol ?? "circle.grid.2x2"
  }
}
