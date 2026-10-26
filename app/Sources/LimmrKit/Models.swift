import Foundation

public enum Wire {
  public static func decode<T: Decodable>(_ type: T.Type, from data: Data, route: String = "model", strictLists: Bool = false) throws -> T {
    let decoder = decoder()
    decoder.userInfo[.wireStrictLists] = strictLists
    decoder.userInfo[.wireRoute] = route.components(separatedBy: "?")[0]
    return try decoder.decode(type, from: data)
  }
  public static func decoder() -> JSONDecoder {
    let decoder = JSONDecoder()
    decoder.keyDecodingStrategy = .convertFromSnakeCase
    return decoder
  }
  public static func encoder() -> JSONEncoder {
    let encoder = JSONEncoder()
    encoder.keyEncodingStrategy = .convertToSnakeCase
    encoder.outputFormatting = [.sortedKeys]
    return encoder
  }
  public static func data<T: Encodable>(_ value: T) throws -> Data { try encoder().encode(value) }
}

public struct Agent: Codable, Identifiable, Sendable, Equatable {
  public var icon: EntityIcon? = nil
  public var preset: String? = nil
  public var id: String
  public var name: String
  public var kind: String
  public var transport: String
  public var createdAt: String
  public var lastSeenAt: String?
  public var enabled: Bool
  public var permissions: Permissions? = nil
  public var access: AgentAccess? = nil
  public var accessSummary: String? = nil
  public var symbol: String {
    switch kind {
    case "codex": "terminal"
    case "chatgpt": "bubble.left.and.bubble.right"
    case "claude_code": "chevron.left.forwardslash.chevron.right"
    case "claude_ai": "asterisk"
    case "cursor": "cursorarrow.rays"
    default: "sparkle"
    }
  }
}

public struct ChatScope: Codable, Sendable, Equatable {
  public var mode: String = "all"
  public var jids: [String] = []
  public init(mode: String = "all", jids: [String] = []) {
    self.mode = mode
    self.jids = jids
  }
  public var summary: String {
    switch mode {
    case "only":
      jids.isEmpty
        ? "Cannot read any chats" : "Can read \(jids.count) chat\(jids.count == 1 ? "" : "s")"
    case "all_except": "Can read all chats except \(jids.count)"
    default: "Can read all visible chats"
    }
  }
}

public struct Permissions: Codable, Sendable, Equatable {
  public var chats: ChatScope
  public var readMessages: Bool
  public var search: Bool
  public var readPeople: Bool
  public var writePeople: Bool
  public var saveSummaries: Bool
  public var downloadMedia: Bool
  public var requestSend: Bool
  public var access: AgentAccess? = nil
  public var sync: Bool? = nil
  public static func preset(_ kind: String) -> Self {
    var p = defaults(remote: kind == "given")
    p.access = kind == "given" ? AgentAccess(circles: .only([]), unsorted: false) : AgentAccess()
    return p
  }
  public static func defaults(remote: Bool) -> Self {
    .init(
      chats: .init(), readMessages: true, search: true, readPeople: true, writePeople: !remote,
      saveSummaries: !remote, downloadMedia: !remote, requestSend: !remote)
  }
}

public struct Approval: Codable, Identifiable, Sendable, Equatable {
  public var id: String
  public var agentId: String
  public var chatJid: String
  public var kind: String
  public var text: String
  public var createdAt: String
  public var expiresAt: String
  public var state: String
  public var attachment: Attachment?
  public var edited: Bool = false
  public var replyTo: String? = nil
  public var messageId: String? = nil
  public var reason: String? = nil
  public var isPending: Bool { state == "pending" }
  public mutating func transition(to state: String, text: String? = nil) throws {
    let valid =
      isPending && ["sent", "failed", "denied", "expired", "approved", "edited"].contains(state)
      || ["approved", "edited"].contains(self.state) && ["sent", "failed"].contains(state)
    guard valid else { throw BackendError.invalidTransition }
    if let text {
      edited = text != self.text
      self.text = text
    }
    self.state = state
  }
}
public struct Attachment: Codable, Sendable, Equatable {
  public var id: String
  public var name: String
  public var size: Int64
  public var sha256: String
}

public struct Chat: Codable, Identifiable, Sendable, Equatable {
  public var jid: String
  public var name: String
  public var isGroup: Bool = false
  public var lastMessageTime: String? = nil
  public var id: String { jid }
}
public struct Message: Codable, Identifiable, Sendable, Equatable {
  public var id: String
  public var chatJid: String
  public var sender: String
  public var text: String
  public var timestamp: String
  public var isFromMe: Bool = false
  public var mediaType: String? = nil
  public var filename: String? = nil
  public var quotedMessageId: String? = nil
  public var deletedAt: String? = nil
  /// Preserve the wire text for processing; use this label when showing a message.
  public var displayText: String {
    if !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { return text }
    switch mediaType {
    case "sticker": return "Sticker"
    case "image": return "Photo"
    case "video": return "Video"
    case "audio", "ptt": return "Voice message"
    default:
      if let filename, !filename.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { return filename }
      return mediaType == "document" ? "Document" : "Message"
    }
  }
  enum CodingKeys: String, CodingKey {
    case id, chatJid, sender
    case text = "content"
    case timestamp, isFromMe, mediaType, filename, quotedMessageId, deletedAt
  }
}
extension Message {
  public init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    id = try c.decode(String.self, forKey: .id)
    chatJid = try c.decode(String.self, forKey: .chatJid)
    sender = try c.decode(String.self, forKey: .sender)
    // The engine omits content on captionless media; older responses may send null.
    text = try c.decodeIfPresent(String.self, forKey: .text) ?? ""
    timestamp = try c.decode(String.self, forKey: .timestamp)
    isFromMe = try c.decodeIfPresent(Bool.self, forKey: .isFromMe) ?? false
    mediaType = try c.decodeIfPresent(String.self, forKey: .mediaType)
    filename = try c.decodeIfPresent(String.self, forKey: .filename)
    quotedMessageId = try c.decodeIfPresent(String.self, forKey: .quotedMessageId)
    deletedAt = try c.decodeIfPresent(String.self, forKey: .deletedAt)
  }
}
public struct Activity: Codable, Identifiable, Sendable, Equatable {
  public var ownerInstruction: String? = nil
  public var queueForApproval: Bool? = nil
  public var instructionReason: String? { ownerInstruction.map { "Because you said: \($0)" } }
  public var status: String? = nil
  public var id: Int64
  public var at: String
  public var agentId: String
  public var tool: String
  public var kind: String
  public var chatJids: [String]
  public var messageIds: [String]
  public var count: Int
  public var query: String?
  public var note: String?
  public var circleId: String? = nil
  public var briefVersion: Int? = nil
  public var confirmedVia: String? = nil
  public var verb: String {
    if tool == "circle_structure_placed" { return note ?? "Limmr placed \(count) people" }
    if tool == "forget_person" { return "Forgot a person" }
    if tool == "review_person_detail" { return "Reviewed a profile suggestion" }
    if tool == "update_brief" { return "Updated a circle brief" }
    if tool == "draft_message", kind == "suggestion" { return "Parked as a suggestion" }
    return switch kind {
    case "read": "Read \(count) message\(count == 1 ? "" : "s")"
    case "search": query.map { "Searched “\($0)”" } ?? "Searched messages"
    case "summary": "Saved a summary"
    case "send_requested": "Asked to send a message"
    case "sent": confirmedVia == "chat" ? "Sent · confirmed in chat" : "Sent"
    case "brief_updated": "Updated a circle brief"
    case "suggestion", "suggested", "send_parked": "Parked as a suggestion"
    case "people_write": "Added a note"
    case "people_read": "Looked up a person"
    case "approved": "Approved a message"
    case "denied": "Denied a message"
    default: kind.replacingOccurrences(of: "_", with: " ").capitalized
    }
  }
}
public struct HealthCheck: Codable, Identifiable, Sendable, Equatable {
  public var id: String
  public var title: String
  public var state: String
  public var detail: String
  public var fixId: String?
}
public struct PersonPhoto: Codable, Sendable, Equatable {
  public var source: String
}
public struct Person: Codable, Identifiable, Sendable, Equatable {
  public var otherNames: [String] = []
  public var photo: PersonPhoto? = nil
  public var icon: EntityIcon? = nil
  public var id: String
  public var jids: [String]
  public var name: String
  public var pushName: String?
  public var label: String?
  public var circle: String
  public var role: String
  public var howKnown: String
  public var notes: String
  public var updatedAt: String
  public var details: PersonDetails? = nil
  public var detailSuggestions: [DetailSuggestion]? = nil
  public var groupsInCommon: [GroupInCommon]? = nil
  public var circleAffinity: [CircleAffinity]? = nil
  public var primaryCircleId: String? = nil
  public var inferredRelationship: RelationshipInference? = nil

  enum CodingKeys: String, CodingKey {
    case otherNames, photo, icon, id, jids, name, pushName, label, circle, role, howKnown, notes, updatedAt, details,
      detailSuggestions, groupsInCommon, circleAffinity, primaryCircleId, inferredRelationship
  }
  public init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    otherNames = try c.decodeIfPresent([String].self, forKey: .otherNames) ?? []
    photo = try c.decodeIfPresent(PersonPhoto.self, forKey: .photo)
    icon = try c.decodeIfPresent(EntityIcon.self, forKey: .icon)
    id = try c.decode(String.self, forKey: .id)
    jids = try c.decodeIfPresent([String].self, forKey: .jids) ?? []
    name = try c.decode(String.self, forKey: .name)
    pushName = try c.decodeIfPresent(String.self, forKey: .pushName)
    label = try c.decodeIfPresent(String.self, forKey: .label)
    circle = try c.decodeIfPresent(String.self, forKey: .circle) ?? ""
    role = try c.decodeIfPresent(String.self, forKey: .role) ?? ""
    howKnown = try c.decodeIfPresent(String.self, forKey: .howKnown) ?? ""
    notes = try c.decodeIfPresent(String.self, forKey: .notes) ?? ""
    updatedAt = try c.decodeIfPresent(String.self, forKey: .updatedAt) ?? ""
    details = try c.decodeIfPresent(PersonDetails.self, forKey: .details)
    detailSuggestions = try c.decodeIfPresent([DetailSuggestion].self, forKey: .detailSuggestions)
    groupsInCommon = try c.decodeIfPresent([GroupInCommon].self, forKey: .groupsInCommon)
    circleAffinity = try c.decodeIfPresent([CircleAffinity].self, forKey: .circleAffinity)
    primaryCircleId = try c.decodeIfPresent(String.self, forKey: .primaryCircleId)
    inferredRelationship = try c.decodeIfPresent(RelationshipInference.self, forKey: .inferredRelationship)
  }
  public init(
    id: String, jids: [String], name: String, pushName: String? = nil, label: String? = nil,
    circle: String = "", role: String = "", howKnown: String = "", notes: String = "",
    updatedAt: String = "", details: PersonDetails? = nil,
    detailSuggestions: [DetailSuggestion]? = nil
  ) {
    self.id = id
    self.jids = jids
    self.name = name
    self.pushName = pushName
    self.label = label
    self.circle = circle
    self.role = role
    self.howKnown = howKnown
    self.notes = notes
    self.updatedAt = updatedAt
    self.details = details
    self.detailSuggestions = detailSuggestions
  }
}
public struct Fact: Codable, Identifiable, Sendable, Equatable {
  public var id: String
  public var subject: String
  public var text: String
  public var sourceAgent: String
  public var evidenceMessageIds: [String]
  public var createdAt: String
  public var status: String
  public var subjectId: String = ""
  public var evidenceChatJids: [String] = []
  enum CodingKeys: String, CodingKey {
    case id, subject, subjectId, text
    case sourceAgent = "source"
    case evidenceMessageIds, evidenceChatJids, createdAt, status
  }
}
public struct PeopleChange: Codable, Identifiable, Sendable, Equatable {
  public var id: String
  public var at: String
  public var source: String
  public var kind: String
  public var subjectId: String
  public var before: Person?
  public var after: Person?
  public var reverted: Bool
}
public struct Relation: Codable, Identifiable, Sendable, Equatable {
  public var id: String
  public var a: String
  public var b: String
  public var kind: String
  public var note: String
  public var source: String
}
public struct PersonDetail: Codable, Sendable {
  public var person: Person
  public var groups: [PeopleGroup]
  public var facts: [Fact]
  public var relations: [Relation]
  public var stats: PersonStats
}
public struct PersonStats: Codable, Sendable {
  // The two numeric suffixes include an underscore on the wire.
  private enum EncodeKeys: String, CodingKey {
    case sharedGroups, lastDm, messagesInSharedGroups
    case dmMessages30 = "dm_messages_30"
    case dmMessages365 = "dm_messages_365"
  }
  public func encode(to encoder: any Encoder) throws {
    var c = encoder.container(keyedBy: EncodeKeys.self)
    try c.encode(sharedGroups, forKey: .sharedGroups)
    try c.encode(dmMessages30, forKey: .dmMessages30)
    try c.encode(dmMessages365, forKey: .dmMessages365)
    try c.encodeIfPresent(lastDm, forKey: .lastDm)
    try c.encode(messagesInSharedGroups, forKey: .messagesInSharedGroups)
  }
  public var sharedGroups: [String]
  public var dmMessages30: Int
  public var dmMessages365: Int
  public var lastDm: String?
  public var messagesInSharedGroups: Int = 0
}
public struct GraphNode: Codable, Identifiable, Sendable, Equatable {
  public var id: String
  public var name: String
  public var kind: String
  public var circle: String
  public var groupsInCommon: [GroupInCommon]? = nil
  public var weight: Double
  enum CodingKeys: String, CodingKey { case id, name, kind, circle, weight, groupsInCommon }
  public init(id: String, name: String, kind: String, circle: String, weight: Double) {
    self.id = id
    self.name = name
    self.kind = kind
    self.circle = circle
    self.weight = weight
  }
  public init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    id = try c.decode(String.self, forKey: .id)
    name = try c.decode(String.self, forKey: .name)
    kind = try c.decode(String.self, forKey: .kind)
    circle = try c.decodeIfPresent(String.self, forKey: .circle) ?? ""
    groupsInCommon = try c.decodeIfPresent([GroupInCommon].self, forKey: .groupsInCommon)
    weight = try c.decode(Double.self, forKey: .weight)
  }
}
public struct GraphEdge: Codable, Sendable, Equatable {
  public var source: String
  public var target: String
  public var weight: Double
  public var recent: Bool
  public var kind: String = "dm"
  enum CodingKeys: String, CodingKey {
    case source = "a"
    case target = "b"
    case weight, recent, kind
  }
}
public struct Graph: Codable, Sendable, Equatable {
  public var nodes: [GraphNode]
  public var edges: [GraphEdge]
  public init(nodes: [GraphNode], edges: [GraphEdge]) {
    self.nodes = nodes
    self.edges = edges
  }
}
public struct Status: Codable, Sendable {
  public var engineVersion: String? = nil
  public var linked: Bool
  public var connected: Bool
  public var accountName: String
  public var pendingApprovals: Int
  public var agents: [StatusAgent]
  public var health: String
}
public struct StatusAgent: Codable, Sendable {
  public var name: String
  public var lastSeenAt: String?
}
public struct LinkEvent: Codable, Sendable {
  public var state: String
  public var code: String?
  public var expiresIn: Int?
  public var progress: Double?
  public var chats: Int?
  public var messages: Int?
  public var account: LinkAccount?
  public var reason: String?
  public var fix: String?
  public init(state: String) { self.state = state }
}
public struct LinkAccount: Codable, Sendable {
  public var name: String
  public var phoneMasked: String
}
public struct RemoteState: Codable, Sendable {
  public var enabled: Bool
  public var publicUrl: String
  public var mcpUrl: String
  public var port: Int
  public var pendingCodes: [RemoteCode]
  public var clients: [RemoteClient]
  enum CodingKeys: String, CodingKey {
    case enabled, publicUrl, mcpUrl, port
    case pendingCodes = "pending"
    case clients
  }
}
public struct RemoteCode: Codable, Identifiable, Sendable {
  public var sync: Bool? = nil
  public var code: String
  public var clientName: String
  public var kind: String
  public var returnsTo: String
  public var requestedAt: String
  public var expiresAt: String
  public var state: String
  public var id: String { code }
  enum CodingKeys: String, CodingKey {
    case code
    case clientName = "name"
    case kind, returnsTo, requestedAt, expiresAt, state, sync
  }
}
public struct RemoteClient: Codable, Identifiable, Sendable {
  public var id: String
  public var name: String
  public var redirectUris: [String]
  public var tokenEndpointAuthMethod: String
  public var agentId: String
  public var approvedAt: String
  public var lastUse: String?
  public var expiresAt: String
  enum CodingKeys: String, CodingKey {
    case id = "clientId"
    case name, redirectUris, tokenEndpointAuthMethod, agentId, approvedAt, lastUse, expiresAt
  }
}
public struct AppSettings: Codable, Sendable, Equatable {
  public var accessoryCue = AccessoryCue()
  public var autoCircles: Bool = true
  public var maxCircles: Int = 8
  public var hiddenChats: [String]
  public var retentionDays: Int
  public var currentDays: Int
  public var launchAtLogin: Bool
  public var owner: String
  public init(
    hiddenChats: [String] = [], retentionDays: Int = 180, launchAtLogin: Bool = false,
    owner: String = "Alex", currentDays: Int = 180
  ) {
    self.hiddenChats = hiddenChats
    self.retentionDays = retentionDays
    self.currentDays = currentDays
    self.launchAtLogin = launchAtLogin
    self.owner = owner
  }
  enum CodingKeys: String, CodingKey {
    case autoCircles, maxCircles, hiddenChats, retentionDays, currentDays, owner, accessoryCue
  }
  public init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    accessoryCue = try c.decodeIfPresent(AccessoryCue.self, forKey: .accessoryCue) ?? .init()
    autoCircles = try c.decodeIfPresent(Bool.self, forKey: .autoCircles) ?? true
    maxCircles = try c.decodeIfPresent(Int.self, forKey: .maxCircles) ?? 8
    hiddenChats = try c.decodeIfPresent([String].self, forKey: .hiddenChats) ?? []
    retentionDays = try c.decode(Int.self, forKey: .retentionDays)
    currentDays = try c.decodeIfPresent(Int.self, forKey: .currentDays) ?? 180
    owner = try c.decode(String.self, forKey: .owner)
    launchAtLogin = false
  }
}
public struct CreatedAgent: Codable, Sendable {
  public var agent: Agent
  public var token: String?
}
public struct Empty: Codable, Sendable { public init() {} }
public enum BackendError: Error, LocalizedError, Sendable {
  case http(Int)
  case starting(progress: String)
  case invalidResponse, invalidTransition
  case unavailable(String)
  public var errorDescription: String? {
    switch self {
    case .http(let code): "The engine returned an error (\(code))."
    case .starting: "The background helper is starting…"
    case .invalidResponse: "The engine response could not be read."
    case .invalidTransition: "This request has already been resolved."
    case .unavailable(let reason): reason
    }
  }
}

// Named response envelopes are part of the v1 contract; paging metadata is retained.
public struct AgentsResponse: Codable, Sendable { public var agents: [Agent] }
public struct ApprovalsResponse: Codable, Sendable { public var approvals: [Approval] }
public struct ChatsResponse: Codable, Sendable {
  public var chats: [Chat]
  public var nextCursor: String? = nil
}
public struct MessagesResponse: Codable, Sendable { public var messages: [Message] }
public struct PeopleResponse: Codable, Sendable {
  public var people: [Person]
  public var nextCursor: String? = nil
}
public struct ChangesResponse: Codable, Sendable { public var changes: [PeopleChange] }
public struct ActivityPage: Codable, Sendable {
  public var items: [Activity]
  public var nextBefore: Int64?
}
public struct HealthResponse: Codable, Sendable {
  public var checks: [HealthCheck]
  public var health: String
}
public struct PeopleGroup: Codable, Identifiable, Sendable, Equatable {
  public var icon: EntityIcon? = nil
  public var isCommunity: Bool? = nil
  public var parentJid: String? = nil
  public var lastActivity: String? = nil
  public var messages30: Int? = nil
  public var circleIds: [String]? = nil
  public var taskIds: [String]? = nil
  public var muted: Bool? = nil
  public var archived: Bool? = nil
  public var photo: Bool? = nil
  public var agentAccess: [GroupAccess]? = nil
  public var jid: String
  public var name: String
  public var description: String
  public var memberCount: Int
  public var iAmAdmin: Bool
  public var id: String { jid }
}
public struct GroupMember: Codable, Sendable {
  public var person: Person
  public var admin: Bool
}
public struct GroupAccess: Codable, Identifiable, Sendable, Equatable {
  public var agentId: String
  public var name: String
  public var read: Bool
  public var send: String
  public var id: String { agentId }
}
public struct ChatSummary: Codable, Sendable {
  public var text: String
  public var agentId: String
  public var createdAt: String
}
public struct PersonSummary: Codable, Sendable {
  public var personId: String
  public var text: String
  public var writtenBy: String
  public var writtenAt: String
  public var basedOnUntil: String
}
public struct PersonSummaryResponse: Codable, Sendable { public var summary: PersonSummary? }
public struct GroupSender: Codable, Sendable {
  public var jid: String
  public var name: String
}
public struct GroupDetail: Codable, Sendable {
  public var recent: [Message]? = nil
  public var senders: [GroupSender]? = nil
  public var summary: ChatSummary? = nil
  public var group: PeopleGroup
  public var members: [GroupMember]
  public var facts: [Fact]
}
public struct GroupsResponse: Codable, Sendable { public var groups: [PeopleGroup] }
public struct MigrationPlan: Codable, Sendable {
  public var available: Bool
  public var dryRun: Bool
  public var steps: [String]
  public var canUndo: Bool
  public var restartRequired: Bool
}
/// Fixed startup metadata only; never include an arbitrary server message in logs.
public struct EngineStartingResponse: Decodable, Sendable {
  public let status: String
  public let stage: String
  public let progress: UInt64
  public var progressKey: String { "\(stage):\(progress)" }
}
