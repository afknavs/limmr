import Foundation

public struct EntityIcon: Codable, Sendable, Equatable, Hashable {
  public var kind: String
  public var value: String
  public init(kind: String, value: String) {
    self.kind = kind
    self.value = value
  }
}
public struct HistoryAgent: Codable, Sendable, Equatable {
  public var reads: Int = 0
  public var sends: Int = 0
}
public struct HistoryDay: Codable, Identifiable, Sendable, Equatable {
  public var date: String
  public var messagesIn: Int
  public var messagesOut: Int
  public var agentSends: Int
  public var agentReads: Int
  public var agents: [String: HistoryAgent]? = nil
  public var id: String { date }
  public var messages: Int { messagesIn + messagesOut }
  public var agentActivity: Int { agentSends + agentReads }
  public init(
    date: String, messagesIn: Int = 0, messagesOut: Int = 0, agentSends: Int = 0,
    agentReads: Int = 0, agents: [String: HistoryAgent]? = nil
  ) {
    self.date = date
    self.messagesIn = messagesIn
    self.messagesOut = messagesOut
    self.agentSends = agentSends
    self.agentReads = agentReads
    self.agents = agents
  }
}
public struct StatsHistory: Codable, Sendable {
  public var days: [HistoryDay]
  public init(days: [HistoryDay] = []) { self.days = days }
  public func period(_ count: Int) -> [HistoryDay] { Array(days.suffix(max(0, count))) }
  /// Monday at the top, real calendar alignment, newest day at the end.
  public var heatmap: [HistoryDay?] {
    guard let first = days.first, let date = Timestamp.date(first.date + "T00:00:00Z") else {
      return []
    }
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = TimeZone(secondsFromGMT: 0)!
    let padding = (calendar.component(.weekday, from: date) + 5) % 7
    let cells: [HistoryDay?] = Array(repeating: nil, count: padding) + days.map { Optional($0) }
    return cells + Array(repeating: nil, count: (7 - cells.count % 7) % 7)
  }
  public static func intensity(_ count: Int, maximum: Int) -> Double {
    count <= 0 ? 0 : Double(min(4, max(1, Int(ceil(Double(count) * 4 / Double(max(1, maximum))))))) / 4
  }
}

public struct ActivityTask: Identifiable, Sendable {
  public var steps: [Activity]
  public var id: Int64 { steps.first!.id }
  public var agentId: String { steps.first!.agentId }
  public var end: String { steps.last!.at }
  public var chatJids: [String] { Array(Set(steps.flatMap(\.chatJids))).sorted() }
  public func title(agent: String, chatName: (String) -> String) -> String {
    if steps.count == 1, steps[0].tool == "circle_structure_placed" { return steps[0].verb }
    let sends = steps.filter { $0.kind == "sent" }
    let reads = steps.filter(ActivityPresentation.isMessageRead)
    let uploads = steps.filter {
      $0.tool == "upload_attachment" && $0.kind != "failed" && $0.kind != "denied"
    }
    let drafts = steps.filter { ActivityPresentation.isSend($0) && $0.kind != "sent" }
    var actions: [String] = []
    if !sends.isEmpty {
      actions.append("\(sends.count) \(sends.count == 1 ? "message sent" : "messages sent")")
    }
    if !uploads.isEmpty {
      actions.append("\(uploads.count) \(uploads.count == 1 ? "file uploaded" : "files uploaded")")
    }
    if !drafts.isEmpty {
      actions.append("\(drafts.count) draft \(drafts.count == 1 ? "step" : "steps")")
    }
    if !reads.isEmpty {
      actions.append(
        "\(reads.reduce(0) { $0 + $1.count }) message\(reads.reduce(0) { $0 + $1.count } == 1 ? "" : "s") read"
      )
    }
    if actions.isEmpty { actions.append(steps.last!.verb) }
    let scope = chatJids.map(chatName).joined(separator: ", ")
    return agent + (scope.isEmpty ? "" : " · " + scope) + ": " + actions.joined(separator: ", ")
  }
  public var status: String {
    if steps.contains(where: { $0.kind == "failed" }) { return "Needs attention" }
    if let sent = steps.last(where: { $0.kind == "sent" }) {
      return ActivityPresentation.sendStatus(sent)
    }
    return ActivityPresentation.sendStatus(steps.last!)
  }
}
extension ActivityPresentation {
  /// Sessions require a shared agent AND scope, with no more than 30 minutes idle.
  /// Unscoped actions join only a single unambiguous, adjacent session.
  public static func tasks(_ rows: [Activity]) -> [ActivityTask] {
    let unique = Dictionary(rows.map { ($0.id, $0) }, uniquingKeysWith: { _, last in last })
    // Keep low-level empty reads and directory scans in the detailed audit log only.
    let sorted = unique.values.filter { !isRead($0) || (isMessageRead($0) && $0.count > 0) }
      .sorted { Activity.newestFirst($1, $0) }
    var tasks: [ActivityTask] = []
    for row in sorted {
      guard let at = Timestamp.date(row.at) else { continue }
      let candidates = tasks.indices.filter { i in
        let task = tasks[i]
        guard task.agentId == row.agentId, let end = Timestamp.date(task.end),
          at.timeIntervalSince(end) <= 1800,
          floor(at.timeIntervalSince1970 / 86400) == floor(end.timeIntervalSince1970 / 86400)
        else { return false }
        return !Set(task.chatJids).isDisjoint(with: row.chatJids)
          || row.circleId != nil && task.steps.contains { $0.circleId == row.circleId }
      }
      let adjacent = tasks.indices.filter { i in
        guard tasks[i].agentId == row.agentId, let end = Timestamp.date(tasks[i].end) else {
          return false
        }
        return at.timeIntervalSince(end) <= 1800
          && floor(at.timeIntervalSince1970 / 86400) == floor(end.timeIntervalSince1970 / 86400)
      }
      let unscopedUpload =
        row.chatJids.isEmpty && row.circleId == nil && row.tool == "upload_attachment"
      let pendingUploads = adjacent.filter {
        tasks[$0].chatJids.isEmpty && tasks[$0].steps.allSatisfy { $0.tool == "upload_attachment" }
      }
      if let i = candidates.last {
        tasks[i].steps.append(row)
      } else if unscopedUpload && adjacent.count == 1 {
        tasks[adjacent[0]].steps.append(row)
      } else if !row.chatJids.isEmpty && pendingUploads.count == 1 && adjacent.count == 1 {
        tasks[pendingUploads[0]].steps.append(row)
      } else {
        tasks.append(ActivityTask(steps: [row]))
      }
    }
    return tasks.sorted { Activity.newestFirst($0.steps.last!, $1.steps.last!) }
  }
}
