import Foundation

// Raw kind/status strings preserve future values; missing fields keep older engines readable.
public struct TaskProgress: Codable, Sendable, Equatable {
  public var done: Int = 0
  public var total: Int = 0
  public init(done: Int = 0, total: Int = 0) {
    self.done = done
    self.total = total
  }
  enum CodingKeys: String, CodingKey { case done, total }
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    done = try c.decodeIfPresent(Int.self, forKey: .done) ?? 0
    total = try c.decodeIfPresent(Int.self, forKey: .total) ?? 0
  }
}

public struct TaskAuthor: Codable, Sendable, Equatable {
  public var agentId: String = ""
  public var name: String = ""
  public init(agentId: String = "", name: String = "") {
    self.agentId = agentId
    self.name = name
  }
  enum CodingKeys: String, CodingKey { case agentId, name }
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    agentId = try c.decodeIfPresent(String.self, forKey: .agentId) ?? ""
    name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
  }
}

public struct TaskSummaryText: Codable, Sendable, Equatable {
  public var text: String = ""
  public var updatedAt: String = ""
  public var updatedBy: String = ""
  public init(text: String = "", updatedAt: String = "", updatedBy: String = "") {
    self.text = text
    self.updatedAt = updatedAt
    self.updatedBy = updatedBy
  }
  enum CodingKeys: String, CodingKey { case text, updatedAt, updatedBy }
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    text = try c.decodeIfPresent(String.self, forKey: .text) ?? ""
    updatedAt = try c.decodeIfPresent(String.self, forKey: .updatedAt) ?? ""
    updatedBy = try c.decodeIfPresent(String.self, forKey: .updatedBy) ?? ""
  }
}

public struct WorkspaceItem: Codable, Sendable, Equatable, Identifiable {
  public var id: String = ""
  public var kind: String = "note"
  public var title: String = ""
  public var url: String? = nil
  public var attachmentId: String? = nil
  public var text: String? = nil
  public var source: String = "ai"
  public var messageRef: EffortMessageRef? = nil
  public var addedBy: String = ""
  public var addedAt: String = ""
  public init(id: String = "", kind: String = "note", title: String = "", url: String? = nil, attachmentId: String? = nil, text: String? = nil, source: String = "ai", messageRef: EffortMessageRef? = nil, addedBy: String = "", addedAt: String = "") {
    self.id = id
    self.kind = kind
    self.title = title
    self.url = url
    self.attachmentId = attachmentId
    self.text = text
    self.source = source
    self.messageRef = messageRef
    self.addedBy = addedBy
    self.addedAt = addedAt
  }
  enum CodingKeys: String, CodingKey { case id, kind, title, url, attachmentId, text, source, messageRef, addedBy, addedAt }
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    id = try c.decodeIfPresent(String.self, forKey: .id) ?? ""
    kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? "note"
    title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
    url = try c.decodeIfPresent(String.self, forKey: .url)
    attachmentId = try c.decodeIfPresent(String.self, forKey: .attachmentId)
    text = try c.decodeIfPresent(String.self, forKey: .text)
    source = try c.decodeIfPresent(String.self, forKey: .source) ?? "ai"
    messageRef = try c.decodeIfPresent(EffortMessageRef.self, forKey: .messageRef)
    addedBy = try c.decodeIfPresent(String.self, forKey: .addedBy) ?? ""
    addedAt = try c.decodeIfPresent(String.self, forKey: .addedAt) ?? ""
  }
}

public struct TaskStep: Codable, Sendable, Equatable, Identifiable {
  public var id: String = ""
  public var text: String = ""
  public var done: Bool = false
  public var doneAt: String? = nil
  public var order: Int = 0
  public init(id: String = "", text: String = "", done: Bool = false, doneAt: String? = nil, order: Int = 0) {
    self.id = id
    self.text = text
    self.done = done
    self.doneAt = doneAt
    self.order = order
  }
  enum CodingKeys: String, CodingKey { case id, text, done, doneAt, order }
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    id = try c.decodeIfPresent(String.self, forKey: .id) ?? ""
    text = try c.decodeIfPresent(String.self, forKey: .text) ?? ""
    done = try c.decodeIfPresent(Bool.self, forKey: .done) ?? false
    doneAt = try c.decodeIfPresent(String.self, forKey: .doneAt)
    order = try c.decodeIfPresent(Int.self, forKey: .order) ?? 0
  }
}

public struct TaskCheckIn: Codable, Sendable, Equatable {
  public var rule: String = "weekly"
  public var weekday: Int? = nil
  public var everyDays: Int? = nil
  public var atTime: String = "18:00"
  public var to: String = ""
  public var note: String = ""
  public var nextAt: String = ""
  public var lastAt: String? = nil
  public init(rule: String = "weekly", weekday: Int? = nil, everyDays: Int? = nil, atTime: String = "18:00", to: String = "", note: String = "", nextAt: String = "", lastAt: String? = nil) {
    self.rule = rule
    self.weekday = weekday
    self.everyDays = everyDays
    self.atTime = atTime
    self.to = to
    self.note = note
    self.nextAt = nextAt
    self.lastAt = lastAt
  }
  enum CodingKeys: String, CodingKey { case rule, weekday, everyDays, atTime, to, note, nextAt, lastAt }
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    rule = try c.decodeIfPresent(String.self, forKey: .rule) ?? "weekly"
    weekday = try c.decodeIfPresent(Int.self, forKey: .weekday)
    everyDays = try c.decodeIfPresent(Int.self, forKey: .everyDays)
    atTime = try c.decodeIfPresent(String.self, forKey: .atTime) ?? "18:00"
    to = try c.decodeIfPresent(String.self, forKey: .to) ?? ""
    note = try c.decodeIfPresent(String.self, forKey: .note) ?? ""
    nextAt = try c.decodeIfPresent(String.self, forKey: .nextAt) ?? ""
    lastAt = try c.decodeIfPresent(String.self, forKey: .lastAt)
  }
}

public struct TaskWaiting: Codable, Sendable, Equatable {
  public var on: EffortEntity? = nil
  public var what: String = ""
  public var since: String = ""
  public var nudgeAfterDays: Int = 3
  public var nudgeState: String = "none"
  public var nudgeSuggestionId: String? = nil
  public init(on: EffortEntity? = nil, what: String = "", since: String = "", nudgeAfterDays: Int = 3, nudgeState: String = "none", nudgeSuggestionId: String? = nil) {
    self.on = on
    self.what = what
    self.since = since
    self.nudgeAfterDays = nudgeAfterDays
    self.nudgeState = nudgeState
    self.nudgeSuggestionId = nudgeSuggestionId
  }
  enum CodingKeys: String, CodingKey { case on, what, since, nudgeAfterDays, nudgeState, nudgeSuggestionId }
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    on = try c.decodeIfPresent(EffortEntity.self, forKey: .on)
    what = try c.decodeIfPresent(String.self, forKey: .what) ?? ""
    since = try c.decodeIfPresent(String.self, forKey: .since) ?? ""
    nudgeAfterDays = try c.decodeIfPresent(Int.self, forKey: .nudgeAfterDays) ?? 3
    nudgeState = try c.decodeIfPresent(String.self, forKey: .nudgeState) ?? "none"
    nudgeSuggestionId = try c.decodeIfPresent(String.self, forKey: .nudgeSuggestionId)
  }
}

public struct TaskWaitingSummary: Codable, Sendable, Equatable {
  public var onName: String? = nil
  public var what: String = ""
  public var since: String = ""
  public var days: Int = 0
  public init(onName: String? = nil, what: String = "", since: String = "", days: Int = 0) {
    self.onName = onName
    self.what = what
    self.since = since
    self.days = days
  }
  enum CodingKeys: String, CodingKey { case onName, what, since, days }
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    onName = try c.decodeIfPresent(String.self, forKey: .onName)
    what = try c.decodeIfPresent(String.self, forKey: .what) ?? ""
    since = try c.decodeIfPresent(String.self, forKey: .since) ?? ""
    days = try c.decodeIfPresent(Int.self, forKey: .days) ?? 0
  }
}

public struct TaskSentUpdate: Codable, Sendable, Equatable {
  public var at: String = ""
  public var chatJid: String = ""
  public var messageId: String = ""
  public var textPreview: String = ""
  public var kind: String = "other"
  public var by: String = ""
  public init(at: String = "", chatJid: String = "", messageId: String = "", textPreview: String = "", kind: String = "other", by: String = "") {
    self.at = at
    self.chatJid = chatJid
    self.messageId = messageId
    self.textPreview = textPreview
    self.kind = kind
    self.by = by
  }
  enum CodingKeys: String, CodingKey { case at, chatJid, messageId, textPreview, kind, by }
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    at = try c.decodeIfPresent(String.self, forKey: .at) ?? ""
    chatJid = try c.decodeIfPresent(String.self, forKey: .chatJid) ?? ""
    messageId = try c.decodeIfPresent(String.self, forKey: .messageId) ?? ""
    textPreview = try c.decodeIfPresent(String.self, forKey: .textPreview) ?? ""
    kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? "other"
    by = try c.decodeIfPresent(String.self, forKey: .by) ?? ""
  }
}

public struct TaskSuggestion: Codable, Sendable, Equatable {
  public var signals: [String] = []
  public var confidence: Double = 0
  public var detectedAt: String = ""
  public var effortIds: [String] = []
  public init(signals: [String] = [], confidence: Double = 0, detectedAt: String = "", effortIds: [String] = []) {
    self.signals = signals; self.confidence = confidence; self.detectedAt = detectedAt; self.effortIds = effortIds
  }
}

public struct TaskSummary: Codable, Sendable, Equatable, Identifiable {
  public var id: String = ""
  public var title: String = ""
  public var kind: String = "long"
  public var status: String = "active"
  public var createdBy: TaskAuthor = .init()
  public var origin: String = "owner"
  public var suggestion: TaskSuggestion? = nil
  public var autoKeptAt: String? = nil
  public var undoChangeId: String? = nil
  public var progress: TaskProgress = .init()
  public var nextCheckInAt: String? = nil
  public var waiting: TaskWaitingSummary? = nil
  public var needsYou: Bool = false
  public var needsYouReason: String? = nil
  public var lastUpdateAt: String? = nil
  public var effortId: String? = nil
  public init(id: String = "", title: String = "", kind: String = "long", status: String = "active", progress: TaskProgress = .init(), nextCheckInAt: String? = nil, waiting: TaskWaitingSummary? = nil, needsYou: Bool = false, needsYouReason: String? = nil, lastUpdateAt: String? = nil, effortId: String? = nil, origin: String = "owner", suggestion: TaskSuggestion? = nil, autoKeptAt: String? = nil, undoChangeId: String? = nil, createdBy: TaskAuthor = .init()) {
    self.id = id
    self.title = title
    self.kind = kind
    self.status = status
    self.origin = origin; self.suggestion = suggestion; self.autoKeptAt = autoKeptAt; self.undoChangeId = undoChangeId
    self.createdBy = createdBy
    self.progress = progress
    self.nextCheckInAt = nextCheckInAt
    self.waiting = waiting
    self.needsYou = needsYou
    self.needsYouReason = needsYouReason
    self.lastUpdateAt = lastUpdateAt
    self.effortId = effortId
  }
  enum CodingKeys: String, CodingKey { case origin, suggestion, autoKeptAt, undoChangeId, createdBy, id, title, kind, status, progress, nextCheckInAt, waiting, needsYou, needsYouReason, lastUpdateAt, effortId }
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    id = try c.decodeIfPresent(String.self, forKey: .id) ?? ""
    title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
    kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? "long"
    status = try c.decodeIfPresent(String.self, forKey: .status) ?? "active"
    origin = try c.decodeIfPresent(String.self, forKey: .origin) ?? "owner"
    suggestion = try c.decodeIfPresent(TaskSuggestion.self, forKey: .suggestion)
    autoKeptAt = try c.decodeIfPresent(String.self, forKey: .autoKeptAt)
    undoChangeId = try c.decodeIfPresent(String.self, forKey: .undoChangeId)
    createdBy = try c.decodeIfPresent(TaskAuthor.self, forKey: .createdBy) ?? .init()
    progress = try c.decodeIfPresent(TaskProgress.self, forKey: .progress) ?? .init()
    nextCheckInAt = try c.decodeIfPresent(String.self, forKey: .nextCheckInAt)
    waiting = try c.decodeIfPresent(TaskWaitingSummary.self, forKey: .waiting)
    needsYou = try c.decodeIfPresent(Bool.self, forKey: .needsYou) ?? false
    needsYouReason = try c.decodeIfPresent(String.self, forKey: .needsYouReason)
    lastUpdateAt = try c.decodeIfPresent(String.self, forKey: .lastUpdateAt)
    effortId = try c.decodeIfPresent(String.self, forKey: .effortId)
  }
}

public struct TaskHistoryEvent: Codable, Sendable, Equatable {
  public var at: String
  public var kind: String
  public var text: String
}

public struct TaskLatestMessage: Codable, Sendable, Equatable {
  public var chatJid: String
  public var messageId: String
  public var chat: String
  public var sender: String
  public var timestamp: String
  public var text: String
  public init(chatJid: String, messageId: String, chat: String, sender: String, timestamp: String, text: String) {
    self.chatJid = chatJid; self.messageId = messageId; self.chat = chat
    self.sender = sender; self.timestamp = timestamp; self.text = text
  }
  public var attempt: TaskSummaryAttempt { .init(chatJid: chatJid, messageId: messageId, timestamp: timestamp) }
}
public struct TaskSummaryAttempt: Codable, Sendable, Equatable {
  public var chatJid: String
  public var messageId: String
  public var timestamp: String
}

public struct TrackedTask: Codable, Sendable, Equatable, Identifiable {
  public var latest: [TaskLatestMessage] = []
  public var summaryAttempt: TaskSummaryAttempt? = nil
  public var id: String = ""
  public var title: String = ""
  public var goal: String = ""
  public var kind: String = "long"
  public var status: String = "active"
  public var origin: String = "owner"
  public var suggestion: TaskSuggestion? = nil
  public var autoKeptAt: String? = nil
  public var undoChangeId: String? = nil
  public var effortId: String? = nil
  public var chats: [EffortEntity] = []
  public var people: [EffortEntity] = []
  public var circleIds: [String] = []
  public var createdBy: TaskAuthor = .init()
  public var createdAt: String = ""
  public var updatedAt: String = ""
  public var closedAt: String? = nil
  public var closeReason: String? = nil
  public var summary: TaskSummaryText = .init()
  public var items: [WorkspaceItem] = []
  public var checklist: [TaskStep] = []
  public var progress: TaskProgress = .init()
  public var checkIn: TaskCheckIn? = nil
  public var waiting: TaskWaiting? = nil
  public var lifespanDays: Int? = nil
  public var updatesSent: [TaskSentUpdate] = []
  public var history: [TaskHistoryEvent] = []
  public var needsYou: Bool = false
  public var needsYouReason: String? = nil
  public init(id: String = "", title: String = "", goal: String = "", kind: String = "long", status: String = "active", effortId: String? = nil, chats: [EffortEntity] = [], people: [EffortEntity] = [], circleIds: [String] = [], createdBy: TaskAuthor = .init(), createdAt: String = "", updatedAt: String = "", closedAt: String? = nil, closeReason: String? = nil, summary: TaskSummaryText = .init(), items: [WorkspaceItem] = [], checklist: [TaskStep] = [], progress: TaskProgress = .init(), checkIn: TaskCheckIn? = nil, waiting: TaskWaiting? = nil, lifespanDays: Int? = nil, updatesSent: [TaskSentUpdate] = [], needsYou: Bool = false, needsYouReason: String? = nil, origin: String = "owner", suggestion: TaskSuggestion? = nil, autoKeptAt: String? = nil, undoChangeId: String? = nil) {
    self.id = id
    self.title = title
    self.goal = goal
    self.kind = kind
    self.status = status
    self.origin = origin; self.suggestion = suggestion; self.autoKeptAt = autoKeptAt; self.undoChangeId = undoChangeId
    self.effortId = effortId
    self.chats = chats
    self.people = people
    self.circleIds = circleIds
    self.createdBy = createdBy
    self.createdAt = createdAt
    self.updatedAt = updatedAt
    self.closedAt = closedAt
    self.closeReason = closeReason
    self.summary = summary
    self.items = items
    self.checklist = checklist
    self.progress = progress
    self.checkIn = checkIn
    self.waiting = waiting
    self.lifespanDays = lifespanDays
    self.updatesSent = updatesSent
    self.needsYou = needsYou
    self.needsYouReason = needsYouReason
  }
  enum CodingKeys: String, CodingKey { case latest, summaryAttempt, history, origin, suggestion, autoKeptAt, undoChangeId, id, title, goal, kind, status, effortId, chats, people, circleIds, createdBy, createdAt, updatedAt, closedAt, closeReason, summary, items, checklist, progress, checkIn, waiting, lifespanDays, updatesSent, needsYou, needsYouReason }
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    latest = try c.decodeIfPresent([TaskLatestMessage].self, forKey: .latest) ?? []
    summaryAttempt = try c.decodeIfPresent(TaskSummaryAttempt.self, forKey: .summaryAttempt)
    history = try c.decodeIfPresent([TaskHistoryEvent].self, forKey: .history) ?? []
    id = try c.decodeIfPresent(String.self, forKey: .id) ?? ""
    title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
    goal = try c.decodeIfPresent(String.self, forKey: .goal) ?? ""
    kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? "long"
    status = try c.decodeIfPresent(String.self, forKey: .status) ?? "active"
    origin = try c.decodeIfPresent(String.self, forKey: .origin) ?? "owner"
    suggestion = try c.decodeIfPresent(TaskSuggestion.self, forKey: .suggestion)
    autoKeptAt = try c.decodeIfPresent(String.self, forKey: .autoKeptAt)
    undoChangeId = try c.decodeIfPresent(String.self, forKey: .undoChangeId)
    effortId = try c.decodeIfPresent(String.self, forKey: .effortId)
    chats = try c.decodeIfPresent([EffortEntity].self, forKey: .chats) ?? []
    people = try c.decodeIfPresent([EffortEntity].self, forKey: .people) ?? []
    circleIds = try c.decodeIfPresent([String].self, forKey: .circleIds) ?? []
    createdBy = try c.decodeIfPresent(TaskAuthor.self, forKey: .createdBy) ?? .init()
    createdAt = try c.decodeIfPresent(String.self, forKey: .createdAt) ?? ""
    updatedAt = try c.decodeIfPresent(String.self, forKey: .updatedAt) ?? ""
    closedAt = try c.decodeIfPresent(String.self, forKey: .closedAt)
    closeReason = try c.decodeIfPresent(String.self, forKey: .closeReason)
    summary = try c.decodeIfPresent(TaskSummaryText.self, forKey: .summary) ?? .init()
    items = try c.decodeIfPresent([WorkspaceItem].self, forKey: .items) ?? []
    checklist = try c.decodeIfPresent([TaskStep].self, forKey: .checklist) ?? []
    progress = try c.decodeIfPresent(TaskProgress.self, forKey: .progress) ?? .init()
    checkIn = try c.decodeIfPresent(TaskCheckIn.self, forKey: .checkIn)
    waiting = try c.decodeIfPresent(TaskWaiting.self, forKey: .waiting)
    lifespanDays = try c.decodeIfPresent(Int.self, forKey: .lifespanDays)
    updatesSent = try c.decodeIfPresent([TaskSentUpdate].self, forKey: .updatesSent) ?? []
    needsYou = try c.decodeIfPresent(Bool.self, forKey: .needsYou) ?? false
    needsYouReason = try c.decodeIfPresent(String.self, forKey: .needsYouReason)
  }
}

public struct TasksResponse: Codable, Sendable, Equatable {
  public var tasks: [TaskSummary] = []
  public init(tasks: [TaskSummary] = []) {
    self.tasks = tasks
  }
  enum CodingKeys: String, CodingKey { case tasks }
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    tasks = try c.decodeIfPresent([TaskSummary].self, forKey: .tasks) ?? []
  }
}

public struct TaskFeedEvent: Codable, Sendable, Equatable {
  public var at: String = ""
  public var kind: String = ""
  public var text: String = ""
  public init(at: String = "", kind: String = "", text: String = "") {
    self.at = at
    self.kind = kind
    self.text = text
  }
  enum CodingKeys: String, CodingKey { case at, kind, text }
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    at = try c.decodeIfPresent(String.self, forKey: .at) ?? ""
    kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? ""
    text = try c.decodeIfPresent(String.self, forKey: .text) ?? ""
  }
}

public struct AccessoryCue: Codable, Sendable, Equatable {
  public var enabled: Bool = false
  public var color: String = "#5e5ce6"
  public var quietHours: AccessoryQuietHours? = nil
  public init(enabled: Bool = false, color: String = "#5e5ce6", quietHours: AccessoryQuietHours? = nil) {
    self.enabled = enabled
    self.color = color
    self.quietHours = quietHours
  }
  enum CodingKeys: String, CodingKey { case enabled, color, quietHours }
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    enabled = try c.decodeIfPresent(Bool.self, forKey: .enabled) ?? false
    color = try c.decodeIfPresent(String.self, forKey: .color) ?? "#5e5ce6"
    quietHours = try c.decodeIfPresent(AccessoryQuietHours.self, forKey: .quietHours)
  }
}

public struct AccessoryQuietHours: Codable, Sendable, Equatable {
  public var start: String = "23:00"
  public var end: String = "08:00"
  public var timezone: String? = nil
  public init(start: String = "23:00", end: String = "08:00", timezone: String? = nil) {
    self.start = start
    self.end = end
    self.timezone = timezone
  }
  enum CodingKeys: String, CodingKey { case start, end, timezone }
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    start = try c.decodeIfPresent(String.self, forKey: .start) ?? "23:00"
    end = try c.decodeIfPresent(String.self, forKey: .end) ?? "08:00"
    timezone = try c.decodeIfPresent(String.self, forKey: .timezone)
  }
}

public extension TaskProgress {
  var text: String { "\(max(0, done)) of \(max(0, total)) steps" }
  var fraction: Double { total > 0 ? min(1, max(0, Double(done) / Double(total))) : 0 }
}
public extension TrackedTask {
  func compact(now: Date) -> TaskSummary {
    .init(id: id, title: title, kind: kind, status: status, progress: progress,
      nextCheckInAt: checkIn?.nextAt,
      waiting: waiting.map { .init(onName: $0.on?.name, what: $0.what, since: $0.since,
        days: TaskPresentation.days(since: $0.since, now: now)) }, needsYou: needsYou,
      needsYouReason: needsYouReason, lastUpdateAt: updatesSent.last?.at, effortId: effortId, origin: origin, suggestion: suggestion, autoKeptAt: autoKeptAt, undoChangeId: undoChangeId, createdBy: createdBy)
  }
}
public enum TaskPresentation {
  public static func accessibilityLabel(_ task: TaskSummary) -> String {
    var parts = ["Open " + task.title, task.needsYou ? "Needs you" : task.status.capitalized]
    if task.progress.total > 0 { parts.append(task.progress.text) }
    if let value = task.waiting { parts.append(waiting(value)) }
    if let at = task.nextCheckInAt { parts.append("Next check-in " + checkIn(at)) }
    return parts.joined(separator: ". ")
  }
  public static func days(since: String, now: Date) -> Int {
    max(0, Int(now.timeIntervalSince(Timestamp.date(since) ?? now) / 86400))
  }
  public static func waiting(_ waiting: TaskWaitingSummary) -> String {
    "Waiting" + (waiting.onName.map { " on " + $0 } ?? "")
      + (waiting.what.isEmpty ? "" : " for " + waiting.what)
      + " · \(waiting.days) \(waiting.days == 1 ? "day" : "days")"
  }
  public static func checkIn(_ value: String, timeZone: TimeZone = .current) -> String {
    guard let date = Timestamp.date(value) else { return "Not scheduled" }
    let f = DateFormatter(); f.locale = Locale(identifier: "en_US_POSIX"); f.timeZone = timeZone
    var calendar = Calendar(identifier: .gregorian); calendar.timeZone = timeZone
    f.dateFormat = calendar.component(.minute, from: date) == 0 ? "EEE h a" : "EEE h:mm a"
    return f.string(from: date).replacingOccurrences(of: "AM", with: "am").replacingOccurrences(of: "PM", with: "pm")
  }
  public static func open(_ tasks: [TaskSummary]) -> [TaskSummary] {
    tasks.filter { ["active", "waiting"].contains($0.status) }.sorted {
      if $0.needsYou != $1.needsYou { return $0.needsYou }
      return $0.title.localizedStandardCompare($1.title) == .orderedAscending
    }
  }
  public static func linkLabel(_ value: String?) -> String {
    switch URL(string: value ?? "")?.host?.lowercased() {
    case "notebooklm.google.com": "NotebookLM"
    case "docs.google.com": "Google Docs"
    case "drive.google.com": "Google Drive"
    case "github.com": "GitHub"
    case .some(let host): host
    default: "Link"
    }
  }
  public static func safeLink(_ value: String?) -> URL? {
    guard let value, let url = URL(string: value), ["https", "http"].contains(url.scheme?.lowercased() ?? ""),
      url.host != nil, url.user == nil, url.password == nil else { return nil }
    return url
  }
}

public struct CreateTaskRequest: Encodable, Sendable {
  public var title: String
  public var goal: String?
  public var kind: String
  public var effortId: String?
  public var chats: [EffortEntity]?
  public var people: [EffortEntity]?
  public init(effort: Effort) {
    title = String(effort.title.prefix(60)); goal = String(effort.summary.prefix(1000)); kind = "long"
    effortId = effort.id; chats = effort.chats; people = effort.people
  }
}
public struct TaskPatch: Encodable, Sendable {
  public var summary: TaskSummaryText?
  public var summaryAttempt: TaskSummaryAttempt?
  public var title: String?
  public var goal: String?
  public var status: String?
  public var kind: String?
  public var lifespanDays: Int?
  public var checkIn: TaskCheckIn?
  public var waiting: TaskWaiting?
  public var clearWaiting = false
  public var clearCheckIn = false
  public init(summary: TaskSummaryText? = nil, summaryAttempt: TaskSummaryAttempt? = nil, status: String? = nil, lifespanDays: Int? = nil, checkIn: TaskCheckIn? = nil,
              waiting: TaskWaiting? = nil, clearWaiting: Bool = false, clearCheckIn: Bool = false) {
    self.summary = summary; self.summaryAttempt = summaryAttempt
    self.status = status; self.lifespanDays = lifespanDays; self.checkIn = checkIn
    self.waiting = waiting; self.clearWaiting = clearWaiting; self.clearCheckIn = clearCheckIn
  }
  enum CodingKeys: String, CodingKey { case summary, summaryAttempt, title, goal, status, kind, lifespanDays, checkIn, waiting }
  public func encode(to encoder: any Encoder) throws {
    var c = encoder.container(keyedBy: CodingKeys.self)
    // The engine stamps the date. An empty display timestamp is not a valid Go time.Time.
    struct SummaryWrite: Encodable { var text: String; var updatedBy: String }
    try c.encodeIfPresent(summary.map { SummaryWrite(text: $0.text, updatedBy: $0.updatedBy) }, forKey: .summary)
    try c.encodeIfPresent(summaryAttempt, forKey: .summaryAttempt)
    try c.encodeIfPresent(title, forKey: .title); try c.encodeIfPresent(goal, forKey: .goal)
    try c.encodeIfPresent(status, forKey: .status); try c.encodeIfPresent(kind, forKey: .kind)
    try c.encodeIfPresent(lifespanDays, forKey: .lifespanDays)
    if clearWaiting { try c.encodeNil(forKey: .waiting) } else { try c.encodeIfPresent(waiting, forKey: .waiting) }
    if clearCheckIn { try c.encodeNil(forKey: .checkIn) } else { try c.encodeIfPresent(checkIn, forKey: .checkIn) }
  }
}
public struct TaskClient: Sendable {
  public let backend: any Backend
  public init(backend: any Backend) { self.backend = backend }
  public static func path(_ id: String) -> String {
    "/v1/tasks/" + (id.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed.subtracting(CharacterSet(charactersIn: "/?#%"))) ?? "")
  }
  public func get(_ id: String) async throws -> TrackedTask { try await backend.get(Self.path(id)) }
  public func create(_ request: CreateTaskRequest) async throws -> TrackedTask {
    try await mutate("/v1/tasks", method: "POST", body: request)
  }
  public func patch(_ id: String, _ request: TaskPatch) async throws -> TrackedTask {
    try await mutate(Self.path(id), method: "PATCH", body: request)
  }
  public func step(_ id: String, step: String, done: Bool) async throws -> TrackedTask {
    let component = Self.path(step).replacingOccurrences(of: "/v1/tasks/", with: "")
    return try await mutate(Self.path(id) + "/checklist/" + component, method: "PATCH", body: ["done": done])
  }
  public func skipNudge(_ id: String) async throws -> TrackedTask {
    try await mutate(Self.path(id) + "/nudge/skip", method: "POST", body: Empty())
  }
  private func mutate<T: Encodable & Sendable>(_ path: String, method: String, body: T) async throws -> TrackedTask {
    try Wire.decode(TrackedTask.self, from: await backend.request(path, method: method, body: Wire.data(body)))
  }
}

public enum TaskSchedule {
  /// Local wall time follows calendar/DST rules. The engine remains authoritative on future slots.
  public static func next(rule: String, time: Date, weekday: Int, everyDays: Int, now: Date,
                          calendar: Calendar = .current) -> Date? {
    if rule == "once" { return time > now ? time : nil }
    let clock = calendar.dateComponents([.hour, .minute], from: time)
    if rule == "weekly" {
      var match = clock; match.weekday = min(6, max(0, weekday)) + 1
      return calendar.nextDate(after: now, matching: match, matchingPolicy: .nextTime)
    }
    guard rule == "every_n_days", let day = calendar.date(byAdding: .day, value: min(60, max(1, everyDays)), to: now) else { return nil }
    return calendar.date(bySettingHour: clock.hour ?? 18, minute: clock.minute ?? 0, second: 0, of: day)
  }
}

/// The status-only task feed carries summaries and fixed event labels, without workspace prose.
public struct TaskFeedItem: Decodable, Sendable {
  public var task: TaskSummary
  public var createdAt: String
  public var updatedAt: String
  public var closedAt: String?
  public var events: [TaskFeedEvent]
  enum CodingKeys: String, CodingKey { case createdAt, updatedAt, closedAt, events }
  public init(from decoder: any Decoder) throws {
    task = try TaskSummary(from: decoder)
    let c = try decoder.container(keyedBy: CodingKeys.self)
    createdAt = try c.decode(String.self, forKey: .createdAt)
    updatedAt = try c.decode(String.self, forKey: .updatedAt)
    closedAt = try c.decodeIfPresent(String.self, forKey: .closedAt)
    events = try c.decodeIfPresent([TaskFeedEvent].self, forKey: .events) ?? []
  }
}
public struct TaskFeedResponse: Decodable, Sendable {
  public var tasks: [TaskFeedItem]
}
