import AppKit
import LimmrKit
import SwiftUI

/// All content is invented. This backend never delegates to an API client or engine.
struct N4SnapshotBackend: Backend {
  static let now = "2026-11-10T12:00:00Z"
  let base = DemoBackend()
  let tasks: [TrackedTask]
  let previews: [String: URL]
  let people: [Person]
  let circles: [LimmrKit.Circle]
  let portraits: [Data]
  static func decode<T: Decodable>(_ type: T.Type, _ value: [String: Any]) throws -> T { try Wire.decode(type, from: JSONSerialization.data(withJSONObject: value)) }
  enum Covers { case standard, plain, busy, rows }
  @MainActor init(population: Int = 220, covers: Covers = .standard) throws {
    portraits = covers == .standard ? [] : N1MapPortraits.make()
    let names = ["Maya Rao", "Isha Sen", "Theo Park", "Nina Das", "Jonah Mehta", "Sam Okafor", "Prof. Anand", "Lena Ortiz", "Kiran Shah", "Dr. Meera Iyer", "Ada Bell", "Noor Haddad", "Ravi Bell"]
    let definitions = [("university", "University", "graduationcap", "Classes, research and campus life"), ("family", "Family", "house", "Home and family"), ("friends", "Close people", "heart", "The people closest to you"), ("collective", "Design Collective", "briefcase", "Making things together"), ("reading", "Reading circle", "book", "Books and conversation"), ("studio", "Studio friends", "paintbrush", "Creative practice"), ("neighbors", "Neighbors", "building.2", "Around the neighborhood"), ("hikes", "Weekend hikes", "mountain.2", "Time outdoors"), ("alumni", "Alumni", "person.3", "Keeping in touch")]
    let circleDefinitions = Array(definitions.prefix(population == 12 ? 1 : population == 500 ? 9 : 4))
    var members: [Person] = []
    for i in 0..<population {
      let position = i % 220
      let circle: String
      if population == 12 { circle = "university" }
      else if population == 500 { circle = i >= 440 ? "" : circleDefinitions[min(8,i*9/440)].0 }
      else { circle = position < 148 ? "university" : position < 154 ? "family" : position < 162 ? "friends" : position < 202 ? "collective" : "" }
      members.append(Person(id: "n4-\(i)", jids: ["1555000\(String(format: "%04d", i))@s.whatsapp.net"], name: i < names.count ? names[i] : "Member \(i + 1)", circle: circle,
        role: i == 1 ? "Research partner" : i == 4 ? "Class representative" : i == 2 ? "Library volunteer" : "Classmate",
        howKnown: i == 1 ? "Your research partner at university since 2025. You run the survey project together, and she is also in the Design Collective." : "",
        notes: i == 1 ? "Prefers messages in the evening. Always confirm before sending anything to the cohort group." : "", updatedAt: Self.now, details: i == 1 ? .init(phones: [.init(label: "mobile", value: "+1 202 555 0146")]) : nil))
    }
    people = members
    var collection: [LimmrKit.Circle] = []
    for (id, name, symbol, description) in circleDefinitions {
      let ids = members.filter { $0.circle == id || (id == "collective" && (["n4-0", "n4-1", "n4-3"]+(133..<148).map { "n4-\($0)" }).contains($0.id)) }
      let c: LimmrKit.Circle = try Self.decode(LimmrKit.Circle.self, ["id": id, "name": name, "kind": id, "symbol": symbol, "description": description, "send_rule": "confirm", "created_by": "you", "confirmed": true, "updated_at": Self.now, "pinned": true,
        "people": ids.map { ["person_id": $0.id, "role": $0.role, "note": "", "source": "you"] },
        "chats": [["chat_jid": "n4-stats@g.us", "role_in_circle": "Group chat", "included_since": Self.now], ["chat_jid": "n4-section@g.us", "role_in_circle": "Group chat", "included_since": Self.now]]])
      collection.append(c)
    }
    circles = collection
    let titles = ["Open studio", "Research proposal", "Reading group notes", "Portfolio review", "Mum’s birthday trip", "Saturday dinner"]
    let reasons = ["Choose the poster layout before 4 PM", "Six survey questions revised — approve to send", "Comparing notes from 3 chats", "Checking the project summaries", "Collecting dates from everyone", "Ends after the dinner · table for six"]
    let done = [4,3,2,4,1,2], totals = [5,6,4,7,5,3]
    let agents = ["You", "You", "Codex", "Claude Code", "Dot", "Dot"]
    var work: [TrackedTask] = []
    var art: [String: URL] = [:]
    for i in 0..<6 {
      var task = TrackedTask(id: "n4-task-\(i)", title: titles[i], goal: i == 1 ? "The survey is sent to the cohort" : reasons[i], people: [try Self.decode(EffortEntity.self, ["id": "n4-\(i == 1 || i == 2 ? 1 : 0)", "name": i == 1 || i == 2 ? "Isha Sen" : "Maya Rao"])], circleIds: [i == 0 || i == 3 ? "collective" : i > 3 ? "family" : "university"], createdBy: .init(agentId: "agent-\(i)", name: agents[i]), createdAt: Self.now, updatedAt: "2026-11-10T11:\(i == 0 ? "48" : i == 1 ? "26" : "52"):00Z", summary: .init(text: reasons[i], updatedAt: Self.now, updatedBy: agents[i]), progress: .init(done: done[i], total: totals[i]), needsYou: i < 2, needsYouReason: i < 2 ? reasons[i] : nil)
      if i < 5 {
        let image = NSImage(size: NSSize(width: 600, height: 800), flipped: false) { bounds in
          NSColor(srgbRed: 0.95, green: 0.94, blue: 0.91, alpha: 1).setFill(); bounds.fill()
          let ink = NSColor(srgbRed: 0.08, green: 0.08, blue: 0.075, alpha: 1)
          (titles[i] as NSString).draw(in: NSRect(x: 48, y: 570, width: 504, height: 160), withAttributes: [.font: NSFont.systemFont(ofSize: 48, weight: .semibold), .foregroundColor: ink])
          ("Design notes / November 2026" as NSString).draw(at: NSPoint(x: 48, y: 520), withAttributes: [.font: NSFont.systemFont(ofSize: 20), .foregroundColor: ink])
          ink.withAlphaComponent(0.18).setFill()
          for line in 0..<10 { NSRect(x: 48, y: 460 - line * 28, width: line % 3 == 0 ? 360 : 504, height: 6).fill() }
          return true
        }
        let root = URL(fileURLWithPath: ProcessInfo.processInfo.environment["LIMMR_HOME"] ?? NSTemporaryDirectory())
        let url = root.appendingPathComponent("n4-document-\(i).png")
        let bitmap = NSBitmapImageRep(data: image.tiffRepresentation!)!
        try bitmap.representation(using: .png, properties: [:])!.write(to: url)
        var cover = WorkspaceItem(id: "n4-cover-\(i)", kind: "file", title: i == 1 ? "Survey-draft-v3.pdf" : titles[i] + ".pdf")
        cover.mediaType = "pdf"
        task.cover = cover; task.coverItemId = cover.id; task.items.insert(cover, at: 0)
        art[task.id] = url
      }
      task.endAt = i == 1 ? "2026-11-13T18:00:00Z" : nil
      if i == 5 { task.checkIn = .init(rule: "once", nextAt: "2026-11-14T17:00:00Z") }
      if i == 1 {
        task.follows = "n4-ended-0"
        task.latest = [.init(chatJid: "n4-stats@g.us", messageId: "n4-message", chat: "Stats project", sender: "Isha Sen", timestamp: "2026-11-10T11:18:00Z", text: "I tightened questions 2 and 5 and dropped the last one. Can you check before I send it to the group?")]
        task.items = [.init(id: "n4-survey", kind: "file", title: "Survey-draft-v3.pdf"), .init(id: "n4-pilot", kind: "file", title: "Pilot-checklist.pdf")]
      }
      work.append(task)
    }
    if covers == .busy {
      let titles = ["Open studio", "Research proposal", "Reading group notes", "Portfolio review", "Birthday trip", "Saturday dinner", "Library visit", "Archive interviews", "Garden afternoon", "Compare venues", "Fieldwork journal", "Workshop materials", "Print invitations", "Mentor catch-up", "Equipment return", "Team lunch", "Book club", "Review draft", "Schedule rehearsal", "Prepare handouts", "Coastal walk", "Collect feedback", "Museum tickets", "Update portfolio", "Volunteer rota", "Community survey", "Check references", "Reserve room", "Sketchbook scans", "Plan exhibition", "Return books", "Lab notes", "Welcome guide", "Travel checklist", "Film discussion", "Edit transcript", "Seminar outline", "Confirm transport", "Project handover", "Sunday picnic"]
      work = (0..<40).map { i in
        var task = work[i % 6]
        task.id = "n4-task-\(i)"; task.title = titles[i]
        task.needsYou = i < 2; task.needsYouReason = i < 2 ? task.needsYouReason : nil
        task.updatedAt = Self.now; task.follows = nil
        if i % 2 == 0 {
          task.cover = nil; task.coverItemId = i % 4 == 0 ? "default:\(i % 3)" : nil
        } else {
          // Reuse an invented document raster; every odd task has a real preview.
          task.cover = work[1].cover; task.coverItemId = work[1].coverItemId
          art[task.id] = art["n4-task-1"]
        }
        if i % 10 == 0 { task.progress = .init(); task.checkIn = .init(rule: "once", nextAt: "2026-11-14T17:00:00Z") }
        return task
      }
    } else if covers == .plain || covers == .rows {
      for i in work.indices where covers == .plain || i == 1 || i == 2 {
        work[i].cover = nil; work[i].coverItemId = i % 2 == 0 ? "default:\(i % 3)" : nil
        art[work[i].id] = nil
      }
    }
    if covers != .standard {
      // The owner's real shape: several people on a task, rarely a checklist.
      let groups: [[Int]] = [[0, 3, 4, 7, 9], [1], [1, 2, 5], [0, 6], [8, 10, 11], [3, 4], [2], [5, 7, 9, 12]]
      for i in work.indices {
        work[i].people = groups[i % groups.count].map { EffortEntity(id: "n4-\($0)", name: names[$0]) }
        if i % 3 != 1 { work[i].progress = .init(); work[i].lastActiveAt = "2026-11-10T\(String(format: "%02d", 11 - i % 9)):1\(i % 6):00Z" }
      }
      if covers == .plain { work[2].people = []; work[2].chats = [EffortEntity(id: "n4-stats@g.us", name: "Stats project")]; work[3].people = [] }
    }
    // A reminder-only fixture exercises date typography, not a fabricated step count.
    if covers != .standard { work[5].progress = .init() }
    if covers != .busy { for i in 0..<18 { var ended = work[1]; ended.id = "n4-ended-\(i)"; ended.title = i == 0 ? "Survey pilot" : "Finished task \(i)"; ended.status = "done"; ended.needsYou = false; ended.needsYouReason = nil; ended.follows = nil; ended.closedAt = "2026-11-02T12:00:00Z"; work.append(ended) } }
    tasks = work
    previews = art
  }
  private func summary(_ task: TrackedTask) -> TaskSummary {
    var value = task.compact(now: Timestamp.date(Self.now)!)
    if let id = task.coverItemId, id.hasPrefix("default:") {
      value.coverCandidate = .init(itemId: id, title: "Task cover", thumbnailUrl: "/v1/tasks/\(task.id)/cover/thumbnail?default=\(id)")
    }
    return value
  }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    let route = (URLComponents(string: path)?.path ?? path).removingPercentEncoding ?? path
    if route == "/v1/people" { return try Wire.data(["people": people]) }
    if route.hasSuffix("/avatar"), let id = route.split(separator: "/").dropLast().last, let index = Int(id.dropFirst(3)), index % 3 != 2, portraits.indices.contains(index) { return portraits[index] }
    if route.contains("/avatar") || route.hasSuffix("/photo") { throw BackendError.http(404) }
    if route == "/v1/people/changes" { return try Wire.data(["changes": []] as [String: [String]]) }
    if route == "/v1/agents" {
      var response: AgentsResponse = try await base.get(path)
      for i in response.agents.indices {
        response.agents[i].name = response.agents[i].id == "agent-1" ? "Claude Code" : response.agents[i].id == "agent-2" ? "Codex" : "Dot"
        response.agents[i].lastSeenAt = response.agents[i].id == "agent-1" ? "2026-11-10T11:00:00Z" : Self.now
        response.agents[i].runsOnPeer = nil
      }
      return try Wire.data(response)
    }
    if route == "/v1/approvals" { return try Wire.data(["approvals": []] as [String: [String]]) }
    if route == "/v1/circles" { return try Wire.data(["circles": circles]) }
    if route.hasPrefix("/v1/circles/") {
      let parts = route.split(separator: "/")
      if parts.count >= 3, let circle = circles.first(where: { $0.id == String(parts[2]) }) {
        let brief: CircleBrief = try Self.decode(CircleBrief.self, ["circle_id": circle.id, "version": 1, "summary": "Choose the poster layout and approve Isha’s survey questions.", "what_changed": "The survey questions are ready for review.", "open_asks_for_you": [], "upcoming": [], "people_notes": [], "evidence_message_ids": [], "written_by": "agent-3", "written_at": "2026-11-10T11:42:00Z", "covers_until": Self.now])
        if route.hasSuffix("/briefs") { return try Wire.data(["briefs": [brief]]) }
        return try Wire.data(circle)
      }
    }
    if route == "/v1/tasks" {
      let person = URLComponents(string: path)?.queryItems?.first { $0.name == "person" }?.value
      let filtered = tasks.filter { person == nil || $0.people.contains { $0.id == person } }
      return try Wire.data(TasksResponse(tasks: filtered.map(summary)))
    }
    if route.hasPrefix("/v1/tasks/"), route.hasSuffix("/preview"), let url = previews[String(route.split(separator: "/")[2])] { return try Wire.data(["path": url.path]) }
    if route.hasPrefix("/v1/tasks/"), let task = tasks.first(where: { $0.id == String(route.split(separator: "/")[2]) }) { return try Wire.data(task) }
    if route == "/v1/stats/history" {
      let messages = [180,171,205,163,192,174,199], actions = [10,11,12,13,14,12,14]
      let end = Timestamp.date(Self.now)!
      let days: [HistoryDay] = (0..<84).map { i in
        let total: Int = i >= 77 ? actions[i - 77] : (i * 3) % 15
        let inbound: Int = i >= 77 ? messages[i - 77] : (i * 37) % 180
        // The split is plain arithmetic on the day's total, so the per-AI rows always add up to it.
        let split: [String: HistoryAgent] = ["agent-1": HistoryAgent(reads: total / 2), "agent-2": HistoryAgent(reads: total / 3), "agent-3": HistoryAgent(reads: total - total / 2 - total / 3)]
        let date = String(end.addingTimeInterval(Double(i - 83) * 86400).ISO8601Format().prefix(10))
        return HistoryDay(date: date, messagesIn: inbound, agentReads: total, agents: split)
      }
      return try Wire.data(StatsHistory(days: days))
    }
    if route == "/v1/home" {
      var home: HomeResponse = try await base.get(path)
      home.tasks = tasks.prefix(6).map(summary); home.queued = []; home.suggestions = []; home.proposals = []; home.waiting = []; home.needsReply = []; home.recentActivity = []
      home.recentLines = [.init(text: "Portfolio files organised", targetId: "n4-task-3", at: "2026-11-10T11:36:00Z"), .init(text: "University brief updated", targetId: "university", kind: "circle", at: "2026-11-10T11:00:00Z"), .init(text: "Dinner reminder set", targetId: "n4-task-5", at: "2026-11-10T09:00:00Z")]
      return try Wire.data(home)
    }
    if route.hasPrefix("/v1/people/"), let person = people.first(where: { $0.id == String(route.split(separator: "/")[2]) }) {
      if route.hasSuffix("/conversation-history") {
        let counts = [3,5,2,6,8,4,7,9,6,10,8,12]
        let weeks: [[String: Any]] = counts.enumerated().map { i,count in ["start": Timestamp.date(Self.now)!.addingTimeInterval(Double(i-11)*7*86400).ISO8601Format().prefix(10).description, "messages": count] }
        let thumbnail = previews["n4-task-1"].flatMap { try? Data(contentsOf: $0) }
        let files: [[String: Any]] = (0..<14).map { i in
          var file: [String: Any] = ["id": "n4-file-\(i)", "chat_jid": person.jids[0], "filename": i == 0 ? "Survey-draft-v3.pdf" : "Study-notes-\(i).pdf", "media_type": "document", "at": Self.now]
          if i < 3, let thumbnail { file["thumbnail"] = thumbnail.base64EncodedString() }
          return file
        }
        return try JSONSerialization.data(withJSONObject: ["weeks": weeks, "shared_files": files, "shared_files_total": 14])
      }
      if route.hasSuffix("/summary") { return try Wire.data(["summary": ["person_id": person.id, "text": "The survey is nearly ready: Isha tightened two questions and dropped one, and is waiting for your approval before sending it to the cohort. The reading-group notes are being merged by Codex.", "written_by": "agent-3", "written_at": "2026-11-10T11:42:00Z", "based_on_until": Self.now]]) }
      var data = try JSONSerialization.jsonObject(with: Wire.data(person)) as! [String: Any]
      data["groups_in_common"] = [["jid": "n4-stats@g.us", "name": "Stats project"], ["jid": "n4-section@g.us", "name": "Section C"]]
      return try JSONSerialization.data(withJSONObject: ["person": data, "groups": [["jid": "n4-stats@g.us", "name": "Stats project", "description": "Survey study group", "member_count": 4, "i_am_admin": false], ["jid": "n4-section@g.us", "name": "Section C", "description": "Class group", "member_count": 71, "i_am_admin": false]], "facts": [], "relations": [], "stats": ["shared_groups": ["n4-stats@g.us", "n4-section@g.us"], "dm_messages_30": 128, "dm_messages_365": 634, "last_dm": "2026-11-10T11:26:00Z", "messages_in_shared_groups": 25]])
    }
    if route == "/v1/presence" { return try JSONSerialization.data(withJSONObject: ["agents": (2...4).map { i -> [String: Any] in ["id": "agent-\(i)", "name": ["Codex", "Claude Code", "Dot"][i-2], "state": "reading", "target": tasks[i].title, "target_ids": [tasks[i].id], "since": Self.now] }]) }
    if route == "/v1/map" || route == "/v1/map/mesh" { return try Wire.data(mesh()) }
    if route == "/v1/chats" { return try JSONSerialization.data(withJSONObject: ["chats": people.prefix(16).enumerated().map { i,p -> [String: Any] in ["jid": p.jids[0], "name": p.name, "is_group": false, "last_message_time": "2026-11-10T11:\(i < 2 ? "26" : "00"):00Z"] }]) }
    if route == "/v1/messages" {
      let lines: [(String, String, Bool)] = [
        ("I tightened questions 2 and 5 and dropped the last one. Can you check before I send it to the group?", "2026-11-10T11:18:00Z", false),
        ("Looks good overall. Let’s keep it under five minutes to fill in.", "2026-11-09T18:42:00Z", true),
        ("Draft three is in the shared folder.", "2026-11-09T18:30:00Z", false)]
      return try JSONSerialization.data(withJSONObject: ["messages": lines.enumerated().map { i, line -> [String: Any] in
        ["id": "n4-message-\(i)", "chat_jid": people[1].jids[0], "sender": line.2 ? "me" : people[1].jids[0], "content": line.0, "timestamp": line.1, "is_from_me": line.2]
      }])
    }

    return try await base.request(path, method: method, body: body)
  }
  func mesh() throws -> CircleMap {
    var nodes = circles.map { CircleMapNode(id: $0.id, kind: "circle", title: $0.name) }
    var edges: [CircleMapEdge] = []
    for (index, person) in people.enumerated() {
      var node = CircleMapNode(id: person.id, kind: "person", title: person.name)
      node.circles = circles.filter { $0.people.contains { $0.personId == person.id } }.map(\.id)
      node.primaryCircleId = person.circle.isEmpty ? nil : person.circle
      node.weight = max(0.05, 1 - Double(index % 148) / 148)
      node.lastActiveAt = Timestamp.date(Self.now)!.addingTimeInterval(-Double(index % 60) * 86400).ISO8601Format()
      node.groupsInCommon = (node.circles ?? []).map { .init(id: $0 + "-group-\(index / 30)", name: "Shared group", circleIds: [$0]) }
      node.howKnown = person.howKnown
      node.sharedGroupIds = node.groupsInCommon?.map(\.id)
      node.needsYou = index < 2
      node.newThisWeek = (people.count == 12 ? [5,7,9] : [13,14,15]).contains(index)
      node.messages30 = index == 1 ? 128 : max(0, 100-index)
      node.closeness = node.weight
      node.lastContact = node.lastActiveAt
      nodes.append(node)
      for circle in node.circles ?? [] { edges.append(.init(source: circle, target: node.id, kind: "membership", state: "confirmed")) }
    }
    let events = try (people.count == 12 ? [5,7,9] : [13,14,15]).map { index in try Self.decode(MapEvent.self, ["id": "n4-new-\(index)", "at": Self.now, "kind": "person_added", "by": "Limmr", "node_ids": ["n4-\(index)"], "text": "New this week"]) }
    return CircleMap(nodes: nodes, edges: edges, pulses: [], events: events)
  }
  func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { .init { $0.finish() } }
}

/// Supplemental fixture: inspect actual 22 pt / 44x32 glyphs and long-title bounds.
struct N4CoverSizesSnapshot: View {
  @Bindable var store: AppStore
  private var examples: [TaskSummary] {
    let titles = ["Open studio", "Research proposal", "Reading notes", "Portfolio review", "Mum’s birthday trip", "Saturday dinner", "Internationalisation review", "研究会の準備"]
    return titles.enumerated().map { i, title in
      var task = TaskSummary(id: "n4-task-\(i)", title: title)
      if i == 1 { task.kind = "document" }
      if i == 4 { task.waiting = .init(onName: "Maya Rao") }
      if i == 5 { task.nextCheckInAt = "2026-11-14T17:00:00Z" }
      let ids = [[0, 3, 4, 7, 9], [1], [1, 2, 5], [], [8, 10], [3, 4], [2], [5, 7, 9]][i]
      task.faces = ids.prefix(4).map { EffortEntity(id: "n4-\($0)", name: store.personName("n4-\($0)")) }; task.faceCount = ids.count
      task.circleIds = [["collective", "university", "university", "collective", "family", "family", "friends", "university"][i]]
      return task
    }
  }
  var body: some View {
    VStack(alignment: .leading, spacing: 24) {
      Text("Paper cover specimens").font(LimmrTheme.font(24, .semibold))
      Text("Invented fixtures · card / 44 × 32 / 22 pt").font(LimmrTheme.font(14)).foregroundStyle(LimmrTheme.ink2)
      LazyVGrid(columns: Array(repeating: GridItem(.fixed(240), spacing: 24), count: 4), spacing: 32) {
        ForEach(examples) { task in
          VStack(alignment: .leading, spacing: 12) {
            PaperTaskPlaceholder(store: store, task: task).frame(width: 240, height: 146).clipShape(RoundedRectangle(cornerRadius: 16))
            Text(task.title).font(LimmrTheme.font(14)).lineLimit(1)
            HStack(spacing: 20) {
              PaperTaskPlaceholder(store: store, task: task).frame(width: 44, height: 32).clipShape(RoundedRectangle(cornerRadius: 6))
              PaperTaskPlaceholder(store: store, task: task).frame(width: 22, height: 22).clipShape(RoundedRectangle(cornerRadius: 4))
            }
          }
        }
      }
    }.padding(40).frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
      .foregroundStyle(LimmrTheme.ink).background(LimmrTheme.panel)
  }
}
