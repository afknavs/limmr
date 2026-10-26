import Foundation

public struct WatchHit: Codable, Sendable, Equatable {
  public var at: String
  public var chat: String
  public var sender: String
  public var text: String
  public var messageId: String?
}
public struct WatchRunner: Codable, Sendable, Equatable {
  public var kind: String
  public var label: String
}
public struct WatchSummary: Codable, Identifiable, Sendable, Equatable {
  public var id: String
  public var kind: String
  public var title: String
  public var goal: String
  public var `where`: String
  public var action: String
  public var status: String
  public var createdBy: String
  public var createdAt: String
  public var expiresAt: String?
  public var newHits: Int
  public var latest: WatchHit?
  public var runner: WatchRunner?
  // Optional scheduling hints. Older /v1/home responses omit these; expiry is not a trigger.
  public var nextFireAt: String? = nil
  public var lastCheckedAt: String? = nil
  public var isOpen: Bool { status == "active" || status == "paused" }
}
public struct WatchInstruction: Codable, Sendable, Equatable {
  public var at: String
  public var text: String
}
public struct WatchStats: Codable, Sendable, Equatable {
  public var runs: Int
  public var replies: Int
  public var lastRunAt: String?
}
/// The detail endpoint extends the summary at the top level, without a wrapper.
public struct WatchDetail: Codable, Sendable, Equatable {
  public var summary: WatchSummary
  public var digest: String
  public var instructions: [WatchInstruction]
  public var hits: [WatchHit]
  public var stats: WatchStats
  private enum CodingKeys: String, CodingKey { case digest, instructions, hits, stats }
  public init(from decoder: any Decoder) throws {
    summary = try WatchSummary(from: decoder)
    let c = try decoder.container(keyedBy: CodingKeys.self)
    digest = try c.decodeIfPresent(String.self, forKey: .digest) ?? ""
    instructions = try c.decodeIfPresent([WatchInstruction].self, forKey: .instructions) ?? []
    hits = try c.decodeIfPresent([WatchHit].self, forKey: .hits) ?? []
    stats = try c.decode(WatchStats.self, forKey: .stats)
  }
  public func encode(to encoder: any Encoder) throws {
    try summary.encode(to: encoder)
    var c = encoder.container(keyedBy: CodingKeys.self)
    try c.encode(digest, forKey: .digest)
    try c.encode(instructions, forKey: .instructions)
    try c.encode(hits, forKey: .hits)
    try c.encode(stats, forKey: .stats)
  }
  init(
    summary: WatchSummary, digest: String, instructions: [WatchInstruction], hits: [WatchHit],
    stats: WatchStats
  ) {
    self.summary = summary
    self.digest = digest
    self.instructions = instructions
    self.hits = hits
    self.stats = stats
  }
}
public struct WatchesResponse: Codable, Sendable { public var watches: [WatchSummary] }

public enum DemoWatches {
  public static let details: [WatchDetail] = [
    make(
      "w_exam", "Exam news", goal: "Tell me when anyone posts about the exam",
      where: "University", action: "notify", text: "IA date moved to 24 Oct",
      chat: "University class", sender: "Dr. Morgan", newHits: 2),
    make(
      "w_atlas", "Atlas updates", goal: "Keep me updated about Atlas from Maya and Robin",
      where: "Atlas renovation", action: "digest", text: "The darker tile samples are ready",
      chat: "Atlas planning", sender: "Maya Rao", newHits: 1,
      digest:
        "Robin approved the layout. Maya has two darker tile samples ready for Wednesday’s visit."),
    make(
      "w_website", "Website changes",
      goal: "Talk to Robin about the website changes until she is happy. Stop after 3 days.",
      where: "Robin Bell", action: "wake_session", text: "Please give the gallery more space",
      chat: "Robin Bell", sender: "Robin Bell", newHits: 0,
      runner: WatchRunner(kind: "codex", label: "Codex · website")),
  ]
  private static func make(
    _ id: String, _ title: String, goal: String, where scope: String,
    action: String, text: String, chat: String, sender: String, newHits: Int,
    digest: String = "", runner: WatchRunner? = nil
  ) -> WatchDetail {
    let hit = WatchHit(
      at: DemoData.now, chat: chat, sender: sender, text: text, messageId: "demo-" + id)
    return WatchDetail(
      summary: WatchSummary(
        id: id, kind: runner == nil ? "watch" : "handoff",
        title: title, goal: goal, where: scope, action: action, status: "active",
        createdBy: "Codex",
        createdAt: "2026-10-17T09:10:00Z", expiresAt: runner == nil ? nil : "2026-10-20T09:10:00Z",
        newHits: newHits, latest: hit, runner: runner), digest: digest, instructions: [],
      hits: [hit],
      stats: WatchStats(
        runs: runner == nil ? 0 : 3, replies: runner == nil ? 0 : 2,
        lastRunAt: runner == nil ? nil : DemoData.now))
  }
}
