import AppKit
import CoreText
import SwiftUI
import LimmrKit

struct PersonAvatar: View {
  @Bindable var store: AppStore
  let id: String
  let name: String
  var size: CGFloat = 32
  var icon: EntityIcon? = nil
  @Environment(\.colorScheme) private var scheme
  @Environment(\.colorSchemeContrast) private var contrast
  private var chosenIcon: EntityIcon? { icon ?? store.people.first { $0.id == id }?.icon }
  static func paletteIndex(_ id: String) -> Int {
    Int(id.utf8.reduce(UInt64(14695981039346656037)) { ($0 ^ UInt64($1)) &* 1099511628211 } % 5)
  }
  nonisolated static func initials(_ name: String) -> String {
    // Letters only, like Contacts: a phone number or emoji gets the silhouette instead.
    let words = name.split(whereSeparator: \.isWhitespace).filter { $0.first?.isLetter == true }
    return [words.first?.first, words.count > 1 ? words.last?.first : nil].compactMap { $0 }.map(String.init).joined().uppercased()
  }
  var body: some View {
    Group {
      if let data = store.avatarData[id] {
        ThumbnailImage(data: data, identity: "avatar-\(store.backendGeneration)-\(id)", pixels: Int(size * 2)).scaledToFill()
      } else if let icon = chosenIcon {
        Group {
          if (icon.kind == "emoji" || icon.kind == "monogram") { Text(icon.value) } else { Image(systemName: icon.value) }
        }.font(.system(size: size * 0.44, weight: .medium)).foregroundStyle(NetworkMapStyle.color(0x1D1D20))
          .frame(width: size, height: size).background(background)
      } else if Self.initials(name).isEmpty {
        Image(systemName: id.hasSuffix("@g.us") ? "person.2.fill" : "person.fill")
          .font(.system(size: size * (id.hasSuffix("@g.us") ? 0.4 : 0.5))).foregroundStyle(NetworkMapStyle.color(0x1D1D20))
          .offset(y: id.hasSuffix("@g.us") ? 0 : size * 0.06)
          .frame(width: size, height: size).background(background)
      } else {
        Canvas { context, dimensions in
          context.withCGContext { cg in
            // Contacts-style initials: white SF Rounded on the grey gradient.
            let base = NSFont.systemFont(ofSize: size * 0.4, weight: .semibold)
            let font = base.fontDescriptor.withDesign(.rounded).flatMap { NSFont(descriptor: $0, size: size * 0.4) } ?? base
            let text = NSAttributedString(string: Self.initials(name), attributes: [
              NSAttributedString.Key(kCTFontAttributeName as String): font,
              NSAttributedString.Key(kCTForegroundColorAttributeName as String): NSColor(calibratedRed: 29/255, green: 29/255, blue: 32/255, alpha: 1).cgColor])
            let line = CTLineCreateWithAttributedString(text)
            let glyphs = CTLineGetBoundsWithOptions(line, .useGlyphPathBounds)
            cg.translateBy(x: dimensions.width / 2 - glyphs.midX, y: dimensions.height / 2 + glyphs.midY)
            cg.scaleBy(x: 1, y: -1); cg.textPosition = .zero; CTLineDraw(line, cg)
          }
        }.background(background)
      }
    }
    .frame(width: size, height: size).clipShape(SwiftUI.Circle())
    .overlay(SwiftUI.Circle().stroke(.primary.opacity(contrast == .increased ? 0.7 : 0.12), lineWidth: contrast == .increased ? 1 : 0.5))
    .accessibilityHidden(true)
     .task(id: "\(store.backendGeneration)-\(id)") {
      await store.loadAvatar(id)
      guard !store.snapshot && !store.demo else { return }
      let generation = store.backendGeneration
      while !Task.isCancelled && generation == store.backendGeneration && store.avatarData[id] == nil {
        do { try await Task.sleep(for: .seconds(15)) } catch { return }
        guard generation == store.backendGeneration else { return }
        store.avatarAttempts.remove(id)
        await store.loadAvatar(id)
      }
    }
  }
  /// Stable soft initial tile shared with the map; AvatarTint provides high-contrast ink.
  private var background: Color { NetworkMapStyle.color(AvatarTint.rgb(id: id)) }

}

extension AppStore {
  func personID(jid: String?, name: String? = nil) -> String? {
    people.first { person in jid.map { person.jids.contains($0) } ?? (person.name == name) }?.id
  }
  func loadAvatar(_ id: String) async {
    guard !id.isEmpty, !avatarAttempts.contains(id) else { return }
    avatarAttempts.insert(id)
    let generation = backendGeneration
    guard let segment = id.addingPercentEncoding(withAllowedCharacters: .alphanumerics),
      let data = try? await backend.request(id.hasSuffix("@g.us") ? "/v1/groups/\(segment)/photo" : "/v1/people/\(segment)/avatar", method: "GET", body: nil),
      data.count <= 5_000_000, await MediaPreviewCache.shared.raster(data, id: "avatar-\(id)", pixels: 144) != nil, generation == backendGeneration
    else { return }
    avatarData[id] = data
  }
}
