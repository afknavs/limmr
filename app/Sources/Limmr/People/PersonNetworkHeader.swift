import SwiftUI
import LimmrKit

/// The profile owns both endpoints so the same avatar has a reversible geometry flight.
struct PersonNetworkHeader: View {
  @Bindable var store: AppStore
  let id: String
  let namespace: Namespace.ID
  var open: () -> Void
  var body: some View {
    VStack(alignment: .leading, spacing: 10) {
      HStack {
        Label("In your network", systemImage: "point.3.connected.trianglepath.dotted").font(.system(size: 14, weight: .medium))
        Spacer()
        Button("Show in map", systemImage: "arrow.up.left.and.arrow.down.right", action: open).peopleQuietButton().font(.caption)
      }
      Button(action: open) {
        NetworkMapView(store: store, map: networkMap, selected: id, compact: true, namespace: namespace)
          .frame(height: 196).allowsHitTesting(false)
      }.peopleQuietButton().accessibilityLabel("Show \(store.personName(id)) in the network map")
    }
  }
  private var networkMap: CircleMap { PersonNetworkPresentation.neighbourhood(store.circleMap, id: id, name: store.personName(id), circles: PersonNetworkPresentation.circles(in: store)) }
}

enum PersonNetworkPresentation {
  /// A local radial arrangement separates direct neighbours from the selected
  /// photo at miniature scale; the full map keeps its cached clustered layout.
  static func miniScene(_ scene: NetworkMapProjection, selected: String) -> NetworkMapProjection {
    let center = CGPoint(x: 800, y: 500)
    var points: [String: CGPoint] = [selected: center]
    let neighbours = scene.nodes.filter { $0.id != selected }.sorted { $0.id < $1.id }
    for (index, node) in neighbours.enumerated() {
      let angle = -.pi / 2 + 2 * .pi * Double(index) / Double(max(1, neighbours.count))
      points[node.id] = CGPoint(x: center.x + cos(angle) * 460, y: center.y + sin(angle) * 320)
    }
    return NetworkMapProjection(map: scene.map, positions: points)
  }
  @MainActor static func circles(in store: AppStore) -> [LimmrKit.Circle] {
    store.circles.map { value in
      var circle = value
      circle.people = store.circleDetails[circle.id]?.people ?? circle.people
      return circle
    }
  }
  static func shouldShow(_ map: CircleMap, person: Person?, id: String, circles: [LimmrKit.Circle] = []) -> Bool {
    guard id != "me", id.lowercased() != "you", person?.isOwner != true, person?.name != "You" else { return false }
    return neighbourhood(map, id: id, name: person?.name ?? id, circles: circles).nodes.count > 1
  }
  /// The miniature contains only the person and direct neighbours. No unrelated
  /// network is laid out or drawn behind it at a tiny scale.
  static func neighbourhood(_ original: CircleMap, id: String, name: String, circles: [LimmrKit.Circle] = []) -> CircleMap {
    var result = map(original, id: id, name: name, circles: circles)
    var ids: Set<String> = [id]
    for edge in result.edges where edge.state != "reverted" && (edge.source == id || edge.target == id) {
      ids.insert(edge.source); ids.insert(edge.target)
    }
    if let person = result.nodes.first(where: { $0.id == id }) {
      ids.formUnion((person.circles ?? []) + [person.circleId, person.primaryCircleId].compactMap { $0 })
    }
    for circle in circles where circle.confirmed && circle.people.contains(where: { $0.personId == id }) {
      ids.insert(circle.id)
      if !result.nodes.contains(where: { $0.id == circle.id }) { result.nodes.append(.init(id: circle.id, kind: "circle", title: circle.name)) }
      if !result.edges.contains(where: { $0.state != "reverted" && (($0.source == id && $0.target == circle.id) || ($0.source == circle.id && $0.target == id)) }) {
        result.edges.append(.init(source: circle.id, target: id, kind: "membership", state: "confirmed"))
      }
    }
    result.nodes = result.nodes.filter { ids.contains($0.id) }
    let present = Set(result.nodes.map(\.id))
    result.edges = result.edges.filter { $0.state != "reverted" && present.contains($0.source) && present.contains($0.target) }
    result.events = []; result.pulses = []; result.summary = nil
    return result
  }
  /// Profiles outside the bounded backend map retain their confirmed circle placements.
  /// Without membership evidence they stay isolated; no inferred link is fabricated.
  static func map(_ original: CircleMap, id: String, name: String, circles: [LimmrKit.Circle] = []) -> CircleMap {
    var map = original
    if !map.nodes.contains(where: { $0.id == id }) {
      let memberships = circles.filter { $0.confirmed && $0.people.contains { $0.personId == id } }.sorted { $0.id < $1.id }
      var person = CircleMapNode(id: id, kind: "person", title: name)
      person.circles = memberships.map(\.id); person.primaryCircleId = memberships.first?.id
      map.nodes.append(person)
      for circle in memberships {
        if !map.nodes.contains(where: { $0.id == circle.id }) {
          map.nodes.append(CircleMapNode(id: circle.id, kind: "circle", title: circle.name))
        }
        map.edges.append(CircleMapEdge(source: circle.id, target: id, kind: "membership", state: "confirmed"))
      }
    }
    return map
  }
}

private struct SnapshotNetworkExpandedKey: EnvironmentKey { static let defaultValue = false }
extension EnvironmentValues {
  var snapshotNetworkExpanded: Bool {
    get { self[SnapshotNetworkExpandedKey.self] }
    set { self[SnapshotNetworkExpandedKey.self] = newValue }
  }
}
