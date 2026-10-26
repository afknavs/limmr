import LimmrKit
import SwiftUI

struct ActivityView: View {
  @Bindable var store: AppStore
  @State var detailedLog = false
  @State var query = ""
  @State private var selection = KeyboardSelection()
  @State private var finishedExpanded = false
  @State private var quietExpanded = false
  @State private var expandedLogs: Set<Int64> = []
  @FocusState private var searchFocused: Bool
  @FocusState private var rowFocused: String?
  private var rowIDs: [String] {
    (detailedLog || store.activityDay != nil) ? rows.map { "log:\($0.id)" } : tasks.map { "task:" + $0.id } + (active + (finishedExpanded ? finished : []) + (quietExpanded ? quiet : [])).map(\.id)
  }
  private func move(_ step: Int) {
    guard store.activityPath.isEmpty else { return }
    selection.move(step, in: rowIDs); rowFocused = selection.id
  }
  private func openSelection() {
    guard store.activityPath.isEmpty, let id = selection.current(in: rowIDs) ?? rowIDs.first else { return }
    if detailedLog || store.activityDay != nil, let log = Int64(id.dropFirst(4)) { expandedLogs.insert(log) }
    else if id.hasPrefix("task:") { store.openTask(String(id.dropFirst(5))) }
    else { store.openEffort(id) }
  }
  private func effortCard(_ effort: Effort) -> some View {
    EffortCard(effort: effort) { store.openEffort(effort.id) }
      .focused($rowFocused, equals: effort.id)
      .keyboardRow(effort.id, selection: selection.id, radius: 10)
  }
  @State private var tasks: [TaskSummary] = []
  @State private var taskError: String?
  @State private var efforts: [Effort] = []
  @State private var loading = true
  @State private var loadError: String?
  @State private var unnamedCount = 0
  @State private var reviewingChange: CircleAutoChange?
  @State private var showAllChanges = false
  private var rows: [Activity] {
    let all = store.activity + (store.dashboard?.recentSends ?? [])
    return Dictionary(all.map { ($0.id, $0) }, uniquingKeysWith: { _, last in last }).values
      .filter { ActivityDayFilter.matches($0.at, day: store.activityDay) }
      .filter { store.activityAgent == "all" || $0.agentId == store.activityAgent }
      .filter {
        store.activityFilter == "all"
          || (store.activityFilter == "reads"
            ? ActivityPresentation.isMessageRead($0) : ActivityPresentation.isSend($0))
      }
      .sorted(by: Activity.newestFirst)
  }
  private var active: [Effort] { efforts.filter { $0.status != "done" && $0.status != "dormant" } }
  private var quiet: [Effort] { efforts.filter { $0.status == "dormant" } }
  private var finished: [Effort] {
    let now = store.demo ? Timestamp.date(DemoData.now)! : Date()
    return efforts.filter {
      $0.status == "done"
        && (!query.isEmpty
          || (Timestamp.date($0.lastAt) ?? .distantPast) >= now.addingTimeInterval(-7 * 86400))
    }
  }
  var body: some View {
    NavigationStack(path: $store.activityPath) {
      content
        .pageSurface()
        .navigationDestination(for: ActivityRoute.self) { route in
          Group {
            switch route {
            case .task(let id): TaskPage(store: store, id: id)
            case .effort(let id): EffortPage(store: store, id: id)
            }
          }
          .pageSurface()
        }
    }
      .task(id: store.pendingActivityRoute) { await store.activityDidAppear() }
      .onDisappear { store.activityNavigationReady = false }
      .environment(\.keyboardRowNavigation, true)
      .onChange(of: rowFocused) { _, id in if let id { selection.id = id } }
      .onChange(of: rowIDs) { _, ids in if selection.current(in: ids) == nil { selection.id = nil } }
      .onChange(of: store.searchRequest) { _, _ in
        detailedLog = false
        Task { @MainActor in await Task.yield(); searchFocused = true; store.pendingSearchPage = nil }
      }
      .onChange(of: store.escapeRequest) { _, _ in
        query = ""; selection.id = nil; reviewingChange = nil; showAllChanges = false
      }
      .onChange(of: store.rowRequest) { _, _ in move(store.rowStep) }
      .onChange(of: store.rowOpenRequest) { _, _ in openSelection() }
      .onKeyPress(.downArrow) { guard store.activityPath.isEmpty else { return .ignored }; move(1); return .handled }
      .onKeyPress(.upArrow) { guard store.activityPath.isEmpty else { return .ignored }; move(-1); return .handled }
      .onKeyPress(.return) { guard store.activityPath.isEmpty, rowFocused != nil else { return .ignored }; openSelection(); return .handled }
      .onExitCommand { store.route(.escape) }
  }
  private var content: some View {
    ScrollViewReader { proxy in
    ScrollView {
      VStack(alignment: .leading, spacing: 24) {
        HStack {
          Text("Activity").font(AppType.pageTitle)
          Spacer()
          Picker("View", selection: Binding(
            get: { detailedLog || store.activityDay != nil },
            set: { detailedLog = $0; if !$0 { store.activityDay = nil } })) {
            Text("By purpose").tag(false)
            Text("Detailed log").tag(true)
          }.pickerStyle(.menu).fixedSize()
        }
        if let day = store.activityDay {
          HStack {
            Text("Activity on " + day).font(.callout)
            Button("Clear day filter") { store.activityDay = nil }.limmrButton()
          }
        }
        if detailedLog || store.activityDay != nil {
          detailedView
        } else {
          TextField("Search work, people or chats", text: $query)
            .textFieldStyle(.roundedBorder).accessibilityLabel("Search work, people or chats")
            .focused($searchFocused).onSubmit { openSelection() }
            .accessibilityHint("Use arrow keys to choose work, then Return to open it")
          if let loadError {
            Text(loadError).foregroundStyle(AccessibleSecondary())
            Button("Try again") { Task { await load() } }.limmrButton()
          } else if loading {
            QuietLoadingCards()
          }
          if query.isEmpty, let namer = store.effortNamer {
            if namer.pending > 0 {
              VStack(alignment: .leading, spacing: 4) {
                Text("\(namer.pending)").font(AppType.rowTitle).monospacedDigit()
                Text("This Mac is naming more").font(AppType.caption)
              }.foregroundStyle(AccessibleSecondary())
              .accessibilityElement(children: .ignore)
              .accessibilityLabel("This Mac is naming \(namer.pending) more")
            } else if let notice = namer.notice {
              Text(notice).font(.callout).foregroundStyle(AccessibleSecondary())
            }
          }
          if let taskError {
            Text(taskError).font(.callout).foregroundStyle(AccessibleSecondary())
            Button("Try tasks again") { Task { await load() } }.limmrButton()
          }
          if !tasks.isEmpty {
            Text("Tasks").font(AppType.blockHeading).foregroundStyle(AccessibleSecondary())
            ForEach(tasks.filter { $0.status != "suggested" }) { task in
              SurfacedTaskRow(store: store, task: task, changed: updateTask)
                .focused($rowFocused, equals: "task:" + task.id)
                .keyboardRow("task:" + task.id, selection: selection.id, radius: 8)
            }
          }
          if !TaskPresentation.suggested(tasks).isEmpty {
            Text("Suggested").font(AppType.blockHeading).foregroundStyle(AccessibleSecondary())
            ForEach(TaskPresentation.suggested(tasks)) { task in
              SurfacedTaskRow(store: store, task: task, changed: updateTask)
                .focused($rowFocused, equals: "task:" + task.id)
                .keyboardRow("task:" + task.id, selection: selection.id, radius: 8)
            }
          }
          if !active.isEmpty {
            Text("Active").font(AppType.blockHeading).foregroundStyle(AccessibleSecondary())
            ForEach(active) { effort in effortCard(effort) }
          }
          if !store.home.circleChanges.isEmpty
            && (query.isEmpty || "Circle sorting".localizedCaseInsensitiveContains(query))
          {
            VStack(alignment: .leading, spacing: 16) {
              HStack {
                Text("Circle sorting").font(AppType.blockHeading)
                Spacer()
                Text("Limmr placements").font(AppType.caption).foregroundStyle(AccessibleSecondary())
              }
              Text("Limmr placed people in your circles. Review the changes or undo them.")
                .font(.callout).foregroundStyle(AccessibleSecondary())
              ForEach(store.home.circleChanges) { change in
                CircleChangeRow(store: store, change: change) { reviewingChange = change }
              }
              if store.home.circleChangesMore > 0 {
                Button("Review all changes") { showAllChanges = true }.limmrButton()
              }
              ActionFeedback(store: store, action: "circle-changes")
            }.padding(16).background(
              .primary.opacity(0.025), in: RoundedRectangle(cornerRadius: 10))
          }
          if !finished.isEmpty {
            DisclosureGroup(isExpanded: $finishedExpanded) {
              VStack(spacing: 16) { ForEach(finished) { effort in effortCard(effort) } }.padding(.top, 12)
            } label: {
              CountLabel(value: finished.count, title: query.isEmpty ? "Finished this week" : "Finished")
            }.font(.callout).foregroundStyle(AccessibleSecondary())
          }
          if !quiet.isEmpty {
            DisclosureGroup(isExpanded: $quietExpanded) {
              VStack(spacing: 16) {
                ForEach(quiet) { effort in
                  HStack(alignment: .top, spacing: 12) {
                    effortCard(effort)
                    Button("Dismiss", systemImage: "eye.slash") {
                      Task {
                        if await store.updateEffort(effort.id, values: ["hidden": true]) != nil {
                          efforts.removeAll { $0.id == effort.id }
                        }
                      }
                    }.limmrButton().disabled(store.busy)
                      .accessibilityLabel("Dismiss \(effort.title)")
                  }
                  ActionFeedback(store: store, action: "effort-\(effort.id)")
                }
              }.padding(.top, 12)
            } label: {
              CountLabel(value: quiet.count, title: "Quiet")
            }.font(.callout).foregroundStyle(AccessibleSecondary())
          }
          if !loading && loadError == nil && efforts.isEmpty && tasks.isEmpty {
            EmptyState(
              symbol: "text.magnifyingglass",
              title: EffortPresentation.emptyTitle(query: query, unnamed: unnamedCount),
              detail: EffortPresentation.emptyDetail(query: query, unnamed: unnamedCount))
          }
        }
      }.frame(maxWidth: 820).padding(32).frame(maxWidth: .infinity)
    }
    .onChange(of: selection.id) { _, id in if let id { proxy.scrollTo(id, anchor: .center) } }
    }
    .task(id: "\(store.backendGeneration):\(store.effortRevision):\(query):\(store.home.circleUpdates.map(\.lastAt).max() ?? "")") {
      await load()
    }
    .task(id: store.activityAgent) {
      if !store.snapshot {
        await store.filterActivity(agent: store.activityAgent, kind: "all", chat: "all")
      }
    }
    .sheet(item: $reviewingChange) { CircleChangeReview(store: store, change: $0).dismissOnEscape() }
    .sheet(isPresented: $showAllChanges) { CircleChangesList(store: store).dismissOnEscape() }
  }
  private func updateTask(_ updated: TaskSummary) {
    if let index = tasks.firstIndex(where: { $0.id == updated.id }) {
      if updated.status == "dropped" { tasks.remove(at: index) }
      else { tasks[index] = updated }
    }
  }
  private func load() async {
    loading = true
    loadError = nil
    taskError = nil
    let generation = store.backendGeneration
    let backend = store.backend
    do {
      let response: TasksResponse = try await backend.get("/v1/tasks?status=all&limit=100")
      try Task.checkCancellation()
      guard generation == store.backendGeneration else { return }
      tasks = response.tasks.filter { $0.status != "dropped" }.filter { query.isEmpty || ($0.title + " " + ($0.waiting?.onName ?? "")).localizedCaseInsensitiveContains(query) }
        .sorted { $0.needsYou && !$1.needsYou }
    } catch {
      if !Task.isCancelled && generation == store.backendGeneration { taskError = "Couldn’t refresh tasks. Showing the last available tasks." }
    }
    do {
      let encoded =
        query.addingPercentEncoding(
          withAllowedCharacters: .urlQueryAllowed.subtracting(CharacterSet(charactersIn: "&+?#")))
        ?? ""
      let response: EffortsResponse = try await store.read("/v1/efforts?q=\(encoded)")
      try Task.checkCancellation()
      guard generation == store.backendGeneration else { return }
      efforts = response.efforts.filter(EffortPresentation.visible)
      loading = false
      // Queue/model availability must not turn a successfully loaded list into a load error.
      let queue: EffortNamingResponse? = try? await backend.get("/v1/efforts/unnamed")
      try Task.checkCancellation()
      guard generation == store.backendGeneration else { return }
      unnamedCount = queue?.total ?? 0
    } catch is CancellationError {} catch {
      if !Task.isCancelled && generation == store.backendGeneration {
        loadError = "Couldn’t refresh work. Showing the last available work."
        loading = false
      }
    }
  }
  private var detailedView: some View {
    VStack(alignment: .leading, spacing: 16) {
      HStack {
        Picker("Integration", selection: $store.activityAgent) {
          Text("All integrations").tag("all")
          ForEach(store.agents) { Text($0.name).tag($0.id) }
        }.frame(maxWidth: 260)
        Picker("Show activity", selection: $store.activityFilter) {
          Text("All").tag("all")
          Text("Sends and drafts").tag("sends")
          Text("Reads").tag("reads")
        }.pickerStyle(.segmented).labelsHidden().accessibilityLabel("Show activity")
      }
      ActionFeedback(store: store)
      Text("Each action, newest first").font(AppType.caption).foregroundStyle(AccessibleSecondary())
      ForEach(rows) { row in
        DisclosureGroup(isExpanded: Binding(get: { expandedLogs.contains(row.id) }, set: { if $0 { expandedLogs.insert(row.id) } else { expandedLogs.remove(row.id) } })) {
          ActivityRow(store: store, row: row)
          Button("Inspect messages") {
            store.selectedActivity = row.id
            Task { await store.loadActivity() }
          }.limmrButton()
          if store.selectedActivity == row.id { ActivityDetail(store: store).frame(minHeight: 240) }
        } label: {
          Text("\(store.time(row.at)) · \(store.agentName(row.agentId)) · \(row.verb)").font(
            .callout)
        }
        .focused($rowFocused, equals: "log:\(row.id)")
        .keyboardRow("log:\(row.id)", selection: selection.id, horizontal: 8, vertical: 6)
        Divider()
      }
      if rows.isEmpty && store.isLoading("/v1/activity") {
        QuietLoadingCards()
      } else if rows.isEmpty && store.hasLoaded("/v1/activity") {
        QuietBlockState(title: "A quiet activity log", detail: "Actions appear here when your integrations work. Try another filter to see earlier activity.")
      }
      Button("Load earlier activity") { Task { await store.loadMoreActivity() } }
        .buttonStyle(NativeGlassButtonStyle()).disabled(
          store.loadingMore || store.activityBefore == nil)
    }
  }
}

struct ActivityRow: View {
  @Bindable var store: AppStore
  let row: Activity
  var compact = false
  private var sending: Bool { ActivityPresentation.isSend(row) }
  var body: some View {
    HStack(alignment: .top, spacing: 16) {
      Image(
        systemName: row.kind == "sent" ? "paperplane" : sending ? "square.and.pencil" : "text.quote"
      )
      .font(.system(size: 15)).foregroundStyle(AccessibleSecondary()).frame(width: 24, height: 24)
      VStack(alignment: .leading, spacing: 8) {
        Text(headline).font(AppType.rowTitle).fixedSize(horizontal: false, vertical: true)
        if sending {
          Text(
            row.note?.split(separator: "\n").first.map(String.init)
              ?? "Message text wasn't recorded."
          )
          .font(.callout).lineLimit(compact ? 2 : 3)
          Text(ActivityPresentation.sendStatus(row))
            .font(.caption.weight(.medium)).foregroundStyle(
              row.kind == "sent" ? Color.accentColor : .secondary)
        } else if row.tool != "circle_structure_placed", let note = row.note {
          Text(note).font(.callout).foregroundStyle(AccessibleSecondary()).lineLimit(2)
        }
        if let reason = row.instructionReason {
          Text(reason).font(.callout).foregroundStyle(AccessibleSecondary())
        }
        Text("\(Timestamp.date(row.at)?.formatted(date: .abbreviated, time: .shortened) ?? row.at)")
          .font(AppType.caption).foregroundStyle(AccessibleSecondary())
      }
      Spacer(minLength: 0)
    }.padding(.vertical, compact ? 0 : 8).frame(maxWidth: .infinity, alignment: .leading)
      .contentShape(Rectangle())
  }
  private var headline: String {
    if row.tool == "circle_structure_placed", let note = row.note { return note }
    let agent = store.agentName(row.agentId)
    let chats = row.chatJids.map(store.chatName).joined(separator: ", ")
    if row.kind == "sent" { return "\(agent) → \(chats.isEmpty ? "Chat" : chats)" }
    if sending { return "\(agent) · \(row.verb)" + (chats.isEmpty ? "" : " · " + chats) }
    return "\(agent) · \(row.verb)"
  }
}

struct ActivityDetail: View {
  @Bindable var store: AppStore
  var body: some View {
    ScrollView {
      if let row = store.activity.first(where: { $0.id == store.selectedActivity }) {
        LazyVStack(alignment: .leading, spacing: 0) {
          VStack(alignment: .leading, spacing: 16) {
            Text(
              row.tool == "notify_me"
                ? (row.kind == "denied" ? "Notification request denied" : "Asked to notify you")
                : row.tool == "upload_attachment"
                  ? (row.kind == "denied" ? "Upload denied" : "Uploaded a file") : row.verb
            ).font(AppType.blockHeading)
            Text("\(store.agentName(row.agentId)) · \(store.time(row.at))")
              .font(.callout).foregroundStyle(AccessibleSecondary())
            if let jid = row.chatJids.first { Text(store.chatName(jid)).font(AppType.rowTitle) }
            if row.tool == "update_brief" || row.kind == "brief_updated" {
              Button("Open brief version") { Task { await store.openActivityBrief(row) } }
                .limmrButton()
              ActionFeedback(store: store, action: "activity-brief")
            }
            Divider()
            if let query = row.query { Text("Search: “\(query)”") }
            if let note = row.note { Text(note).textSelection(.enabled) }
            if let reason = row.instructionReason { Text(reason).textSelection(.enabled) }
            ForEach(store.activityMessages) { message in
              VStack(alignment: .leading, spacing: 8) {
                Text(store.senderName(message.sender)).font(AppType.rowTitle)
                Text(message.displayText).textSelection(.enabled)
                Text(store.time(message.timestamp)).font(AppType.caption).foregroundStyle(AccessibleSecondary())
              }
              Divider()
            }
            if !row.messageIds.isEmpty, store.activityMessages.isEmpty {
              Text("The original messages are no longer available.").foregroundStyle(AccessibleSecondary())
            }
            ActionFeedback(store: store, action: "activity-detail")
            DisclosureGroup("Show details") {
              Text(row.tool).font(.system(.caption, design: .monospaced))
            }
          }
        }
      }
    }
  }
}
