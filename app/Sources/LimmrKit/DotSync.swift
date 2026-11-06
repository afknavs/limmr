import Foundation

public struct DotSyncStatus: Codable, Sendable {
  public struct Peer: Codable, Sendable {
    public var agentId: String? = nil
    public var engineId: String
    public var name: String
    public var lastSyncAt: String
    public var lastError: String
    enum CodingKeys: String, CodingKey { case agentId, engineId, name, lastSyncAt, lastError }
    public init(agentId: String? = nil, engineId: String, name: String, lastSyncAt: String, lastError: String) {
      self.agentId = agentId; self.engineId = engineId; self.name = name; self.lastSyncAt = lastSyncAt; self.lastError = lastError
    }
    public init(from decoder: Decoder) throws {
      let c = try decoder.container(keyedBy: CodingKeys.self)
      agentId = try c.decodeIfPresent(String.self, forKey: .agentId)
      engineId = try c.decodeIfPresent(String.self, forKey: .engineId) ?? ""
      name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
      lastSyncAt = try c.decodeIfPresent(String.self, forKey: .lastSyncAt) ?? ""
      lastError = try c.decodeIfPresent(String.self, forKey: .lastError) ?? ""
    }
  }
  public struct Lease: Codable, Sendable {
    public var holder: String
    public var thisMac: Bool
  }
  public struct Review: Codable, Sendable, Identifiable {
    public var id: String
    public var name: String
  }
  public var connectionTitle: String { !paired ? "Not paired" : paused ? "Paused" : peer.lastError.isEmpty ? "Connected" : "Needs attention" }
  public var connectionAction: String { paired ? "Manage" : "Pair…" }
  public var sendingSummary: String? = nil
  public var paired: Bool
  public var paused: Bool
  public var peer: Peer
  public var lease: Lease
  public var itemsIn: Int
  public var itemsOut: Int
  public var mergeReviews: [Review]
  public static func demo(_ state: String) -> Self {
    .init(paired: state != "never", paused: false,
      peer: .init(engineId: "demo-dot", name: "Your dot", lastSyncAt: state == "error" ? "2026-10-15T12:00:00Z" : DemoData.now,
        lastError: state == "error" ? "Your dot hasn't synced for 2 days" : ""),
      lease: .init(holder: "demo-mac", thisMac: true), itemsIn: 12, itemsOut: 8,
      mergeReviews: state == "review" ? [.init(id: "demo-review", name: "Maya's exam prep")] : [])
  }
}
