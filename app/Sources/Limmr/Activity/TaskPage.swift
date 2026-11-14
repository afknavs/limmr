import AppKit
import Foundation
import LimmrKit
import Observation
import SwiftUI

@MainActor @Observable final class TaskWorkspace {
  let assetPreviews = TaskAssetTileCache()
  var task: TrackedTask?
  var error: String?
  var historyErrors: [String: String] = [:]
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
  func removeMembers(chats: [String] = [], people: [String] = []) async {
    guard !saving else { return }
    saving = true; error = nil
    defer { saving = false }
    do { task = try await client.removeMembers(id, chats: chats, people: people) }
    catch { self.error = "The task could not be saved. Try again." }
  }
  func change(_ action: () async throws -> TrackedTask) async {
    guard !saving else { return }
    saving = true; error = nil
    defer { saving = false }
    do { task = try await action(); await load(force: true) }
    catch { self.error = "The task could not be saved. Try again." }
  }
  func undoChange(_ event: TaskHistoryEvent, now: Date) async {
    guard !saving, event.canUndo(now: now), let changeId = event.changeId else { return }
    saving = true; historyErrors[changeId] = nil
    defer { saving = false }
    do {
      task = try await client.undoChange(id, changeId: changeId)
      task = try await client.get(id)
      loadedAt = Date()
    } catch { historyErrors[changeId] = error.localizedDescription }
  }
  func resolveSuggestion(_ action: TaskSurfacingAction) async {
    guard !saving, task?.status == "suggested" else { return }
    saving = true; error = nil
    defer { saving = false }
    do { task = try await client.surface(id, action: action); loadedAt = Date() }
    catch { self.error = error.localizedDescription }
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
          Button { store.leaveActivityRoute() } label: {
            HStack(spacing: 6) { Image(systemName: "chevron.left").font(.system(size: 10, weight: .semibold)); Text(store.activityBackTitle) }
              .font(LimmrTheme.font(12, .semibold)).foregroundStyle(LimmrTheme.ink2).padding(.horizontal, 12).frame(height: 28)
              .background(LimmrTheme.surface2, in: RoundedRectangle(cornerRadius: 12)).contentShape(Rectangle())
          }.buttonStyle(.plain).accessibilityLabel(store.activityBackTitle)
          TaskWorkspaceView(store: store, id: id)
        }.padding(.horizontal, 28).padding(.top, 24).padding(.bottom, 24).paperReadingWidth()
      }.defaultScrollAnchor(startAtHistory ? .bottom : .top)
    }.navigationBarBackButtonHidden(true).n1PageSurface()
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
  @Environment(\.limmrWindowWidth) private var windowWidth
  private var summaryRow: TaskSummary? { (store.allTasks + store.home.tasks).first { $0.id == task.id } }
  private var hairline: some View { Rectangle().fill(LimmrTheme.hairline).frame(height: 1) }
  private var now: Date { store.presentationNow }
  private var closed: Bool { ["done", "dropped"].contains(task.status) }
  private var linkedEfforts: [Effort] { model.efforts.map(\.effort) }
  private var needsYou: Bool { !closed && task.status != "suggested" && task.needsYou }
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
    unique(task.chats)
  }
  private var people: [EffortEntity] {
    unique(task.people)
  }
  private func unique(_ values: [EffortEntity]) -> [EffortEntity] {
    var seen = Set<String>(); return values.filter { seen.insert($0.id).inserted }
  }
  var body: some View {
    let width = PaperLayout.contentWidth(windowWidth), wide = !embedded && width >= 900
    return VStack(alignment: .leading, spacing: 20) {
      HStack(alignment: .top, spacing: 16) {
        if !embedded, let summaryRow {
          PaperTaskArtwork(store: store, task: summaryRow).frame(width: 112, height: 84).clipShape(RoundedRectangle(cornerRadius: 12))
        }
        VStack(alignment: .leading, spacing: 8) {
          Text(embedded ? "Task workspace" : task.title).font(embedded ? LimmrTheme.TypeScale.section : LimmrTheme.TypeScale.page)
            .fixedSize(horizontal: false, vertical: true)
          if let prior = task.followsContext {
            Button("Follows: \(prior.title)") { store.openTask(prior.id) }.buttonStyle(.plain).font(LimmrTheme.font(12, .semibold)).foregroundStyle(LimmrTheme.ink2)
          }
          PaperFlow(spacing: 6) {
            Text("\(TaskListPresentation.kind(task.kind)) · \(closed ? "Closed" : task.status.capitalized)")
              .font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2).frame(height: 24)
            ForEach(task.circleIds, id: \.self) { PaperChip(text: store.circleName($0), kind: "plain") }
            if task.circleIds.isEmpty { PaperChip(text: "No circle", kind: "plain") }
            Menu {
              Button("No circle") { var request = TaskPatch(); request.circleIds = []; patch(request) }
              ForEach(store.circles) { circle in
                Button((task.circleIds.contains(circle.id) ? "✓ " : "") + circle.name) {
                  var request = TaskPatch(); request.circleIds = task.circleIds.contains(circle.id) ? task.circleIds.filter { $0 != circle.id } : task.circleIds + [circle.id]; patch(request)
                }
              }
            } label: { Text("Change circles").font(LimmrTheme.font(12, .semibold)).foregroundStyle(LimmrTheme.ink2) }
              .menuStyle(.borderlessButton).menuIndicator(.hidden).fixedSize().frame(height: 24)
          }
          ForEach(task.circleSuggestions, id: \.circleId) { suggestion in
            HStack {
              Text("Suggested \(store.circleName(suggestion.circleId)) · \(suggestion.reason)").font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2)
              Button("Undo") { var request = TaskPatch(); request.circleIds = task.circleIds.filter { $0 != suggestion.circleId }; patch(request) }.buttonStyle(PaperButtonStyle())
            }
          }
        }
        Spacer(minLength: 12)
        if task.status == "suggested" {
          HStack {
            Button("Keep") { resolveSuggestion(.keep) }.buttonStyle(PaperButtonStyle(primary: true))
            Button("Dismiss") { resolveSuggestion(.dismiss) }.buttonStyle(PaperButtonStyle())
          }.disabled(model.saving)
        } else {
          HStack(spacing: 8) {
            if !closed && !embedded { Button("Done") { patch(.init(status: "done")) }.buttonStyle(PaperButtonStyle()).accessibilityLabel("Mark task done") }
            Menu {
              if closed { Button("Reopen task") { reopen() } }
              else {
                Button(task.status == "quiet" ? "Wake task" : "Make quiet") { patch(.init(status: task.status == "quiet" ? "active" : "quiet")) }
                Button("Done") { patch(.init(status: "done")) }
                Button("Not now") { patch(.init(status: "dropped")) }
              }
              if embedded { Button("Open task") { store.openTask(task.id) } }
            } label: { Image(systemName: "ellipsis").font(.system(size: 13, weight: .semibold)).foregroundStyle(LimmrTheme.ink2).frame(width: 28, height: 28) }
              .menuStyle(.borderlessButton).menuIndicator(.hidden).frame(width: 32, height: 28)
              .background(LimmrTheme.surface2, in: RoundedRectangle(cornerRadius: 12)).accessibilityLabel("Task actions")
          }
        }
      }
      // Wide windows: the work (status, files, history) on the left, who and notes in a side column.
      if wide {
        HStack(alignment: .top, spacing: 24) {
          VStack(alignment: .leading, spacing: 20) { statusPanel; filesPanel; historyPanel }.frame(maxWidth: .infinity, alignment: .leading)
          VStack(alignment: .leading, spacing: 20) { peoplePanel; TaskNotesWidget(model: model, task: task) }
            .frame(width: min(380, max(300, width * 0.27)), alignment: .leading)
        }
      } else {
        statusPanel; TaskNotesWidget(model: model, task: task); filesPanel; peoplePanel; historyPanel
      }
      if let error = model.error { Text(error).font(LimmrTheme.font(14)).foregroundStyle(LimmrTheme.ink2) }
    }.font(LimmrTheme.TypeScale.body).foregroundStyle(LimmrTheme.ink)
      .disabled(model.saving).frame(maxWidth: embedded ? 760 : .infinity, alignment: .leading)
      .sheet(isPresented: $schedule) { TaskScheduleEditor(model: model, task: task, now: now) { await store.refreshTaskHome() } }
      .sheet(isPresented: $waiting) { TaskWaitingEditor(model: model, task: task, now: now) { await store.refreshTaskHome() } }
  }
  private var statusPanel: some View {
    block("Where it stands") {
      if closed {
        HStack {
          Text(task.closeReason == "lifespan" || task.closeReason == "agent_done" ? "Closed automatically" : "Task closed")
          Spacer()
          Button("Undo") { reopen() }.buttonStyle(PaperButtonStyle())
        }
      } else if let until = task.laterUntil, let at = Timestamp.date(until), now < at {
        HStack {
          Text("Left for later until " + TaskLaterTime.status(until))
          Spacer()
          Button("Bring back") { Task { await store.bringBackTask(task.id) } }.buttonStyle(PaperButtonStyle())
        }
      } else if needsYou {
        PaperChip(text: "Needs you", kind: "need")
      }
      if !summary.text.isEmpty {
        Text(summary.text).font(LimmrTheme.font(14)).textSelection(.enabled)
        if !summary.updatedBy.isEmpty { Text("by " + summaryAuthor).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2) }
      }
      else if !task.goal.isEmpty { Text(task.goal).font(LimmrTheme.font(14)).textSelection(.enabled) }
      else { Text("Follow the latest chat activity below.").font(LimmrTheme.font(14)).foregroundStyle(LimmrTheme.ink2) }
      if !task.checklist.isEmpty {
        PaperSteps(progress: task.progress).frame(maxWidth: 280).accessibilityLabel("\(task.progress.done) of \(task.progress.total) steps done")
        ForEach(task.checklist.sorted { $0.order < $1.order }) { step in
          Toggle(step.text, isOn: Binding(get: { step.done }, set: { done in
            Task { await model.change { try await model.client.step(task.id, step: step.id, done: done) }; await store.refreshTaskHome() }
          })).toggleStyle(.checkbox).disabled(closed)
        }
      }
      if let value = task.compact(now: now).waiting { Text(TaskPresentation.waiting(value)).font(LimmrTheme.font(14)) }
      compactCheckIn
      if !closed { Button(task.waiting == nil ? "Set waiting" : "Edit waiting") { waiting = true }.buttonStyle(PaperButtonStyle()) }
      if task.kind == "short" { Text("Closes after \(task.lifespanDays ?? 14) quiet days").font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2) }
    }
  }
  private func whoRow<Leading: View>(_ name: String, action: @escaping () -> Void, @ViewBuilder leading: () -> Leading) -> some View {
    Button(action: action) {
      HStack(spacing: 10) {
        leading().frame(width: 28, height: 28)
        Text(name).font(LimmrTheme.font(14, .medium)).lineLimit(1)
        Spacer(minLength: 8)
        Image(systemName: "chevron.right").font(.system(size: 10, weight: .semibold)).foregroundStyle(LimmrTheme.ink3)
      }.padding(.horizontal, 14).frame(height: 44).contentShape(Rectangle())
    }.quietCard().help(name)
  }
  private var peoplePanel: some View {
    VStack(alignment: .leading, spacing: 10) {
      Text("People · \(people.count)").font(LimmrTheme.TypeScale.section)
      VStack(alignment: .leading, spacing: 0) {
        if people.isEmpty && chats.isEmpty { Text("No known participants yet.").font(LimmrTheme.font(14)).foregroundStyle(LimmrTheme.ink2).padding(14) }
        ForEach(chats) { chat in
          whoRow(chat.name) { openChat(chat.id) } leading: {
            Image(systemName: chat.id.hasSuffix("@g.us") ? "person.2.fill" : "bubble.left.fill").font(.system(size: 11)).foregroundStyle(LimmrTheme.ink3)
              .frame(width: 28, height: 28).background(LimmrTheme.surface2, in: SwiftUI.Circle())
          }.contextMenu {
            Button("Remove from task") { Task { await model.removeMembers(chats: [chat.id]) } }
              .disabled(model.saving)
          }
          if chat.id != chats.last?.id || !people.isEmpty { hairline }
        }
        ForEach(people) { person in
          whoRow(person.name) { store.selectPage(.circles); store.circlePath = [.person(person.id)] } leading: {
            PersonAvatar(store: store, id: person.id, name: person.name, size: 28)
          }.contextMenu {
            if person.id != "me" {
              Button("Remove from task") { Task { await model.removeMembers(people: [person.id]) } }
                .disabled(model.saving)
            }
          }
          if person.id != people.last?.id { hairline }
        }
      }.frame(maxWidth: .infinity, alignment: .leading).n1Card().clipShape(RoundedRectangle(cornerRadius: LimmrTheme.cardRadius))
    }
  }
  private var filesPanel: some View {
    block("Files & links") {
      PaperFlow(spacing: 8) {
        HStack(spacing: 8) {
          Image(systemName: "magnifyingglass").foregroundStyle(LimmrTheme.ink3)
          TextField("Search files & links", text: $assetQuery).textFieldStyle(.plain).font(LimmrTheme.font(14))
            .accessibilityHint("Search descriptions, names, text in images, people and chats")
        }.padding(.horizontal, 12).frame(width: 280, height: 28).background(LimmrTheme.surface2, in: RoundedRectangle(cornerRadius: 12))
        PaperSegments(values: ["All", "Images", "Files", "Links"], selection: $assetFilter).accessibilityLabel("Show")
      }
      let items = TaskAssetsPresentation.items(task.items, filter: assetFilter, query: assetQuery)
      if items.isEmpty {
        Text(task.items.isEmpty ? "Files and links shared in this task’s chats appear here." : "No matching files or links.")
          .font(LimmrTheme.font(14)).foregroundStyle(LimmrTheme.ink2)
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
          .font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2)
        if !closed {
          Menu {
            ForEach(["Tomorrow morning", "In 3 days", "Next week", "When someone replies"], id: \.self) { option in
              Button(option) { suggestCheckIn(option) }.disabled(chats.isEmpty)
            }
            Button("Custom…") { schedule = true }
            if task.checkIn != nil { Button("Remove") { patch(.init(clearCheckIn: true)) } }
          } label: { Text("Change").font(LimmrTheme.font(12, .semibold)) }
            .menuStyle(.borderlessButton).menuIndicator(.hidden).fixedSize().accessibilityLabel("Schedule check-in")
        }
      }
      if let fired = task.history.first(where: { $0.kind == "due" }) {
        Text("Last check-in fired \(store.relativeTime(fired.at))")
          .font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2)
        let received = task.history.contains { $0.kind == "check_in_received" && $0.at >= fired.at }
        Text(received ? "Sent to your dot" : "Waiting for your dot to sync")
          .font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2)
        let agent = task.createdBy.name.isEmpty || task.createdBy.agentId == "you" ? "your AI" : task.createdBy.name
        Text("Waiting for \(agent) to check in. Limmr can’t wake an AI; it checks in on its own schedule.")
          .font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2).fixedSize(horizontal: false, vertical: true)
      }
    }
  }
  private var historyPanel: some View {
    block("History") {
      let latest = task.latest
      if latest.isEmpty {
        Text(summary.text.isEmpty ? "No recent chat activity yet." : summary.text).font(LimmrTheme.font(14)).textSelection(.enabled)
      }
      ForEach(Array(latest.enumerated()), id: \.offset) { index, message in
        VStack(alignment: .leading, spacing: 4) {
          HStack(alignment: .firstTextBaseline) {
            Text(message.sender).font(LimmrTheme.font(14, .semibold))
            Text(message.chat).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2).lineLimit(1)
            Spacer()
            Text(store.relativeTime(message.timestamp)).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3)
          }
          Text(message.text).font(LimmrTheme.font(14)).lineLimit(3).textSelection(.enabled)
        }.padding(.vertical, 2)
        if index < latest.count - 1 { hairline }
      }
      if closed && (task.history.contains { $0.text == "Closed automatically" } || ["lifespan", "agent_done"].contains(task.closeReason ?? "")) {
        HStack { Text("Closed automatically").font(LimmrTheme.font(14)); Spacer(); Button("Undo") { reopen() }.buttonStyle(PaperButtonStyle()) }
      }
      // Automatic changes stay on the task and remain visible without opening Details.
      ForEach(Array(task.history.filter { $0.changeId != nil }.enumerated()), id: \.offset) { _, event in
        historyRow(event)
      }
      VStack(alignment: .leading, spacing: 0) {
        Button { showAllHistory.toggle() } label: {
          HStack(spacing: 8) {
            Text("Details").font(LimmrTheme.font(12, .semibold)).foregroundStyle(LimmrTheme.ink2)
            Spacer()
            Image(systemName: showAllHistory ? "chevron.down" : "chevron.right").font(.system(size: 10, weight: .semibold)).foregroundStyle(LimmrTheme.ink3)
          }.padding(.horizontal, 12).frame(height: 32).contentShape(Rectangle())
        }.buttonStyle(.plain).accessibilityValue(showAllHistory ? "Expanded" : "Collapsed")
        if showAllHistory {
          VStack(alignment: .leading, spacing: 10) {
            ForEach(Array(task.history.filter { $0.changeId == nil }.enumerated()), id: \.offset) { _, event in
              historyRow(event)
            }
            if let suggestion = task.suggestion { Text(suggestion.signals.joined(separator: " · ")).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2) }
          }.padding(.horizontal, 12).padding(.bottom, 12).task { await model.loadDetails() }
        }
      }.background(LimmrTheme.surface2, in: RoundedRectangle(cornerRadius: 12))
    }
  }
  private func historyRow(_ event: TaskHistoryEvent) -> some View {
    VStack(alignment: .leading, spacing: 4) {
      HStack(alignment: .firstTextBaseline) {
        VStack(alignment: .leading, spacing: 3) {
          Text(event.text).font(LimmrTheme.font(12)).fixedSize(horizontal: false, vertical: true)
          if let reason = event.reason, !reason.isEmpty, !event.text.contains(reason) {
            Text(reason).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2)
          }
        }
        Spacer(minLength: 8)
        Text(store.relativeTime(event.at)).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2)
        if event.undone { Text("Undone").font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2) }
        else {
          TimelineView(.periodic(from: now, by: 1)) { context in
            if event.canUndo(now: store.snapshot ? now : context.date) {
              Button("Undo") { Task { await model.undoChange(event, now: store.presentationNow) } }
                .buttonStyle(PaperButtonStyle()).disabled(model.saving)
                .accessibilityLabel("Undo \(event.text)")
            }
          }
        }
      }
      if let id = event.changeId, let error = model.historyErrors[id] {
        Text(error).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2).textSelection(.enabled)
      }
    }.padding(.vertical, 3)
  }
  private func resolveSuggestion(_ action: TaskSurfacingAction) {
    Task {
      await model.resolveSuggestion(action)
      if let target = model.task, target.id != task.id { store.openTask(target.id) }
      await store.refreshAllTasks(); await store.refreshTaskHome()
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
      let targetID: String
      if task.status == "suggested" { targetID = try await model.client.surface(task.id, action: .keep).id }
      else { targetID = task.id }
      return try await model.client.patch(targetID, request)
    }; await store.refreshTaskHome() }
  }
  private func block<Content: View>(_ title: String, @ViewBuilder content: () -> Content) -> some View {
    VStack(alignment: .leading, spacing: 10) {
      Text(title).font(LimmrTheme.TypeScale.section)
      VStack(alignment: .leading, spacing: 10) { content() }
        .frame(maxWidth: .infinity, alignment: .leading).padding(16).n1Card()
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
