import Foundation
import LimmrKit

/// Read-only invented workspaces for the N1 acceptance captures.
struct N1TasksSnapshotBackend: Backend {
  let page: String
  let base = DemoBackend()
  var tasks: [TrackedTask] {
    let circle = page == "n1-tasks-circle" ? ["university"] : []
    var active = TrackedTask(id: "tsk_exam", title: "Exam preparation", goal: "Prepare memory and learning revision material for Maya.", circleIds: circle,
      createdBy: .init(agentId: "agent-2", name: "Codex"), createdAt: "2026-10-15T09:00:00Z", updatedAt: DemoData.now,
      summary: .init(text: "The practice set is ready. Review the recall questions next.", updatedAt: DemoData.now, updatedBy: "Codex"), needsYou: true, needsYouReason: "Practice set ready to review")
    active.lastActiveAt = "2026-10-17T10:30:00Z"
    var newer = TrackedTask(id: "tsk_print", title: "Print the study guide", circleIds: circle, createdBy: .init(agentId: "agent-2", name: "Codex"), createdAt: DemoData.now, updatedAt: DemoData.now)
    newer.lastActiveAt = "2026-10-17T12:00:00Z"
    var quiet = TrackedTask(id: "tsk_quiet", title: "Design Club reading list", status: "quiet", circleIds: circle, createdAt: "2026-10-01T09:00:00Z", updatedAt: DemoData.now)
    quiet.lastActiveAt = "2026-10-09T09:00:00Z"
    var done = TrackedTask(id: "tsk_done", title: "Collect the syllabus", status: "done", circleIds: circle, createdAt: "2026-10-10T09:00:00Z", updatedAt: DemoData.now,
      closedAt: "2026-10-16T12:00:00Z", closeReason: "agent_done")
    done.lastActiveAt = done.closedAt
    if page == "n1-tasks-cover" {
      active.needsYou = false; active.needsYouReason = nil
      active.history = [.init(at: DemoData.now, kind: "cover_reviewed", text: "Automatic cover reviewed · This Mac")]
      var meme = WorkspaceItem(id: "meme-rejected", kind: "file", title: "Reaction.png", attachmentId: "media:meme", source: "chat", messageRef: try! Wire.decode(EffortMessageRef.self, from: Wire.data(["chat_jid": DemoData.personJid(3), "message_id": "meme"])), addedBy: "chat", addedAt: DemoData.now)
      meme.mediaType = "image"; meme.caption = "When you finish exam preparation lol"; meme.width = 1200; meme.height = 1600
      active.items = [meme]; active.cover = nil
      return [active]
    }
    return [active, newer, quiet, done]
  }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if path == "/v1/home" {
      var home: HomeResponse = try await base.get(path)
      home.tasks = tasks.filter { ["active", "waiting"].contains($0.status) }.map { $0.compact(now: Timestamp.date(DemoData.now)!) }
      home.recently = page == "n1-tasks-cover" ? [] : ["3 tasks changed in the last 24 hours.", "1 note update · 1 check-in · 1 other AI action.", "Codex · Exam preparation: Practice set ready for review.", "You · Print the study guide: Added a note."]
      home.waiting = []; home.watches = []; home.queued = []; home.suggestions = []; home.proposals = []; home.needsReply = []
      home.circleUpdates = []; home.circleChanges = []; home.circleAdditions = []
      return try Wire.data(home)
    }
    if path.hasPrefix("/v1/tasks?") { return try Wire.data(TasksResponse(tasks: tasks.map { $0.compact(now: Timestamp.date(DemoData.now)!) })) }
    if let task = tasks.first(where: { path == TaskClient.path($0.id) }) { return try Wire.data(task) }
    if path == "/v1/circles/university/briefs" { return Data(#"{"briefs":[]}"#.utf8) }
    if path == "/v1/circles/university" {
      var circle: LimmrKit.Circle = try await base.get(path)
      circle.people = []; circle.topics = []; circle.chats = []
      return try Wire.data(circle)
    }
    return try await base.request(path, method: method, body: body)
  }
  func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { .init { $0.finish() } }
}
