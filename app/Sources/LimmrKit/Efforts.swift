import Foundation

public struct EffortEntity: Codable, Sendable, Identifiable, Equatable {
  public var id: String
  public var name: String
}
public struct EffortMessageRef: Codable, Sendable, Equatable {
  public var chatJid: String
  public var messageId: String
}
public struct Effort: Codable, Sendable, Identifiable, Equatable {
  public var id: String
  public var title: String
  public var summary: String
  public var status: String
  public var due: String?
  public var people: [EffortEntity]
  public var chats: [EffortEntity]
  public var circleIds: [String]
  public var messageRefs: [EffortMessageRef]
  public var agentActionRefs: [Int64]
  public var firstAt: String
  public var lastAt: String
  public var createdBy: String
  public var confidence: Double
  public var pinned: Bool
  public var hidden: Bool
  public var statusLabel: String { Self.label(status, due: due) }
  public static func label(_ status: String, due: String?) -> String {
    let label = status.replacingOccurrences(of: "_", with: " ")
    guard let date = due.flatMap(Timestamp.date) else { return label }
    return label + " · due " + date.formatted(.dateTime.weekday(.abbreviated))
  }
  public var meta: String {
    let days = max(
      1,
      Int(
        (Timestamp.date(lastAt) ?? .distantPast).timeIntervalSince(
          Timestamp.date(firstAt) ?? .distantPast) / 86400) + 1)
    return
      "\(people.map(\.name).joined(separator: ", ")) · \(chats.count) chats · \(messageRefs.count) messages over \(days) days · \(agentActionRefs.count) agent \(agentActionRefs.count == 1 ? "action" : "actions")"
  }
}
public struct EffortsResponse: Codable, Sendable { public var efforts: [Effort] }
public struct EffortNamingCandidate: Codable, Sendable, Identifiable {
  public var id: String
  public var revision: String
  public var topTerms: [String]
  public var artefactNames: [String]
  public var participantFirstNames: [String]?
  public var snippets: [String]?
  public var messageCount: Int
  public var chatCount: Int
  public var participantCount: Int
}
public struct EffortNamingResponse: Codable, Sendable {
  public var efforts: [EffortNamingCandidate]
  public var total: Int
}
public struct EffortTimelineItem: Codable, Sendable, Identifiable {
  public var id: String
  public var at: String
  public var message: Message?
  public var action: Activity?
}
public struct EffortDetailResponse: Codable, Sendable {
  public var effort: Effort
  public var timeline: [EffortTimelineItem]
  public var taskId: String? = nil
  // The wire detail flattens the summary fields alongside timeline.
  public init(from decoder: any Decoder) throws {
    effort = try Effort(from: decoder)
    let c = try decoder.container(keyedBy: CodingKeys.self)
    timeline = try c.decodeIfPresent([EffortTimelineItem].self, forKey: .timeline) ?? []
    taskId = try c.decodeIfPresent(String.self, forKey: .taskId)
  }
  public init(effort: Effort, timeline: [EffortTimelineItem]) {
    self.effort = effort
    self.timeline = timeline
  }
  enum CodingKeys: String, CodingKey { case timeline, taskId }
  public func encode(to encoder: any Encoder) throws {
    try effort.encode(to: encoder)
    var c = encoder.container(keyedBy: CodingKeys.self)
    try c.encode(timeline, forKey: .timeline)
    try c.encodeIfPresent(taskId, forKey: .taskId)
  }
}
public struct CircleEffortUpdate: Codable, Sendable, Identifiable {
  public var circle: EffortEntity
  public var effortId: String
  public var title: String
  public var status: String
  public var line: String
  public var due: String?
  public var lastAt: String
  public var changed: Bool? = nil
  public var id: String { circle.id + ":" + effortId }
  public var statusLabel: String { Effort.label(status, due: due) }
}

/// Acknowledge only rendered timestamps so a concurrent new message stays new.
public struct CircleEffortSeen: Encodable, Sendable {
  public var seen: [String: String] = [:]
  public init(_ updates: [CircleEffortUpdate]) {
    for update in updates {
      guard let at = Timestamp.date(update.lastAt) else { continue }
      if at > (seen[update.circle.id].flatMap(Timestamp.date) ?? .distantPast) {
        seen[update.circle.id] = update.lastAt
      }
    }
  }
}

/// Invented purposes, shared by demo API responses and deterministic snapshots.
public enum DemoEfforts {
  public static let efforts: [Effort] = [
    (
      "slides", "Pitch deck with Maya", "on_you",
      "Maya has finished the layout. Review the slides before Thursday.", "university", "Maya Rao",
      "2026-10-22T12:00:00Z"
    ),
    (
      "lab", "Lab report", "waiting_on_them",
      "Your results are ready. Noor is checking the method section.", "university", "Noor Chen",
      "2026-10-23T12:00:00Z"
    ),
    (
      "weekend", "Weekend plan", "moving",
      "The group is choosing a route. The next step is to confirm a meeting point.", "friends",
      "Sam Patel", ""
    ),
    ("garden", "Garden supplies", "dormant", "", "family", "Riley Park", ""),
    (
      "poster", "Design Club poster", "done", "Maya submitted the final poster.", "university",
      "Maya Rao", ""
    ),
    (
      "budget", "", "moving", "", "university", "Noor Chen", ""
    ),
  ].enumerated().map { index, row in
    let personIndex = [3, 4, 5, 9, 3, 4][index]
    return Effort(
      id: "effort-" + row.0, title: row.1, summary: row.3, status: row.2,
      due: row.6.isEmpty ? nil : row.6,
      people: [.init(id: DemoData.personId(personIndex), name: row.5)],
      chats: [
        .init(id: DemoData.personJid(personIndex), name: row.5),
        .init(id: DemoData.groupJid(2), name: "University class"),
      ],
      circleIds: [row.4],
      messageRefs: (0..<8).map {
        .init(chatJid: DemoData.personJid(personIndex), messageId: "effort-\(index)-\($0)")
      },
      agentActionRefs: [Int64(index + 1)],
      firstAt: DemoData.date(index == 3 ? 20 * 1440 : 5 * 1440),
      lastAt: DemoData.date(index == 3 ? 15 * 1440 : index * 30 + 5),
      createdBy: index == 0 ? "Codex" : index == 1 ? "This Mac" : "Limmr",
      confidence: 0.82, pinned: false, hidden: false)
  }
  public static func search(_ query: String) -> [Effort] {
    efforts.filter {
      EffortPresentation.visible($0) && (query.isEmpty
        || ($0.title + " " + $0.summary + " " + $0.meta).localizedCaseInsensitiveContains(query)
      )
    }
  }
  public static var updates: [CircleEffortUpdate] {
    efforts.filter { EffortPresentation.visible($0) && $0.status != "dormant" }.map {
      .init(
        circle: .init(id: $0.circleIds[0], name: $0.circleIds[0].capitalized), effortId: $0.id,
        title: $0.title, status: $0.status, line: $0.summary, due: $0.due, lastAt: $0.lastAt)
    }
  }
  public static func detail(_ effort: Effort) -> EffortDetailResponse {
    let messages: [String]
    switch effort.id {
    case "effort-slides":
      messages = [
        "Can you review the slides? Due Thursday.", "The slide layout is ready.",
        "I will check the opening slides.", "Here is the first slide outline.",
      ]
    case "effort-lab":
      messages = [
        "I will check the method section.", "Can you check the experiment results?",
        "The lab results are ready.", "Let us compare our measurements.",
      ]
    case "effort-weekend":
      messages = [
        "We can meet by the trail entrance.", "That hiking route works for me.",
        "Shall we use the river trail?", "Let us plan a weekend hike.",
      ]
    case "effort-garden":
      messages = [
        "The seedlings list is ready.", "I added compost to the supplies.",
        "Which seedlings should we order?", "Let us list the garden supplies.",
      ]
    case "effort-budget":
      messages = [
        "Can you review the festival budget proposal?", "The budget is for the proposal.",
        "I will review the proposal budget.", "We need a budget for the proposal.",
      ]
    default:
      messages = [
        "The poster was submitted.", "The final poster looks ready.",
        "I revised the poster typography.", "Here is the poster draft.",
      ]
    }
    var items = Array(DemoData.messages.prefix(4)).enumerated().map { i, message in
      var m = message
      m.id = effort.messageRefs[i].messageId
      m.chatJid = effort.messageRefs[i].chatJid
      m.sender = effort.chats[0].id
      m.isFromMe = i == 2
      m.text = messages[i]
      m.timestamp = DemoData.date(i * 1440 + 5)
      return EffortTimelineItem(id: "message-\(i)", at: m.timestamp, message: m, action: nil)
    }
    if var action = DemoData.activity.first {
      action.agentId = "agent-2"
      action.id = effort.agentActionRefs.first ?? 1
      action.tool = "efforts.update"
      action.kind = "summary"
      action.note = "Codex clarified the next step and due date."
      action.at = DemoData.date(1)
      items.insert(.init(id: "action-1", at: action.at, message: nil, action: action), at: 0)
    }
    return .init(effort: effort, timeline: items)
  }
}

/// Defense at the presentation boundary for stale engines and older saved data.
public enum EffortPresentation {
  public static func emptyTitle(query: String, unnamed: Int) -> String {
    !query.isEmpty ? "No matching work" : unnamed > 0 ? "Work is waiting for names" : "No work here yet"
  }
  public static func emptyDetail(query: String, unnamed: Int) -> String {
    if !query.isEmpty { return "Try a person, chat or topic." }
    if unnamed > 0 {
      return "Related conversations are here. This Mac can name them when its on-device model is available, or ask your AI to name the waiting work. Individual actions are in Detailed log."
    }
    return "Related conversations will appear here as shared purposes. Individual actions are in Detailed log."
  }
  public static func hasTitle(_ title: String) -> Bool {
    let words = title.lowercased().split { !$0.isLetter && !$0.isNumber }.map(String.init)
    guard let first = words.first else { return false }
    return ![
      "image", "photo", "video", "audio", "media", "sticker", "reaction", "deleted", "otp",
      "verification", "document",
    ].contains(first)
      && title.lowercased() != "shared work"
  }
  public static func visible(_ effort: Effort) -> Bool { !effort.hidden && hasTitle(effort.title) }
  public static func homeUpdates(_ updates: [CircleEffortUpdate], now: Date) -> [CircleEffortUpdate]
  {
    var seen: Set<String> = []
    return Array(
      updates.filter {
        guard $0.status != "dormant", hasTitle($0.title), !seen.contains($0.effortId) else {
          return false
        }
        let dueSoon =
          $0.due.flatMap(Timestamp.date).map {
            $0 >= now && $0 <= now.addingTimeInterval(7 * 86400)
          } ?? false
        // Older engines already filtered changed rows, so absent metadata means changed.
        guard $0.status == "on_you" || dueSoon || ($0.changed ?? true) else { return false }
        seen.insert($0.effortId)
        return true
      }.prefix(4))
  }
}

extension EffortEntity {
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    id = try c.decodeIfPresent(String.self, forKey: .id) ?? ""
    name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
  }
}
extension EffortMessageRef {
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    chatJid = try c.decodeIfPresent(String.self, forKey: .chatJid) ?? ""
    messageId = try c.decodeIfPresent(String.self, forKey: .messageId) ?? ""
  }
}
