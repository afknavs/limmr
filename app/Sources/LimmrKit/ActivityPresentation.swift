import Foundation

public struct ReadSession: Identifiable, Sendable, Equatable {
  public var id: String { "read-\(agentId)-\(activityIds.first ?? 0)" }
  public var agentId: String
  public var start: String
  public var end: String
  public var chatJids: [String]
  public var circleIds: [String]
  public var count: Int
  public var activityIds: [Int64]
  public func text(agent: String, chatName: (String) -> String, circleName: (String) -> String)
    -> String
  {
    let names = Array(Set(chatJids.map(chatName) + circleIds.map(circleName))).sorted()
    let scope = names.isEmpty ? "messages" : names.joined(separator: " and ")
    return "\(agent) caught up on \(scope) · \(count) message\(count == 1 ? "" : "s")"
  }
}

public enum ActivityPresentation {
  public static func isRead(_ row: Activity) -> Bool {
    ["read", "search", "people_read"].contains(row.kind)
  }
  public static func isMessageRead(_ row: Activity) -> Bool {
    ["read", "search"].contains(row.kind)
      && (["list_messages", "search_messages", "get_message_context"].contains(row.tool)
        || (["read", "find"].contains(row.tool) && !row.messageIds.isEmpty))
  }
  public static func isSend(_ row: Activity) -> Bool {
    [
      "sent", "send_requested", "draft", "drafted", "suggestion", "suggested", "send_parked",
      "failed", "denied", "expired", "approved",
    ].contains(row.kind)
      && (row.tool.contains("send") || row.tool.contains("draft") || row.tool.contains("approv")
        || row.tool == "deny" || row.tool == "suggest")
  }
  public static func visible(_ rows: [Activity], filter: String = "all") -> [Activity] {
    rows.filter { row in
      guard !isRead(row) else { return false }  // Read sessions have their own presentation.
      return filter == "all" || (filter == "sends" && isSend(row))
    }.sorted {
      let left = $0.kind == "sent" ? 0 : isSend($0) ? 1 : 2
      let right = $1.kind == "sent" ? 0 : isSend($1) ? 1 : 2
      if left != right { return left < right }
      return Activity.newestFirst($0, $1)
    }
  }
  public static func sessions(_ rows: [Activity]) -> [ReadSession] {
    let reads = rows.filter { isMessageRead($0) && $0.count > 0 && Timestamp.date($0.at) != nil }
      .sorted { Activity.newestFirst($1, $0) }
    var result: [ReadSession] = []
    var last: [String: Int] = [:]
    for row in reads {
      if let i = last[row.agentId], let at = Timestamp.date(row.at),
        let end = Timestamp.date(result[i].end),
        at.timeIntervalSince(end) <= 1800,
        floor(at.timeIntervalSince1970 / 86400) == floor(end.timeIntervalSince1970 / 86400)
      {
        result[i].end = row.at
        result[i].count += row.count
        result[i].chatJids = Array(Set(result[i].chatJids + row.chatJids)).sorted()
        result[i].circleIds = Array(Set(result[i].circleIds + [row.circleId].compactMap { $0 }))
          .sorted()
        result[i].activityIds.append(row.id)
      } else {
        last[row.agentId] = result.count
        result.append(
          ReadSession(
            agentId: row.agentId, start: row.at, end: row.at,
            chatJids: Array(Set(row.chatJids)).sorted(),
            circleIds: [row.circleId].compactMap { $0 },
            count: row.count, activityIds: [row.id]))
      }
    }
    return result.sorted {
      let left = Timestamp.date($0.end) ?? .distantPast
      let right = Timestamp.date($1.end) ?? .distantPast
      return left == right ? $0.id < $1.id : left > right
    }
  }
  public static func sendStatus(_ row: Activity) -> String {
    (row.status
      ?? (row.kind == "sent" ? "sent" : row.kind == "send_requested" ? "waiting" : row.kind))
      .replacingOccurrences(of: "_", with: " ").capitalized
  }
}

public struct ActivityDigest: Codable, Identifiable, Sendable, Equatable {
  public var id: String
  public var agentId: String
  public var start: String
  public var end: String
  public var sessions: Int
  public var messages: Int
  public var text: String
}
public struct ActivityDigests: Codable, Sendable {
  public var digests: [ActivityDigest]
  public init(digests: [ActivityDigest] = []) { self.digests = digests }
}
public struct UsagePeriod: Codable, Sendable {
  public var messages: Int
  public var sends: Int
  public var readSessions: Int
  public init(
    messages: Int = 0, sends: Int = 0,
    readSessions: Int = 0
  ) {
    self.messages = messages
    self.sends = sends
    self.readSessions = readSessions
  }
}
public struct AgentUsage: Codable, Sendable, Identifiable {
  public var todaySends: Int? = nil
  public var weekSends: Int? = nil
  public var id: String
  public var today: Int
  public var week: Int
}
public struct DashboardStats: Codable, Sendable {
  public var today: UsagePeriod
  public var week: UsagePeriod
  public var agents: [AgentUsage]
  public var recentSends: [Activity]
  public var lastSyncAt: String?
  public var trackingSince: String?
  public init(
    today: UsagePeriod = .init(), week: UsagePeriod = .init(), agents: [AgentUsage] = [],
    recentSends: [Activity] = [], lastSyncAt: String? = nil, trackingSince: String? = nil
  ) {
    self.today = today
    self.week = week
    self.agents = agents
    self.recentSends = recentSends
    self.lastSyncAt = lastSyncAt
    self.trackingSince = trackingSince
  }
}
