import LimmrKit
import SwiftUI

struct ActivityView: View {
  @Bindable var store: AppStore
  @State var detailedLog = false
  @State var query = ""
  @State private var selection = KeyboardSelection()
  @State private var finishedExpanded = false
  @State private var quietExpanded = false
  @State private var endedTasksExpanded = false
  @State private var expandedLogs: Set<Int64> = []
  @Environment(\.limmrWindowWidth) private var windowWidth
  @FocusState private var rowFocused: String?
  private var rowIDs: [String] {
    (detailedLog || store.activityDay != nil) ? rows.map { "log:\($0.id)" } : shownTasks.map { "task:" + $0.id } + (active + (finishedExpanded ? finished : []) + (quietExpanded ? quiet : [])).map(\.id)
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
    ActivityPresentation.detailed(store.activity + (store.dashboard?.recentSends ?? []),
      day: store.activityDay, agent: store.activityAgent, filter: store.activityFilter)
  }

  private var active: [Effort] { efforts.filter { $0.status != "done" && $0.status != "dormant" } }
  private var quiet: [Effort] { efforts.filter { $0.status == "dormant" } }
  private var finished: [Effort] {
    let now = store.presentationNow
    return efforts.filter {
      $0.status == "done"
        && (!query.isEmpty
          || (Timestamp.date($0.lastAt) ?? .distantPast) >= now.addingTimeInterval(-7 * 86400))
    }
  }
  var body: some View {
    let _ = SpeedTrace.body("Activity")
    content
      .n1PageSurface()
      .environment(\.keyboardRowNavigation, true)
      .onChange(of: rowFocused) { _, id in if let id { selection.id = id } }
      .onChange(of: rowIDs) { _, ids in if selection.current(in: ids) == nil { selection.id = nil } }
      .onChange(of: store.searchRequest) { _, _ in
        detailedLog = false
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
  private var byLog: Bool { detailedLog || store.activityDay != nil }
  /// Open tasks first; ended ones stay folded until asked for (a search shows every match).
  private var endedTasks: [TaskSummary] { TaskListPresentation.history(tasks) }
  private var openTasks: [TaskSummary] {
    let ended = Set(endedTasks.map(\.id))
    return tasks.filter { $0.status != "suggested" && !ended.contains($0.id) }
  }
  private var shownTasks: [TaskSummary] { openTasks + (endedTasksExpanded || !query.isEmpty ? endedTasks : []) }
  private var hasWork: Bool {
    !active.isEmpty || !finished.isEmpty || !quiet.isEmpty
      || (!store.home.circleChanges.isEmpty && (query.isEmpty || "Circle sorting".localizedCaseInsensitiveContains(query)))
  }
  private var hairline: some View { Rectangle().fill(LimmrTheme.hairline).frame(height: 1) }
  private var content: some View {
    let width = PaperLayout.contentWidth(windowWidth)
    return ScrollViewReader { proxy in
    ScrollView {
      LazyVStack(alignment: .leading, spacing: 20) {
        PaperPageHeader(title: "Activity", subtitle: windowWidth >= 1200 ? "What your AIs read, sent and worked on" : "") {
          PaperSearch(store: store, query: $query, prompt: "Search work, people or chats").frame(width: 300)
            .onSubmit { openSelection() }
            .accessibilityHint("Use arrow keys to choose work, then Return to open it")
        }
        HStack(spacing: 8) {
          PaperSegments(values: ["By purpose", "Detailed log"], selection: Binding(
            get: { byLog ? "Detailed log" : "By purpose" },
            set: { detailedLog = $0 == "Detailed log"; if !detailedLog { store.activityDay = nil } }))
          if let day = store.activityDay {
            Button { store.activityDay = nil } label: {
              HStack(spacing: 6) { Text(Self.dayTitle(day)); Image(systemName: "xmark").font(.system(size: 9, weight: .semibold)) }
                .font(LimmrTheme.font(12, .semibold)).padding(.horizontal, 12).frame(height: 28)
                .background(LimmrTheme.surface2, in: RoundedRectangle(cornerRadius: 12))
            }.buttonStyle(.plain).accessibilityLabel("Clear day filter, \(day)")
          }
          Spacer(minLength: 0)
        }
        if let history = store.history { overview(history, width: width) }
        if byLog {
          detailedView
        } else {
          if let loadError {
            notice(loadError, button: "Try again")
          } else if loading {
            QuietLoadingCards()
          }
          if query.isEmpty, let namer = store.effortNamer {
            if namer.pending > 0 {
              Text("This Mac is naming \(namer.pending) more").font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2)
            } else if let notice = namer.notice {
              Text(notice).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2)
            }
          }
          if let taskError { notice(taskError, button: "Try tasks again") }
          // Wide windows put tasks and the work Limmr noticed side by side; each list hugs its rows.
          if width >= 900 && !(openTasks + endedTasks).isEmpty && hasWork {
            HStack(alignment: .top, spacing: 24) {
              taskColumn.frame(width: (width - 24) / 2, alignment: .leading)
              workColumn.frame(width: (width - 24) / 2, alignment: .leading)
            }
          } else {
            if !(openTasks + endedTasks).isEmpty { taskColumn }
            if hasWork { workColumn }
          }
          if !loading && loadError == nil && efforts.isEmpty && tasks.isEmpty {
            EmptyState(
              symbol: "text.magnifyingglass",
              title: EffortPresentation.emptyTitle(query: query, unnamed: unnamedCount),
              detail: EffortPresentation.emptyDetail(query: query, unnamed: unnamedCount))
          }
        }
      }.font(LimmrTheme.TypeScale.body).foregroundStyle(LimmrTheme.ink)
        .padding(.horizontal, 28).padding(.top, 24).padding(.bottom, 24).paperReadingWidth()
    }
    .onChange(of: selection.id) { _, id in if let id { proxy.scrollTo(id, anchor: .center) } }
    }
    .task(id: "\(detailedLog):\(store.activityDay ?? ""):\(store.backendGeneration):\(store.effortRevision):\(query):\(store.home.circleUpdates.map(\.lastAt).max() ?? ""):\(store.allTasks.map { $0.id + $0.status }.hashValue)") {
      if detailedLog || store.activityDay != nil {
        await store.refreshDetailedActivity()
      } else { await load() }
    }
    .task(id: store.activityAgent) {
      if !store.snapshot, detailedLog || store.activityDay != nil {
        await store.filterActivity(agent: store.activityAgent, kind: "all", chat: "all")
      }
    }
    .sheet(item: $reviewingChange) { CircleChangeReview(store: store, change: $0).dismissOnEscape() }
    .sheet(isPresented: $showAllChanges) { CircleChangesList(store: store).dismissOnEscape() }
  }
  static func dayTitle(_ day: String) -> String {
    Timestamp.date(day + "T00:00:00Z").map { $0.formatted(.dateTime.weekday(.abbreviated).day().month(.abbreviated).locale(Locale(identifier: "en_GB"))) } ?? day
  }
  private func sectionTitle(_ text: String, link: String? = nil, action: @escaping () -> Void = {}) -> some View {
    HStack(alignment: .firstTextBaseline) {
      Text(text).font(LimmrTheme.TypeScale.section)
      Spacer(minLength: 8)
      if let link { Button(link, action: action).buttonStyle(.plain).font(LimmrTheme.font(12, .semibold)).foregroundStyle(LimmrTheme.ink2) }
    }
  }
  private func notice(_ text: String, button: String) -> some View {
    HStack(spacing: 12) {
      Text(text).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2)
      Button(button) { Task { await load() } }.buttonStyle(PaperButtonStyle())
    }
  }
  /// This week's numbers, the heat-map at its natural size, and (when there is room left) each AI's week.
  private func overview(_ history: StatsHistory, width: Double) -> some View {
    let week = history.period(7), stacked = width < 640, inner = width - 32
    let available = stacked ? inner : inner - 176
    let weeks = ActivityHeatmap.weeks(width: available, days: history.days.count)
    let natural = ActivityHeatmap.labels + Double(weeks) * (ActivityHeatmap.cell + ActivityHeatmap.gap) - ActivityHeatmap.gap
    let agents = store.agents.compactMap { agent -> (Agent, Int, Int)? in
      let reads = week.reduce(0) { $0 + ($1.agents?[agent.id]?.reads ?? 0) }, sends = week.reduce(0) { $0 + ($1.agents?[agent.id]?.sends ?? 0) }
      return reads + sends > 0 ? (agent, reads, sends) : nil
    }.sorted { $0.1 + $0.2 > $1.1 + $1.2 }
    let side = !stacked && !agents.isEmpty && available - natural >= 280
    let map = side ? natural : min(available, natural * 1.6)
    let numbers = stacked ? AnyLayout(HStackLayout(alignment: .top, spacing: 24)) : AnyLayout(VStackLayout(alignment: .leading, spacing: 12))
    let card = stacked ? AnyLayout(VStackLayout(alignment: .leading, spacing: 16)) : AnyLayout(HStackLayout(alignment: .top, spacing: 0))
    return card {
      VStack(alignment: .leading, spacing: 12) {
        Text("This week").font(LimmrTheme.font(12, .semibold)).foregroundStyle(LimmrTheme.ink2)
        numbers {
          PaperNumber(value: week.reduce(0) { $0 + $1.agentActivity }.formatted(), label: "AI actions")
          PaperNumber(value: week.reduce(0) { $0 + $1.agentSends }.formatted(), label: "sent by AIs")
          PaperNumber(value: week.reduce(0) { $0 + $1.messages }.formatted(), label: "messages")
        }
      }.frame(width: stacked ? nil : 176, alignment: .leading)
      VStack(alignment: .leading, spacing: 8) {
        Text("AI actions a day").font(LimmrTheme.font(12, .semibold)).foregroundStyle(LimmrTheme.ink2)
        ActivityHeatmap(history: history, agents: store.agents, width: map, openDay: store.openActivityDay)
        Text("Last \(weeks) weeks · choose a day to see its log").font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3)
      }
      if side {
        Rectangle().fill(LimmrTheme.hairline).frame(width: 1).padding(.horizontal, 24)
        VStack(alignment: .leading, spacing: 10) {
          Text("Your AIs this week").font(LimmrTheme.font(12, .semibold)).foregroundStyle(LimmrTheme.ink2)
          let top = Double(max(1, agents.first.map { $0.1 + $0.2 } ?? 1))
          ForEach(agents.prefix(5), id: \.0.id) { agent, reads, sends in
            Button { store.activityAgent = agent.id; detailedLog = true } label: {
              VStack(alignment: .leading, spacing: 5) {
                HStack(alignment: .firstTextBaseline) {
                  Text(agent.name).font(LimmrTheme.font(14, .semibold)).lineLimit(1)
                  Spacer(minLength: 8)
                  Text(sends > 0 ? "\(reads) read · \(sends) sent" : "\(reads) read").font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2).lineLimit(1)
                }
                GeometryReader { bar in
                  ZStack(alignment: .leading) {
                    Capsule().fill(LimmrTheme.ink.opacity(0.07))
                    Capsule().fill(LimmrTheme.ink.opacity(0.62)).frame(width: max(4, bar.size.width * Double(reads + sends) / top))
                  }
                }.frame(height: 4)
              }.contentShape(Rectangle())
            }.buttonStyle(.plain).accessibilityLabel("\(agent.name), \(reads) read, \(sends) sent this week").accessibilityHint("Opens this AI's log")
          }
        }.frame(maxWidth: .infinity, alignment: .leading)
      }
    }.padding(16).frame(maxWidth: .infinity, alignment: .leading).n1Card()
  }
  private var taskColumn: some View {
    VStack(alignment: .leading, spacing: 10) {
      sectionTitle("Tasks", link: "All tasks") { store.selectPage(.tasks) }
      VStack(alignment: .leading, spacing: 0) {
        ForEach(shownTasks) { task in
          Button { store.openTask(task.id) } label: {
            HStack(spacing: 12) {
              PaperTaskArtwork(store: store, task: task).frame(width: 44, height: 34).clipShape(RoundedRectangle(cornerRadius: 7))
              VStack(alignment: .leading, spacing: 3) {
                Text(task.title).font(LimmrTheme.font(14, .semibold)).lineLimit(1)
                Text([store.taskCircleName(task), task.needsYouReason ?? N1TaskStatus.text(task)].filter { !$0.isEmpty }.joined(separator: " · "))
                  .font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2).lineLimit(1)
              }
              Spacer(minLength: 8)
              if task.needsYou { PaperMarker() } else if store.paperWorkingAgent(task) != nil { PaperDot() }
              if let at = task.lastUpdateAt ?? task.lastActiveAt {
                Text(store.relativeTime(at)).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3).lineLimit(1).fixedSize()
              }
            }.padding(.horizontal, 14).padding(.vertical, 10).contentShape(Rectangle())
          }.quietCard()
            .focused($rowFocused, equals: "task:" + task.id)
            .keyboardRow("task:" + task.id, selection: selection.id, radius: 8)
            .modifier(PaperTaskContext(store: store, task: task))
            .accessibilityLabel(TaskPresentation.accessibilityLabel(task))
          if task.id != shownTasks.last?.id { hairline }
        }
      }.n1Card().clipShape(RoundedRectangle(cornerRadius: LimmrTheme.cardRadius))
      if query.isEmpty && !endedTasks.isEmpty { fold("Ended", count: endedTasks.count, open: $endedTasksExpanded) }
    }
  }
  private func fold(_ title: String, count: Int, open: Binding<Bool>) -> some View {
    Button { open.wrappedValue.toggle() } label: {
      HStack(spacing: 10) {
        Text(title).font(LimmrTheme.font(14, .semibold)); Text("\(count)").foregroundStyle(LimmrTheme.ink3)
        Spacer(); Image(systemName: open.wrappedValue ? "chevron.down" : "chevron.right").foregroundStyle(LimmrTheme.ink3)
      }.font(LimmrTheme.font(12)).padding(.horizontal, 14).frame(height: 44).frame(maxWidth: .infinity).n1Card().contentShape(Rectangle())
    }.quietCard().accessibilityLabel("\(title), \(count)").accessibilityValue(open.wrappedValue ? "Expanded" : "Collapsed")
  }
  private var workColumn: some View {
    VStack(alignment: .leading, spacing: 10) {
      if !active.isEmpty {
        sectionTitle("Active")
        ForEach(active) { effort in effortCard(effort) }
      }
      if !store.home.circleChanges.isEmpty
        && (query.isEmpty || "Circle sorting".localizedCaseInsensitiveContains(query))
      {
        sectionTitle("Circle sorting").padding(.top, active.isEmpty ? 0 : 10)
        VStack(alignment: .leading, spacing: 12) {
          Text("Limmr placed people in your circles. Review the changes or undo them.")
            .font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2)
          ForEach(store.home.circleChanges) { change in
            CircleChangeRow(store: store, change: change) { reviewingChange = change }
            if change.id != store.home.circleChanges.last?.id { hairline }
          }
          if store.home.circleChangesMore > 0 {
            Button("Review all changes") { showAllChanges = true }.buttonStyle(PaperButtonStyle())
          }
          ActionFeedback(store: store, action: "circle-changes")
        }.padding(14).frame(maxWidth: .infinity, alignment: .leading).n1Card()
      }
      if !finished.isEmpty {
        fold(query.isEmpty ? "Finished this week" : "Finished", count: finished.count, open: $finishedExpanded)
        if finishedExpanded { ForEach(finished) { effort in effortCard(effort) } }
      }
      if !quiet.isEmpty {
        fold("Quiet", count: quiet.count, open: $quietExpanded)
        if quietExpanded {
          ForEach(quiet) { effort in
            HStack(alignment: .top, spacing: 12) {
              effortCard(effort)
              Button("Dismiss", systemImage: "eye.slash") {
                Task {
                  if await store.updateEffort(effort.id, values: ["hidden": true]) != nil {
                    efforts.removeAll { $0.id == effort.id }
                  }
                }
              }.buttonStyle(PaperButtonStyle()).disabled(store.busy)
                .accessibilityLabel("Dismiss \(effort.title)")
            }
            ActionFeedback(store: store, action: "effort-\(effort.id)")
          }
        }
      }
    }
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
      tasks = response.tasks.filter { !["dropped", "suggested"].contains($0.status) }.filter { query.isEmpty || ($0.title + " " + ($0.waiting?.onName ?? "")).localizedCaseInsensitiveContains(query) }
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
    let visibleRows = rows
    return VStack(alignment: .leading, spacing: 12) {
      ViewThatFits(in: .horizontal) {
        HStack(spacing: 8) { logFilters; Spacer(minLength: 8); Text("Each action, newest first").font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3) }
        logFilters
      }
      ActionFeedback(store: store)
      if !visibleRows.isEmpty {
        LazyVStack(alignment: .leading, spacing: 0) {
          ForEach(visibleRows) { row in
            logRow(row)
            if row.id != visibleRows.last?.id { hairline }
          }
        }.n1Card().clipShape(RoundedRectangle(cornerRadius: LimmrTheme.cardRadius))
      }
      if visibleRows.isEmpty && store.isLoading("/v1/activity") {
        QuietLoadingCards()
      } else if visibleRows.isEmpty && store.hasLoaded("/v1/activity") {
        QuietBlockState(title: "A quiet activity log", detail: "Actions appear here when your integrations work. Try another filter to see earlier activity.")
      }
      if let before = store.activityBefore {
        Color.clear.frame(height: 1).id("activity-tail-\(before)")
          .onAppear { Task { await store.loadMoreActivity() } }
      }
      Button("Load earlier activity") { Task { await store.loadMoreActivity() } }
        .buttonStyle(PaperButtonStyle()).disabled(
          store.loadingMore || store.activityBefore == nil)
    }
  }
  private var logFilters: some View {
    HStack(spacing: 8) {
      Menu {
        Picker("Integration", selection: $store.activityAgent) {
          Text("All integrations").tag("all")
          ForEach(store.agents) { Text($0.name).tag($0.id) }
        }
      } label: { Label(store.activityAgent == "all" ? "All integrations" : store.agentName(store.activityAgent), systemImage: "line.3.horizontal.decrease") }
        .menuStyle(.borderlessButton).fixedSize().padding(.horizontal, 12).frame(height: 28)
        .background(LimmrTheme.surface2, in: RoundedRectangle(cornerRadius: 12)).font(LimmrTheme.font(12, .semibold))
        .accessibilityLabel("Integration")
      PaperSegments(values: ["all", "sends", "reads"], selection: $store.activityFilter,
        labels: ["all": "All", "sends": "Sends and drafts", "reads": "Reads"]).accessibilityLabel("Show activity")
    }
  }
  private func logRow(_ row: Activity) -> some View {
    let open = expandedLogs.contains(row.id)
    let sending = ActivityPresentation.isSend(row)
    return VStack(alignment: .leading, spacing: 0) {
      Button { if open { expandedLogs.remove(row.id) } else { expandedLogs.insert(row.id) } } label: {
        HStack(spacing: 12) {
          Image(systemName: row.kind == "sent" ? "paperplane" : sending ? "square.and.pencil" : "text.quote")
            .font(.system(size: 13)).foregroundStyle(LimmrTheme.ink3).frame(width: 20)
          Text(store.agentName(row.agentId)).font(LimmrTheme.font(14, .semibold)).lineLimit(1).fixedSize()
          Text(row.verb + (row.chatJids.isEmpty ? "" : " · " + row.chatJids.map(store.chatName).joined(separator: ", ")))
            .font(LimmrTheme.font(14)).foregroundStyle(LimmrTheme.ink2).lineLimit(1)
          Spacer(minLength: 8)
          Text(store.time(row.at)).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3).lineLimit(1).fixedSize()
          Image(systemName: open ? "chevron.down" : "chevron.right").font(.system(size: 10, weight: .semibold)).foregroundStyle(LimmrTheme.ink3)
        }.padding(.horizontal, 14).frame(minHeight: 44).contentShape(Rectangle())
      }.buttonStyle(.plain).accessibilityValue(open ? "Expanded" : "Collapsed")
      if open {
        VStack(alignment: .leading, spacing: 10) {
          ActivityRow(store: store, row: row)
          Button("Inspect messages") {
            store.selectedActivity = row.id
            Task { await store.loadActivity() }
          }.buttonStyle(PaperButtonStyle())
          if store.selectedActivity == row.id { ActivityDetail(store: store).frame(minHeight: 240) }
        }.padding(.leading, 46).padding(.trailing, 14).padding(.bottom, 14)
      }
    }
    .focused($rowFocused, equals: "log:\(row.id)")
    .keyboardRow("log:\(row.id)", selection: selection.id, horizontal: 0, vertical: 0)
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
      .font(.system(size: 13)).foregroundStyle(LimmrTheme.ink3).frame(width: 20, height: 20)
      VStack(alignment: .leading, spacing: 6) {
        Text(headline).font(LimmrTheme.font(14, .semibold)).fixedSize(horizontal: false, vertical: true)
        if let line = row.firstContactLine {
          Text(line).font(LimmrTheme.font(14, .medium)).fixedSize(horizontal: false, vertical: true)
        }
        if sending {
          Text(
            row.note?.split(separator: "\n").first.map(String.init)
              ?? "Message text wasn't recorded."
          )
          .font(LimmrTheme.font(14)).lineLimit(compact ? 2 : 3)
          Text(ActivityPresentation.sendStatus(row))
            .font(LimmrTheme.font(12, .medium)).foregroundStyle(
              row.kind == "sent" ? LimmrTheme.infoText : LimmrTheme.ink2)
        } else if row.tool != "circle_structure_placed", let note = row.note {
          Text(note).font(.callout).foregroundStyle(AccessibleSecondary()).lineLimit(2)
        }
        if let id = row.taskId {
          Button("Open task") { store.openTask(id) }.buttonStyle(.link)
        }
        ForEach(row.chatJids, id: \.self) { jid in
          Button(store.chatName(jid), systemImage: "bubble.left") {
            store.selectPage(.circles)
            store.circlePath = jid.hasSuffix("@g.us") ? [.group(jid)] : [.chat(jid, store.chatName(jid))]
          }.buttonStyle(.link)
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
    return "\(agent) · \(row.verb)" + (chats.isEmpty ? "" : " · " + chats)
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
            if let line = row.firstContactLine { Text(line).font(.callout.weight(.medium)) }
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
