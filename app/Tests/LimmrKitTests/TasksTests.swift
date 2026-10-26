import Foundation
import Testing
@testable import LimmrKit

@Suite("Q4 task contract") struct TasksTests {
  // Literal wire fixture follows TASKS-CONTRACT; it does not depend on our encoder or demo models.
  static let fixture = #"""
  {
    "id":"tsk_study","title":"Riya's exam prep","goal":"Make study material","kind":"long","status":"waiting",
    "effort_id":"effort-study","chats":[{"id":"study@example.test","name":"Study group"}],
    "people":[{"id":"maya","name":"Maya"}],"circle_ids":["study"],
    "created_by":{"agent_id":"agent-test","name":"Study AI"},"created_at":"2026-10-10T12:00:00Z","updated_at":"2026-10-17T12:00:00Z",
    "closed_at":null,"close_reason":null,"summary":{"text":"Practice set ready","updated_at":"2026-10-17T12:00:00Z","updated_by":"Study AI"},
    "items":[
      {"id":"link","kind":"link","title":"Notebook","url":"https://notebooklm.google.com/","source":"chat","message_ref":{"chat_jid":"study@example.test","message_id":"msg1"},"added_by":"Study AI","added_at":"2026-10-17T12:00:00Z"},
      {"id":"file","kind":"file","title":"Practice.pdf","attachment_id":"attachment1","source":"ai","added_by":"Study AI","added_at":"2026-10-17T12:00:00Z"},
      {"id":"note","kind":"note","title":"Next session","text":"Recall first","source":"owner","added_by":"You","added_at":"2026-10-17T12:00:00Z"}
    ],
    "checklist":[{"id":"step1","text":"Read","done":true,"done_at":"2026-10-17T12:00:00Z","order":0}],
    "progress":{"done":1,"total":3},
    "check_in":{"rule":"weekly","weekday":0,"at_time":"18:00","to":"study@example.test","note":"Progress","next_at":"2026-10-18T18:00:00Z","last_at":"2026-10-11T18:00:00Z"},
    "waiting":{"on":{"id":"maya","name":"Maya"},"what":"the syllabus","since":"2026-10-13T12:00:00Z","nudge_after_days":3,"nudge_state":"queued","nudge_suggestion_id":"sug1"},
    "lifespan_days":14,"updates_sent":[{"at":"2026-10-11T18:00:00Z","chat_jid":"study@example.test","message_id":"msg2","text_preview":"Here is the set","kind":"check_in","by":"Study AI"}],
    "needs_you":true,"needs_you_reason":"Nudge ready","future_field":{"anything":true}
  }
  """#
  @Test func decodesEveryWorkspaceShapeFromLiteralContractJSON() throws {
    let task = try Wire.decode(TrackedTask.self, from: Data(Self.fixture.utf8))
    #expect(task.id == "tsk_study" && task.effortId == "effort-study")
    #expect(task.createdBy.agentId == "agent-test" && task.createdBy.name == "Study AI")
    #expect(task.people.first?.name == "Maya" && task.chats.first?.name == "Study group")
    #expect(task.circleIds == ["study"] && task.closedAt == nil && task.closeReason == nil)
    #expect(task.summary.text == "Practice set ready" && task.summary.updatedBy == "Study AI")
    #expect(task.items.map(\.kind) == ["link", "file", "note"])
    #expect(task.items[0].messageRef?.messageId == "msg1")
    #expect(task.items[1].attachmentId == "attachment1" && task.items[2].text == "Recall first")
    #expect(task.checklist[0].done && task.checklist[0].order == 0 && task.checklist[0].doneAt != nil)
    #expect(task.progress == TaskProgress(done: 1, total: 3))
    #expect(task.checkIn?.rule == "weekly" && task.checkIn?.weekday == 0 && task.checkIn?.atTime == "18:00")
    #expect(task.checkIn?.to == "study@example.test" && task.checkIn?.lastAt != nil)
    #expect(task.waiting?.on?.name == "Maya" && task.waiting?.nudgeSuggestionId == "sug1")
    #expect(task.waiting?.nudgeAfterDays == 3 && task.waiting?.nudgeState == "queued")
    #expect(task.updatesSent[0].messageId == "msg2" && task.updatesSent[0].kind == "check_in")
    #expect(task.needsYou && task.needsYouReason == "Nudge ready" && task.lifespanDays == 14)
  }
  @Test func missingAndFutureFieldsStayReadable() throws {
    let task = try Wire.decode(TrackedTask.self, from: Data(#"{"id":"tsk_future","kind":"future","status":"paused","summary":{},"check_in":{},"waiting":{"on":null},"items":[{"kind":"future"}],"checklist":[{}],"progress":{},"updates_sent":[{}]}"#.utf8))
    #expect(task.kind == "future" && task.status == "paused")
    #expect(task.title.isEmpty && task.chats.isEmpty && task.progress.total == 0)
    #expect(task.waiting?.on == nil && task.waiting?.nudgeAfterDays == 3)
    #expect(task.checkIn?.atTime == "18:00" && task.items[0].kind == "future")
    #expect(try Wire.decode(TasksResponse.self, from: Data("{}".utf8)).tasks.isEmpty)
    #expect(try Wire.decode(HomeResponse.self, from: Data("{}".utf8)).tasks.isEmpty)
    let suggestion = try Wire.decode(CircleSuggestion.self, from: Data(#"{"id":"s1","kind":"check_in","task_id":"tsk_future"}"#.utf8))
    #expect(suggestion.taskId == "tsk_future" && suggestion.draftText == nil)
    let oldSettings = try Wire.decode(AppSettings.self, from: Data(#"{"owner":"Alex","retention_days":180}"#.utf8))
    #expect(!oldSettings.accessoryCue.enabled)
  }
  @Test func summaryAndHomeEnvelopesAndAccessorySetting() throws {
    let json = #"{"tasks":[{"id":"tsk_1","title":"Syllabus","kind":"long","status":"waiting","progress":{"done":2,"total":4},"next_check_in_at":"2026-10-18T18:00:00Z","waiting":{"on_name":"Maya","what":"the syllabus","since":"2026-10-13T12:00:00Z","days":4},"needs_you":true,"needs_you_reason":"Nudge","last_update_at":"2026-10-11T18:00:00Z","effort_id":"effort-study"}]}"#
    let list = try Wire.decode(TasksResponse.self, from: Data(json.utf8))
    let home = try Wire.decode(HomeResponse.self, from: Data(json.utf8))
    #expect(home.tasks == list.tasks && home.tasks[0].waiting?.days == 4)
    #expect(home.tasks[0].lastUpdateAt != nil && home.tasks[0].effortId == "effort-study")
    let settings = try Wire.decode(AppSettings.self, from: Data(##"{"owner":"Alex","retention_days":180,"accessory_cue":{"enabled":true,"color":"#5e5ce6","quiet_hours":{"start":"23:00","end":"08:00"}}}"##.utf8))
    #expect(settings.accessoryCue.enabled && settings.accessoryCue.quietHours?.end == "08:00")
    let wire = try #require(JSONSerialization.jsonObject(with: Wire.data(settings)) as? [String: Any])
    #expect((wire["accessory_cue"] as? [String: Any])?["enabled"] as? Bool == true)
  }
  @Test func progressWaitingAndLocalCheckInCopy() throws {
    #expect(TaskProgress(done: 2, total: 4).text == "2 of 4 steps")
    #expect(TaskProgress().fraction == 0 && TaskProgress(done: 8, total: 3).fraction == 1)
    let date = try #require(Timestamp.date("2026-10-17T12:00:00Z"))
    #expect(TaskPresentation.days(since: "2026-10-13T12:00:00Z", now: date) == 4)
    #expect(TaskPresentation.waiting(.init(onName: "Maya", days: 4)) == "Waiting on Maya · 4 days")
    #expect(TaskPresentation.waiting(.init(onName: "Maya", what: "the syllabus", days: 4)) == "Waiting on Maya for the syllabus · 4 days")
    #expect(TaskPresentation.checkIn("2026-10-18T18:00:00Z", timeZone: TimeZone(secondsFromGMT: 0)!) == "Sun 6 pm")
    #expect(TaskPresentation.checkIn("2026-10-18T12:30:00Z", timeZone: TimeZone(identifier: "Asia/Kolkata")!) == "Sun 6 pm")
    #expect(TaskPresentation.safeLink("file:///private/secret") == nil)
    #expect(TaskPresentation.safeLink("javascript:alert(1)") == nil)
    #expect(TaskPresentation.linkLabel("https://notebooklm.google.com/") == "NotebookLM")
    #expect(TaskPresentation.linkLabel("https://docs.google.com/") == "Google Docs")
  }
  @Test func createTickScheduleWaitingCloseAndSkipPersistInDemo() async throws {
    let backend = DemoBackend(); let client = TaskClient(backend: backend)
    let created = try await client.create(.init(effort: DemoEfforts.efforts[2]))
    #expect(created.effortId == DemoEfforts.efforts[2].id && created.chats.count == 2)
    let detail: EffortDetailResponse = try await backend.get("/v1/efforts/\(DemoEfforts.efforts[2].id)")
    #expect(detail.taskId == created.id)
    let ticked = try await client.step("tsk_study", step: "s3", done: true)
    #expect(ticked.progress.done == 3 && ticked.checklist[2].done)
    let waiting = TaskWaiting(what: "a time", since: DemoData.now, nudgeAfterDays: 5)
    let changed = try await client.patch(created.id, .init(lifespanDays: 10, checkIn: .init(rule: "every_n_days", everyDays: 2, to: "fake@example.test"), waiting: waiting))
    #expect(changed.lifespanDays == 10 && changed.checkIn?.everyDays == 2 && changed.status == "waiting")
    let cleared = try await client.patch(created.id, .init(clearWaiting: true, clearCheckIn: true))
    #expect(cleared.waiting == nil && cleared.checkIn == nil && cleared.status == "active")
    let skipped = try await client.skipNudge("tsk_syllabus")
    #expect(skipped.waiting?.nudgeState == "skipped" && !skipped.needsYou)
    let home: HomeResponse = try await backend.get("/v1/home")
    #expect(home.tasks.first?.needsYou == true && !home.queued.contains { $0.id == "task-nudge" })
    let finished = try await client.step("tsk_appointment", step: "a2", done: true)
    #expect(finished.status == "done" && finished.closedAt != nil)
    let dropped = try await client.patch(created.id, .init(status: "dropped"))
    #expect(dropped.closeReason == "owner")
    let closed: TasksResponse = try await backend.get("/v1/tasks?status=closed")
    #expect(closed.tasks.contains { $0.id == created.id })
  }
  @Test func schedulesUseLocalWallTimeAndRejectPastOneOffs() throws {
    var calendar = Calendar(identifier: .gregorian); calendar.timeZone = TimeZone(identifier: "Asia/Kolkata")!
    let now = try #require(Timestamp.date("2026-10-17T12:00:00Z"))
    let time = try #require(Timestamp.date("2026-10-17T12:30:00Z"))
    let weekly = try #require(TaskSchedule.next(rule: "weekly", time: time, weekday: 0, everyDays: 7, now: now, calendar: calendar))
    #expect(weekly == Timestamp.date("2026-10-18T12:30:00Z"))
    let repeated = TaskSchedule.next(rule: "every_n_days", time: time, weekday: 0, everyDays: 2, now: now, calendar: calendar)
    #expect(repeated == Timestamp.date("2026-10-19T12:30:00Z"))
    #expect(TaskSchedule.next(rule: "once", time: now.addingTimeInterval(-60), weekday: 0, everyDays: 7, now: now) == nil)
  }
  @Test func clearingUsesExplicitNullAndDoesNotOverwriteUneditedFields() throws {
    let object = try #require(JSONSerialization.jsonObject(with: Wire.data(TaskPatch(clearWaiting: true, clearCheckIn: true))) as? [String: Any])
    #expect(object["waiting"] is NSNull && object["check_in"] is NSNull)
    #expect(object["title"] == nil && object["status"] == nil && object["clear_waiting"] == nil)
  }
}
