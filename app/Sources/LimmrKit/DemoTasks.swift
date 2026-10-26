import Foundation

/// All names and messages are invented. Dates share the sample app's fixed clock.
public enum DemoTasks {
  public static let tasks: [TrackedTask] = {
    let maya = EffortEntity(id: DemoData.personId(3), name: "Maya")
    let mayaChat = EffortEntity(id: DemoData.personJid(3), name: "Maya Rao")
    let riya = EffortEntity(id: DemoData.personId(4), name: "Noor")
    let studyChat = EffortEntity(id: DemoData.personJid(4), name: "Noor Chen")
    let study = TrackedTask(id: "tsk_study", title: "Noor’s exam prep", goal: "Make study material for Noor and track her learning each week.",
      effortId: "effort-lab", chats: [studyChat], people: [riya], circleIds: ["university"],
      createdBy: .init(agentId: "agent-2", name: "Codex"), createdAt: "2026-10-10T12:00:00Z", updatedAt: DemoData.now,
      summary: .init(text: "Noor finished the first practice set. Focus next on memory and learning; the weekly update is ready for review.", updatedAt: DemoData.now, updatedBy: "Codex"),
      items: [
        .init(id: "wi_notebook", kind: "link", title: "Learning revision notebook", url: "https://notebooklm.google.com/", source: "chat", messageRef: .init(chatJid: studyChat.id, messageId: "study-link"), addedBy: "Codex", addedAt: DemoData.now),
        .init(id: "wi_file", kind: "file", title: "Practice set.pdf", attachmentId: "demo-practice", source: "ai", addedBy: "Codex", addedAt: DemoData.now),
        .init(id: "wi_note", kind: "note", title: "Next session", text: "Use short recall questions before reading the notes again.", source: "owner", addedBy: "You", addedAt: DemoData.now)
      ],
      checklist: [.init(id: "s1", text: "Collect the syllabus", done: true, doneAt: DemoData.now, order: 0),
        .init(id: "s2", text: "Make the first practice set", done: true, doneAt: DemoData.now, order: 1),
        .init(id: "s3", text: "Review memory and learning", order: 2), .init(id: "s4", text: "Try a timed paper", order: 3)],
      progress: .init(done: 2, total: 4),
      checkIn: .init(rule: "weekly", weekday: 0, atTime: "18:00", to: studyChat.id, note: "Progress, practice scores and the next topic", nextAt: "2026-10-18T12:30:00Z"),
      updatesSent: [.init(at: "2026-10-11T12:30:00Z", chatJid: studyChat.id, messageId: "study-update", textPreview: "The first practice set is ready. Let’s start with recall questions.", kind: "check_in", by: "Codex")],
      needsYou: true, needsYouReason: "Weekly update is ready")
    let appointment = TrackedTask(id: "tsk_appointment", title: "Book a dentist appointment", goal: "Find a morning appointment next week.", kind: "short",
      createdBy: .init(agentId: "agent-2", name: "Codex"), createdAt: DemoData.now, updatedAt: DemoData.now,
      summary: .init(text: "Two morning slots are available. Choose a time before confirming."),
      checklist: [.init(id: "a1", text: "Find available times", done: true, order: 0), .init(id: "a2", text: "Confirm an appointment", order: 1)],
      progress: .init(done: 1, total: 2), lifespanDays: 14)
    let waiting = TrackedTask(id: "tsk_syllabus", title: "Syllabus from Maya", goal: "Collect the updated syllabus for the study group.", status: "waiting",
      effortId: "effort-slides", chats: [mayaChat], people: [maya], createdBy: .init(agentId: "agent-2", name: "Codex"),
      createdAt: "2026-10-13T12:00:00Z", updatedAt: DemoData.now,
      summary: .init(text: "Maya is checking the final syllabus. A gentle nudge is ready."),
      waiting: .init(on: maya, what: "the syllabus", since: "2026-10-13T12:00:00Z", nudgeState: "queued", nudgeSuggestionId: "task-nudge"),
      needsYou: true, needsYouReason: "Nudge ready to review")
    // Y2 task surfacing fixtures; kept separate from People and Groups sample data.
    var autoKept = appointment
    autoKept.origin = "agent"; autoKept.autoKeptAt = DemoData.now; autoKept.undoChangeId = "demo-auto-appointment"
    let suggestions = [
      TrackedTask(id: "tsk_cv", title: "Maya’s CV review", status: "suggested", createdAt: DemoData.now,
        updatedAt: DemoData.now, origin: "limmr", suggestion: .init(signals: ["Maya is waiting for feedback on her CV"], confidence: 0.7, detectedAt: DemoData.now)),
      TrackedTask(id: "tsk_print", title: "Print the study guide", kind: "short", status: "suggested", createdAt: DemoData.now,
        updatedAt: DemoData.now, origin: "limmr", suggestion: .init(signals: ["The study guide is due this week"], confidence: 0.65, detectedAt: DemoData.now))
    ]
    return ([study, autoKept, waiting] + suggestions).map { value in
      var task = value
      task.history = [
        .init(at: DemoData.now, kind: "step_done", text: "Practice set completed · Codex"),
        .init(at: "2026-10-16T12:00:00Z", kind: "item_added", text: "Revision notebook added · Codex"),
        .init(at: "2026-10-15T12:00:00Z", kind: "kept", text: "Task kept · you"),
        .init(at: "2026-10-14T12:00:00Z", kind: "suggested", text: "Limmr spotted this work")
      ]
      if task.status == "suggested" {
        task.history = [.init(at: task.createdAt, kind: "suggested", text: "Limmr spotted this work")]
      } else if task.autoKeptAt != nil {
        task.history = [.init(at: DemoData.now, kind: "auto_kept", text: "Started tracking automatically · Codex")]
      } else if task.id != "tsk_study" {
        task.history = [.init(at: task.createdAt, kind: "created", text: "Task created · Codex")]
      }
      if task.id == "tsk_cv" {
        task.chats = [mayaChat]; task.people = [maya]
        task.suggestion?.effortIds = ["effort-slides"]
      }
      return task
    }
  }()
  public static var suggestions: [CircleSuggestion] {
    [CircleSuggestion(id: "task-check-in", circleId: "university", kind: "check_in", chatJid: DemoData.personJid(4),
      draftText: "Hi Noor, you’ve finished the first practice set. Shall we focus on memory and learning next?", why: "Weekly update for Noor is due", createdBy: "agent-2", expiresAt: "", taskId: "tsk_study"),
     CircleSuggestion(id: "task-nudge", circleId: "university", kind: "nudge", chatJid: DemoData.personJid(3),
      draftText: "Hi Maya, just checking in on the syllabus. No rush!", why: "Waiting on Maya · 4 days", createdBy: "agent-2", expiresAt: "", taskId: "tsk_syllabus", draftSource: "template")]
  }
}
