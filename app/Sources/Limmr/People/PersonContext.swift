import LimmrKit
import SwiftUI

struct PersonCircleOverlap: View {
  @Bindable var store: AppStore
  let id: String
  private var circles: [LimmrKit.Circle] {
    store.circles.filter { circle in
      let groups = store.profile?.person.id == id ? (store.profile?.groups.map(\.jid) ?? []) : []
      let memberships = store.circleMap.nodes.first(where: { $0.id == id }).map { UnderstandingLayout.memberships($0, map: store.circleMap) } ?? []
      return circle.confirmed && (memberships.contains(circle.id) || (circle.people + (store.circleDetails[circle.id]?.people ?? [])).contains { $0.personId == id }
        || circle.chats.contains { groups.contains($0.chatJid) })
    }
  }
  var body: some View {
    if !circles.isEmpty {
      VStack(alignment: .leading, spacing: 10) {
        Text("Shared circles").font(.callout.weight(.semibold))
        ForEach(circles) { circle in
          Button { store.circlePath.append(.circle(circle.id)) } label: {
            HStack(spacing: 8) {
              CirclePicture(circle: circle, size: 24)
              VStack(alignment: .leading, spacing: 2) {
                Text(circle.name).font(AppType.caption).lineLimit(1)
                if let role = (store.circleDetails[circle.id]?.people ?? circle.people).first(where: { $0.personId == id })?.role, !role.isEmpty {
                  Text(role).font(.caption2).foregroundStyle(AccessibleSecondary()).lineLimit(1)
                }
              }
              Spacer(minLength: 0)
            }.contentShape(Rectangle())
          }.peopleQuietButton()
        }
      }
    }
  }
}
struct PersonConversations: View {
  @Bindable var store: AppStore
  let person: Person
  @State private var messages: [Message] = []
  @State private var failed = false
  var body: some View {
    VStack(alignment: .leading, spacing: 10) {
      if !messages.isEmpty || failed {
        Text("Recent conversations").font(.callout.weight(.semibold))
        ForEach(messages.prefix(3)) { message in
          Button {
            store.circlePath.append(.chat(message.chatJid, store.chatName(message.chatJid)))
          } label: {
            VStack(alignment: .leading, spacing: 4) {
              Text(message.text).font(AppType.caption).foregroundStyle(.primary).lineLimit(2)
              Text("\(message.isFromMe ? "You" : store.personName(person.id)) · \(store.relativeTime(message.timestamp))")
                .font(.caption2).foregroundStyle(AccessibleSecondary())
            }.frame(maxWidth: .infinity, alignment: .leading).contentShape(Rectangle())
          }.peopleQuietButton()
        }
        if failed { Button("Try loading conversations again") { Task { await load() } }.peopleQuietButton().font(AppType.caption) }
      }
    }.task(id: "\(store.backendGeneration):\(person.id)") { await load() }
  }
  private func load() async {
    let source = store.backend
    let generation = store.backendGeneration
    var recent: [Message] = []
    do {
      for jid in person.jids.filter({ !$0.hasSuffix("@g.us") }).prefix(2) {
        var query = URLComponents(); query.queryItems = [.init(name: "chat_jid", value: jid), .init(name: "limit", value: "3")]
        let page: MessagesResponse = try await source.get("/v1/messages?" + (query.percentEncodedQuery ?? ""))
        recent += page.messages.filter { !$0.text.isEmpty && $0.deletedAt == nil }
      }
      try Task.checkCancellation()
      guard generation == store.backendGeneration else { return }
      var seen = Set<String>()
      messages = recent.sorted { $0.timestamp > $1.timestamp }.filter { seen.insert($0.id).inserted }
      failed = false
    } catch { if !Task.isCancelled { failed = true } }
  }
}
struct MapPersonInspector: View {
  @Bindable var store: AppStore
  let id: String
  private var person: Person? { store.profile?.person.id == id ? store.profile?.person : (PersonPageCache.cached(id, store: store)?.person ?? store.peoplePreview(id)) }
  var body: some View {
    ScrollView {
      VStack(alignment: .leading, spacing: 18) {
        HStack {
          Text("Person").font(AppType.caption).foregroundStyle(AccessibleSecondary())
          Spacer()
          Button { store.closePeopleInspector() } label: { Image(systemName: "xmark") }
            .peopleQuietButton().accessibilityLabel("Close person panel")
        }
        if let person {
          HStack(spacing: 10) {
            PersonAvatar(store: store, id: id, name: store.personName(id), size: 36)
            Text(store.personName(id)).font(.headline).lineLimit(2)
          }
          Button("Open profile", systemImage: "arrow.up.right") { store.openPeopleRoute(.person(id)) }
            .peopleQuietButton().font(AppType.caption)
          if !person.howKnown.isEmpty { Text(person.howKnown).font(AppType.caption).foregroundStyle(AccessibleSecondary()) }
          PersonCircleOverlap(store: store, id: id)
          PersonConversations(store: store, person: person).id(person.id)
        }
      }.padding(16)
    }.frame(width: 236, height: 410)
      .background(.primary.opacity(0.025), in: RoundedRectangle(cornerRadius: 10))
      .task(id: id) { await store.loadPersonProgressively(id) }
  }
}

struct MapConnectionMarker: Identifiable {
  let id: String
  let point: CGPoint
  let symbol: String
  let label: String
  static func make(map: CircleMap, positions: [String: CGPoint], selected: String) -> [Self] {
    guard let start = positions[selected] else { return [] }
    var shown = Set<String>()
    var markers: [Self] = []
    for edge in map.edges.filter({ $0.state != "reverted" && ($0.source == selected || $0.target == selected) }).sorted(by: { $0.key < $1.key }) {
      let other = edge.source == selected ? edge.target : edge.source
      guard shown.insert(other).inserted, let end = positions[other], let node = map.nodes.first(where: { $0.id == other }) else { continue }
      let point = CGPoint(x: start.x * 0.45 + end.x * 0.55, y: start.y * 0.45 + end.y * 0.55)
      guard hypot(point.x-start.x, point.y-start.y) > 22,
        hypot(point.x-end.x, point.y-end.y) > 22,
        !markers.contains(where: { hypot($0.point.x-point.x, $0.point.y-point.y) < 32 }) else { continue }
      markers.append(Self(id: edge.key, point: point,
        symbol: node.kind == "circle" ? "person.2" : node.kind == "chat" ? "bubble.left.and.bubble.right" : "link",
        label: node.title + " · " + edge.hoverText))
      if markers.count == 4 { break }
    }
    return markers
  }
}
