import SwiftUI

/// Static neutral shapes, without animation or invented counts while the first read is pending.
struct QuietPlaceholder: View {
  var height: Double = 16
  var body: some View {
    RoundedRectangle(cornerRadius: 4).fill(.primary.opacity(0.055))
      .frame(height: height).accessibilityHidden(true)
  }
}
struct QuietLoadingCards: View {
  var body: some View {
    VStack(spacing: 12) {
      ForEach(0..<2, id: \.self) { _ in
        QuietPlaceholder(height: 96)
      }
    }.accessibilityElement(children: .ignore).accessibilityLabel("Loading")
  }
}
