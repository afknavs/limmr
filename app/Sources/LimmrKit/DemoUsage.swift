import Foundation

extension DemoData {
  static func history() -> StatsHistory {
    let end = Timestamp.date(String(now.prefix(10)) + "T00:00:00Z")!
    let formatter = ISO8601DateFormatter()
    return StatsHistory(
      // Y2: a full invented year keeps every responsive calendar width populated.
      days: (0..<364).map { i in
        let date = String(
          formatter.string(from: end.addingTimeInterval(Double(i - 363) * 86400)).prefix(10))
        let incoming = i % 11 == 0 ? 0 : (i * 17 + 9) % 95
        let reads = i % 4 == 0 ? 0 : i % 9 + 1
        let sends = i % 5
        var agents: [String: HistoryAgent] = [:]
        for (index, agent) in Self.agents.enumerated() {
          agents[agent.id] = HistoryAgent(
            reads: index == 0 ? reads : 0, sends: index == 1 ? sends : 0)
        }
        return HistoryDay(
          date: date, messagesIn: incoming, messagesOut: i % 13, agentSends: sends,
          agentReads: reads, agents: agents)
      })
  }

  static func stats(activity: [Activity]) -> DashboardStats {
    let now = Timestamp.date(Self.now)!
    let today = String(Self.now.prefix(10))
    let weekStart = "2026-10-12"
    func period(_ start: String) -> UsagePeriod {
      let rows = activity.filter {
        $0.at >= start && (Timestamp.date($0.at) ?? .distantFuture) <= now
      }
      let sends = rows.filter { $0.kind == "sent" }
      return UsagePeriod(
        messages: messages.filter { $0.timestamp >= start && $0.timestamp <= Self.now }.count,
        sends: sends.count, readSessions: ActivityPresentation.sessions(rows).count)
    }
    let sessions = ActivityPresentation.sessions(activity)
    return DashboardStats(
      today: period(today), week: period(weekStart),
      agents: agents.map { a in
        AgentUsage(
          todaySends: activity.filter { $0.agentId == a.id && $0.kind == "sent" && $0.at >= today }
            .count,
          weekSends: activity.filter {
            $0.agentId == a.id && $0.kind == "sent" && $0.at >= weekStart
          }.count, id: a.id,
          today: sessions.filter { $0.agentId == a.id && $0.start >= today }.count,
          week: sessions.filter { $0.agentId == a.id && $0.start >= weekStart }.count)
      },
      recentSends: activity.filter { $0.kind == "sent" }.sorted(by: Activity.newestFirst),
      lastSyncAt: Self.now, trackingSince: "2026-10-12T00:00:00Z")
  }
  static func digests(activity: [Activity]) -> ActivityDigests {
    let reads = activity.filter { ActivityPresentation.isMessageRead($0) && $0.count > 0 }
    let buckets = Dictionary(grouping: reads) { row in
      let at = Timestamp.date(row.at)!.timeIntervalSince1970
      return row.agentId + "/" + String(Int(floor(at / 14400)))
    }
    let formatter = ISO8601DateFormatter()
    return ActivityDigests(
      digests: buckets.map { key, rows in
        let sessions = ActivityPresentation.sessions(rows)
        let at = Timestamp.date(rows[0].at)!.timeIntervalSince1970
        let start = Date(timeIntervalSince1970: floor(at / 14400) * 14400)
        let count = sessions.reduce(0) { $0 + $1.count }
        let names = Array(
          Set(
            rows.flatMap(\.chatJids).map { jid in chats.first { $0.jid == jid }?.name ?? "a chat" })
        ).sorted()
        let agent = agents.first { $0.id == rows[0].agentId }?.name ?? "Integration"
        return ActivityDigest(
          id: key, agentId: rows[0].agentId, start: formatter.string(from: start),
          end: formatter.string(from: start.addingTimeInterval(14400)), sessions: sessions.count,
          messages: count,
          text:
            "\(agent) caught up on \(names.isEmpty ? "messages" : names.joined(separator: " and ")) · \(count) \(count == 1 ? "message" : "messages") across \(sessions.count) \(sessions.count == 1 ? "read session" : "read sessions")."
        )
      }.sorted { $0.start == $1.start ? $0.id < $1.id : $0.start > $1.start })
  }
}
