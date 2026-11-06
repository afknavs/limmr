import AppKit
import Foundation
import LimmrKit
import Observation
import SwiftUI

@MainActor @Observable final class TaskWorkspace {
  let assetPreviews = TaskAssetTileCache()
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
  var loadedAt: Date?
  var loading = false
  var detailsLoaded = false
  func load(force: Bool = false) async {
    guard !loading, force || loadedAt == nil || Date().timeIntervalSince(loadedAt!) > 30 else { return }
    loading = true
    defer { loading = false }
    do {
      let value = try await client.get(id); try Task.checkCancellation(); task = value; error = nil
      loadedAt = Date()

    }
    catch { if !Task.isCancelled { self.error = "Couldn’t refresh this task." } }
  }
  func loadDetails() async {
    if task == nil { await load() }
    guard !detailsLoaded, let task else { return }
    let ids = Set((task.suggestion?.effortIds ?? []) + (task.effortId.map { [$0] } ?? []))
    let backend = client.backend
    efforts = await withTaskGroup(of: EffortDetailResponse?.self) { group in
      for id in ids { group.addTask { try? await backend.get("/v1/efforts/" + id) } }
      var values: [EffortDetailResponse] = []
      for await value in group { if let value { values.append(value) } }
      return values
    }
    detailsLoaded = true
    if efforts.count == ids.count, let summaryModel,
       let written = try? await OnDeviceTaskSummaryWriter(client: client, model: summaryModel).write(task: task, efforts: efforts.map(\.effort)) {
      self.task = written
    }
  }
  func change(_ action: () async throws -> TrackedTask) async {
    guard !saving else { return }
    saving = true; error = nil
    defer { saving = false }
    do { task = try await action(); await load(force: true) }
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
      ScrollView {
        VStack(alignment: .leading, spacing: 16) {
          HStack {
            Button(store.activityBackTitle, systemImage: "chevron.left") { store.leaveActivityRoute() }.buttonStyle(.bordered)
            Spacer()
          }
          TaskWorkspaceView(store: store, id: id)
        }.padding(20).frame(maxWidth: 1120).frame(maxWidth: .infinity)
      }.defaultScrollAnchor(startAtHistory ? .bottom : .top)
    }.navigationBarBackButtonHidden(true).background { Color(nsColor: .windowBackgroundColor) }
  }
}

struct TaskWorkspaceView: View {
  @Bindable var store: AppStore
  @State private var model: TaskWorkspace
  var embedded = false
  init(store: AppStore, id: String, initial: TrackedTask? = nil, embedded: Bool = false) {
    self.store = store; self.embedded = embedded
    _model = State(initialValue: store.workspace(id, initial: initial))
  }
  var body: some View {
    Group {
      if let task = model.task {
        TaskWorkspaceContent(store: store, model: model, task: task, embedded: embedded)
      } else if let error = model.error {
        VStack(alignment: .leading, spacing: 12) {
          Text(error).foregroundStyle(.secondary)
          Button("Try again") { Task { await model.load() } }.buttonStyle(.bordered)
        }.padding(28)
      } else { ProgressView().padding(28).accessibilityLabel("Loading task") }
    }.task(id: "\(store.backendGeneration):\(model.id)") {
      let current = store.workspace(model.id, initial: model.task)
      model = current
      await current.load()
      guard !store.demo, !store.snapshot else { return }
      while !Task.isCancelled {
        do { try await Task.sleep(for: .seconds(30)) } catch { return }
        await current.load()
      }
    }
  }
}

struct TaskWorkspaceContent: View {
  @Bindable var store: AppStore
  @Bindable var model: TaskWorkspace
  let task: TrackedTask
  var embedded = false
  @State private var showAllHistory = false
  @State private var assetFilter = "All"
  @State private var assetQuery = ""
  @State private var schedule = false
  @State private var waiting = false
  private var now: Date { store.presentationNow }
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
    VStack(alignment: .leading, spacing: 18) {
      HStack(alignment: .top) {
        VStack(alignment: .leading, spacing: 6) {
          Text(embedded ? "Task workspace" : task.title).font(embedded ? .title3.weight(.semibold) : .largeTitle.bold())
          Text("\(task.kind == "short" ? "Short task" : "Ongoing task") · \(closed ? "Closed" : task.status.capitalized)")
            .font(.caption).foregroundStyle(.secondary)
        }
        Spacer()
        Menu {
          if closed { Button("Reopen task") { reopen() } }
          else {
            Button(task.status == "quiet" ? "Wake task" : "Make quiet") { patch(.init(status: task.status == "quiet" ? "active" : "quiet")) }
            Button("Done") { patch(.init(status: "done")) }
            Button("Not now") { patch(.init(status: "dropped")) }
          }
          if embedded { Button("Open task") { store.openTask(task.id) } }
        } label: { Image(systemName: "ellipsis").frame(width: 24, height: 24) }
          .menuStyle(.borderedButton).fixedSize().accessibilityLabel("Task actions")
      }
      HStack(alignment: .top) {
        VStack(alignment: .leading, spacing: 6) {
          Text(task.circleIds.isEmpty ? "No circle" : task.circleIds.map { store.circleName($0) }.joined(separator: " · ")).font(.callout)
          ForEach(task.circleSuggestions, id: \.circleId) { suggestion in
            HStack {
              Text("Suggested \(store.circleName(suggestion.circleId)) · \(suggestion.reason)").font(.caption).foregroundStyle(.secondary)
              Button("Undo") { var request = TaskPatch(); request.circleIds = task.circleIds.filter { $0 != suggestion.circleId }; patch(request) }
            }
          }
        }
        Spacer()
        Menu("Change circles") {
          Button("No circle") { var request = TaskPatch(); request.circleIds = []; patch(request) }
          ForEach(store.circles) { circle in
            Button((task.circleIds.contains(circle.id) ? "✓ " : "") + circle.name) {
              var request = TaskPatch(); request.circleIds = task.circleIds.contains(circle.id) ? task.circleIds.filter { $0 != circle.id } : task.circleIds + [circle.id]; patch(request)
            }
          }
        }.fixedSize()
      }
      ViewThatFits(in: .horizontal) {
        HStack(alignment: .top, spacing: 16) {
          statusPanel.frame(minWidth: 340, maxWidth: .infinity)
          TaskNotesWidget(model: model, task: task).frame(width: 300)
        }
        VStack(spacing: 16) { TaskNotesWidget(model: model, task: task); statusPanel }
      }
      filesPanel
      ViewThatFits(in: .horizontal) {
        HStack(alignment: .top, spacing: 16) {
          historyPanel.frame(minWidth: 340, maxWidth: .infinity)
          peoplePanel.frame(width: 300)
        }
        VStack(spacing: 16) { peoplePanel; historyPanel }
      }
      if let error = model.error { Text(error).font(.callout).foregroundStyle(.secondary) }
    }.disabled(model.saving).frame(maxWidth: embedded ? 760 : 1072, alignment: .leading)
      .sheet(isPresented: $schedule) { TaskScheduleEditor(model: model, task: task, now: now) { await store.refreshTaskHome() } }
      .sheet(isPresented: $waiting) { TaskWaitingEditor(model: model, task: task, now: now) { await store.refreshTaskHome() } }
  }
  private var statusPanel: some View {
    block("Status & next step") {
      if closed {
        HStack {
          Text(task.closeReason == "lifespan" || task.closeReason == "agent_done" ? "Closed automatically" : "Task closed")
          Spacer()
          Button("Undo") { reopen() }.buttonStyle(.bordered)
        }
      } else if needsYou {
        Label("Needs you", systemImage: "circle.fill").font(.callout.weight(.medium)).foregroundStyle(.orange)
      }
      if !summary.text.isEmpty {
        Text(summary.text).font(.callout).textSelection(.enabled)
        if !summary.updatedBy.isEmpty { Text("by " + summaryAuthor).font(.caption).foregroundStyle(.secondary) }
      }
      else if !task.goal.isEmpty { Text(task.goal).font(.callout).textSelection(.enabled) }
      else { Text("Follow the latest chat activity below.").font(.callout).foregroundStyle(.secondary) }
      if !task.checklist.isEmpty {
        ProgressView(value: task.progress.fraction)
        ForEach(task.checklist.sorted { $0.order < $1.order }) { step in
          Toggle(step.text, isOn: Binding(get: { step.done }, set: { done in
            Task { await model.change { try await model.client.step(task.id, step: step.id, done: done) }; await store.refreshTaskHome() }
          })).toggleStyle(.checkbox).disabled(closed)
        }
      }
      if let value = task.compact(now: now).waiting { Text(TaskPresentation.waiting(value)).font(.callout) }
      compactCheckIn
      if !closed { Button(task.waiting == nil ? "Set waiting" : "Edit waiting") { waiting = true }.buttonStyle(.bordered) }
      if task.kind == "short" { Text("Closes after \(task.lifespanDays ?? 14) quiet days").font(.caption).foregroundStyle(.secondary) }
    }
  }
  private var peoplePanel: some View {
    block("People · \(people.count)") {
      ForEach(chats) { chat in
        Button(chat.name, systemImage: chat.id.hasSuffix("@g.us") ? "person.3" : "bubble.left") { openChat(chat.id) }
          .buttonStyle(.bordered).lineLimit(1)
      }
      if people.isEmpty { Text("No known participants yet.").font(.callout).foregroundStyle(.secondary) }
      LazyVGrid(columns: [GridItem(.adaptive(minimum: 115))], alignment: .leading, spacing: 8) {
        ForEach(people) { person in
          Button(person.name, systemImage: "person.crop.circle") {
            store.selectPage(.circles); store.circlePath = [.person(person.id)]
          }.buttonStyle(.bordered).lineLimit(1).help(person.name)
        }
      }
    }
  }
  private var filesPanel: some View {
    block("Files & links") {
      HStack(spacing: 10) {
        Image(systemName: "magnifyingglass").foregroundStyle(.secondary)
        TextField("Search files & links", text: $assetQuery).textFieldStyle(.plain)
          .accessibilityHint("Search descriptions, names, text in images, people and chats")
      }.padding(9).background(.primary.opacity(0.035), in: RoundedRectangle(cornerRadius: 7))
      Picker("Show", selection: $assetFilter) {
        ForEach(["All", "Images", "Files", "Links"], id: \.self) { Text($0).tag($0) }
      }.pickerStyle(.segmented).labelsHidden().frame(maxWidth: 320)
      let items = TaskAssetsPresentation.items(task.items, filter: assetFilter, query: assetQuery)
      if items.isEmpty {
        Text(task.items.isEmpty ? "Files and links shared in this task’s chats appear here." : "No matching files or links.")
          .font(.callout).foregroundStyle(.secondary)
      }
      LazyVGrid(columns: [GridItem(.adaptive(minimum: 190), spacing: 8)], alignment: .leading, spacing: 8) {
        ForEach(items) { item in
          TaskAssetTile(backend: store.backend, taskID: task.id, item: item, cachedPreviews: model.assetPreviews, allowModel: !store.demo && !store.snapshot) { updated in
            model.task = updated
            if let index = store.home.tasks.firstIndex(where: { $0.id == updated.id }) { store.home.tasks[index].cover = updated.cover }
          }
        }
      }
    }
  }
  private var compactCheckIn: some View {
    VStack(alignment: .leading, spacing: 4) {
      HStack(spacing: 8) {
        Label("Check-in · " + (task.checkIn.map { $0.rule == "reply" ? "When someone replies" : TaskPresentation.checkIn($0.nextAt) } ?? "Not scheduled"), systemImage: "clock")
          .font(.caption).foregroundStyle(.secondary)
        if !closed {
          Menu {
            ForEach(["Tomorrow morning", "In 3 days", "Next week", "When someone replies"], id: \.self) { option in
              Button(option) { suggestCheckIn(option) }.disabled(chats.isEmpty)
            }
            Button("Custom…") { schedule = true }
            if task.checkIn != nil { Button("Remove") { patch(.init(clearCheckIn: true)) } }
          } label: { Image(systemName: "ellipsis").font(.caption) }
            .menuStyle(.borderlessButton).fixedSize().accessibilityLabel("Schedule check-in")
        }
      }
      if let fired = task.history.first(where: { $0.kind == "due" }) {
        Text("Last check-in fired \(store.relativeTime(fired.at))")
          .font(.caption).foregroundStyle(.secondary)
        let received = task.history.contains { $0.kind == "check_in_received" && $0.at >= fired.at }
        Text(received ? "Sent to your dot" : "Waiting for your dot to sync")
          .font(.caption).foregroundStyle(.secondary)
        let agent = task.createdBy.name.isEmpty || task.createdBy.agentId == "you" ? "your AI" : task.createdBy.name
        Text("Waiting for \(agent) to check in. Limmr can’t wake an AI; it checks in on its own schedule.")
          .font(.caption).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
      }
    }
  }
  private var historyPanel: some View {
    block("History") {
      let latest = task.latest
      if latest.isEmpty {
        Text(summary.text.isEmpty ? "No recent chat activity yet." : summary.text).font(.callout).textSelection(.enabled)
      }
      ForEach(Array(latest.enumerated()), id: \.offset) { _, message in
        VStack(alignment: .leading, spacing: 4) {
          HStack { Text(message.sender).font(.callout.weight(.semibold)); Spacer(); Text(store.relativeTime(message.timestamp)).font(.caption).foregroundStyle(.secondary) }
          Text(message.text).font(.callout).lineLimit(3).textSelection(.enabled)
          Text(message.chat).font(.caption).foregroundStyle(.secondary)
        }.padding(.vertical, 4)
      }
      if closed && (task.history.contains { $0.text == "Closed automatically" } || ["lifespan", "agent_done"].contains(task.closeReason ?? "")) {
        HStack { Text("Closed automatically").font(.callout); Spacer(); Button("Undo") { reopen() }.buttonStyle(.bordered) }
      }
      DisclosureGroup("Details", isExpanded: $showAllHistory) {
        VStack(alignment: .leading, spacing: 10) {
          ForEach(Array(task.history.enumerated()), id: \.offset) { _, event in
            HStack(alignment: .firstTextBaseline) {
              Text(event.text).font(.caption); Spacer()
              Text(store.relativeTime(event.at)).font(.caption).foregroundStyle(.secondary)
            }
          }
          if let suggestion = task.suggestion { Text(suggestion.signals.joined(separator: " · ")).font(.caption).foregroundStyle(.secondary) }
        }.padding(.top, 10).task { await model.loadDetails() }
      }.padding(10).background(.primary.opacity(0.035), in: RoundedRectangle(cornerRadius: 8))
    }
  }
  private func suggestCheckIn(_ option: String) {
    guard let chat = chats.first else { return }
    let days = option == "In 3 days" ? 3 : option == "Next week" ? 7 : 1
    let day = Calendar.current.date(byAdding: .day, value: days, to: now) ?? now
    let date = option == "When someone replies" ? now : Calendar.current.date(bySettingHour: 9, minute: 0, second: 0, of: day) ?? day
    patch(.init(checkIn: .init(rule: option == "When someone replies" ? "reply" : "once", atTime: "09:00", to: chat.id,
      note: "Review the latest activity and next step", nextAt: ISO8601DateFormatter().string(from: date))))
  }
  private func reopen() {
    Task { await model.change {
      _ = try await model.client.backend.request("/v1/tasks/" + task.id + "/reopen", method: "POST", body: nil)
      return try await model.client.get(task.id)
    }; await store.refreshTaskHome() }
  }
  private func openChat(_ id: String) {
    store.selectPage(.circles)
    store.circlePath = id.hasSuffix("@g.us") ? [.group(id)] : [.chat(id, chats.first { $0.id == id }?.name ?? store.chatName(id))]
  }
  private func patch(_ request: TaskPatch) {
    Task { await model.change {
      if task.status == "suggested" { _ = try await model.client.surface(task.id, action: .keep) }
      return try await model.client.patch(task.id, request)
    }; await store.refreshTaskHome() }
  }
  private func block<Content: View>(_ title: String, @ViewBuilder content: () -> Content) -> some View {
    GroupBox {
      VStack(alignment: .leading, spacing: 8) { content() }
        .frame(maxWidth: .infinity, alignment: .leading).padding(8)
    } label: {
      Text(title).font(.headline)
    }
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
    _enabled = State(initialValue: task.checkIn != nil); _rule = State(initialValue: task.checkIn?.rule == "reply" ? "once" : task.checkIn?.rule ?? "weekly")
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
