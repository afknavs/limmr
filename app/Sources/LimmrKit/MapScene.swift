/// Stable seed shared by map placement and tinting.
public enum MapSceneMotion {
  public static func stableHash(_ value: String) -> UInt64 {
    value.utf8.reduce(UInt64(14_695_981_039_346_656_037)) { ($0 ^ UInt64($1)) &* 1_099_511_628_211 }
  }
}

import Foundation
import CoreGraphics

/// Camera math is shared by drawing, viewport culling and pointer hit testing.
public struct NetworkMapCamera: Sendable {
  public var viewport: CGSize
  public var world: CGSize
  public var zoom: Double
  public var pan: CGSize
  public var focus: CGPoint?
  public init(viewport: CGSize, world: CGSize, zoom: Double = 1, pan: CGSize = .zero, focus: CGPoint? = nil) {
    self.viewport = viewport; self.world = world; self.zoom = zoom; self.pan = pan; self.focus = focus
  }
  public var scale: Double { min(viewport.width / max(1, world.width), viewport.height / max(1, world.height)) * zoom }
  public var showsAvatars: Bool { scale >= 0.38 }
  public func screen(_ point: CGPoint) -> CGPoint {
    let center = focus ?? CGPoint(x: world.width / 2, y: world.height / 2)
    return CGPoint(x: viewport.width / 2 + (point.x - center.x) * scale + pan.width,
                   y: viewport.height / 2 + (point.y - center.y) * scale + pan.height)
  }
  public func isVisible(_ point: CGPoint, radius: Double = 20) -> Bool {
    CGRect(origin: .zero, size: viewport).insetBy(dx: -radius, dy: -radius).contains(screen(point))
  }
}

public struct NetworkMapProjection: Sendable {
  public var preparationMilliseconds = 0.0
  public var map = CircleMap.empty
  public var positions: [String: CGPoint] = [:]
  public var world = CGSize(width: 1600, height: 1000)
  public var nodes: [CircleMapNode] = []
  public var nodesByID: [String: CircleMapNode] = [:]
  public var edges: [CircleMapEdge] = []
  private var adjacent: [String: Set<String>] = [:]
  public var revision: String { nodes.map(\.id).joined(separator: ":") + String(edges.count) }
  public var peopleCount: Int { nodes.filter { $0.kind == "person" }.count }
  public var circleCount: Int { nodes.filter { $0.kind == "circle" }.count }
  public init() {}
  public init(map: CircleMap, positions: [String: CGPoint]) {
    self.map = map; self.positions = positions
    // Dense layouts grow their world to retain every avatar and the hub clear zones.
    world = CGSize(width: max(1600, (positions.values.map(\.x).max() ?? 0) + 32),
                   height: max(1000, (positions.values.map(\.y).max() ?? 0) + 40))
    nodes = map.nodes.sorted { $0.id < $1.id }
    nodesByID = Dictionary(uniqueKeysWithValues: nodes.map { ($0.id, $0) })
    edges = map.edges.filter { $0.state != "reverted" }.sorted { $0.key < $1.key }
    for edge in edges {
      adjacent[edge.source, default: []].insert(edge.target)
      adjacent[edge.target, default: []].insert(edge.source)
    }
    // Membership metadata still highlights circles even when the backend bounded its edge payload.
    for node in nodes where node.kind == "person" {
      for id in Set((node.circles ?? []) + [node.circleId, node.primaryCircleId].compactMap { $0 }) where nodesByID[id]?.kind == "circle" {
        adjacent[node.id, default: []].insert(id)
        adjacent[id, default: []].insert(node.id)
      }
    }
  }
  public func neighbours(of id: String?) -> Set<String> {
    guard let id else { return [] }; return (adjacent[id] ?? []).union([id])
  }
  public func visible(camera: NetworkMapCamera) -> [CircleMapNode] {
    nodes.filter { node in positions[node.id].map { camera.isVisible($0) } ?? false }
  }
  /// Fit the selected person's actual direct neighbourhood before applying user zoom.
  /// A broad network must not leave highlighted circles offscreen just to force a zoom factor.
  public func focusedCamera(viewport: CGSize, selected: String, zoom: Double = 1, pan: CGSize = .zero,
                            padding: CGSize = CGSize(width: 48, height: 56)) -> NetworkMapCamera {
    let points = neighbours(of: selected).compactMap { positions[$0] }
    guard let point = positions[selected], !points.isEmpty else {
      return NetworkMapCamera(viewport: viewport, world: world, zoom: zoom, pan: pan)
    }
    let bounds = points.reduce(CGRect(origin: point, size: CGSize(width: 1, height: 1))) {
      $0.union(CGRect(origin: $1, size: CGSize(width: 1, height: 1)))
    }
    let fit = min(viewport.width / world.width, viewport.height / world.height)
    // Padding is in screen points: world-space padding shrinks at mini-map scales.
    let scale = min(1.8, max(1, viewport.width - padding.width * 2) / max(1, bounds.width), max(1, viewport.height - padding.height * 2) / max(1, bounds.height))
    return NetworkMapCamera(viewport: viewport, world: world, zoom: scale / max(0.001, fit) * zoom, pan: pan,
      focus: CGPoint(x: bounds.midX, y: bounds.midY))
  }
  public func hit(_ point: CGPoint, camera: NetworkMapCamera) -> String? {
    if let label = NetworkCircleLabels.frames(scene: self, camera: camera).sorted(by: { $0.key < $1.key }).first(where: { $0.value.contains(point) }) { return label.key }
    return visible(camera: camera).compactMap { node -> (String, Double)? in
      guard let p = positions[node.id].map(camera.screen) else { return nil }
      let d = hypot(p.x - point.x, p.y - point.y)
      if node.kind == "circle" { return nil }
      return d <= (camera.showsAvatars ? 12 : 9) ? (node.id, d) : nil
    }.min { $0.1 < $1.1 }?.0
  }
}

extension CircleMapNode {
  public init(id: String, kind: String, title: String) {
    self.init(id: id, kind: kind, title: title, circleId: nil)
  }
}
extension CircleMapEdge {
  public init(source: String, target: String, kind: String, state: String) {
    self.init(source: source, target: target, kind: kind, id: "", state: state)
  }
}

/// Circle names remain legible at compact window sizes. This bounded screen-space
/// label pass shares its rectangles with hit testing; it never changes node layout.
public enum NetworkCircleLabels {
  public static func frames(scene: NetworkMapProjection, camera: NetworkMapCamera) -> [String: CGRect] {
    let bounds = CGRect(origin: .zero, size: camera.viewport).insetBy(dx: 8, dy: 8)
    var result: [String: CGRect] = [:]
    for node in scene.visible(camera: camera) where node.kind == "circle" {
      guard let world = scene.positions[node.id] else { continue }
      let anchor = camera.screen(world)
      let width = min(132, max(36, Double(node.title.count) * 6 + 12))
      for step in 0..<257 {
        let ring = Double((step + 7) / 8) * 12
        let angle = Double(step % 8) * .pi / 4
        let dx = step == 0 ? 0 : cos(angle) * ring
        let dy = step == 0 ? 0 : sin(angle) * ring
        let box = CGRect(x: min(max(bounds.minX, anchor.x + dx - width / 2), max(bounds.minX, bounds.maxX-width)),
          y: min(max(bounds.minY, anchor.y + dy - 11), max(bounds.minY, bounds.maxY-22)), width: width, height: 22)
        if !result.values.contains(where: { $0.insetBy(dx: -3, dy: -3).intersects(box) }) {
          result[node.id] = box; break
        }
      }
    }
    return result
  }
}
