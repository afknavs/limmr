import Foundation

public struct DotSyncStatus: Codable, Sendable {
  public struct Peer: Codable, Sendable {
    public var engineId: String
    public var name: String
    public var lastSyncAt: String
    public var lastError: String
  }
  public struct Lease: Codable, Sendable {
    public var holder: String
    public var thisMac: Bool
  }
  public struct Review: Codable, Sendable, Identifiable {
    public var id: String
    public var name: String
  }
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
