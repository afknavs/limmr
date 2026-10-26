import Foundation

public enum SendRule: String, Codable, CaseIterable, Sendable {
  case free, confirm
  case askInLimmr = "ask_in_limmr"
  case never
  public var title: String {
    switch self {
    case .free: "Send freely"
    case .confirm: "Confirm in chat"
    case .askInLimmr: "Ask me in Limmr"
    case .never: "Never"
    }
  }
  public var explanation: String {
    switch self {
    case .free: "Integrations can send immediately."
    case .confirm:
      "When you ask an own AI to send, it sends. Otherwise, review and confirm the draft in your conversation. Unattended drafts wait in Limmr."
    case .askInLimmr: "Drafts wait on Home for you to review and send in Limmr."
    case .never: "Integrations cannot send here. You can still read and send messages yourself."
    }
  }
}
public struct Circle: Codable, Identifiable, Sendable, Equatable {
  public var icon: EntityIcon? = nil
  public var id: String
  public var name: String
  public var kind: String
  public var symbol: String
  public var description: String
  public var sendRule: SendRule
  public var createdBy: String
  public var confirmed: Bool
  public var updatedAt: String
  public var pinned: Bool = false
  public var chats: [CircleChat] = []
  public var people: [CirclePerson] = []
  public var topics: [CircleTopic] = []
}
public struct CircleChat: Codable, Identifiable, Sendable, Equatable {
  public var chatJid: String
  public var roleInCircle: String
  public var includedSince: String
  public var id: String { chatJid }
}
public struct CirclePerson: Codable, Identifiable, Sendable, Equatable {
  public var personId: String
  public var role: String
  public var note: String
  public var source: String
  public var id: String { personId }
}
public struct CircleTopic: Codable, Identifiable, Sendable, Equatable {
  public var id: String
  public var title: String
  public var status: String
  public var chats: [String]
  public var people: [String]
  public var keywords: [String]
  public var lastActivityAt: String
}
public struct CircleUpcoming: Codable, Sendable, Equatable {
  public var date: String
  public var text: String
  public init(date: String, text: String) {
    self.date = date
    self.text = text
  }
}
public struct CircleContribution: Codable, Sendable, Equatable {
  public var personId: String
  public var contribution: String
}
public struct CircleBrief: Codable, Identifiable, Sendable, Equatable {
  public var circleId: String
  public var version: Int
  public var summary: String
  public var whatChanged: String
  public var openAsksForYou: [String]
  public var upcoming: [CircleUpcoming]
  public var peopleNotes: [CircleContribution]
  public var evidenceMessageIds: [String]
  public var chatJids: [String] = []
  public var writtenBy: String
  public var writtenAt: String
  public var coversUntil: String
  public var id: Int { version }
}
public struct CircleDetail: Codable, Sendable {
  public var circle: Circle
  public var chats: [CircleChat]
  public var people: [CirclePerson]
  public var topics: [CircleTopic]
  public var brief: CircleBrief?
  public init(
    circle: Circle, chats: [CircleChat], people: [CirclePerson], topics: [CircleTopic],
    brief: CircleBrief?
  ) {
    self.circle = circle
    self.chats = chats
    self.people = people
    self.topics = topics
    self.brief = brief
  }
}
public struct CircleSuggestion: Codable, Identifiable, Sendable, Equatable {
  public var id: String
  public var circleId: String
  public var kind: String
  public var chatJid: String?
  public var draftText: String?
  public var why: String
  public var createdBy: String
  public var expiresAt: String
  public var createdAt: String = ""
  public var state: String = "pending"
  public var draftId: String? = nil
  public var variations: [String] = []
  public var body: String? = nil
  public var taskId: String? = nil
  public var draftSource: String? = nil
}
public struct CircleToday: Codable, Identifiable, Sendable {
  public var circle: Circle
  public var brief: CircleBrief?
  public var newMessages: Int
  public var openAsks: Int
  public var upcoming: Int
  public var pinned: Bool = false
  public var whyShown: String = "busy"
  public var id: String { circle.id }
}
public struct CircleIntegration: Codable, Identifiable, Sendable {
  public var access: AgentAccess? = nil
  public var accessSummary: String? = nil
  public var id: String
  public var name: String
  public var kind: String
  public var lastSeenAt: String?
}
public struct HomeResponse: Codable, Sendable {
  public var tasks: [TaskSummary] = []
  public var circleUpdates: [CircleEffortUpdate] = []
  public var circleChanges: [CircleAutoChange] = []
  public var circleChangesMore: Int = 0
  public var circleAdditions: [CircleAddition] = []
  public var today: [CircleToday]
  public var suggestions: [CircleSuggestion]
  public var proposals: [CircleProposal]
  public var integrations: [CircleIntegration]
  public var watches: [WatchSummary] = []
  public var waiting: [WaitingItem] = []
  public var queued: [CircleSuggestion] = []
  public var needsReply: [ReplyAsk] = []
  public var circles: [CircleToday] = []
  public var recentActivity: [Activity] = []
  /// False while the engine is still warming efforts; circle updates arrive on a later read.
  public var effortsReady: Bool? = nil
  public static let empty = HomeResponse(
    today: [], suggestions: [], proposals: [], integrations: [])
}
public struct CirclesResponse: Codable, Sendable { public var circles: [Circle] }
public struct BriefsResponse: Codable, Sendable { public var briefs: [CircleBrief] }
public enum CirclesPresentation {
  public static func homeColumns(contentWidth: Double) -> Int { contentWidth >= 900 ? 2 : 1 }
  public static func today(_ items: [CircleToday], expanded: Bool = false) -> [CircleToday] {
    let active = items.filter { $0.newMessages > 0 }.sorted {
      $0.newMessages == $1.newMessages ? $0.id < $1.id : $0.newMessages > $1.newMessages
    }
    return expanded ? active : Array(active.prefix(4))
  }
  public static func suggestions(_ items: [CircleSuggestion], expanded: Bool = false)
    -> [CircleSuggestion]
  {
    expanded ? items : Array(items.prefix(3))
  }
  public static let homeSections = [
    "greeting", "waiting", "watches", "queued", "circles", "needs_reply", "recent_activity",
    "proposals",
    "integrations",
  ]
}

public struct CircleProposal: Codable, Identifiable, Sendable {
  public var id: String
  public var circle: Circle
  public var why: String
  public var source: String
  public var batchId: String? = nil
  public var createdBy: String? = nil
}

extension Circle {
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    icon = try c.decodeIfPresent(EntityIcon.self, forKey: .icon)
    id = try c.decode(String.self, forKey: .id)
    name = try c.decode(String.self, forKey: .name)
    kind = try c.decode(String.self, forKey: .kind)
    symbol = try c.decode(String.self, forKey: .symbol)
    description = try c.decode(String.self, forKey: .description)
    sendRule = try c.decode(SendRule.self, forKey: .sendRule)
    createdBy = try c.decode(String.self, forKey: .createdBy)
    confirmed = try c.decode(Bool.self, forKey: .confirmed)
    updatedAt = try c.decode(String.self, forKey: .updatedAt)
    pinned = try c.decodeIfPresent(Bool.self, forKey: .pinned) ?? false
    chats = try c.decodeIfPresent([CircleChat].self, forKey: .chats) ?? []
    people = try c.decodeIfPresent([CirclePerson].self, forKey: .people) ?? []
    topics = try c.decodeIfPresent([CircleTopic].self, forKey: .topics) ?? []
  }
}

extension CircleTopic {
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    id = try c.decode(String.self, forKey: .id)
    title = try c.decode(String.self, forKey: .title)
    status = try c.decode(String.self, forKey: .status)
    chats = try c.decodeIfPresent([String].self, forKey: .chats) ?? []
    people = try c.decodeIfPresent([String].self, forKey: .people) ?? []
    keywords = try c.decodeIfPresent([String].self, forKey: .keywords) ?? []
    lastActivityAt = try c.decode(String.self, forKey: .lastActivityAt)
  }
}

extension CircleBrief {
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    circleId = try c.decode(String.self, forKey: .circleId)
    version = try c.decode(Int.self, forKey: .version)
    summary = try c.decode(String.self, forKey: .summary)
    whatChanged = try c.decode(String.self, forKey: .whatChanged)
    openAsksForYou = try c.decode([BriefAskText].self, forKey: .openAsksForYou).map(\.text)
    upcoming = try c.decodeIfPresent([CircleUpcoming].self, forKey: .upcoming) ?? []
    peopleNotes = try c.decodeIfPresent([CircleContribution].self, forKey: .peopleNotes) ?? []
    evidenceMessageIds = try c.decodeIfPresent([String].self, forKey: .evidenceMessageIds) ?? []
    chatJids = try c.decodeIfPresent([String].self, forKey: .chatJids) ?? []
    writtenBy = try c.decode(String.self, forKey: .writtenBy)
    writtenAt = try c.decode(String.self, forKey: .writtenAt)
    coversUntil = try c.decode(String.self, forKey: .coversUntil)
  }
}

extension HomeResponse {
  enum CodingKeys: String, CodingKey {
    case circleUpdates, circleChanges, circleChangesMore, circleAdditions, today, suggestions, proposals, integrations, watches,
      waiting, queued, needsReply, circles,
      recentActivity, cards, effortsReady, tasks
  }
  public func encode(to encoder: any Encoder) throws {
    var c = encoder.container(keyedBy: CodingKeys.self)
    try c.encode(tasks, forKey: .tasks)
    try c.encode(circleUpdates, forKey: .circleUpdates)
    try c.encode(circleChanges, forKey: .circleChanges)
    try c.encode(circleChangesMore, forKey: .circleChangesMore)
    try c.encode(circleAdditions, forKey: .circleAdditions)
    try c.encode(today, forKey: .today)
    try c.encode(suggestions, forKey: .suggestions)
    try c.encode(proposals, forKey: .proposals)
    try c.encode(integrations, forKey: .integrations)
    try c.encode(watches, forKey: .watches)
    try c.encode(waiting, forKey: .waiting)
    try c.encode(queued, forKey: .queued)
    try c.encode(needsReply, forKey: .needsReply)
    try c.encode(circles, forKey: .circles)
    try c.encode(recentActivity, forKey: .recentActivity)
    try c.encodeIfPresent(effortsReady, forKey: .effortsReady)
  }
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    tasks = try c.decodeIfPresent([TaskSummary].self, forKey: .tasks) ?? []
    circleUpdates = try c.decodeIfPresent([CircleEffortUpdate].self, forKey: .circleUpdates) ?? []
    circleChanges = try c.decodeIfPresent([CircleAutoChange].self, forKey: .circleChanges) ?? []
    circleChangesMore = try c.decodeIfPresent(Int.self, forKey: .circleChangesMore) ?? 0
    circleAdditions = try c.decodeIfPresent([CircleAddition].self, forKey: .circleAdditions) ?? []
    today = try c.decodeIfPresent([CircleToday].self, forKey: .today) ?? []
    suggestions = try c.decodeIfPresent([CircleSuggestion].self, forKey: .suggestions) ?? []
    proposals = try c.decodeIfPresent([CircleProposal].self, forKey: .proposals) ?? []
    integrations = try c.decodeIfPresent([CircleIntegration].self, forKey: .integrations) ?? []
    watches = try c.decodeIfPresent([WatchSummary].self, forKey: .watches) ?? []
    waiting = try c.decodeIfPresent([WaitingItem].self, forKey: .waiting) ?? []
    queued =
      try c.decodeIfPresent([CircleSuggestion].self, forKey: .queued)
      ?? suggestions.filter { ($0.kind == "reply" && $0.draftText != nil) || ["check_in", "nudge"].contains($0.kind) }
    needsReply = try c.decodeIfPresent([ReplyAsk].self, forKey: .needsReply) ?? []
    circles =
      try c.decodeIfPresent([CircleToday].self, forKey: .circles)
      ?? c.decodeIfPresent([CircleToday].self, forKey: .cards) ?? today
    recentActivity = try c.decodeIfPresent([Activity].self, forKey: .recentActivity) ?? []
    effortsReady = try c.decodeIfPresent(Bool.self, forKey: .effortsReady)
  }
}

extension CirclesResponse {
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    circles = try c.decodeIfPresent([Circle].self, forKey: .circles) ?? []
  }
}

extension BriefsResponse {
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    briefs = try c.decodeIfPresent([CircleBrief].self, forKey: .briefs) ?? []
  }
}

public struct CircleDraft: Codable, Sendable {
  public var ownerInstruction: String? = nil
  public var queueForApproval: Bool? = nil
  public var draftId: String
  public var agentId: String
  public var chatJid: String
  public var finalText: String
  public var rule: SendRule
  public var needsConfirmation: Bool
  public var createdAt: String
  public var expiresAt: String
  public var state: String
  public var confirmedVia: String?
  public var messageId: String?
  public var suggestionId: String?
}

private struct BriefAsk: Decodable { var text: String }
public struct ReplyAsk: Codable, Identifiable, Sendable, Equatable {
  public var id: String
  public var circleId: String
  public var circle: String
  public var text: String
  public var chat: String?
  public var chatJid: String?
  public var from: String?
  public var at: String?
}
extension CircleToday {
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    circle = try c.decode(Circle.self, forKey: .circle)
    brief = try c.decodeIfPresent(CircleBrief.self, forKey: .brief)
    newMessages = try c.decodeIfPresent(Int.self, forKey: .newMessages) ?? 0
    openAsks = try c.decodeIfPresent(Int.self, forKey: .openAsks) ?? 0
    upcoming = try c.decodeIfPresent(Int.self, forKey: .upcoming) ?? 0
    pinned = try c.decodeIfPresent(Bool.self, forKey: .pinned) ?? circle.pinned
    whyShown = try c.decodeIfPresent(String.self, forKey: .whyShown) ?? (pinned ? "pinned" : "busy")
  }
}
extension CircleSuggestion {
  enum CodingKeys: String, CodingKey {
    case id, circleId, kind, chatJid, draftText, why, createdBy, expiresAt, createdAt, state,
      draftId
    case variations, body, taskId, draftSource
  }
  public func encode(to encoder: any Encoder) throws {
    var c = encoder.container(keyedBy: CodingKeys.self)
    try c.encode(id, forKey: .id)
    try c.encode(circleId, forKey: .circleId)
    try c.encode(kind, forKey: .kind)
    try c.encodeIfPresent(chatJid, forKey: .chatJid)
    try c.encodeIfPresent(draftText, forKey: .draftText)
    try c.encode(why, forKey: .why)
    try c.encode(createdBy, forKey: .createdBy)
    try c.encode(expiresAt, forKey: .expiresAt)
    try c.encode(createdAt, forKey: .createdAt)
    try c.encode(state, forKey: .state)
    try c.encodeIfPresent(draftId, forKey: .draftId)
    try c.encode(variations, forKey: .variations)
    try c.encodeIfPresent(body, forKey: .body)
    try c.encodeIfPresent(taskId, forKey: .taskId)
    try c.encodeIfPresent(draftSource, forKey: .draftSource)
  }
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    id = try c.decode(String.self, forKey: .id)
    circleId = try c.decodeIfPresent(String.self, forKey: .circleId) ?? ""
    kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? "reply"
    chatJid = try c.decodeIfPresent(String.self, forKey: .chatJid)
    draftText = try c.decodeIfPresent(String.self, forKey: .draftText)
    why = try c.decodeIfPresent(String.self, forKey: .why) ?? ""
    createdBy = try c.decodeIfPresent(String.self, forKey: .createdBy) ?? ""
    expiresAt = try c.decodeIfPresent(String.self, forKey: .expiresAt) ?? ""
    createdAt = try c.decodeIfPresent(String.self, forKey: .createdAt) ?? ""
    state = try c.decodeIfPresent(String.self, forKey: .state) ?? "pending"
    draftId = try c.decodeIfPresent(String.self, forKey: .draftId)
    variations = try c.decodeIfPresent([String].self, forKey: .variations) ?? []
    body = try c.decodeIfPresent(String.self, forKey: .body)
    taskId = try c.decodeIfPresent(String.self, forKey: .taskId)
    draftSource = try c.decodeIfPresent(String.self, forKey: .draftSource)
  }
}

private struct BriefAskText: Decodable {
  var text: String
  init(from decoder: any Decoder) throws {
    if let value = try? decoder.singleValueContainer().decode(String.self) { text = value }
    else {
      let c = try decoder.container(keyedBy: CodingKeys.self)
      text = try c.decode(String.self, forKey: .text)
    }
  }
  enum CodingKeys: String, CodingKey { case text }
}
