import SwiftUI

/// Native buttons retain keyboard/VoiceOver activation without the accent wash.
/// Focus uses a thin secondary outline; hover and press use neutral system ink.
struct QuietCardButtonStyle: ButtonStyle {
  func makeBody(configuration: Configuration) -> some View {
    QuietCardBody(configuration: configuration)
  }
  private struct QuietCardBody: View {
    let configuration: ButtonStyleConfiguration
    @State private var hovering = false
    @Environment(\.isFocused) private var focused
    var body: some View {
      configuration.label
        .background(Color.primary.opacity(configuration.isPressed ? 0.07 : hovering ? 0.035 : 0), in: RoundedRectangle(cornerRadius: 8))
        .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(focused ? Color.secondary.opacity(0.5) : .clear, lineWidth: 1))
        .onHover { hovering = $0 }
        .focusEffectDisabled()
    }
  }
}
private struct QuietCardActivationStyle: PrimitiveButtonStyle {
  @Environment(\.isEnabled) private var enabled
  func makeBody(configuration: Configuration) -> some View {
    Button(role: configuration.role, action: configuration.trigger) { configuration.label }
      .buttonStyle(QuietCardButtonStyle()).focusEffectDisabled()
      .onKeyPress(.return) {
        guard enabled else { return .ignored }
        configuration.trigger(); return .handled
      }
  }
}
extension View {
  func quietCard() -> some View { buttonStyle(QuietCardActivationStyle()).focusEffectDisabled() }
  func quietKeyboardRow(_ id: String, selection: String?) -> some View {
    overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(selection == id ? Color.secondary.opacity(0.5) : .clear, lineWidth: 1).allowsHitTesting(false))
      .accessibilityAddTraits(selection == id ? [.isSelected] : [])
      .id(id)
  }
}
