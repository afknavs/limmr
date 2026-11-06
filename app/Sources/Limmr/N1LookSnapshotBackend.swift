import AppKit
import Foundation
import LimmrKit

/// Local, invented acceptance data. All previews are created under LIMMR_HOME.
struct N1LookSnapshotBackend: Backend {
  let base: DemoBackend
  let tasks: [TrackedTask]
  let previews: [String: URL]
  static func make() throws -> Self {
    let root = URL(fileURLWithPath: ProcessInfo.processInfo.environment["LIMMR_HOME"]!).appendingPathComponent("n1-look-assets")
    try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    func cover(_ name: String, ink: NSColor, title: String, label: String, document: Bool = false) throws -> URL {
      let image = NSImage(size: .init(width: 1000, height: 1250), flipped: false) { bounds in
        ink.setFill(); bounds.fill()
        if document {
          NSColor.white.withAlphaComponent(0.95).setFill(); NSBezierPath(roundedRect: .init(x: 90, y: 90, width: 820, height: 1070), xRadius: 12, yRadius: 12).fill()
          ink.withAlphaComponent(0.18).setFill()
          for y in stride(from: 220, to: 830, by: 55) { NSRect(x: 150, y: y, width: y % 2 == 0 ? 620 : 520, height: 10).fill() }
        } else {
          NSColor.white.withAlphaComponent(0.08).setStroke()
          for radius in stride(from: 150, to: 1100, by: 90) { let path = NSBezierPath(ovalIn: .init(x: 500 - radius, y: 720 - radius, width: radius*2, height: radius*2)); path.lineWidth = 3; path.stroke() }
          NSColor.white.withAlphaComponent(0.12).setFill(); NSBezierPath(ovalIn: .init(x: 500, y: -200, width: 700, height: 700)).fill()
        }
        let foreground = document ? ink : NSColor.white
        (label.uppercased() as NSString).draw(in: .init(x: 150, y: document ? 1050 : 980, width: 700, height: 65), withAttributes: [.font: NSFont.systemFont(ofSize: 25, weight: .semibold), .foregroundColor: foreground])
        (title as NSString).draw(in: .init(x: 150, y: document ? 860 : 420, width: 700, height: document ? 160 : 350), withAttributes: [.font: NSFont.systemFont(ofSize: document ? 66 : 102, weight: .bold), .foregroundColor: foreground])
        if !document { ("A DESIGN CLUB SESSION\n5 NOVEMBER · NORTH HALL" as NSString).draw(in: .init(x: 150, y: 110, width: 700, height: 110), withAttributes: [.font: NSFont.systemFont(ofSize: 27, weight: .medium), .foregroundColor: foreground]) }
        return true
      }
      let url = root.appendingPathComponent(name + ".png")
      let bitmap = NSBitmapImageRep(data: image.tiffRepresentation!)!
      try bitmap.representation(using: .png, properties: [:])!.write(to: url)
      return url
    }
    let poster = try cover("poster", ink: NSColor(srgbRed: 0.12, green: 0.20, blue: 0.40, alpha: 1), title: "Make ideas\nwith AI.", label: "Design Club / 2026")
    let guide = try cover("guide", ink: NSColor(srgbRed: 0.55, green: 0.38, blue: 0.20, alpha: 1), title: "Memory &\nlearning", label: "Practice set 02", document: true)
    let trail = try cover("trail", ink: NSColor(srgbRed: 0.18, green: 0.38, blue: 0.31, alpha: 1), title: "Out of\nthe city.", label: "Weekend walks")
    var items: [WorkspaceItem] = []
    for (id, name) in [("poster", "Workshop poster"), ("guide", "Memory practice set"), ("trail", "Weekend route")] {
      var item = WorkspaceItem(id: id, kind: "file", title: name + ".png", addedBy: "Codex", addedAt: DemoData.now)
      item.aiName = name; item.description = "Ready for your review"; item.mediaType = "image"; item.namedOnDevice = true; item.width = 1000; item.height = 1250; items.append(item)
    }
    var workshop = DemoTasks.tasks[0]
    workshop.id = "tsk_workshop"; workshop.title = "AI workshop"; workshop.goal = "Prepare the Design Club session for 5 November."
    workshop.items = items; workshop.cover = items[0]; workshop.needsYouReason = "Poster ready for your review"
    workshop.summary = .init(text: "The poster and practice guide are ready. Confirm North Hall, then review the session plan.", updatedAt: DemoData.now, updatedBy: "Codex")
    workshop.ownerNotes = []; workshop.latest = []; workshop.history = []; workshop.checkIn = nil; workshop.waiting = nil
    workshop.checklist = Array(workshop.checklist.prefix(2))
    workshop.checklist[0].text = "Prepare the poster and practice guide"; workshop.checklist[0].done = true
    workshop.checklist[1].text = "Confirm North Hall"; workshop.checklist[1].done = false
    workshop.progress = .init(done: 1, total: 2)
    var revision = DemoTasks.tasks[0]; revision.id = "tsk_revision"; revision.title = "Memory revision"; revision.cover = items[1]; revision.items = [items[1]]; revision.needsYou = false; revision.needsYouReason = nil
    var weekend = revision; weekend.id = "tsk_weekend"; weekend.title = "Weekend trail"; weekend.circleIds = ["friends"]; weekend.cover = items[2]; weekend.items = [items[2]]; weekend.createdBy = .init(agentId: "agent-3", name: "Pip"); weekend.progress = .init(done: 1, total: 3)
    var dinner = revision; dinner.id = "tsk_dinner"; dinner.title = "Family dinner"; dinner.circleIds = ["family"]; dinner.cover = nil; dinner.items = []; dinner.progress = .init(done: 2, total: 3); dinner.createdBy = .init(name: "You")
    var quiet = dinner; quiet.id = "tsk_quiet"; quiet.title = "Book club reading list"; quiet.status = "quiet"; quiet.circleIds = ["friends"]
    var done = dinner; done.id = "tsk_done"; done.title = "Collect the syllabus"; done.status = "done"; done.closedAt = DemoData.now; done.closeReason = "agent_done"
    return .init(base: DemoBackend(dotState: "healthy"), tasks: [workshop, revision, weekend, dinner, quiet, done], previews: ["poster": poster, "guide": guide, "trail": trail])
  }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    let route = URLComponents(string: path)?.path ?? path
    if route == "/v1/home" {
      var home: HomeResponse = try await base.get(path)
      home.tasks = tasks.filter { $0.status == "active" }.map { $0.compact(now: Timestamp.date(DemoData.now)!) }
      home.queued = []; home.suggestions = []; home.proposals = []; home.needsReply = []; home.waiting = []
      home.recently = ["Codex · Memory revision: Practice set completed."]
      home.recentLines = [.init(text: "Codex finished the memory practice set.", targetId: "tsk_revision", at: DemoData.now, by: "Codex"), .init(text: "Pip added a route for your weekend walk.", targetId: "tsk_weekend", at: DemoData.now, by: "Pip")]
      return try Wire.data(home)
    }
    if route == "/v1/approvals" { return Data(#"{"approvals":[]}"#.utf8) }
    if route == "/v1/agents" {
      var response: AgentsResponse = try await base.get(path)
      if let i = response.agents.firstIndex(where: { $0.id == "agent-3" }) { response.agents[i].name = "Pip"; response.agents[i].runsOnPeer = "demo-dot"; response.agents[i].lastSeenAt = "2026-10-17T10:00:00Z" }
      return try Wire.data(response)
    }
    if route == "/v1/tasks" { return try Wire.data(TasksResponse(tasks: tasks.map { $0.compact(now: Timestamp.date(DemoData.now)!) })) }
    if route.hasPrefix("/v1/tasks/") {
      let parts = route.split(separator: "/")
      if parts.count == 6, parts[3] == "items", parts[5] == "preview", let url = previews[String(parts[4])] { return try Wire.data(["path": url.path]) }
      if parts.count == 3, let task = tasks.first(where: { $0.id == String(parts[2]) }) { return try Wire.data(task) }
    }
    return try await base.request(path, method: method, body: body)
  }
  func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { .init { $0.finish() } }
}
