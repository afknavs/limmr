import LimmrKit
import SwiftUI

/// Generated tiles work offline and stay identical on every synced device.
/// A picked tile lives in entity_icons, independently of AI circle edits.
enum CirclePictures {
  static func suggestions(_ circle: LimmrKit.Circle) -> [EntityIcon] {
    let text = (circle.kind + " " + circle.name).lowercased()
    let choices: [(String, String)] = [
      ("family home", "🏠"), ("work studio design", "🎨"), ("student university study college", "🎓"),
      ("friend close", "☕️"), ("book reading", "📚"), ("sport gym", "🏀"),
      ("music", "🎵"), ("self you", "🌱"), ("community collective", "💬")]
    let emoji = choices.first { pair in pair.0.split(separator: " ").contains { text.contains($0) } }?.1 ?? "🌿"
    let initials = PersonAvatar.initials(circle.name)
    return [.init(kind: "monogram", value: initials.isEmpty ? "C" : initials),
            .init(kind: "emoji", value: emoji), .init(kind: "symbol", value: circle.displaySymbol)]
  }
  static func chosen(_ circle: LimmrKit.Circle) -> EntityIcon { circle.icon ?? .init(kind: "symbol", value: circle.displaySymbol) }
}
struct CirclePicture: View {
  let circle: LimmrKit.Circle
  var icon: EntityIcon? = nil
  var size: CGFloat = 34
  var body: some View {
    let value = icon ?? CirclePictures.chosen(circle)
    Group {
      if value.kind == "monogram" { Text(value.value) }
      else { Image(systemName: value.kind == "symbol" ? value.value : circle.displaySymbol) }
    }.font(size >= 40 ? .title2 : .body).symbolRenderingMode(.hierarchical)
      .frame(width: size, height: size)
      .foregroundStyle(.secondary)


      .accessibilityHidden(true)
  }
}
struct CirclePictureButton: View {
  @Bindable var store: AppStore
  let circle: LimmrKit.Circle
  var size: CGFloat = 34
  @State private var showing = false
  var body: some View {
    Button { showing = true } label: { CirclePicture(circle: circle, size: size) }
      .limmrButton().help("Change picture for \(circle.name)")
      .accessibilityLabel("Change picture for \(circle.name)")
      .popover(isPresented: $showing) {
        CirclePicturePicker(store: store, circle: circle) { showing = false }
          .dismissOnEscape()
      }
  }
}
struct CirclePicturePicker: View {
  @Bindable var store: AppStore
  let circle: LimmrKit.Circle
  var close: () -> Void = {}
  var body: some View {
    VStack(alignment: .leading, spacing: 14) {
      Text("Circle picture").font(.headline)
      Text("Suggested for \(circle.name)").font(.caption).foregroundStyle(.secondary)
      HStack(spacing: 10) {
        ForEach(CirclePictures.suggestions(circle), id: \.self) { icon in
          Button {
            Task { if await store.setIcon(bucket: "circles", id: circle.id, icon: icon) { close() } }
          } label: {
            CirclePicture(circle: circle, icon: icon, size: 48)
              .overlay(alignment: .bottomTrailing) {
                if CirclePictures.chosen(circle) == icon { Image(systemName: "checkmark.circle.fill").font(.caption).foregroundStyle(Color.accentColor).background(.background, in: SwiftUI.Circle()) }
              }
          }.limmrButton().accessibilityLabel("Use \(icon.kind == "monogram" ? "initials" : icon.value)")
        }
      }
      HStack {
        ForEach(CircleHue.allCases, id: \.self) { hue in
          Button { Task { await store.setCircleHue(circle, hue: hue) } } label: {
            SwiftUI.Circle().fill(hue.color).frame(width: 23, height: 23)
              .overlay { if circle.hue == hue { Image(systemName: "checkmark").font(.caption.bold()).foregroundStyle(.white) } }
          }.quietCard().accessibilityLabel("Use \(hue.rawValue) colour")
        }
      }
      IconPickerView(recent: store.recentIcons) { icon in
        Task { if await store.setIcon(bucket: "circles", id: circle.id, icon: icon) { close() } }
      }
      Button("Use automatic picture") {
        Task { if await store.setIcon(bucket: "circles", id: circle.id, icon: nil) { close() } }
      }.limmrButton().font(.caption)
      ActionFeedback(store: store, action: "icon")
    }.padding(16).frame(width: 248)
  }
}

/// Prefer the circle's picture, then its first available group photo. Misses stay
/// monochrome; loading an image never creates a tinted placeholder.
struct HomeCirclePicture: View {
  @Bindable var store: AppStore
  let circle: LimmrKit.Circle
  var size: CGFloat = 34
  private var groups: [String] { circle.chats.map(\.chatJid).filter { $0.hasSuffix("@g.us") } }
  private var picturePath: String? {
    guard let icon = circle.icon, ["photo", "image"].contains(icon.kind),
      icon.value.hasPrefix("/v1/"), !icon.value.contains("..") else { return nil }
    return icon.value
  }
  var body: some View {
    Group {
      if let path = picturePath, let data = store.circlePictureData[path] {
        ThumbnailImage(data: data, identity: "circle-\(store.backendGeneration)-" + path, pixels: Int(size * 2)).scaledToFill()
      } else if let data = groups.compactMap({ store.avatarData[$0] }).first {
        ThumbnailImage(data: data, identity: "circle-group-\(store.backendGeneration)-" + circle.id, pixels: Int(size * 2)).scaledToFill()
      } else { CirclePicture(circle: circle, size: size) }
    }.frame(width: size, height: size).clipShape(RoundedRectangle(cornerRadius: 7))
      .accessibilityHidden(true)
      .task(id: "\(store.backendGeneration)-" + circle.id + (picturePath ?? "") + groups.joined()) {
        if let path = picturePath, store.circlePictureData[path] == nil {
          let generation = store.backendGeneration
          if let data = try? await store.backend.request(path, method: "GET", body: nil),
            data.count <= 5_000_000,
            await MediaPreviewCache.shared.raster(data, id: path, pixels: 128) != nil,
            generation == store.backendGeneration { store.circlePictureData[path] = data }
        }
        if picturePath.flatMap({ store.circlePictureData[$0] }) == nil {
          for group in groups {
            await store.loadAvatar(group)
            if store.avatarData[group] != nil { break }
          }
        }
      }
  }
}
