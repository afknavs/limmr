import Foundation

/// Shared by /v1/waiting and the additive `waiting` field on /v1/home.
public struct WaitingItem: Codable, Identifiable, Sendable, Equatable {
  public var id: String
  public var agent: String
  public var kind: String
  public var sentence: String
  public var `where`: String
  public var since: String
  public var newHits: Int
  public var latest: WatchHit?
}

public struct WaitingResponse: Codable, Sendable {
  public var waiting: [WaitingItem]
}

public enum DemoWaiting {
  /// Each item has a real in-memory watch detail, including the always-on feed.
  public static let details: [WatchDetail] = DemoWatches.details + [
    WatchDetail(
      summary: WatchSummary(
        id: "w_feed", kind: "feed", title: "Daily updates",
        goal: "Keep me up to date with my circles", where: "All circles",
        action: "digest", status: "active", createdBy: "ChatGPT",
        createdAt: "2026-10-16T12:00:00Z", newHits: 0),
      digest: "", instructions: [], hits: [], stats: WatchStats(runs: 0, replies: 0))
  ]
}
