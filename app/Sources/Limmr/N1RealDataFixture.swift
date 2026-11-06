import Foundation
import LimmrKit

/// Invented data with the shapes the owner's preview exposed. Never reads a live store.
enum N1RealDataFixture {
  static let now = "2026-11-01T12:00:00Z"
  static let longTitle = "Prepare the Northbridge University workshop, confirm the room and review the student follow-up materials"
  static let longStatus = String(repeating: "Waiting for the room booking and the final list of materials. ", count: 12)
  static func agents(linked: Bool) throws -> [Agent] {
    let raw = #"{"agents":[{"id":"this-mac","name":"This Mac","kind":"on_device","transport":"local","created_at":"2026-10-27T00:00:00Z","enabled":true},{"id":"A","name":"Your dot","kind":"custom","transport":"remote","created_at":"2026-10-27T00:00:00Z","enabled":true},{"id":"B","name":"Pip","kind":"chatgpt","transport":"remote","created_at":"2026-10-27T00:00:00Z","enabled":true}]}"#
    var agents = try Wire.decode(AgentsResponse.self, from: Data(raw.utf8)).agents
    if linked { agents[2].runsOnPeer = "E" }
    return agents
  }
  static var sync: DotSyncStatus {
    var sync = DotSyncStatus.demo("healthy")
    sync.peer.agentId = "A"; sync.peer.engineId = "E"; sync.peer.name = "Your dot"
    sync.peer.lastSyncAt = "2026-10-27T12:00:00Z"
    return sync
  }
  static var circles: [LimmrKit.Circle] {
    DemoCircles.circles.filter { ["you", "university", "flatmates"].contains($0.id) }.map { original in
      var circle = original
      circle.symbol = circle.id == "you" ? "" : "circle.grid.2x2"; circle.icon = nil; circle.topics = []
      if circle.id == "university" { circle.name = "Northbridge University College of Art and Design" }
      circle.people = people.filter { $0.circle == circle.id }.map { person in
        try! Wire.decode(CirclePerson.self, from: Wire.data(["person_id": person.id, "role": "Member", "note": "", "source": "you"]))
      }
      return circle
    }
  }
  static var people: [Person] {
    (0..<120).map { index in
      var person = DemoPeople.people[0]
      person.id = "invented-\(index)"
      person.name = ["Avery", "Casey", "Rowan", "Maya", "Noor", "Robin"][index % 6] + " Field \(index + 1)"
      person.label = ""; person.pushName = person.name; person.photo = nil
      person.jids = ["155501\(String(format: "%05d", index))@s.whatsapp.net"]
      person.circle = index < 80 ? "university" : "flatmates"
      person.details = nil
      return person
    }
  }
  static var denseMap: CircleMap {
    var nodes = [
      CircleMapNode(id: "university", kind: "circle", title: "Northbridge University College of Art and Design"),
      CircleMapNode(id: "flatmates", kind: "circle", title: "Flatmates"),
      CircleMapNode(id: "you", kind: "circle", title: "You")]
    nodes += people.map { person in
      var node = CircleMapNode(id: person.id, kind: "person", title: person.name)
      node.circles = [person.circle]; node.primaryCircleId = person.circle
      return node
    }
    return CircleMap(nodes: nodes, edges: people.map {
      CircleMapEdge(source: $0.circle, target: $0.id, kind: "member", state: "confirmed")
    }, pulses: [])
  }
  static var tasks: [TaskSummary] {
    var task = TaskSummary(id: "invented-workshop", title: longTitle)
    task.needsYouReason = longStatus; task.circleIds = ["university"]; task.lastActiveAt = "2026-10-30T12:00:00Z"
    var files = TaskSummary(id: "invented-files", title: "Prepare the workshop handouts")
    files.circleIds = ["university"]; files.lastActiveAt = "2026-10-29T12:00:00Z"
    var route = TaskSummary(id: "invented-route", title: "Plan the weekend walk")
    route.circleIds = ["flatmates"]; route.lastActiveAt = "2026-10-31T12:00:00Z"
    return [task, files, route]
  }
  static var recentLines: [HomeRecentLine] { [
    .init(text: longTitle + ": Step completed.", targetId: "invented-workshop", at: "2026-10-30T12:00:00Z"),
    .init(text: "Prepare the workshop handouts: Added 3 files.", targetId: "invented-files", at: "2026-10-29T12:00:00Z"),
    .init(text: "Plan the weekend walk: Added a link: example.com.", targetId: "invented-route", at: "2026-10-31T12:00:00Z")]
  }
  static var activity: [Activity] {
    var read = DemoData.activity[0]
    read.agentId = "B"; read.tool = "list_messages"; read.kind = "read"; read.count = 1
    read.at = "2026-11-01T11:30:00Z"; read.chatJids = []; read.messageIds = ["invented-message"]
    var rows = (0..<12).map { index in var row = read; row.id = Int64(8200 + index); return row }
    var summary = read; summary.id = 8300; summary.kind = "summary"; summary.tool = "save_summary"
    summary.note = "Saved a workshop summary"; summary.at = "2026-11-01T11:35:00Z"
    rows.append(summary)
    return rows
  }
}

actor N1RealDataSnapshotBackend: Backend {
  let base = DemoBackend()
  let linked: Bool
  init(linked: Bool) { self.linked = linked }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    let route = URLComponents(string: path)?.path ?? path
    guard method == "GET" else { throw BackendError.http(405) }
    switch route {
    case "/v1/agents": return try Wire.data(["agents": try N1RealDataFixture.agents(linked: linked)])
    case "/v1/sync": return try Wire.data(N1RealDataFixture.sync)
    case "/v1/settings/briefs": return try Wire.data(BriefSettings(onDeviceEnabled: !linked))
    case "/v1/approvals": return Data(#"{"approvals":[]}"#.utf8)
    case "/v1/efforts": return Data(#"{"efforts":[]}"#.utf8)
    case "/v1/efforts/unnamed": return Data(#"{"efforts":[],"total":0}"#.utf8)
    case "/v1/activity": return try Wire.data(["items": N1RealDataFixture.activity])
    case "/v1/people": return try Wire.data(["people": N1RealDataFixture.people])
    case "/v1/circles": return try Wire.data(["circles": N1RealDataFixture.circles])
    case "/v1/circles/you":
      return try Wire.data(CircleDetail(circle: N1RealDataFixture.circles.first { $0.id == "you" }!, chats: [], people: [], topics: [], brief: nil))
    case "/v1/map": return try Wire.data(N1RealDataFixture.denseMap)
    case "/v1/tasks":
      return try Wire.data(TasksResponse(tasks: path.contains("status=all") ? [N1RealDataFixture.tasks[0]] : N1RealDataFixture.tasks))
    case "/v1/stats/history":
      return try Wire.data(StatsHistory(days: [HistoryDay(date: "2026-11-01", agentReads: 12), HistoryDay(date: "2026-10-28", messagesIn: 20), HistoryDay(date: "2026-09-12", messagesIn: 8)]))
    case "/v1/home":
      var home: HomeResponse = try await base.get(path)
      home.tasks = N1RealDataFixture.tasks; home.recentLines = N1RealDataFixture.recentLines
      home.recently = home.recentLines.map(\.text); home.recentActivity = N1RealDataFixture.activity
      home.queued = []; home.suggestions = []; home.proposals = []; home.needsReply = []; home.waiting = []; home.circleChanges = []
      return try Wire.data(home)
    default: return try await base.request(path, method: method, body: body)
    }
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { .init { $0.finish() } }
}
