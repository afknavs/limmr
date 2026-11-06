import LimmrKit
import SwiftUI

extension CircleHue {
  var color: Color {
    switch self {
    case .blue: .blue
    case .orange: .orange
    case .purple: .purple
    case .green: .green
    case .yellow: .yellow
    case .teal: .teal
    case .pink: .pink
    case .indigo: .indigo
    }
  }
}
extension LimmrKit.Circle {
  var hue: CircleHue { CircleHue.stable(id: id, icon: icon) }
  var hueInk: Color { hue.color }
}
struct CircleIdentityMenu: View {
  @Bindable var store: AppStore
  let circle: LimmrKit.Circle
  var body: some View {
    Menu("Circle symbol") {
      ForEach(["graduationcap", "heart", "star", "briefcase", "house", "person.3", "paintbrush", "book"], id: \.self) { symbol in
        Button(symbol, systemImage: symbol) { Task { _ = await store.setIcon(bucket: "circles", id: circle.id, icon: .init(kind: "symbol", value: symbol, hue: circle.icon?.hue)) } }
      }
    }
    Menu("Circle colour") {
      ForEach(CircleHue.allCases, id: \.self) { hue in
        Button(hue.rawValue.capitalized, systemImage: circle.hue == hue ? "checkmark.circle.fill" : "circle.fill") {
          Task { await store.setCircleHue(circle, hue: hue) }
        }
      }
    }
  }
}
extension AppStore {
  func setCircleHue(_ circle: LimmrKit.Circle, hue: CircleHue) async {
    var icon = circle.icon ?? .init(kind: "symbol", value: circle.displaySymbol)
    icon.hue = hue.rawValue
    _ = await setIcon(bucket: "circles", id: circle.id, icon: icon)
  }
}
