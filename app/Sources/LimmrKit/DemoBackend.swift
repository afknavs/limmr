import Foundation

/// Matches engine/internal/demo/demo.go: names, directory, memberships, message text and agent kinds.
/// UI-only QR, health, OAuth request and resolved-history fixtures are synthetic additions.
public enum DemoData {
  public static let now = "2026-10-17T12:00:00Z"
  static let epoch = ISO8601DateFormatter().date(from: now)!
  static func date(_ minutes: Int) -> String {
    ISO8601DateFormatter().string(from: epoch.addingTimeInterval(Double(-minutes * 60)))
  }
  public static let agents: [Agent] = zip(
    ["Claude Code", "Codex", "ChatGPT"],
    ["claude_code", "codex", "chatgpt"]
  ).enumerated().map { i, pair in
    let permission = Permissions.preset("own")
    return .init(
      preset: "own", id: "agent-\(i + 1)", name: pair.0, kind: pair.1,
      transport: i == 2 ? "remote" : "local", createdAt: "2026-09-17T12:00:00Z",
      lastSeenAt: date((i + 1) * 5), enabled: true, permissions: permission)
  }
  public static let groupNames = DemoPeople.groupNames
  public static func groupJid(_ i: Int) -> String { String(format: "120363000%03d@g.us", i + 1) }
  public static func personId(_ i: Int) -> String {
    i == 0 ? "me" : String(format: "person-%02d", i + 1)
  }
  public static func personJid(_ i: Int) -> String {
    String(format: "1555010%04d@s.whatsapp.net", i + 1)
  }
  public static let people = DemoPeople.people
  public static let chats: [Chat] =
    groupNames.enumerated().map {
      Chat(
        jid: groupJid($0.offset), name: $0.element, isGroup: true,
        lastMessageTime: date(10 + $0.offset * 10))
    }
    + people.enumerated().map {
      Chat(
        jid: personJid($0.offset), name: $0.element.name,
        lastMessageTime: date($0.offset == 12 ? 730 * 1440 : 2 + $0.offset * 9))
    }
    + ["Former school batch", "Old chess club", "Alumni notices"].enumerated().map {
      Chat(
        jid: groupJid(9 + $0.offset), name: $0.element, isGroup: true,
        lastMessageTime: date(730 * 1440))
    }
    + [Chat(jid: groupJid(12), name: "Campus network", isGroup: true, lastMessageTime: date(60))]
  public static let groups = DemoPeople.groups
  /// Group messages come from that group's members, as they would in WhatsApp.
  static func groupSender(_ group: Int, _ j: Int) -> String {
    let members = group < DemoPeople.members.count ? DemoPeople.members[group].filter { $0 != 0 } : []
    return members.isEmpty ? personJid((group * 4 + j) % 40) : personJid(members[j % members.count])
  }
  public static let messages: [Message] = {
    var result: [Message] = []
    for i in 0..<min(40, people.count) {
      for j in 0..<(i == 3 ? 5 : 4) {
        result.append(
          .init(
            id: String(format: "dm-%02d-%02d", i + 1, j + 1), chatJid: personJid(i),
            sender: j % 2 == 1 ? "me" : personJid(i),
            text: [
              "Shall we meet at the studio?", "The draft is ready for review.",
              "Thanks, I will bring the notes.", "See you on Saturday.",
            ][j % 4], timestamp: date(i == 12 ? 730 * 1440 : (i + 1 + j) * 60), isFromMe: j % 2 == 1))
      }
    }
    for i in 0..<groupNames.count {
      for j in 0..<10 {
        result.append(
          .init(
            id: String(format: "group-%02d-%02d", i + 1, j + 1), chatJid: groupJid(i),
            sender: groupSender(i, j),
            text: i == 4 && j == 0 ? "" : "The next session is on Saturday. Bring one idea to share.",
            timestamp: date((i + 1) * 20 + j * 15), mediaType: i == 4 && j == 0 ? "sticker" : nil))
      }
    }
    return result
  }()
  public static let approvals: [Approval] = [
    .init(
      id: "a1", agentId: "agent-1", chatJid: groupJid(0), kind: "message",
      text:
        "I can bring the revised sketches to Saturday's session.",
      createdAt: now, expiresAt: "2026-10-17T12:30:00Z", state: "pending"),
    .init(
      id: "a2", agentId: "agent-2", chatJid: groupJid(1), kind: "message",
      text: "Shall we start the walk at nine?", createdAt: now, expiresAt: "2026-10-17T12:30:00Z",
      state: "pending"),
    .init(
      id: "a3", agentId: "agent-1", chatJid: groupJid(0), kind: "message",
      text: "Thanks, I will bring the notes.", createdAt: date(70), expiresAt: now, state: "sent"),
  ]
  public static let activity: [Activity] = {
    [
      .init(id: 25, at: date(4), agentId: "agent-1", tool: "list_messages", kind: "read", chatJids: [groupJid(1)], messageIds: [], count: 3),
      .init(id: 24, at: date(2), agentId: "agent-1", tool: "list_messages", kind: "read", chatJids: [groupJid(0)], messageIds: [], count: 0),
      .init(ownerInstruction: "Send the revised sketches now.", status: "delivered", id: 23, at: date(8), agentId: "agent-2", tool: "send_draft", kind: "sent", chatJids: [groupJid(0)], messageIds: [], count: 1, note: "The revised sketches are ready for Saturday.") ,
      .init(
        id: 22, at: date(1), agentId: "agent-1", tool: "update_brief", kind: "brief_updated",
        chatJids: [groupJid(7)], messageIds: [], count: 1,
        note: "Kitchen layout approved. Darker tiles requested.", circleId: "atlas", briefVersion: 3
      ),
      .init(
        id: 21, at: date(2), agentId: "agent-3", tool: "send_draft", kind: "sent",
        chatJids: [groupJid(2)], messageIds: [], count: 1,
        note: "I’ll bring the outline on Monday.", confirmedVia: "chat"),
      .init(
        id: 20, at: date(3), agentId: "agent-2", tool: "draft_message", kind: "suggestion",
        chatJids: [groupJid(2)], messageIds: [], count: 1,
        note: "Research outline reply is waiting on Home.", circleId: "university"),
      .init(
        id: 19, at: date(5), agentId: "agent-1", tool: "list_messages", kind: "read",
        chatJids: [groupJid(0)], messageIds: ["group-01-01", "group-01-02"], count: 14),
      .init(
        id: 18, at: date(10), agentId: "agent-2", tool: "save_summary", kind: "summary",
        chatJids: [groupJid(1)], messageIds: [], count: 3,
        note:
          "Over the last three days, Weekend Walks agreed to meet at nine on Sunday and bring water."
      ),
      .init(
        id: 17, at: date(20), agentId: "agent-3", tool: "search_messages", kind: "search",
        chatJids: [groupJid(2)], messageIds: [], count: 6, query: "rent"),
      .init(
        id: 16, at: date(35), agentId: "agent-1", tool: "add_fact", kind: "people_write",
        chatJids: [personJid(3)], messageIds: [], count: 1,
        note: "Maya prepares material samples for Atlas and is also a classmate."),
      .init(
        id: 15, at: date(55), agentId: "you", tool: "approve", kind: "approved",
        chatJids: [groupJid(0)], messageIds: [], count: 1,
        note: "Thanks, I will bring the notes."),
      .init(
        id: 14, at: date(60), agentId: "agent-1", tool: "send_message", kind: "send_requested",
        chatJids: [groupJid(0)], messageIds: [], count: 1),
      .init(
        id: 13, at: date(75), agentId: "you", tool: "deny", kind: "denied", chatJids: [groupJid(1)],
        messageIds: [], count: 1, note: "Codex’s proposed message was denied. Nothing was sent."),
      .init(
        id: 12, at: date(90), agentId: "agent-2", tool: "list_messages", kind: "read",
        chatJids: [groupJid(1)], messageIds: ["group-02-01"], count: 8),
      .init(
        id: 11, at: date(110), agentId: "agent-3", tool: "save_summary", kind: "summary",
        chatJids: [groupJid(2)], messageIds: [], count: 2,
        note: "The flatmates have agreed on this month’s shared costs."),
      .init(
        id: 10, at: date(140), agentId: "agent-1", tool: "get_person", kind: "people_read",
        chatJids: [personJid(1)], messageIds: [], count: 1),
      .init(
        id: 9, at: date(1500), agentId: "agent-2", tool: "search_messages", kind: "search",
        chatJids: [groupJid(1)], messageIds: [], count: 3, query: "Sunday"),
    ].sorted(by: Activity.newestFirst)
  }()
  public static let facts: [Fact] = [
    .init(
      id: "f1", subject: "person", text: "Prepares material samples for Atlas; also a classmate.",
      sourceAgent: "you",
      evidenceMessageIds: ["group-01-01"], createdAt: now, status: "active", subjectId: "person-04",
      evidenceChatJids: [groupJid(0)]),
    .init(
      id: "f2", subject: "person", text: "Coordinates the material library for the studio.",
      sourceAgent: "agent-2", evidenceMessageIds: [], createdAt: date(35), status: "suggested",
      subjectId: "person-04"),
    .init(
      id: "f3", subject: "person",
      text: "Works with the Atlas design studio. Sample of a coming-soon source.",
      sourceAgent: "gmail", evidenceMessageIds: [], createdAt: date(70), status: "active",
      subjectId: "person-04"),
  ]
  public static let changes: [PeopleChange] = [
    .init(id: "f1", at: now, source: "you", kind: "fact", subjectId: "person-04", reverted: false),
    .init(
      id: "r1", at: now, source: "you", kind: "relation", subjectId: "person-04", reverted: false),
  ]
  public static let health: [HealthCheck] = [
    .init(
      id: "connection", title: "WhatsApp connection", state: "ok",
      detail: "Connected. Your phone was seen a few minutes ago."),
    .init(id: "sync", title: "History sync", state: "ok", detail: "All 21 chats are up to date."),
    .init(id: "engine", title: "Engine", state: "ok", detail: "Running in the background."),
    .init(
      id: "db", title: "Message storage", state: "ok",
      detail: "Integrity check passed. Stored on this Mac."),
    .init(
      id: "local", title: "Local connections", state: "ok",
      detail: "Your local integrations can connect."),
    .init(
      id: "remote", title: "Remote access", state: "ok",
      detail: "Your public connection is reachable."),
    .init(
      id: "groups", title: "Group directory", state: "warn",
      detail: "Membership was last refreshed yesterday.", fixId: "refresh_groups"),
    .init(
      id: "logs", title: "Logs", state: "ok",
      detail: "Within the storage limit. No message content is logged."),
  ]
  public static func graph(count: Int = 22) -> Graph { DemoPeople.graph(count: count) }
}

/// Entirely in-memory. No WhatsApp client, filesystem, processes or network are used.
public actor DemoBackend: Backend {
  private var dotSync = DotSyncStatus.demo("never")
  private var groups = DemoPeople.groups
  private var customPhotos: [String: Data] = ["person-04": DemoPhotos.person, "person-05": DemoPhotos.person]
  private var contactsState = ContactsDemoState()
  private var personSummaries: [String: PersonSummary] = ["person-04": .init(personId: "person-04", text: "You and Maya have been preparing the studio presentation and comparing material samples. She is gathering the updated syllabus for your study group; the next step is to review the slides together.", writtenBy: "This Mac", writtenAt: DemoData.now, basedOnUntil: DemoData.now)]
  private struct ContactsDemoState: Codable { var enabled = false; var lastImportedAt: String? = nil }
  private var tasks = DemoTasks.tasks
  private var efforts = DemoEfforts.efforts
  private var effortNamingRevisions: [String: Int] = [:]
  private var watches = DemoWatches.details
  private var circleDetails = DemoCircles.details
  private var circleChanges: [CircleAutoChange] = []
  private var circleSuggestions = DemoCircles.suggestions + DemoTasks.suggestions
  private var circleProposals = DemoCircles.proposals.map { proposal in
    var value = proposal
    value.batchId = "batch-codex-1"
    value.createdBy = "agent-2"
    return value
  }
  private var briefSettings = BriefSettings()
  private var peopleInferenceEnabled: Bool?
  private var briefHistory: [String: [CircleBrief]] = Dictionary(
    uniqueKeysWithValues: DemoCircles.details.compactMap { detail in
      guard let latest = detail.brief else { return nil }
      var old = latest
      old.version = 2
      old.summary = "Earlier understanding: " + old.summary
      old.whatChanged = "Previous brief, before the latest messages."
      return (detail.circle.id, [latest, old])
    })
  private var agents = DemoData.agents
  private var permissions: [String: Permissions] = [:]
  private var approvals = DemoData.approvals
  private var activity = DemoData.activity
  private var people = DemoData.people
  private var facts = DemoData.facts
  private var changes = DemoData.changes
  private var settings = AppSettings()
  private var remote = RemoteState(
    enabled: true, publicUrl: "https://limmr.example",
    mcpUrl: "https://limmr.example/mcp", port: 18801,
    pendingCodes: [
      .init(
        code: "482916", clientName: "ChatGPT", kind: "chatgpt", returnsTo: "chatgpt.com",
        requestedAt: DemoData.now, expiresAt: "2026-10-17T12:10:00Z", state: "waiting")
    ],
    clients: [
      .init(
        id: "c1", name: "ChatGPT", redirectUris: ["https://chatgpt.com/callback"],
        tokenEndpointAuthMethod: "none", agentId: "agent-3", approvedAt: DemoData.now,
        expiresAt: "2026-10-17T13:00:00Z")
    ])
  private var health = DemoData.health
  private var completedAsks: Set<String> = []
  private func suggestionResult(_ suggestion: CircleSuggestion, object: [String: Any]) -> String {
    if suggestion.taskId != nil { return object["text"] as? String ?? suggestion.body ?? suggestion.draftText ?? "" }
    let home = DemoCircles.home(details: circleDetails, suggestions: [suggestion], proposals: [])
    let item = home.queued[0]
    let index = object["variation"] as? Int ?? 0
    return object["text"] as? String ?? item.variations[index]
  }
  private var waitingItems: [WaitingItem] {
    watches.map(\.summary).filter(\.isOpen).map { watch in
      WaitingItem(
        id: watch.id,
        agent: watch.id == "w_exam"
          ? "Claude Code" : watch.id == "w_feed" ? "ChatGPT" : watch.createdBy,
        kind: watch.kind, sentence: watch.goal, where: watch.where, since: watch.createdAt,
        newHits: watch.newHits, latest: watch.latest)
    }
  }
  private var ownerProfile = OwnerProfile.sample
  private var n1ReadyShape = false
  public init(n1ReadyShape: Bool = false, waitingCount: Int? = nil, structurePlacements: Bool = false,
    unnamedEffortsOnly: Bool = false, dotState: String = "never") {
    dotSync = .demo(dotState)
    if unnamedEffortsOnly { efforts = efforts.filter { $0.title.isEmpty } }
    if structurePlacements {
      let placed: [Person] = (1...40).map { i in
        var person = DemoData.people[3]
        person.id = "structure-member-\(i)"
        person.name = "Workshop member \(i)"
        person.pushName = person.name
        person.jids = [DemoData.personJid(100 + i)]
        return person
      }
      people += placed
      let circleIndex = circleDetails.firstIndex { $0.circle.id == "university" }!
      let placements = Dictionary(uniqueKeysWithValues: placed.map { ($0.id, "placement-" + $0.id) })
      circleDetails[circleIndex].people = placed.map {
        CirclePerson(personId: $0.id, role: "", note: "Member of several shared groups", source: "structure")
      }
      circleDetails[circleIndex].circle.people = circleDetails[circleIndex].people
      circleChanges = [CircleAutoChange(id: "structure-run", circleId: circleDetails[circleIndex].circle.id,
        name: circleDetails[circleIndex].circle.name, agent: "Limmr", action: "placed", at: DemoData.now,
        count: placed.count, changeIds: placed.map { placements[$0.id]! }, peopleIds: placed.map(\.id),
        peopleChanges: placements, runId: "demo-structure", byKind: "structure")]
      circleProposals = []
    }
    if let waitingCount { watches = Array(DemoWaiting.details.prefix(max(0, waitingCount))) }
    self.n1ReadyShape = n1ReadyShape
    if n1ReadyShape {
      circleProposals = []
      while circleDetails.count < 7 {
        var detail = circleDetails[0]
        detail.circle.id = "n1-\(circleDetails.count)"
        detail.circle.name = "Project \(circleDetails.count)"
        detail.brief = nil
        circleDetails.append(detail)
      }
    }
  }
  public func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if path.hasPrefix("/v1/sync") {
      if method == "DELETE" { dotSync = .demo("never") }
      if path == "/v1/sync/pair", method == "POST" { dotSync = .demo("healthy") }
      if path == "/v1/sync/pause", let body,
         let object = try JSONSerialization.jsonObject(with: body) as? [String: Bool] { dotSync.paused = object["paused"] ?? false }
      if path == "/v1/sync/lease", let body,
         let object = try JSONSerialization.jsonObject(with: body) as? [String: Bool] { dotSync.lease.thisMac = object["take"] ?? true }
      if path.contains("/reviews/"), method == "POST" { dotSync.mergeReviews = [] }
      return try Wire.data(dotSync)
    }
    let route = String(path.split(separator: "?").first ?? "")
    let parts = route.split(separator: "/").map { String($0).removingPercentEncoding ?? String($0) }
    let object = (try? JSONSerialization.jsonObject(with: body ?? Data())) as? [String: Any] ?? [:]
    if route == "/v1/settings/people/contacts" {
      if method == "PUT", let body { contactsState = try Wire.decode(ContactsDemoState.self, from: body) }
      return try Wire.data(contactsState)
    }
    if parts.count == 4, parts[1] == "people", parts[3] == "summary" {
      if method == "PUT" { personSummaries[parts[2]] = PersonSummary(personId: parts[2], text: object["text"] as? String ?? "", writtenBy: "This Mac", writtenAt: DemoData.now, basedOnUntil: DemoData.now) }
      return try Wire.data(PersonSummaryResponse(summary: personSummaries[parts[2]]))
    }
    if parts.count == 4, parts[1] == "people", parts[3] == "photo" {
      if method == "DELETE" { customPhotos[parts[2]] = nil } else { customPhotos[parts[2]] = body }
      if let i = people.firstIndex(where: { $0.id == parts[2] }) { people[i].photo = method == "DELETE" ? nil : PersonPhoto(source: "custom") }
      return Data("{}".utf8)
    }
    if parts.count == 4, parts[1] == "groups", parts[3] == "photo" {
      guard groups.first(where: { $0.jid == parts[2] })?.photo == true else { throw BackendError.http(404) }
      return DemoPhotos.group
    }
    if parts.count == 4, parts[1] == "groups", parts[3] == "access", let i = groups.firstIndex(where: { $0.jid == parts[2] }), let j = groups[i].agentAccess?.firstIndex(where: { $0.agentId == object["agent_id"] as? String }) {
      if let read = object["read"] as? Bool { groups[i].agentAccess?[j].read = read }
      if let send = object["send"] as? String { groups[i].agentAccess?[j].send = send }
      return Data("{}".utf8)
    }
    if parts.count >= 2, parts[1] == "tasks" {
      return try taskRequest(path, method: method, object: object)
    }
    if route == "/v1/settings/accessory-cue/test", method == "POST" { return Data(#"{"ok":true,"running":true}"#.utf8) }
    if parts.count == 3, method == "PATCH", object.keys.contains("icon") {
      let icon =
        object["icon"] is NSNull
        ? nil
        : try Wire.decode(
          EntityIcon.self, from: JSONSerialization.data(withJSONObject: object["icon"]!))
      if parts[1] == "groups", let i = groups.firstIndex(where: { $0.jid == parts[2] }) { groups[i].icon = icon; return try Wire.data(groups[i]) }
      if parts[1] == "agents", let i = agents.firstIndex(where: { $0.id == parts[2] }) {
        agents[i].icon = icon
        return try Wire.data(agents[i])
      }
      if parts[1] == "people", let i = people.firstIndex(where: { $0.id == parts[2] }) {
        people[i].icon = icon
        return try Wire.data(people[i])
      }
      if parts[1] == "circles",
        let i = circleDetails.firstIndex(where: { $0.circle.id == parts[2] })
      {
        circleDetails[i].circle.icon = icon
        return try Wire.data(circleDetails[i].circle)
      }
      throw BackendError.http(404)
    }
    if route == "/v1/suggestions", method == "POST", object["kind"] as? String == "nudge",
      object["created_by"] as? String == "this-mac", let taskID = object["task_id"] as? String,
      let i = circleSuggestions.firstIndex(where: { $0.taskId == taskID && $0.draftSource == "template" && $0.state == "pending" }),
      let text = object["draft_text"] as? String {
      circleSuggestions[i].draftText = text; circleSuggestions[i].body = text
      circleSuggestions[i].variations = [text]; circleSuggestions[i].draftSource = "this-mac"
      return try Wire.data(tasks.first { $0.id == taskID })
    }
    if route == "/v1/suggestions", method == "GET" {
      struct Response: Encodable { var suggestions: [CircleSuggestion] }
      return try Wire.data(Response(suggestions: circleSuggestions))
    }
    if route == "/v1/stats/history", method == "GET" { return try Wire.data(DemoData.history()) }
    if parts.count == 4, parts[1] == "circles", parts[3] == "seen", method == "POST" {
      return Data("{}".utf8)
    }
    if parts.count == 6, parts[1] == "circles", parts[3] == "asks", parts[5] == "draft",
      method == "POST"
    {
      let ask = DemoCircles.home(
        details: circleDetails, suggestions: circleSuggestions, proposals: []
      ).needsReply.first { $0.id == parts[4] }
      guard let ask, let agent = object["agent_id"] as? String,
        agents.contains(where: { $0.id == agent && $0.enabled })
      else { throw BackendError.http(404) }
      let suggestion = CircleSuggestion(
        id: "request-" + ask.id, circleId: ask.circleId, kind: "action", chatJid: ask.chatJid,
        why: "Draft a reply for review: “" + ask.text + "”", createdBy: agent,
        expiresAt: "2026-10-19T18:00:00Z")
      circleSuggestions.append(suggestion)
      // Simulate the selected AI answering the request, without running an AI or sending.
      circleSuggestions.append(CircleSuggestion(
        id: "demo-reply-" + ask.id, circleId: ask.circleId, kind: "reply", chatJid: ask.chatJid,
        draftText: "Thanks for checking. I’ll review it today and get back to you.",
        why: "Sample reply for review", createdBy: agent, expiresAt: "2026-10-19T18:00:00Z"))
      return try Wire.data(suggestion)
    }
    if route == "/v1/owner" {
      if method == "PUT" {
        if let v = object["name"] as? String {
          ownerProfile.name = v
          settings.owner = v
        }
        if let v = object["about"] as? String {
          guard v.count <= 600 else { throw BackendError.http(400) }
          ownerProfile.about = v
        }
        if let v = object["studies"] as? String { ownerProfile.studies = v }
        if let v = object["work"] as? String { ownerProfile.work = v }
        if let v = object["people"] {
          ownerProfile.people = try Wire.decode(
            [OwnerProfile.ClosePerson].self, from: JSONSerialization.data(withJSONObject: v))
        }
        if let v = object["projects"] {
          ownerProfile.projects = try Wire.decode(
            [OwnerProfile.Project].self, from: JSONSerialization.data(withJSONObject: v))
        }
        ownerProfile.updatedBy = "you"
        ownerProfile.updatedAt = DemoData.now
      }
      return try Wire.data(ownerProfile)
    }
    if parts.count == 5, parts[1] == "owner", parts[2] == "suggestions", method == "POST" {
      guard let i = ownerProfile.suggestions.firstIndex(where: { $0.id == parts[3] }) else {
        throw BackendError.http(404)
      }
      let v = ownerProfile.suggestions.remove(at: i)
      if parts[4] == "confirm", case .text(let text) = v.value {
        switch v.field {
        case "work": ownerProfile.work = text
        case "studies": ownerProfile.studies = text
        case "about": ownerProfile.about = text
        case "name": ownerProfile.name = text
        default: break
        }
      }
      return try Wire.data(ownerProfile)
    }
    if parts.count == 4, parts[1] == "agents", parts[3] == "preset", method == "POST" {
      guard let i = agents.firstIndex(where: { $0.id == parts[2] }),
        let kind = object["preset"] as? String, ["own", "given"].contains(kind)
      else { throw BackendError.http(400) }
      agents[i].preset = kind
      agents[i].permissions = .preset(kind)
      agents[i].access = agents[i].permissions?.access
      permissions[agents[i].id] = agents[i].permissions
      return try Wire.data(agents[i])
    }
    if route == "/v1/setup/status", method == "GET" {
      var value = SetupStatus(
        stage: circleProposals.isEmpty && circleDetails.isEmpty
          ? "new"
          : circleProposals.isEmpty
            ? (circleDetails.count >= 3
              ? "ready" : (circleDetails.contains { $0.brief == nil } ? "filling" : "ready"))
            : "sorting")
      value.owner = ownerProfile.name
      value.ownerProfile = ownerProfile
      value.linked = true
      value.chats =
        DemoData.chats.filter {
          CurrentChats.isCurrent($0, days: settings.currentDays, now: DemoData.epoch)
        }.count
      value.circles = .init(confirmed: circleDetails.count, proposed: circleProposals.count)
      value.people = .init(
        total: people.count, withoutDetails: people.filter { $0.details == nil }.count)
      value.circlesWithoutBrief = circleDetails.filter { $0.brief == nil }.map { $0.circle.id }
      value.unsortedActiveChats = n1ReadyShape ? (value.chats ?? 0) * 60 / 100 : 0
      if n1ReadyShape {
        value.people = .init(total: people.count, withoutDetails: people.count * 9 / 10)
      }
      value.nextSteps = []
      return try Wire.data(value)
    }
    if route == "/v1/circles/reset", method == "POST" {
      guard object["confirm"] as? Bool == true else { throw BackendError.http(400) }
      circleDetails = []
      circleProposals = []
      circleSuggestions = []
      briefHistory = [:]
      completedAsks = []
      activity.insert(
        Activity(
          id: (activity.map(\.id).max() ?? 0) + 1, at: DemoData.now, agentId: "you",
          tool: "reset_circles", kind: "people_write", chatJids: [], messageIds: [], count: 0,
          note: "Started circles over. Chats and people kept."), at: 0)
      return Data("{}".utf8)
    }
    if route == "/v1/settings/people" {
      if method == "PUT" {
        guard let enabled = object["enabled"] as? Bool else { throw BackendError.http(400) }
        peopleInferenceEnabled = enabled
      }
      return try Wire.data(["enabled": peopleInferenceEnabled])
    }
    if route == "/v1/settings/briefs" {
      if method == "PUT" {
        briefSettings = try Wire.decode(BriefSettings.self, from: body ?? Data())
      }
      return try Wire.data(briefSettings)
    }
    if route == "/v1/agents/this-mac/enable", method == "POST" {
      if !agents.contains(where: { $0.id == "this-mac" }) {
        agents.append(
          Agent(
            id: "this-mac", name: "This Mac", kind: "on_device", transport: "local",
            createdAt: DemoData.now, enabled: true))
      }
      return Data("{}".utf8)
    }
    if parts.count == 4, parts[1] == "agents", parts[3] == "access", method == "PATCH" {
      guard let i = agents.firstIndex(where: { $0.id == parts[2] }) else {
        throw BackendError.http(404)
      }
      let access = try Wire.decode(AgentAccess.self, from: body ?? Data())
      agents[i].access = access
      agents[i].accessSummary = access.summary(
        names: Dictionary(
          uniqueKeysWithValues: circleDetails.map { ($0.circle.id, $0.circle.name) }),
        at: DemoData.epoch)
      var permission =
        permissions[parts[2]] ?? agents[i].permissions
        ?? .defaults(remote: agents[i].transport == "remote")
      permission.access = access
      permissions[parts[2]] = permission
      agents[i].permissions = permission
      return try Wire.data(agents[i])
    }
    if route == "/v1/circles/proposals/accept" || route == "/v1/circles/proposals/dismiss",
      method == "POST"
    {
      let ids = object["ids"] as? [String] ?? []
      guard !ids.isEmpty, ids.allSatisfy({ id in circleProposals.contains { $0.id == id } }) else {
        throw BackendError.http(400)
      }
      if route.hasSuffix("accept") {
        for proposal in circleProposals where ids.contains(proposal.id) {
          var circle = proposal.circle
          circle.confirmed = true
          circleDetails.append(
            CircleDetail(
              circle: circle, chats: circle.chats, people: circle.people, topics: circle.topics,
              brief: nil))
        }
      }
      circleProposals.removeAll { ids.contains($0.id) }
      return Data("{}".utf8)
    }
    if route == "/v1/stats", method == "GET" {
      return try Wire.data(DemoData.stats(activity: activity))
    }
    if route == "/v1/activity/digests", method == "GET" {
      return try Wire.data(DemoData.digests(activity: activity))
    }
    if route == "/v1/waiting", method == "GET" {
      return try Wire.data(WaitingResponse(waiting: waitingItems))
    }
    if route == "/v1/circles/changes", method == "GET" {
      return try Wire.data(CircleChangesResponse(changes: circleChanges))
    }
    if route == "/v1/circles/changes/undo" || (parts.count == 4 && parts[1] == "circle-changes" && parts[3] == "undo") {
      let ids = Set(object["change_ids"] as? [String] ?? [parts[2]])
      for i in circleChanges.indices {
        let removed = Set(circleChanges[i].peopleChanges.filter { ids.contains($0.value) }.keys)
        if let c = circleDetails.firstIndex(where: { $0.circle.id == circleChanges[i].circleId }) {
          circleDetails[c].people.removeAll { removed.contains($0.personId) }
          circleDetails[c].circle.people = circleDetails[c].people
        }
        circleChanges[i].peopleChanges = circleChanges[i].peopleChanges.filter { !ids.contains($0.value) }
        circleChanges[i].peopleIds.removeAll { removed.contains($0) }
        circleChanges[i].changeIds.removeAll { ids.contains($0) }
        circleChanges[i].count = circleChanges[i].peopleIds.count
      }
      circleChanges.removeAll { $0.count == 0 }
      return Data("{\"ok\":true}".utf8)
    }
    if route == "/v1/efforts/unnamed", method == "GET" {
      let pending = efforts.filter { !$0.hidden && !EffortPresentation.hasTitle($0.title) }.map {
        EffortNamingCandidate(id: $0.id, revision: "demo-\($0.id)-v\(effortNamingRevisions[$0.id, default: 1])",
          topTerms: ["budget", "proposal"], artefactNames: [], participantFirstNames: ["Noor"],
          snippets: ["Noor: Can you review the festival budget proposal?", "You: I will review the proposal budget."],
          messageCount: $0.messageRefs.count, chatCount: $0.chats.count, participantCount: $0.people.count)
      }
      return try Wire.data(EffortNamingResponse(efforts: pending, total: pending.count))
    }
    if route == "/v1/efforts", method == "GET" {
      let person = URLComponents(string: path)?.queryItems?.first { $0.name == "person" }?.value
      let query = URLComponents(string: path)?.queryItems?.first { $0.name == "q" }?.value ?? ""
      return try Wire.data(EffortsResponse(efforts: efforts.filter {
        (person == nil || $0.people.contains { $0.id == person }) && EffortPresentation.visible($0) && (query.isEmpty
          || ($0.title + " " + $0.summary + " " + $0.meta).localizedCaseInsensitiveContains(query))
      }))
    }
    if parts.count == 3, parts[1] == "efforts", let i = efforts.firstIndex(where: { $0.id == parts[2] }) {
      if method == "PATCH" {
        let conditional = object["only_unnamed"] as? Bool == true
        let expectedRevision = object["expected_revision"] as? String
        if conditional && (EffortPresentation.hasTitle(efforts[i].title) || efforts[i].hidden
          || expectedRevision != "demo-\(efforts[i].id)-v\(effortNamingRevisions[efforts[i].id, default: 1])") {
          return try Wire.data(efforts[i])
        }
        if let title = object["title"] as? String {
          guard !title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty, title.count <= 40
          else { throw BackendError.http(400) }
          efforts[i].title = title
        }
        if let summary = object["summary"] as? String { efforts[i].summary = summary }
        if object["title"] != nil || object["summary"] != nil {
          efforts[i].createdBy = conditional ? "This Mac" : "you"
        }
        if let status = object["status"] as? String { efforts[i].status = status }
        if let pinned = object["pinned"] as? Bool { efforts[i].pinned = pinned }
        if let hidden = object["hidden"] as? Bool { efforts[i].hidden = hidden }
        effortNamingRevisions[efforts[i].id, default: 1] += 1
        return try Wire.data(efforts[i])
      }
      if method == "GET" {
        var detail = DemoEfforts.detail(efforts[i])
        detail.taskId = tasks.first { $0.effortId == efforts[i].id }?.id
        return try Wire.data(detail)
      }
    }
    if route == "/v1/efforts/seen", method == "POST" { return Data("{}".utf8) }
    if route == "/v1/home" {
      var home = DemoCircles.home(
        details: circleDetails, suggestions: circleSuggestions, proposals: circleProposals)
      // Y2: open work first, then a bounded suggestion slice.
      let summaries = tasks.map { $0.compact(now: Timestamp.date(DemoData.now)!) }
      home.tasks = Array(TaskPresentation.open(summaries).prefix(6)) + Array(TaskPresentation.suggested(summaries).prefix(3))
      home.queued += circleSuggestions.filter { ["check_in", "nudge"].contains($0.kind) }
      home.effortsReady = true
      home.circleUpdates = efforts.filter { EffortPresentation.visible($0) && $0.status != "dormant" }.map {
        CircleEffortUpdate(circle: .init(id: $0.circleIds[0], name: $0.circleIds[0].capitalized),
          effortId: $0.id, title: $0.title, status: $0.status, line: $0.summary,
          due: $0.due, lastAt: $0.lastAt)
      }
      home.circleChanges = Array(circleChanges.prefix(3))
      home.circleChangesMore = max(0, circleChanges.count - 3)
      home.integrations = agents.map { agent in
        var result = CircleIntegration(
          id: agent.id, name: agent.name, kind: agent.kind, lastSeenAt: agent.lastSeenAt)
        result.access = agent.access ?? agent.permissions?.access
        result.accessSummary = agent.accessSummary
        return result
      }
      home.needsReply.removeAll { ask in
        completedAsks.contains(ask.id) || !circleDetails.contains { $0.circle.id == ask.circleId }
      }
      home.recentActivity = Array(activity.sorted(by: Activity.newestFirst).prefix(6))
      home.watches = watches.map(\.summary).filter(\.isOpen)
      home.waiting = waitingItems
      return try Wire.data(home)
    }
    if route == "/v1/watches", method == "GET" {
      let open = watches.map(\.summary).filter(\.isOpen)
      let closed = watches.map(\.summary).filter { !$0.isOpen }.sorted {
        $0.createdAt > $1.createdAt
      }
      return try Wire.data(WatchesResponse(watches: open + closed.prefix(5)))
    }
    if parts.count >= 3, parts[1] == "watches" {
      guard let i = watches.firstIndex(where: { $0.summary.id == parts[2] }) else {
        throw BackendError.http(404)
      }
      if parts.count == 3, method == "GET" { return try Wire.data(watches[i]) }
      guard parts.count == 4, method == "POST" else { throw BackendError.http(404) }
      switch parts[3] {
      case "seen":
        watches[i].summary.newHits = 0
        return Data()
      case "instructions":
        guard watches[i].summary.isOpen else { throw BackendError.http(409) }
        guard let text = object["text"] as? String,
          !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
          text.count <= 500
        else { throw BackendError.http(400) }
        watches[i].instructions.append(WatchInstruction(at: DemoData.now, text: text))
        return try Wire.data(watches[i])
      case "pause", "resume", "stop":
        guard watches[i].summary.isOpen else { throw BackendError.http(409) }
        watches[i].summary.status =
          parts[3] == "pause" ? "paused" : parts[3] == "resume" ? "active" : "stopped"
        return try Wire.data(watches[i].summary)
      default: throw BackendError.http(404)
      }
    }
    if route == "/v1/circles" {
      return try Wire.data(CirclesResponse(circles: circleDetails.map(\.circle)))
    }
    if route == "/v1/map" {
      var map = DemoCircles.understanding(circleDetails, proposals: circleProposals)
      let peopleIDs = Set(people.map(\.id))
      map.nodes.removeAll { $0.kind == "person" && !peopleIDs.contains($0.id) }
      let nodeIDs = Set(map.nodes.map(\.id))
      map.edges.removeAll { !nodeIDs.contains($0.source) || !nodeIDs.contains($0.target) }
      map.events.removeAll { !$0.nodeIds.allSatisfy(nodeIDs.contains) }
      if let url = URLComponents(string: path),
        let since = url.queryItems?.first(where: { $0.name == "since" })?.value,
        let date = Timestamp.date(since)
      {
        map.events = map.events.filter { (Timestamp.date($0.at) ?? .distantPast) > date }
      }
      return try Wire.data(map)
    }
    if route == "/v1/presence" {
      return try Wire.data(
        PresenceResponse(agents: [
          AgentPresence(
            id: "agent-2", name: "Codex",
            state: "reading", target: "University", targetIds: ["university"],
            since: DemoData.date(1))
        ]))
    }
    if parts.count == 4, parts[1] == "people", parts[3] == "avatar" {
      guard let data = customPhotos[parts[2]] else { throw BackendError.http(404) }; return data
    }

    if parts.count >= 3, parts[1] == "suggestions", method == "POST" {
      if parts.last == "dismiss" {
        guard let body, (try? JSONSerialization.jsonObject(with: body)) is [String: Any] else {
          throw BackendError.http(400)
        }
      }
      guard let i = circleSuggestions.firstIndex(where: { $0.id == parts[2] }) else {
        throw BackendError.http(404)
      }
      if parts.last == "send" {
        let suggestion = circleSuggestions[i]
        guard suggestion.chatJid != nil else {
          throw BackendError.unavailable(
            "This is an action to do yourself; dismiss it when finished.")
        }
        let matching = circleDetails.filter {
          $0.chats.contains { $0.chatJid == suggestion.chatJid }
        }
        guard !matching.contains(where: { $0.circle.sendRule == .never }) else {
          throw BackendError.http(403)
        }
        let variants = object["variation"] as? Int ?? 0
        guard (0..<3).contains(variants)
        else { throw BackendError.http(400) }
        activity.insert(
          Activity(
            id: (activity.map(\.id).max() ?? 0) + 1, at: DemoData.now,
            agentId: suggestion.createdBy, tool: "send_suggestion", kind: "sent",
            chatJids: [suggestion.chatJid!], messageIds: [], count: 1,
            note: suggestionResult(suggestion, object: object),
            confirmedVia: "mac"), at: 0)
      }
      let suggestion = circleSuggestions[i]
      if let taskId = suggestion.taskId, let taskIndex = tasks.firstIndex(where: { $0.id == taskId }) {
        tasks[taskIndex].needsYou = false
        tasks[taskIndex].needsYouReason = nil
        if suggestion.kind == "nudge" { tasks[taskIndex].waiting?.nudgeState = parts.last == "send" ? "sent" : "skipped" }
        if parts.last == "send" { tasks[taskIndex].updatesSent.append(.init(at: DemoData.now, chatJid: suggestion.chatJid ?? "", messageId: "demo-sent-" + suggestion.id, textPreview: String(suggestionResult(suggestion, object: object).prefix(120)), kind: suggestion.kind, by: "You")) }
      }
      circleSuggestions.remove(at: i)
      if parts.last == "send" {
        return try Wire.data(
          CircleDraft(
            draftId: "draft-" + suggestion.id, agentId: suggestion.createdBy,
            chatJid: suggestion.chatJid ?? "",
            finalText: suggestionResult(suggestion, object: object),
            rule: .confirm, needsConfirmation: true, createdAt: DemoData.now,
            expiresAt: suggestion.expiresAt, state: "sent", confirmedVia: "mac",
            messageId: "demo-sent-" + suggestion.id, suggestionId: suggestion.id))
      }
      return Data("{\"ok\":true}".utf8)
    }
    if parts.count >= 3, parts[1] == "circles" {
      let id = parts[2]
      if parts.last == "confirm", let i = circleProposals.firstIndex(where: { $0.id == id }) {
        var circle = circleProposals.remove(at: i).circle
        circle.confirmed = true
        if let name = object["name"] as? String { circle.name = name }
        circleDetails.append(
          CircleDetail(
            circle: circle, chats: circle.chats, people: circle.people, topics: circle.topics,
            brief: nil))
        return try Wire.data(circle)
      }
      if method == "DELETE" {
        circleProposals.removeAll { $0.id == id }
        circleDetails.removeAll { $0.circle.id == id }
        return Data("{}".utf8)
      }
      if method == "PATCH", let p = circleProposals.firstIndex(where: { $0.id == id }) {
        if let name = object["name"] as? String { circleProposals[p].circle.name = name }
        return try Wire.data(circleProposals[p].circle)
      }
      guard let i = circleDetails.firstIndex(where: { $0.circle.id == id }) else {
        throw BackendError.http(404)
      }
      if parts.count == 4, parts[3] == "brief", method == "PUT" {
        let brief = CircleBrief(
          circleId: id, version: (briefHistory[id]?.first?.version ?? 0) + 1,
          summary: object["summary"] as? String ?? "",
          whatChanged: object["what_changed"] as? String ?? "",
          openAsksForYou: object["open_asks"] as? [String] ?? [],
          upcoming: (object["upcoming"] as? [[String: String]] ?? []).map {
            CircleUpcoming(date: $0["date"] ?? "", text: $0["text"] ?? "")
          },
          peopleNotes: [], evidenceMessageIds: object["evidence_message_ids"] as? [String] ?? [],
          chatJids: circleDetails[i].chats.map(\.chatJid),
          writtenBy: object["written_by"] as? String ?? "you",
          writtenAt: DemoData.now, coversUntil: object["covers_until"] as? String ?? DemoData.now)
        circleDetails[i].brief = brief
        briefHistory[id, default: []].insert(brief, at: 0)
        activity.append(
          Activity(
            id: (activity.map(\.id).max() ?? 0) + 1, at: DemoData.now, agentId: brief.writtenBy,
            tool: "update_brief", kind: "summary", chatJids: brief.chatJids,
            messageIds: brief.evidenceMessageIds, count: 1, circleId: id))
        return try Wire.data(brief)
      }
      if parts.count == 6, parts[3] == "asks", parts.last == "done", method == "POST" {
        completedAsks.insert(parts[4])
        return Data()
      }
      if parts.count == 4, parts.last == "briefs" {
        return try Wire.data(BriefsResponse(briefs: briefHistory[id] ?? []))
      }
      if parts.count == 6, parts.last == "revert", let version = Int(parts[4]) {
        guard var brief = briefHistory[id]?.first(where: { $0.version == version }) else {
          throw BackendError.http(404)
        }
        brief.version = (briefHistory[id]?.map(\.version).max() ?? 0) + 1
        brief.writtenBy = "you"
        brief.writtenAt = DemoData.now
        circleDetails[i].brief = brief
        briefHistory[id, default: []].insert(brief, at: 0)
        return try Wire.data(brief)
      }
      if parts.last == "chats", method == "POST", let jid = object["chat_jid"] as? String {
        circleDetails[i].chats.append(
          CircleChat(chatJid: jid, roleInCircle: "Included chat", includedSince: DemoData.now))
      }
      if method == "PATCH" {
        if let pinned = object["pinned"] as? Bool { circleDetails[i].circle.pinned = pinned }
        if let rule = object["send_rule"] as? String, let value = SendRule(rawValue: rule) {
          circleDetails[i].circle.sendRule = value
        }
        if let name = object["name"] as? String { circleDetails[i].circle.name = name }
      }
      var result = circleDetails[i].circle
      result.chats = circleDetails[i].chats
      result.people = circleDetails[i].people
      result.topics = circleDetails[i].topics
      return try Wire.data(result)
    }
    switch (method, route) {
    case ("GET", "/v1/status"):
      return try Wire.data(
        Status(
          linked: true, connected: true, accountName: "Alex",
          pendingApprovals: approvals.filter(\.isPending).count,
          agents: agents.map { .init(name: $0.name, lastSeenAt: $0.lastSeenAt) }, health: "warn"))
    case ("GET", "/v1/agents"): return try Wire.data(["agents": agents])
    case ("POST", "/v1/agents"):
      let agent = Agent(
        preset: "own", id: UUID().uuidString, name: object["name"] as? String ?? "New AI",
        kind: object["kind"] as? String ?? "custom",
        transport: object["transport"] as? String ?? "local", createdAt: DemoData.now,
        enabled: true, permissions: .preset("own"))
      agents.append(agent)
      return try Wire.data(
        CreatedAgent(
          agent: agent, token: agent.transport == "remote" ? nil : "demo-credential-not-for-use"))
    case ("GET", "/v1/chats"):
      let query = URLComponents(string: path)?.queryItems?.first { $0.name == "query" }?.value ?? ""
      return try Wire.data([
        "chats": DemoData.chats.filter { query.isEmpty || $0.name.localizedCaseInsensitiveContains(query) }
      ])
    case ("GET", "/v1/approvals"):
      let state = URLComponents(string: path)?.queryItems?.first { $0.name == "state" }?.value
      return try Wire.data(
        ApprovalsResponse(approvals: approvals.filter { state == nil || $0.state == state }))
    case ("GET", "/v1/activity"):
      let query = URLComponents(string: path)?.queryItems ?? []
      func value(_ key: String) -> String? { query.first { $0.name == key }?.value }
      let rows = activity.sorted(by: Activity.newestFirst).filter { row in
        (value("agent") == nil || row.agentId == value("agent"))
          && (value("kind") == nil || row.kind == value("kind"))
          && (value("chat") == nil || row.chatJids.contains(value("chat")!))
          && (value("before") == nil || row.id < (Int64(value("before")!) ?? 0))
      }
      let page = Array(rows.prefix(Int(value("limit") ?? "50") ?? 50))
      return try Wire.data(ActivityPage(items: page, nextBefore: page.last?.id))
    case ("GET", "/v1/health"):
      return try Wire.data(
        HealthResponse(
          checks: health, health: health.contains { $0.state == "warn" } ? "warn" : "ok"))
    case ("GET", "/v1/people"):
      let query = URLComponents(string: path)?.queryItems?.first { $0.name == "query" }?.value ?? ""
      return try Wire.data(["people": people.filter {
        query.isEmpty || ($0.name + " " + ($0.label ?? "")).localizedCaseInsensitiveContains(query)
      }])
    case ("GET", "/v1/people/changes"): return try Wire.data(["changes": changes])
    case ("GET", "/v1/graph"):
      return try Wire.data(DemoData.graph(count: path.contains("limit=2000") ? 300 : 22))
    case ("GET", "/v1/settings"): return try Wire.data(settings)
    case ("PUT", "/v1/settings"):
      settings = try Wire.decoder().decode(AppSettings.self, from: body ?? Data())
      return try Wire.data(settings)
    case ("GET", "/v1/remote"): return try Wire.data(remote)
    case ("POST", "/v1/remote"):
      remote.enabled = object["enabled"] as? Bool ?? remote.enabled
      remote.publicUrl = object["public_url"] as? String ?? remote.publicUrl
      remote.mcpUrl = remote.publicUrl + "/mcp"
      return try Wire.data(remote)
    case ("POST", "/v1/remote/revoke"):
      remote.clients.removeAll { $0.id == object["client_id"] as? String }
      return Data("{\"ok\":true}".utf8)
    case ("POST", "/v1/remote/approve"), ("POST", "/v1/remote/deny"):
      if route.hasSuffix("approve") {
        var id = object["agent_id"] as? String
        if let newAgent = object["new_agent"] as? [String: String] {
          let agent = Agent(
            id: UUID().uuidString, name: newAgent["name"] ?? "ChatGPT",
            kind: newAgent["kind"] ?? "chatgpt", transport: "remote", createdAt: DemoData.now,
            enabled: true, permissions: .preset("own"))
          agents.append(agent)
          id = agent.id
        }
        let preset = object["preset"] as? String ?? "own"
        if let i = agents.firstIndex(where: { $0.id == id }) {
          agents[i].preset = preset
          agents[i].permissions = .preset(preset)
          permissions[agents[i].id] = agents[i].permissions
        }
        if preset == "given", let i = agents.firstIndex(where: { $0.id == id }),
          let raw = object["access"]
        {
          let access = try Wire.decode(
            AgentAccess.self, from: JSONSerialization.data(withJSONObject: raw))
          agents[i].access = access
          agents[i].permissions?.access = access
          agents[i].accessSummary = access.summary(at: DemoData.epoch)
          permissions[agents[i].id] = agents[i].permissions
        }
      }
      remote.pendingCodes.removeAll { $0.code == object["code"] as? String }
      return Data("{\"ok\":true}".utf8)
    case ("GET", "/v1/diagnostics"):
      return Data(
        "Limmr demo diagnostics\nConnection: connected\nDatabase: healthy\nCredentials and message content: omitted\n"
          .utf8)
    case ("POST", "/v1/migrate/whatsapp-mcp"):
      return try Wire.data(
        MigrationPlan(
          available: false, dryRun: true, steps: [], canUndo: false, restartRequired: false))
    default: break
    }
    if parts.count == 3, parts[1] == "agents",
      let agent = agents.first(where: { $0.id == parts[2] })
    {
      if method == "DELETE" {
        agents.removeAll { $0.id == parts[2] }
        return Data("{\"ok\":true}".utf8)
      }
      if method == "PATCH", let i = agents.firstIndex(where: { $0.id == parts[2] }) {
        if let enabled = object["enabled"] as? Bool { agents[i].enabled = enabled }
        if let name = object["name"] as? String {
          do { agents[i].name = try AgentName.normalized(name) } catch {
            throw BackendError.http(400)
          }
        }
      }
      var result = agents.first { $0.id == agent.id } ?? agent
      result.permissions = permissions[agent.id] ?? agent.permissions
      return try Wire.data(result)
    }
    if route == "/v1/messages" {
      let query = URLComponents(string: path)?.queryItems
      let id = query?.first { $0.name == "message_id" }?.value
      let chat = query?.first { $0.name == "chat_jid" }?.value
      return try Wire.data(
        MessagesResponse(
          messages: Array(
            DemoData.messages.filter {
              (id == nil || $0.id == id) && (chat == nil || $0.chatJid == chat)
            }.sorted { $0.timestamp > $1.timestamp }.prefix(
              Int(query?.first { $0.name == "limit" }?.value ?? "50") ?? 50))))
    }
    if parts.count >= 4, parts[1] == "agents" {
      let id = parts[2]
      if parts[3] == "permissions" {
        if method == "GET" {
          return try Wire.data(
            permissions[id]
              ?? .defaults(remote: agents.first { $0.id == id }?.transport == "remote"))
        }
        permissions[id] = try Wire.decoder().decode(Permissions.self, from: body ?? Data())
        guard var agent = agents.first(where: { $0.id == id }) else { throw BackendError.http(404) }
        agent.permissions = permissions[id]
        agent.access = permissions[id]?.access
        agent.accessSummary = agent.access?.summary(at: DemoData.epoch)
        if let index = agents.firstIndex(where: { $0.id == id }) { agents[index] = agent }
        return try Wire.data(agent)
      }
      if parts[3] == "revoke", let i = agents.firstIndex(where: { $0.id == id }) {
        agents[i].enabled = false
      }
      if parts[3] == "rotate" { return Data("{\"token\":\"demo-credential-not-for-use\"}".utf8) }
      return Data("{\"ok\":true}".utf8)
    }
    if parts.count == 4, parts[1] == "approvals",
      let index = approvals.firstIndex(where: { $0.id == parts[2] })
    {
      guard ["approve", "deny"].contains(parts[3]), method == "POST" else {
        throw BackendError.http(404)
      }
      let approved = parts[3] == "approve"
      try approvals[index].transition(
        to: approved ? "sent" : "denied", text: object["text"] as? String)
      let decision = approvals[index]
      activity.append(
        .init(
          id: (activity.map(\.id).max() ?? 0) + 1,
          at: ISO8601DateFormatter().string(
            from: DemoData.epoch.addingTimeInterval(Double(activity.count))),
          agentId: "you", tool: approved ? "approve" : "deny",
          kind: approved ? "approved" : "denied",
          chatJids: [decision.chatJid], messageIds: [], count: 1,
          note: approved ? decision.text : "The proposed message was denied. Nothing was sent."))
      return try Wire.data(approvals[index])
    }
    if route == "/v1/groups", method == "GET" {
      return try Wire.data(GroupsResponse(groups: groups))
    }
    if parts.count == 3, parts[1] == "groups", method == "GET",
      let group = groups.first(where: { $0.jid == parts[2] })
    {
      let recent = Array(DemoData.messages.filter { $0.chatJid == group.jid }.prefix(20))
      let senders = Set(recent.map(\.sender)).compactMap { jid in
        people.first { $0.jids.contains(jid) }.map { GroupSender(jid: jid, name: $0.label ?? $0.name) }
      }
      return try Wire.data(
        GroupDetail(
          recent: recent, senders: senders,
          summary: ChatSummary(text: "The group is preparing the next studio review and sharing material samples.", agentId: "agent-1", createdAt: DemoData.now),
          group: group,
          members: DemoPeople.members[DemoData.groups.firstIndex(where: { $0.jid == group.jid })!]
            .compactMap { index in
              people.first { $0.id == DemoData.personId(index) }.map {
                GroupMember(person: $0, admin: false)
              }
            }, facts: []))
    }
    if route == "/v1/people/export", method == "GET" {
      struct Export: Encodable {
        var people: [PersonDetail]
        var circles: [Circle]
      }
      let profiles = people.map { person in
        PersonDetail(
          person: person, groups: DemoPeople.groups(for: person.id),
          facts: facts.filter {
            $0.subjectId == person.id && ["active", "suggested"].contains($0.status)
          },
          relations: [],
          stats: .init(
            sharedGroups: DemoPeople.groups(for: person.id).map(\.jid),
            dmMessages30: person.id == "person-04" ? 5 : 4, dmMessages365: person.id == "person-04" ? 5 : 4, lastDm: DemoData.now))
      }
      return try Wire.data(Export(people: profiles, circles: circleDetails.map(\.circle)))
    }
    if parts.count == 6, parts[1] == "people", parts[3] == "suggestions",
      let i = people.firstIndex(where: { $0.id == parts[2] }),
      let j = people[i].detailSuggestions?.firstIndex(where: { $0.id == parts[4] })
    {
      let value = people[i].detailSuggestions![j]
      if parts[5] == "confirm" {
        var d = people[i].details ?? PersonDetails()
        let labeled = LabeledValue(label: value.label, value: value.value)
        switch value.field {
        case "phones": d.phones.append(labeled)
        case "emails": d.emails.append(labeled)
        case "handles": d.handles.append(labeled)
        case "birthday": d.birthday = value.value
        case "location": d.location = value.value
        case "pronouns": d.pronouns = value.value
        default: break
        }
        people[i].details = d
      }
      people[i].detailSuggestions?.remove(at: j)
      return try Wire.data(people[i])
    }
    if parts.count == 3, parts[1] == "people",
      let personIndex = people.firstIndex(where: { $0.id == parts[2] })
    {
      if method == "DELETE" {
        let id = people[personIndex].id
        people.remove(at: personIndex)
        facts.removeAll { $0.subjectId == id }
        changes.removeAll { $0.subjectId == id }
        activity.append(
          .init(
            id: (activity.map(\.id).max() ?? 0) + 1, at: DemoData.now,
            agentId: "you", tool: "forget_person", kind: "people_write", chatJids: [],
            messageIds: [], count: 1,
            note: "Forgot a person and their profile, facts, relations and circle memberships."))
        for i in circleDetails.indices {
          circleDetails[i].people.removeAll { $0.personId == id }
          circleDetails[i].circle.people.removeAll { $0.personId == id }
        }
        return Data("{}".utf8)
      }
      if method == "PATCH", let body {
        struct Patch: Decodable {
          var details: PersonDetails?
          var howKnown: String?
          var label: String?
          var otherNames: [String]?
        }
        let patch = try Wire.decode(Patch.self, from: body)
        if var details = patch.details {
          let derived = people[personIndex].details?.phones.filter { $0.source == "whatsapp" } ?? []
          details.phones += derived.filter { phone in
            !details.phones.contains { $0.value == phone.value }
          }
          people[personIndex].details = details
        }
        if let howKnown = patch.howKnown { people[personIndex].howKnown = howKnown }
        if let label = patch.label { people[personIndex].label = label }
        if let names = patch.otherNames { people[personIndex].otherNames = names }
        return try Wire.data(people[personIndex])
      }
      let person = people[personIndex]
      return try Wire.data(
        PersonDetail(
          person: person, groups: DemoPeople.groups(for: person.id),
          facts: facts.filter {
            $0.subjectId == person.id && ["active", "suggested"].contains($0.status)
          },
          relations: [
            .init(
              id: "r1", a: "me", b: person.id, kind: "friend", note: "Met through Design Club",
              source: "you")
          ],
          stats: .init(
            sharedGroups: DemoPeople.groups(for: person.id).map(\.jid), dmMessages30: person.id == "person-04" ? 5 : 4,
            dmMessages365: person.id == "person-04" ? 5 : 4, lastDm: DemoData.now)))
    }
    if parts.count == 5, parts[1] == "people", parts[2] == "changes",
      let i = changes.firstIndex(where: { $0.id == parts[3] })
    {
      changes[i].reverted = true
      if let f = facts.firstIndex(where: { $0.id == parts[3] }) { facts[f].status = "reverted" }
      return Data("{\"ok\":true}".utf8)
    }
    if parts.count == 4, parts[1] == "facts", let i = facts.firstIndex(where: { $0.id == parts[2] })
    {
      facts[i].status = parts[3] == "confirm" ? "active" : "reverted"
      if parts[3] == "confirm" { facts[i].sourceAgent = "you" }
      if let c = changes.firstIndex(where: { $0.id == parts[2] }) { changes[c].reverted = true }
      return Data("{\"ok\":true}".utf8)
    }
    if parts.count == 3, parts[1] == "messages" {
      return try Wire.data(DemoData.messages.first { $0.id == parts[2] } ?? DemoData.messages[0])
    }
    if route.hasPrefix("/v1/fix/") {
      health = health.map {
        var check = $0
        if check.fixId == parts.last {
          check.state = "ok"
          check.detail = "Refreshed just now."
          check.fixId = nil
        }
        return check
      }
      return Data("{\"ok\":true}".utf8)
    }
    if method == "POST",
      ["/v1/link/start", "/v1/link/cancel", "/v1/unlink", "/v1/migrate/undo"].contains(route)
    {
      return Data("{\"ok\":true}".utf8)
    }
    throw BackendError.unavailable("This demo action is not available.")
  }
  public nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { continuation in
      if path == "/v1/link/events" {
        let data =
          "{\"state\":\"qr\",\"code\":\"LIMMR-DEMO-PAIRING-NOT-A-REAL-SESSION\",\"expires_in\":60}"
        continuation.yield(.init(event: "link", data: data))
      }
      continuation.finish()
    }
  }
}


extension DemoBackend {
  private func taskRequest(_ path: String, method: String, object: [String: Any]) throws -> Data {
    let parts = String(path.split(separator: "?").first ?? "").split(separator: "/").map(String.init)
    let now = Timestamp.date(DemoData.now)!
    if parts.count == 2 {
      if method == "GET" {
        let status = URLComponents(string: "http://demo" + path)?.queryItems?.first { $0.name == "status" }?.value ?? "open"
        let person = URLComponents(string: "http://demo" + path)?.queryItems?.first { $0.name == "person" }?.value
        let all = tasks.filter { person == nil || $0.people.contains { $0.id == person } }.map { $0.compact(now: now) }
        let filtered = status == "all" ? all : status == "suggested" ? TaskPresentation.suggested(all) : status == "closed" ? all.filter { ["done", "dropped"].contains($0.status) } : TaskPresentation.open(all)
        return try Wire.data(TasksResponse(tasks: filtered))
      }
      if method == "POST", let title = object["title"] as? String {
        if let effort = object["effort_id"] as? String, let existing = tasks.first(where: { $0.effortId == effort }) { return try Wire.data(existing) }
        func entities(_ key: String) -> [EffortEntity] {
          guard let value = object[key], let data = try? JSONSerialization.data(withJSONObject: value) else { return [] }
          return (try? Wire.decode([EffortEntity].self, from: data)) ?? []
        }
        let task = TrackedTask(id: "tsk_demo_" + String(tasks.count + 1), title: title, goal: object["goal"] as? String ?? "",
          kind: object["kind"] as? String ?? "long", effortId: object["effort_id"] as? String,
          chats: entities("chats"), people: entities("people"), createdBy: .init(name: "You"), createdAt: DemoData.now, updatedAt: DemoData.now)
        tasks.append(task); return try Wire.data(task)
      }
      throw BackendError.http(400)
    }
    guard let index = tasks.firstIndex(where: { $0.id == parts[2] }) else { throw BackendError.http(404) }
    if parts.count == 3, method == "GET" { return try Wire.data(tasks[index]) }
    // Y2: local-only surfacing actions mirror the merged control API.
    if parts.count == 4, method == "POST", ["keep", "dismiss", "undo"].contains(parts[3]) {
      let action = parts[3]
      if action == "undo" {
        guard TaskPresentation.canUndo(tasks[index].compact(now: now), now: now) else { throw BackendError.http(409) }
        tasks[index].undoChangeId = nil
      } else { guard tasks[index].status == "suggested" else { throw BackendError.http(409) } }
      tasks[index].status = action == "keep" ? "active" : "dropped"
      tasks[index].updatedAt = DemoData.now
      if action != "keep" {
        tasks[index].closeReason = action == "dismiss" ? "not_a_task" : "owner"
        tasks[index].closedAt = DemoData.now
      }
      return try Wire.data(tasks[index])
    }

    if parts.count == 3, method == "PATCH" {
      let data = try Wire.data(tasks[index])
      var merged = try JSONSerialization.jsonObject(with: data) as! [String: Any]
      for (key, value) in object { merged[key] = value }
      tasks[index] = try Wire.decode(TrackedTask.self, from: JSONSerialization.data(withJSONObject: merged))
      if object["waiting"] is NSNull { tasks[index].status = "active" }
      else if object["waiting"] != nil { tasks[index].status = "waiting" }
    } else if parts.count == 5, parts[3] == "checklist", method == "PATCH",
      let step = tasks[index].checklist.firstIndex(where: { $0.id == parts[4] }), let done = object["done"] as? Bool {
      tasks[index].checklist[step].done = done
      tasks[index].checklist[step].doneAt = done ? DemoData.now : nil
      tasks[index].progress = .init(done: tasks[index].checklist.filter(\.done).count, total: tasks[index].checklist.count)
      if tasks[index].kind == "short", tasks[index].progress.done == tasks[index].progress.total { tasks[index].status = "done" }
    } else if parts.count == 5, parts[3] == "nudge", parts[4] == "skip", method == "POST" {
      tasks[index].waiting?.nudgeState = "skipped"
      tasks[index].needsYou = false
      circleSuggestions.removeAll { $0.taskId == tasks[index].id && $0.kind == "nudge" }
    } else { throw BackendError.http(400) }
    tasks[index].updatedAt = DemoData.now
    if ["done", "dropped"].contains(tasks[index].status) {
      tasks[index].closedAt = DemoData.now; tasks[index].closeReason = tasks[index].status == "done" ? "done" : "owner"
      tasks[index].needsYou = false
      circleSuggestions.removeAll { $0.taskId == tasks[index].id }
    }
    return try Wire.data(tasks[index])
  }
}
