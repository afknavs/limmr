import SwiftUI
import LimmrKit

/// Map navigation remains a peer of the accessible People and Circles lists.
struct PeopleWebHeader: View {
  @Bindable var store: AppStore
  @State private var focusedMap: CircleMap?
  @State private var focusError: String?
  @Environment(\.snapshotRendering) private var snapshot
  private var map: CircleMap { focusedMap ?? store.circleMap }
  var body: some View {
    VStack(alignment: .leading, spacing: 12) {
      HStack(alignment: .top, spacing: 16) {
        VStack(spacing: 0) {
          PeopleNetworkReplay(store: store, map: map) { node in
            store.selectPeopleMapNode(node)
          }.frame(height: 280)
          if store.mapSelection.person != nil {
            HStack {
              Spacer()
              Button("Clear highlight") { store.closePeopleInspector() }.limmrButton().font(.caption)
            }.padding(12)
          }
          Divider()
          HStack(spacing: 0) {
            PeopleMapCountLabel(value: map.nodes.filter { $0.kind == "person" }.count, title: "People shown").frame(maxWidth: .infinity, alignment: .leading)
            PeopleMapCountLabel(value: map.nodes.filter { $0.kind == "circle" }.count, title: "Circles").frame(maxWidth: .infinity, alignment: .leading)
            PeopleMapCountLabel(value: store.circleMap.summary?.connections ?? store.circleMap.edges.filter { $0.state != "reverted" }.count, title: "Links").frame(maxWidth: .infinity, alignment: .leading)
            PeopleMapCountLabel(value: NetworkMapMetrics.aiThisWeek(store.circleMap, now: store.presentationNow), title: "AI-found this week").frame(maxWidth: .infinity, alignment: .leading)
          }.padding(16)
        }.background(.primary.opacity(0.025), in: RoundedRectangle(cornerRadius: 14))
        if let id = store.mapSelection.person { MapPersonInspector(store: store, id: id) }
      }
      if store.mapSelection.circle != nil {
        Button("Back to the map", systemImage: "arrow.left") { store.mapSelection = MapSelection() }.limmrButton().font(.caption)
      }
      if let focusError { Text(focusError).font(.caption).foregroundStyle(AccessibleSecondary()) }
    }
    .task(id: (store.mapSelection.circle ?? "") + (store.circleMap.cursor ?? "")) {
      focusedMap = nil; focusError = nil
      guard let id = store.mapSelection.circle else { return }
      do {
        var query = URLComponents(); query.queryItems = [.init(name: "focus", value: id)]
        let result: CircleMap = try Wire.decode(CircleMap.self, from: await MapLoadRetry.request(store.backend, path: "/v1/map?" + (query.percentEncodedQuery ?? "")))
        if !Task.isCancelled && store.mapSelection.circle == id { focusedMap = result }
      } catch { if !Task.isCancelled { focusError = "This circle couldn't load. Select it again to retry." } }
    }
    .onExitCommand { store.closePeopleInspector() }
    .onChange(of: store.escapeRequest) { _, _ in store.closePeopleInspector() }
    .onAppear {
      if !store.demo && !snapshot { PeopleMapStorage.removeRetiredSetting(in: RuntimePaths().home) }
    }
  }
}

private struct SnapshotMapFrameKey: EnvironmentKey { static let defaultValue: String? = nil }
extension EnvironmentValues {
  var snapshotMapFrame: String? {
    get { self[SnapshotMapFrameKey.self] }
    set { self[SnapshotMapFrameKey.self] = newValue }
  }
}

/// Replay advances the attributed events without moving the seeded field or running when idle.
private struct PeopleNetworkReplay: View {
  @Bindable var store: AppStore
  let map: CircleMap
  var onSelect: (CircleMapNode?) -> Void
  @State private var start: Date?
  @Environment(\.accessibilityReduceMotion) private var systemReduceMotion
  @Environment(\.snapshotReducedMotion) private var snapshotReduceMotion
  @Environment(\.snapshotRendering) private var snapshot
  @Environment(\.scenePhase) private var phase
  private var reduceMotion: Bool { systemReduceMotion || snapshotReduceMotion }
  private var events: [MapEvent] { map.events.sorted { $0.at < $1.at } }
  var body: some View {
    TimelineView(.animation(minimumInterval: 1 / 30, paused: start == nil || reduceMotion || snapshot || phase != .active)) { timeline in
      let elapsed = start.map { timeline.date.timeIntervalSince($0) } ?? 0
      let event = start == nil ? nil : event(at: min(1, elapsed / 8))
      NetworkMapView(store: store, map: map, selected: store.mapSelection.person, focusesSelection: false, replayEvent: event, onSelect: onSelect)
        .overlay(alignment: .topTrailing) {
          Button(start == nil ? "Replay" : "Replaying", systemImage: "arrow.counterclockwise") { start = Date() }
            .limmrButton().font(.caption).padding(10).background(.regularMaterial, in: Capsule()).padding(12)
            .disabled(reduceMotion || events.isEmpty || start != nil)
            .accessibilityValue(reduceMotion ? "Unavailable with Reduce Motion" : "Last 14 days")
        }
        .overlay(alignment: .topLeading) {
          if let event {
            Text(event.capsule(nodes: map.nodes)).font(.caption).lineLimit(2)
              .padding(10).frame(maxWidth: 320, alignment: .leading)
              .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 10)).padding(12)
              .allowsHitTesting(false)
          }
        }
        .onChange(of: elapsed >= 8) { _, done in if done { start = nil } }
    }
    .onChange(of: reduceMotion) { _, reduced in if reduced { start = nil } }
    .onChange(of: phase) { _, phase in if phase != .active { start = nil } }
  }
  private func event(at progress: Double) -> MapEvent? {
    guard let first = events.first.flatMap({ Timestamp.date($0.at) }),
      let last = events.last.flatMap({ Timestamp.date($0.at) }) else { return nil }
    let at = first.addingTimeInterval(last.timeIntervalSince(first) * progress)
    return events.last { (Timestamp.date($0.at) ?? .distantFuture) <= at }
  }
}

/// Preserve the original footer's fallback when older backends omit ai_this_week.
enum NetworkMapMetrics {
  static func aiThisWeek(_ map: CircleMap, now: Date) -> Int {
    if let count = map.summary?.aiThisWeek { return count }
    return map.edges.filter { edge in
      guard edge.state != "reverted", let origin = edge.origin, origin.byKind == "agent",
        let at = Timestamp.date(origin.at) else { return false }
      return at <= now && at >= now.addingTimeInterval(-7 * 86400)
    }.count
  }
}
