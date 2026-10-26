import Foundation

public struct CircleMapNode: Codable, Identifiable, Sendable, Equatable {
  public var id: String
  public var kind: String
  public var title: String
  public var circleId: String? = nil
  public var initials: String? = nil
  public var weight: Double? = nil
  public var lastActiveAt: String? = nil
  public var newMessages: Int? = nil
  public var circles: [String]? = nil
  public var groupsInCommon: [GroupInCommon]? = nil
  public var groupsInCommonCount: Int? = nil
  public var circleAffinity: [CircleAffinity]? = nil
  public var primaryCircleId: String? = nil
  public var howKnown: String? = nil
  public var radius: Double {
    kind == "circle" ? 12 : kind == "chat" ? 2 : 2.5 + 1.5 * min(1, max(0, weight ?? 0.3))
  }
}
public struct MapOrigin: Codable, Sendable, Equatable {
  public var by: String
  public var byKind: String
  public var at: String
  public var evidence: Int
  public var briefVersion: Int? = nil
  public var changeId: String? = nil
}
public struct CircleMapEdge: Codable, Identifiable, Sendable, Equatable {
  public var source: String
  public var target: String
  public var kind: String = "membership"
  public var id: String = ""
  public var label: String? = nil
  public var origin: MapOrigin? = nil
  public var strength: Double? = nil
  public var state: String? = nil
  public var key: String { id.isEmpty ? source + "→" + target + ":" + kind : id }
  public var style: MapEdgeStyle {
    MapEdgeStyle(
      tone: "neutral",
      width: origin?.byKind == "structure" || origin == nil ? 0.4 : 0.6,
      opacity: (origin?.byKind == "you" ? 0.38 : origin?.byKind == "agent" ? 0.26 : 0.16)
        + 0.08 * min(1, max(0, strength ?? 0.5)),
      dashed: state == "suggested")
  }

  public var hoverText: String {
    var parts = [label ?? kind.capitalized]
    if let origin {
      parts.append(origin.byKind == "structure" ? "seen in chats" : "linked by \(origin.by)")
      if let date = Timestamp.date(origin.at) {
        parts.append(date.formatted(.dateTime.day().month(.abbreviated)))
      }
      parts.append("from \(origin.evidence) message\(origin.evidence == 1 ? "" : "s")")
    } else {
      parts.append("seen in chats")
    }
    if state == "suggested" { parts.append("suggested") }
    return parts.joined(separator: " · ")
  }
}
public struct MapEdgeStyle: Equatable, Sendable {
  public var tone: String
  public var width: Double
  public var opacity: Double
  public var dashed: Bool
}
public struct CirclePulse: Codable, Sendable {
  public var id: String
  public var newMessages: Int
}
public struct MapEvent: Codable, Identifiable, Sendable, Equatable {
  public var id: String
  public var at: String
  public var kind: String
  public var by: String
  public var edgeId: String? = nil
  public var nodeIds: [String]
  public var text: String
  public func capsule(nodes: [CircleMapNode]) -> String {
    let names = nodeIds.compactMap { id in nodes.first { $0.id == id }?.title }
    guard kind == "edge_added" else { return "\(by) · \(text)" }
    return names.count >= 2 ? "\(by) · linked \(names[0]) → \(names[1])" : "\(by) · \(text)"
  }
}
public struct MapSummary: Codable, Sendable {
  public var aiThisWeek: Int? = nil
  public var connections: Int
  public var byAi: Int
  public var thisWeek: Int
  public var latest: String
  public var text: String {
    "\(connections) connections · \(byAi) found by your AIs · \(thisWeek) this week"
      + (latest.isEmpty ? "" : " · latest: \(latest)")
  }
}
public struct CircleMap: Codable, Sendable {
  public var cursor: String? = nil
  public var delta: Bool = false
  public var removedNodes: [String] = []
  public var removedEdges: [String] = []
  public var nodes: [CircleMapNode]
  public var edges: [CircleMapEdge]
  public var pulses: [CirclePulse]
  public var events: [MapEvent] = []
  public var summary: MapSummary? = nil
  public static let empty = CircleMap(nodes: [], edges: [], pulses: [])
  public init(
    nodes: [CircleMapNode], edges: [CircleMapEdge], pulses: [CirclePulse],
    events: [MapEvent] = [], summary: MapSummary? = nil
  ) {
    self.nodes = nodes
    self.edges = edges
    self.pulses = pulses
    self.events = events
    self.summary = summary
  }
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    cursor = try c.decodeIfPresent(String.self, forKey: .cursor)
    delta = try c.decodeIfPresent(Bool.self, forKey: .delta) ?? false
    removedNodes = try c.decodeIfPresent([String].self, forKey: .removedNodes) ?? []
    removedEdges = try c.decodeIfPresent([String].self, forKey: .removedEdges) ?? []
    nodes = try c.decodeIfPresent([CircleMapNode].self, forKey: .nodes) ?? []
    edges = try c.decodeIfPresent([CircleMapEdge].self, forKey: .edges) ?? []
    pulses = try c.decodeIfPresent([CirclePulse].self, forKey: .pulses) ?? []
    events = try c.decodeIfPresent([MapEvent].self, forKey: .events) ?? []
    summary = try c.decodeIfPresent(MapSummary.self, forKey: .summary)
  }
}
extension CircleMapEdge {
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    source = try c.decode(String.self, forKey: .source)
    target = try c.decode(String.self, forKey: .target)
    kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? "membership"
    id = try c.decodeIfPresent(String.self, forKey: .id) ?? ""
    label = try c.decodeIfPresent(String.self, forKey: .label)
    origin = try c.decodeIfPresent(MapOrigin.self, forKey: .origin)
    strength = try c.decodeIfPresent(Double.self, forKey: .strength)
    state = try c.decodeIfPresent(String.self, forKey: .state)
  }
}
public struct AgentPresence: Codable, Identifiable, Sendable, Equatable {
  public var id: String
  public var name: String
  public var state: String
  public var target: String
  public var targetIds: [String]
  public var since: String
  public var line: String? {
    let verb: String
    switch state {
    case "reading": verb = "reading"
    case "writing_brief": verb = "updating the brief for"
    case "drafting": verb = "drafting in"
    case "handoff": verb = "working on a handoff for"
    default: return nil
    }
    return "\(name) is \(verb) \(target)"
  }
}
public struct PresenceResponse: Codable, Sendable {
  public var agents: [AgentPresence]
  public init(agents: [AgentPresence]) { self.agents = agents }
}

/// First load establishes the baseline. Refresh and SSE may repeat IDs; animate each only once.
public struct MapEventDiff: Sendable {
  private var seen: Set<String> = []
  private var initialized = false
  public init() {}
  public mutating func ingest(_ events: [MapEvent]) -> [MapEvent] {
    let fresh = events.filter { seen.insert($0.id).inserted }
    defer { initialized = true }
    return initialized ? fresh.sorted { $0.at < $1.at } : []
  }
}

public enum AvatarTint {
  // Fixed light backgrounds and dark ink keep contrast in both appearances.
  public static let palette: [UInt32] = [0xDDE5EC, 0xE5E1EC, 0xDAE7E2, 0xEDE3D8, 0xE9DEE1]
  public static func rgb(id: String) -> UInt32 {
    palette[Int(MapSceneMotion.stableHash(id) % UInt64(palette.count))]
  }
}

extension CircleMap {
  public func applying(_ update: CircleMap) -> CircleMap {
    guard update.delta else { return update }
    var result = self
    let updatedNodes = Set(update.nodes.map(\.id)).union(update.removedNodes)
    result.nodes.removeAll { updatedNodes.contains($0.id) }
    result.nodes += update.nodes
    result.nodes.sort { $0.id < $1.id }
    let updatedEdges = Set(update.edges.map(\.key)).union(update.removedEdges)
    result.edges.removeAll { updatedEdges.contains($0.key) }
    result.edges += update.edges
    result.edges.sort { $0.key < $1.key }
    let ids = Set(result.nodes.map(\.id))
    result.edges.removeAll { !ids.contains($0.source) || !ids.contains($0.target) }
    result.pulses = update.pulses
    let events = Set(update.events.map(\.id))
    result.events.removeAll { events.contains($0.id) }
    result.events += update.events
    result.events.sort { $0.at > $1.at }
    result.events = Array(result.events.prefix(60))
    result.summary = update.summary
    result.cursor = update.cursor
    return result
  }
}
