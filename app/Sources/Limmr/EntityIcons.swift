import LimmrKit
import SwiftUI

struct EntityIconButton: View {
  @Bindable var store: AppStore
  let bucket: String
  let id: String
  let icon: EntityIcon?
  var fallback = "person.crop.circle"
  var initials: String? = nil
  var title: String? = nil
  @State private var showing = false
  var body: some View {
    Button {
      showing = true
    } label: {
      if let title {
        HStack(spacing: 5) {
          if let icon, icon.kind == "emoji" { Text(icon.value) }
          else if let icon { Image(systemName: icon.value) }
          Text(title)
          Image(systemName: "chevron.down").font(.system(size: 9, weight: .medium))
        }
        .font(AppType.caption).foregroundStyle(AccessibleSecondary())
        .padding(.vertical, 6).contentShape(Rectangle())
      } else {
        Group {
          if let icon, icon.kind == "emoji" {
            Text(icon.value)
          } else if let icon {
            Image(systemName: icon.value)
          } else if let initials {
            Text(initials).font(.caption.weight(.medium))
          } else {
            Image(systemName: fallback)
          }
        }.frame(width: 28, height: 28).contentShape(RoundedRectangle(cornerRadius: 6))
      }
    }.limmrButton().help("Change icon")
      .accessibilityLabel("Change icon for \(bucket == "agents" ? store.agentName(id) : bucket == "people" ? store.personName(id) : store.circleName(id))")
      .accessibilityValue(icon?.value ?? "Default icon")
      .accessibilityHint("Choose a symbol or emoji")
      .popover(isPresented: $showing) {
        IconPickerView(recent: store.recentIcons) { picked in
          Task { if await store.setIcon(bucket: bucket, id: id, icon: picked) { showing = false } }
        }.dismissOnEscape().overlay(alignment: .bottom) { ActionFeedback(store: store, action: "icon") }
      }
  }
}
struct IconPickerView: View {
  var recent: [EntityIcon] = []
  let select: (EntityIcon?) -> Void
  @State private var query = ""
  private let symbols = [
    "person.crop.circle", "person.2", "person.3", "house", "graduationcap", "briefcase", "heart",
    "book", "basketball", "leaf", "music.note", "paintbrush", "terminal",
    "bubble.left.and.bubble.right", "asterisk", "globe", "folder", "camera", "cup.and.saucer",
    "bicycle", "star", "calendar", "puzzlepiece.extension",
  ]
  private let emoji: [(String, String)] = [
    ("🎓", "study university"), ("🏠", "home family"), ("📚", "books reading"), ("🎨", "art design"),
    ("🌱", "plant nature"), ("💬", "chat"), ("🤖", "robot ai"), ("🎵", "music"), ("☕️", "coffee"),
    ("🏀", "sport basketball"),
  ]
  var body: some View {
    VStack(alignment: .leading, spacing: 16) {
      Text("Choose an icon").font(.headline)
      TextField("Search symbols or emoji", text: $query).textFieldStyle(.roundedBorder)
      if !recent.isEmpty && query.isEmpty {
        Text("Recent").font(.caption).foregroundStyle(AccessibleSecondary())
        HStack { ForEach(recent.prefix(6), id: \.self) { pick($0) } }
      }
      ScrollView {
        LazyVGrid(columns: Array(repeating: GridItem(.fixed(36)), count: 6), spacing: 8) {
          ForEach(
            symbols.filter { query.isEmpty || $0.localizedCaseInsensitiveContains(query) },
            id: \.self
          ) { pick(EntityIcon(kind: "symbol", value: $0)) }
          ForEach(
            emoji.filter {
              query.isEmpty || $0.1.localizedCaseInsensitiveContains(query) || $0.0.contains(query)
            }, id: \.0
          ) { pick(EntityIcon(kind: "emoji", value: $0.0)) }
        }
      }.frame(height: 240)
      if !query.isEmpty && query.unicodeScalars.allSatisfy({ $0.isASCII && (CharacterSet.lowercaseLetters.contains($0) || CharacterSet.decimalDigits.contains($0) || $0 == ".") }) {
        Button("Use symbol “\(query)”") { select(.init(kind: "symbol", value: query)) }.limmrButton()
      } else if !query.isEmpty
        && query.contains(where: {
          $0.unicodeScalars.contains { $0.properties.isEmojiPresentation }
        })
      {
        Button("Use \(query)") { select(.init(kind: "emoji", value: query)) }.limmrButton()
      }
      Button("Use default icon") { select(nil) }.limmrButton().font(.caption)
    }.padding(16).frame(width: 296)
  }
  private func pick(_ icon: EntityIcon) -> some View {
    Button {
      select(icon)
    } label: {
      Group { if icon.kind == "emoji" { Text(icon.value) } else { Image(systemName: icon.value) } }
        .frame(width: 32, height: 32).background(
          .primary.opacity(0.04), in: RoundedRectangle(cornerRadius: 6))
    }.limmrButton().help(icon.value).accessibilityLabel(icon.value)
  }
}
extension AppStore {
  func refreshHistory() async {
    await load("/v1/stats/history?days=364", title: "Message history") { (value: StatsHistory) in
      history = value
    }
  }

  func setIcon(bucket: String, id: String, icon: EntityIcon?) async -> Bool {
    var succeeded = false
    await perform(action: "icon") { [self] in
      struct Patch: Encodable {
        let icon: EntityIcon?
        func encode(to encoder: Encoder) throws {
          var c = encoder.container(keyedBy: Keys.self)
          try c.encode(icon, forKey: .icon)
        }
        enum Keys: String, CodingKey { case icon }
      }
      _ = try await backend.request(
        "/v1/\(bucket)/\(id)", method: "PATCH", body: Wire.data(Patch(icon: icon)))
      if let icon { recentIcons = Array(([icon] + recentIcons.filter { $0 != icon }).prefix(6)) }
      await refresh()
      if bucket == "circles" { await loadCircle(id) }
      succeeded = true
    }
    return succeeded
  }
  func requestReplyDraft(_ ask: ReplyAsk, agent: Agent) async {
    await perform(action: "ask-\(ask.id)") { [self] in
      _ = try await backend.request(
        "/v1/circles/\(ask.circleId)/asks/\(ask.id)/draft", method: "POST",
        body: Wire.data(["agent_id": agent.id]))
      requestedReplyDrafts.insert(ask.id)
      await refresh()
    }
  }
}
