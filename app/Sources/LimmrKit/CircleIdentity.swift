import Foundation

public enum CircleHue: String, CaseIterable, Codable, Sendable {
  case blue, orange, purple, green, yellow, teal, pink, indigo
  public static func stable(id: String, icon: EntityIcon?) -> Self {
    if let chosen = icon?.hue.flatMap(Self.init(rawValue:)) { return chosen }
    // Swift's Hasher is randomized between launches; FNV-1a is stable on every device.
    let hash = id.utf8.reduce(UInt64(14695981039346656037)) { ($0 ^ UInt64($1)) &* 1099511628211 }
    return allCases[Int(hash % UInt64(allCases.count))]
  }
}

public struct HomeRecentLine: Codable, Sendable, Equatable, Identifiable {
  public var text: String
  public var targetId: String
  public var kind: String
  public var at: String?
  public var by: String?
  public var id: String { kind + ":" + targetId }
  public init(text: String, targetId: String, kind: String = "task", at: String? = nil, by: String? = nil) {
    self.text = text; self.targetId = targetId; self.kind = kind; self.at = at; self.by = by
  }
}

public enum AIIdentity {
  public static func isPresent(_ agent: Agent, now: Date) -> Bool {
    agent.enabled && agent.lastSeenAt.flatMap(Timestamp.date).map { now.timeIntervalSince($0) >= 0 && now.timeIntervalSince($0) <= 15 * 60 } == true
  }
  public static func isLinked(_ agent: Agent, sync: DotSyncStatus?) -> Bool {
    guard let sync, sync.paired else { return false }
    return agent.runsOnPeer == sync.peer.engineId || agent.id == sync.peer.agentId
  }
  public static func name(_ agent: Agent, sync: DotSyncStatus?) -> String {
    guard isLinked(agent, sync: sync), let sync else { return agent.name }
    return agent.id == sync.peer.agentId ? (sync.peer.name.isEmpty ? agent.name : sync.peer.name) : "Dot · " + agent.name
  }
  public static func symbol(_ agent: Agent, sync: DotSyncStatus?) -> String {
    isLinked(agent, sync: sync) ? "desktopcomputer" : agent.symbol
  }
  public static func syncState(_ agent: Agent, sync: DotSyncStatus?) -> String? {
    guard isLinked(agent, sync: sync), let sync else { return nil }
    if sync.paused { return "Sync paused" }
    if !sync.peer.lastError.isEmpty { return "Sync needs attention" }
    guard let at = Timestamp.date(sync.peer.lastSyncAt) else { return "Not synced yet" }
    let date = DateFormatter(); date.locale = Locale(identifier: "en_GB"); date.timeZone = .current
    date.dateFormat = "d MMM"
    return "Last synced " + date.string(from: at)
  }
  /// A transport credential is hidden only when an owner linked a separate AI to its peer.
  public static func rows(_ agents: [Agent], sync: DotSyncStatus?) -> [Agent] {
    let agents = agents.filter { agent in
      guard !agent.isOnDevice else { return false }
      // Old unpairing clears the peer ID but retains its transport credential.
      let retiredDot = sync?.paired == false && agent.transport == "remote"
        && agent.kind == "custom" && agent.name == "Your dot"
        && agent.permissions?.sync == false
      return !retiredDot
    }
    guard let sync, sync.paired, agents.contains(where: { $0.runsOnPeer == sync.peer.engineId && $0.id != sync.peer.agentId }) else { return agents }
    return agents.filter { $0.id != sync.peer.agentId }
  }
}

/// Stored owner symbols win; old generic/empty values receive a contextual default.
public extension Circle {
  var displaySymbol: String {
    if let icon, icon.kind == "symbol", !icon.value.isEmpty { return icon.value }
    if !symbol.isEmpty && symbol != "circle.grid.2x2" { return symbol }
    if id == "you" || kind == "self" || name.lowercased() == "you" { return "person.crop.circle" }
    let text = (kind + " " + name).lowercased()
    let choices: [(String, String)] = [
      ("family", "house"), ("friend close", "heart"),
      ("university college school class", "graduationcap"),
      ("work collective company team", "briefcase"), ("fundraiser charity", "gift"),
      ("project build studio", "hammer"), ("trip travel", "airplane"),
      ("flat home house", "key")]
    return choices.first { words, _ in words.split(separator: " ").contains { text.contains($0) } }?.1 ?? "circle.grid.2x2"
  }
}
