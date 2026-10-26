import Foundation
import LimmrKit
import Observation
import SwiftUI

@MainActor @Observable final class TaskWorkspace {
  var task: TrackedTask?
  var error: String?
  var saving = false
  var efforts: [EffortDetailResponse] = []
  let id: String
  let client: TaskClient
  let summaryModel: (any TaskSummaryModel)?
  init(id: String, backend: any Backend, initial: TrackedTask? = nil, summaryModel: (any TaskSummaryModel)? = nil) {
    self.id = id; client = TaskClient(backend: backend); task = initial; self.summaryModel = summaryModel
  }
  func load() async {
    do {
      let value = try await client.get(id); try Task.checkCancellation(); task = value; error = nil
      let ids = Set((value.suggestion?.effortIds ?? []) + (value.effortId.map { [$0] } ?? []))
      var linked: [EffortDetailResponse] = []
      for id in ids.sorted() {
        if let detail: EffortDetailResponse = try? await client.backend.get("/v1/efforts/" + id) { linked.append(detail) }
      }
      try Task.checkCancellation(); efforts = linked
      if linked.count == ids.count, let summaryModel {
        // Model/claim failures leave the source messages visible and the task readable.
        if let written = try? await OnDeviceTaskSummaryWriter(client: client, model: summaryModel).write(task: value, efforts: linked.map(\.effort)) {
          task = written
        }
      }
    }
    catch { if !Task.isCancelled { self.error = "Couldn’t refresh this task." } }
  }
  func change(_ action: () async throws -> TrackedTask) async {
    guard !saving else { return }
    saving = true; error = nil
    defer { saving = false }
    do { task = try await action(); await load() }
    catch { self.error = "The task could not be saved. Try again." }
  }
}

extension AppStore {
  func openTask(_ id: String) {
    openActivityRoute(.task(id))
  }
  func skipTaskSuggestion(_ suggestion: CircleSuggestion) async {
    if suggestion.kind == "nudge", let id = suggestion.taskId {
      await perform(action: "suggestion-\(suggestion.id)") {
        _ = try await TaskClient(backend: self.backend).skipNudge(id)
        await self.refresh()
      }
    } else { await decideSuggestion(suggestion, send: false) }
  }
}

struct TaskPage: View {
  @Bindable var store: AppStore
  let id: String
  var startAtHistory = false
  var body: some View {
    VStack(alignment: .leading, spacing: 0) {
      Button(store.activityBackTitle, systemImage: "chevron.left") { store.leaveActivityRoute() }
        .limmrButton().padding(.horizontal, 28).padding(.top, 20)
      ScrollView { TaskWorkspaceView(store: store, id: id) }.defaultScrollAnchor(startAtHistory ? .bottom : .top)
    }.navigationBarBackButtonHidden(true).background { AppBackground() }
  }
}

struct TaskWorkspaceView: View {
  @Bindable var store: AppStore
  @State private var model: TaskWorkspace
  var embedded = false
  init(store: AppStore, id: String, initial: TrackedTask? = nil, embedded: Bool = false) {
    self.store = store; self.embedded = embedded
    _model = State(initialValue: TaskWorkspace(id: id, backend: store.backend, initial: initial, summaryModel: TaskSummaryWriterPolicy.model(demo: store.demo, snapshot: store.snapshot)))
  }
  var body: some View {
    Group {
      if let task = model.task {
        TaskWorkspaceContent(store: store, model: model, task: task, embedded: embedded)
      } else if let error = model.error {
        VStack(alignment: .leading, spacing: 12) {
          Text(error).foregroundStyle(AccessibleSecondary())
          Button("Try again") { Task { await model.load() } }.limmrButton()
        }.padding(28)
      } else { ProgressView().padding(28).accessibilityLabel("Loading task") }
    }.task(id: "\(store.backendGeneration):\(model.id)") {
      let current = TaskWorkspace(id: model.id, backend: store.backend, initial: model.task, summaryModel: TaskSummaryWriterPolicy.model(demo: store.demo, snapshot: store.snapshot))
      model = current
      await current.load()
    }
  }
}

struct TaskWorkspaceContent: View {
  @Bindable var store: AppStore
  @Bindable var model: TaskWorkspace
  let task: TrackedTask
  var embedded = false
  @State private var showAllHistory = false
  @State private var schedule = false
  @State private var waiting = false
  private var now: Date { store.demo ? Timestamp.date(DemoData.now)! : Date() }
  private var closed: Bool { ["done", "dropped"].contains(task.status) }
  private var linkedEfforts: [Effort] { model.efforts.map(\.effort) }
  private var needsYou: Bool { task.needsYou || linkedEfforts.contains { $0.status == "on_you" } }
  private var reply: Message? {
    model.efforts.filter { $0.effort.status == "on_you" }.flatMap(\.timeline).compactMap(\.message)
      .filter { !$0.isFromMe }.sorted { $0.timestamp > $1.timestamp }.first
  }
  private var summary: TaskSummaryText {
    TaskContextPresentation.summary(task, efforts: linkedEfforts)
  }
  private var summaryAuthor: String {
    let value = summary.updatedBy
    if value == "This Mac" || store.agents.contains(where: { $0.name == value }) { return value }
    return store.agentName(value)
  }
  private var chats: [EffortEntity] {
    unique(task.chats + linkedEfforts.flatMap(\.chats))
  }
  private var people: [EffortEntity] {
    unique(task.people + linkedEfforts.flatMap(\.people))
  }
  private func unique(_ values: [EffortEntity]) -> [EffortEntity] {
    var seen = Set<String>(); return values.filter { seen.insert($0.id).inserted }
  }
  var body: some View {
    VStack(alignment: .leading, spacing: 28) {
      VStack(alignment: .leading, spacing: 10) {
        Text(embedded ? "Task workspace" : task.title).font(embedded ? AppType.blockHeading : AppType.pageTitle)
          .fixedSize(horizontal: false, vertical: true)
        Text(task.kind == "short" ? "Short task · \(task.status)" : "Long task · \(task.status)")
          .font(AppType.caption).foregroundStyle(AccessibleSecondary())
        if !task.goal.isEmpty { Text(task.goal).font(.callout).textSelection(.enabled) }
        if embedded { Button("Open task") { store.openTask(task.id) }.limmrButton() }
      }
      if needsYou {
        block("Needs you") {
          Text(reply?.text ?? task.needsYouReason ?? linkedEfforts.first { $0.status == "on_you" }?.summary ?? "This task needs your attention.")
            .font(.callout).textSelection(.enabled)
          if let chat = reply?.chatJid ?? chats.first?.id {
            Button("Open chat", systemImage: "bubble.left") { openChat(chat) }.limmrButton()
          }
        }.padding(16).background(Color.accentColor.opacity(0.07), in: RoundedRectangle(cornerRadius: 10))
      }
      if !summary.text.isEmpty {
        block("What’s happening") {
          Text(summary.text).textSelection(.enabled)
          Text("\(summaryAuthor) · \(store.relativeTime(summary.updatedAt))")
            .font(AppType.caption).foregroundStyle(AccessibleSecondary())
        }
      }
      let latest = TaskContextPresentation.latest(task, efforts: linkedEfforts)
      if !latest.isEmpty {
        block("What’s happening") {
          Text("Latest in the chat").font(.callout.weight(.medium))
          ForEach(Array(latest.enumerated()), id: \.offset) { _, message in
            VStack(alignment: .leading, spacing: 4) {
              HStack(alignment: .firstTextBaseline) {
                Text("\(message.sender) · \(message.chat)").font(AppType.caption).foregroundStyle(AccessibleSecondary())
                Spacer()
                Text(store.relativeTime(message.timestamp)).font(AppType.caption).foregroundStyle(AccessibleSecondary())
              }
              Text(message.text).font(.callout).textSelection(.enabled)
            }.padding(.vertical, 4)
          }
        }
      }
      if !chats.isEmpty || !people.isEmpty {
        block("Chats and people") {
          ForEach(chats) { chat in
            Button(chat.name, systemImage: chat.id.hasSuffix("@g.us") ? "person.3" : "bubble.left") { openChat(chat.id) }
              .limmrButton().accessibilityLabel("Open chat with \(chat.name)")
          }
          ForEach(people) { person in
            Button(person.name, systemImage: "person.crop.circle") {
              store.selectPage(.circles); store.circlePath = [.person(person.id)]
            }.limmrButton().accessibilityLabel("Open \(person.name)’s profile")
          }
        }
      }
      if let suggestion = task.suggestion, !suggestion.signals.isEmpty {
        block("Why Limmr spotted this") {
          Text(suggestion.signals.joined(separator: " · ")).font(AppType.caption).foregroundStyle(AccessibleSecondary())
        }
      }
      if !task.checklist.isEmpty {
        block("Progress") {
          ProgressView(value: task.progress.fraction).accessibilityLabel(task.progress.text)
          Text(task.progress.text).font(AppType.caption).foregroundStyle(AccessibleSecondary())
          ForEach(task.checklist.sorted { $0.order < $1.order }) { step in
            Toggle(step.text, isOn: Binding(get: { step.done }, set: { done in
              Task { await model.change { try await model.client.step(task.id, step: step.id, done: done) }; await store.refresh() }
            })).toggleStyle(.checkbox).disabled(model.saving || closed)
              .accessibilityLabel(step.text).accessibilityValue(step.done ? "Complete" : "Incomplete")
          }
        }
      }
      if !task.items.isEmpty {
        block("Workspace") {
          ForEach(task.items) { item in
            HStack(alignment: .top, spacing: 12) {
              Image(systemName: item.kind == "link" ? "link" : item.kind == "file" ? "doc" : "note.text")
                .foregroundStyle(AccessibleSecondary()).frame(width: 20)
              VStack(alignment: .leading, spacing: 4) {
                if let url = TaskPresentation.safeLink(item.url), item.kind == "link" {
                  Link(item.title, destination: url)
                } else { Text(item.title).font(.callout.weight(.medium)) }
                if let text = item.text, !text.isEmpty { Text(text).font(.callout).textSelection(.enabled) }
                Text(itemMeta(item)).font(AppType.caption).foregroundStyle(AccessibleSecondary())
                if let ref = item.messageRef {
                  Button("From \(task.chats.first { $0.id == ref.chatJid }?.name ?? store.chatName(ref.chatJid))") {
                    store.selectPage(.circles)
                    store.circlePath = [.chat(ref.chatJid, task.chats.first { $0.id == ref.chatJid }?.name ?? store.chatName(ref.chatJid))]
                  }.limmrButton().font(AppType.caption).accessibilityHint("Open the source chat")
                }
              }
            }.padding(.vertical, 4)
          }
        }
      }
      if let checkIn = task.checkIn {
        block("Check-in") {
          Text("Next check-in · \(TaskPresentation.checkIn(checkIn.nextAt))")
          Text("To \(task.chats.first { $0.id == checkIn.to }?.name ?? store.chatName(checkIn.to)) · \(checkIn.note)")
            .font(.callout).foregroundStyle(AccessibleSecondary())
          if !closed { Button("Edit check-in and lifespan") { schedule = true }.limmrButton() }
        }
      } else if !closed { Button("Schedule a check-in") { schedule = true }.limmrButton() }
      if task.kind == "short" { Text("Closes after \(task.lifespanDays ?? 14) quiet days").font(AppType.caption).foregroundStyle(AccessibleSecondary()) }
      if task.waiting != nil { block("Waiting") {
        if let value = task.compact(now: now).waiting {
          Text(TaskPresentation.waiting(value))
          if !closed {
            Button("Clear waiting") { patch(.init(clearWaiting: true)) }.limmrButton()
          }
        }
        if !closed { Button("Edit waiting") { waiting = true }.limmrButton() }
      } } else if !closed { Button("Set waiting") { waiting = true }.limmrButton() }
      if !task.updatesSent.isEmpty {
        block("Updates sent") {
          ForEach(Array(task.updatesSent.enumerated().reversed()), id: \.offset) { _, update in
            VStack(alignment: .leading, spacing: 4) {
              Text(update.textPreview).font(.callout).textSelection(.enabled)
              Text("\(Timestamp.date(update.at)?.formatted(date: .abbreviated, time: .shortened) ?? update.at) · \(update.kind.replacingOccurrences(of: "_", with: " ")) · \(update.by)")
                .font(AppType.caption).foregroundStyle(AccessibleSecondary())
            }
          }
        }
      }
      if !task.history.isEmpty {
        block("History") {
          ForEach(Array(task.history.prefix(showAllHistory ? task.history.count : 8).enumerated()), id: \.offset) { _, event in
            HStack(alignment: .firstTextBaseline, spacing: 12) {
              Image(systemName: "circle.fill").font(.system(size: 5)).accessibilityHidden(true)
              Text(event.text).font(.callout)
              Spacer()
              Text(store.relativeTime(event.at)).font(AppType.caption).foregroundStyle(AccessibleSecondary())
            }
          }
          if task.history.count > 8 && !showAllHistory { Button("Show all") { showAllHistory = true }.limmrButton() }
        }
      }
      if !closed {
        HStack(spacing: 20) {
          Button("Close task", systemImage: "checkmark") { patch(.init(status: "done")) }
          Button("Drop task") { patch(.init(status: "dropped")) }
        }.limmrButton()
      } else { Text("Closed · \(task.closeReason ?? task.status)").font(.callout).foregroundStyle(AccessibleSecondary()) }
      if let error = model.error { Text(error).font(.callout).foregroundStyle(AccessibleSecondary()) }
      if model.saving { ProgressView().controlSize(.small).accessibilityLabel("Saving task") }
    }.disabled(model.saving).frame(maxWidth: 760, alignment: .leading)
      .padding(embedded ? 0 : 28).frame(maxWidth: .infinity, alignment: embedded ? .leading : .center)
      .sheet(isPresented: $schedule) { TaskScheduleEditor(model: model, task: task, now: now) { await store.refresh() } }
      .sheet(isPresented: $waiting) { TaskWaitingEditor(model: model, task: task, now: now) { await store.refresh() } }
  }
  private func openChat(_ id: String) {
    store.selectPage(.circles)
    store.circlePath = id.hasSuffix("@g.us") ? [.group(id)] : [.chat(id, chats.first { $0.id == id }?.name ?? store.chatName(id))]
  }
  private func itemMeta(_ item: WorkspaceItem) -> String {
    let kind = item.kind == "link" ? TaskPresentation.linkLabel(item.url) : item.kind.capitalized
    return "\(kind) · \(item.source == "chat" ? "From chat" : item.addedBy.isEmpty ? item.source : item.addedBy)"
  }
  private func patch(_ request: TaskPatch) {
    Task { await model.change { try await model.client.patch(task.id, request) }; await store.refresh() }
  }
  private func block<Content: View>(_ title: String, @ViewBuilder content: () -> Content) -> some View {
    VStack(alignment: .leading, spacing: 10) {
      Text(title).font(AppType.blockHeading).foregroundStyle(AccessibleSecondary())
      content()
    }.frame(maxWidth: .infinity, alignment: .leading)
  }
}

struct TaskScheduleEditor: View {
  @Bindable var model: TaskWorkspace
  let task: TrackedTask
  let saved: () async -> Void
  let now: Date
  @Environment(\.dismiss) private var dismiss
  @State private var enabled: Bool
  @State private var rule: String
  @State private var weekday: Int
  @State private var everyDays: Int
  @State private var time: Date
  @State private var destination: String
  @State private var note: String
  @State private var lifespan: Int
  init(model: TaskWorkspace, task: TrackedTask, now: Date, saved: @escaping () async -> Void) {
    self.model = model; self.task = task; self.saved = saved; self.now = now
    _enabled = State(initialValue: task.checkIn != nil); _rule = State(initialValue: task.checkIn?.rule ?? "weekly")
    _weekday = State(initialValue: task.checkIn?.weekday ?? 0); _everyDays = State(initialValue: task.checkIn?.everyDays ?? 7)
    let components = (task.checkIn?.atTime ?? "18:00").split(separator: ":").compactMap { Int($0) }
    _time = State(initialValue: Calendar.current.date(bySettingHour: components.first ?? 18, minute: components.last ?? 0, second: 0, of: now) ?? now)
    _destination = State(initialValue: task.checkIn?.to ?? task.chats.first?.id ?? "")
    _note = State(initialValue: task.checkIn?.note ?? ""); _lifespan = State(initialValue: task.lifespanDays ?? 14)
  }
  var body: some View {
    VStack(alignment: .leading, spacing: 20) {
      Text("Check-in and lifespan").font(.title2.bold())
      Form {
        Toggle("Scheduled check-in", isOn: $enabled).disabled(task.chats.isEmpty)
        if task.chats.isEmpty { Text("Ask your AI to add a chat before scheduling a check-in.").font(.callout) }
        if enabled {
          Picker("Repeat", selection: $rule) { Text("Weekly").tag("weekly"); Text("Every few days").tag("every_n_days"); Text("Once").tag("once") }
          if rule == "weekly" {
            Picker("Day", selection: $weekday) { ForEach(0..<7) { Text(Calendar.current.weekdaySymbols[$0]).tag($0) } }
          } else if rule == "every_n_days" { Stepper("Every \(everyDays) days", value: $everyDays, in: 1...60) }
          DatePicker(rule == "once" ? "On" : "At", selection: $time, displayedComponents: rule == "once" ? [.date, .hourAndMinute] : [.hourAndMinute])
          Picker("To", selection: $destination) { ForEach(task.chats) { Text($0.name).tag($0.id) } }
          TextField("What should the update cover?", text: $note, axis: .vertical)
        }
        if task.kind == "short" { Stepper("Close after \(lifespan) quiet days", value: $lifespan, in: 1...365) }
      }
      if let error = model.error { Text(error).font(.callout) }
      HStack {
        Button("Cancel") { dismiss() }.keyboardShortcut(.cancelAction)
        Spacer()
        Button("Save") {
          Task {
            let f = DateFormatter(); f.dateFormat = "HH:mm"
            let checkIn: TaskCheckIn? = enabled ? .init(rule: rule, weekday: rule == "weekly" ? weekday : nil,
              everyDays: rule == "every_n_days" ? everyDays : nil, atTime: f.string(from: time), to: destination,
              note: note, nextAt: TaskSchedule.next(rule: rule, time: time, weekday: weekday, everyDays: everyDays, now: now).map { ISO8601DateFormatter().string(from: $0) } ?? "") : nil
            await model.change { try await model.client.patch(task.id, .init(lifespanDays: task.kind == "short" ? lifespan : nil, checkIn: checkIn, clearCheckIn: !enabled)) }
            if model.error == nil { await saved(); dismiss() }
          }
        }.keyboardShortcut(.defaultAction).disabled(model.saving || (enabled && (destination.isEmpty || note.count > 300 || TaskSchedule.next(rule: rule, time: time, weekday: weekday, everyDays: everyDays, now: now) == nil)))
      }
    }.padding(28).frame(width: 480).disabled(model.saving)
  }
}

struct TaskWaitingEditor: View {
  @Bindable var model: TaskWorkspace
  let task: TrackedTask
  let now: Date
  let saved: () async -> Void
  @Environment(\.dismiss) private var dismiss
  @State private var person: String
  @State private var what: String
  @State private var days: Int
  init(model: TaskWorkspace, task: TrackedTask, now: Date, saved: @escaping () async -> Void) {
    self.model = model; self.task = task; self.now = now; self.saved = saved
    _person = State(initialValue: task.waiting?.on?.id ?? "")
    _what = State(initialValue: task.waiting?.what ?? "")
    _days = State(initialValue: task.waiting?.nudgeAfterDays ?? 3)
  }
  var body: some View {
    VStack(alignment: .leading, spacing: 20) {
      Text("Waiting").font(.title2.bold())
      Form {
        Picker("On", selection: $person) {
          Text("Something else").tag("")
          ForEach(task.people) { Text($0.name).tag($0.id) }
        }
        TextField("What are you waiting for?", text: $what, axis: .vertical)
        Stepper("Offer a nudge after \(days) days", value: $days, in: 1...30)
      }
      if let error = model.error { Text(error).font(.callout) }
      HStack {
        Button("Cancel") { dismiss() }.keyboardShortcut(.cancelAction)
        Spacer()
        Button("Save") {
          Task {
            let on = task.people.first { $0.id == person }
            let sameWait = task.waiting?.on?.id == on?.id && task.waiting?.what == what
            let value = TaskWaiting(on: on, what: what,
              since: sameWait ? task.waiting?.since ?? ISO8601DateFormatter().string(from: now) : ISO8601DateFormatter().string(from: now),
              nudgeAfterDays: days, nudgeState: sameWait ? task.waiting?.nudgeState ?? "none" : "none",
              nudgeSuggestionId: sameWait ? task.waiting?.nudgeSuggestionId : nil)
            await model.change { try await model.client.patch(task.id, .init(waiting: value)) }
            if model.error == nil { await saved(); dismiss() }
          }
        }.keyboardShortcut(.defaultAction).disabled(model.saving || what.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || what.count > 200)
      }
    }.padding(28).frame(width: 480).disabled(model.saving)
  }
}
