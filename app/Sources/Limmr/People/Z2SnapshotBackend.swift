import Foundation
import LimmrKit

actor Z2SnapshotBackend: Backend {
  let minimal: Bool
  let taskLatest: Bool
  let taskPreview: Bool
  let base = DemoBackend()
  init(minimal: Bool, taskPreview: Bool = false, taskLatest: Bool = false) { self.taskLatest = taskLatest; self.minimal = minimal; self.taskPreview = taskPreview }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if taskLatest && path == "/v1/tasks/tsk_cv" {
      var task: TrackedTask = try await base.get(path)
      task.title = "Maya’s pitch deck review"; task.summary = .init(); task.effortId = nil
      task.suggestion?.effortIds = []; task.goal = ""; task.needsYou = false
      task.suggestion?.signals = ["Maya is waiting for feedback on her pitch deck"]
      task.latest = [
        .init(chatJid: DemoData.personJid(3), messageId: "deck-3", chat: "Maya Rao", sender: "Maya Rao", timestamp: "2026-10-17T11:50:00Z", text: "The revised deck is ready. Can you review the opening slides before Friday?"),
        .init(chatJid: DemoData.personJid(3), messageId: "deck-2", chat: "Maya Rao", sender: "You", timestamp: "2026-10-17T11:40:00Z", text: "I’ll check the opening and send my notes here."),
        .init(chatJid: DemoData.personJid(3), messageId: "deck-1", chat: "Maya Rao", sender: "Maya Rao", timestamp: "2026-10-17T11:30:00Z", text: "Document")
      ]
      return try Wire.data(task)
    }
    if taskPreview && path == "/v1/tasks/tsk_cv" {
      var task: TrackedTask = try await base.get(path)
      task.title = "Maya’s pitch deck review"
      task.suggestion?.signals = ["Maya is waiting for feedback on her pitch deck"]
      return try Wire.data(task)
    }
    if minimal {
      if path == "/v1/people/person-04/avatar" { throw BackendError.http(404) }
      if path == "/v1/people/person-04" {
        var detail: PersonDetail = try await base.get(path)
        detail.person = Person(id: "person-04", jids: [DemoData.personJid(3)], name: "Maya Rao",
          details: .init(phones: [.init(label: "mobile", value: "+1 555-0103", source: "whatsapp")]))
        detail.facts = []; detail.groups = []; detail.relations = []
        detail.stats = try Wire.decode(PersonStats.self, from: Data(#"{"shared_groups":[],"dm_messages_30":0,"dm_messages_365":0,"messages_in_shared_groups":0}"#.utf8))
        return try Wire.data(detail)
      }
      if path == "/v1/circles" { return Data(#"{"circles":[]}"#.utf8) }
      if path.hasPrefix("/v1/people/person-04/summary") { return Data(#"{"summary":null}"#.utf8) }
      if path.hasPrefix("/v1/tasks?person=") { return Data(#"{"tasks":[]}"#.utf8) }
      if path.hasPrefix("/v1/efforts?person=") { return Data(#"{"efforts":[]}"#.utf8) }
    }
    return try await base.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { AsyncThrowingStream { $0.finish() } }
}
