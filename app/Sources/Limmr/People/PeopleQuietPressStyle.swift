import SwiftUI

/// Retains the native Button action and accessibility, with a neutral press state.
struct PeopleQuietPressStyle: ButtonStyle {
  func makeBody(configuration: Configuration) -> some View {
    configuration.label
      .background(.primary.opacity(configuration.isPressed ? 0.07 : 0), in: RoundedRectangle(cornerRadius: 6))
      .opacity(configuration.isPressed ? 0.78 : 1)
  }
}
extension View {
  func peopleQuietButton() -> some View { buttonStyle(PeopleQuietPressStyle()).focusEffectDisabled() }
}
