import LimmrKit
import SwiftUI

struct PaperTasksPage: View {
  @Bindable var store: AppStore
  @SceneStorage("n1.tasks.layout") private var layout = "gallery"
  @Environment(\.snapshotTaskLayout) private var snapshotLayout
  @Environment(\.limmrWindowWidth) private var windowWidth
  @Environment(\.limmrWindowHeight) private var windowHeight
  private var condensed: Bool { PaperLayout.taskListWidth(windowWidth) < 760 }
  @State private var query = ""
  @State private var scope = "Open"
  @State private var circle = "all"
  @State private var sort = "Last updated"
  @State private var selected: String?
  @FocusState private var rowFocus: String?
  private var matched: [TaskSummary] {
    let matches = TaskListPresentation.search(store.allTasks, title: query).filter { circle == "all" || (circle == "none" ? $0.circleIds.isEmpty : $0.circleIds.contains(circle)) }
    return matches.sorted { a,b in
      if scope == "Open", a.needsYou != b.needsYou { return a.needsYou }
      switch sort {
      case "Title": return a.title.localizedStandardCompare(b.title) == .orderedAscending
      case "Progress": return a.progress.fraction > b.progress.fraction
      case "Circle": return store.taskCircleName(a) == store.taskCircleName(b) ? a.id < b.id : store.taskCircleName(a) < store.taskCircleName(b)
      case "Who": return a.createdBy.name == b.createdBy.name ? a.id < b.id : a.createdBy.name < b.createdBy.name
      default: return (a.lastActiveAt ?? a.createdAt ?? "") == (b.lastActiveAt ?? b.createdAt ?? "") ? a.id < b.id : (a.lastActiveAt ?? a.createdAt ?? "") > (b.lastActiveAt ?? b.createdAt ?? "")
      }
    }
  }
  private var ended: [TaskSummary] { TaskListPresentation.history(matched) }
  private var open: [TaskSummary] { TaskListPresentation.open(matched) }
  private var visible: [TaskSummary] { scope == "Open" ? open : ended }
  private var current: TaskSummary? { visible.first { $0.id == selected } ?? visible.first(where: \.needsYou) ?? visible.first }
  private var isList: Bool { (snapshotLayout ?? layout) == "list" }
  var body: some View {
    ScrollViewReader { proxy in
      ScrollView {
        VStack(alignment: .leading, spacing: 14) {
          PaperPageHeader(title: "Tasks", subtitle: windowWidth >= 1200 ? "\(open.filter(\.needsYou).count) need you · \(open.filter { store.paperWorkingAgent($0) != nil }.count) working · \(open.filter { $0.nextCheckInAt != nil }.count) scheduled" : "") {
            PaperSearch(store: store, query: $query, prompt: "Search tasks").frame(width: 300)
          }
          controls
          if let error = store.allTasksError { HStack { Text(error); Button("Try again") { Task { await store.refreshAllTasks() } } } }
          if visible.isEmpty { ContentUnavailableView(query.isEmpty ? "No tasks" : "No matching tasks", systemImage: "checklist", description: Text("Tasks you ask for appear here.")) }
          else if isList {
            let layout = PaperLayout.sideBySide(windowWidth) ? AnyLayout(HStackLayout(alignment: .top, spacing: 12)) : AnyLayout(VStackLayout(alignment: .leading, spacing: 12))
            layout {
              list.frame(maxWidth: .infinity)
              if let current { PaperTaskInspector(store: store, summary: current).id("inspector:" + current.id).frame(width: PaperLayout.inspectorWidth(windowWidth)) }
            }
          } else {
            VStack(alignment: .leading, spacing: 14) {
            LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 12), count: PaperLayout.taskColumns(windowWidth)), spacing: 12) {
              ForEach(visible) { task in
                PaperTaskCard(store: store, task: task).focused($rowFocus, equals: task.id)
                  .quietKeyboardRow(task.id, selection: rowFocus)
              }
            }
            Spacer(minLength: 0)
            if scope == "Open" { endedRow }
            }.frame(minHeight: max(0, windowHeight - 192), alignment: .topLeading)
          }
          SuggestedTasksRow(store: store, tasks: TaskPresentation.suggested(matched))
          if let notice = store.taskQuietNotice { HStack { Text(notice); Button("Undo") { Task { await store.undoQuietTask() } } }.font(LimmrTheme.font(12)) }
        }.padding(.horizontal, 28).padding(.top, 24).padding(.bottom, 24).paperReadingWidth()
      }
      .onChange(of: selected) { _, id in if let id, rowFocus == id { proxy.scrollTo(id, anchor: .center) } }
    }
      .onChange(of: store.rowRequest) { _, _ in move(store.rowStep) }
      .onChange(of: store.rowOpenRequest) { _, _ in if let current { store.openTask(current.id) } }
      .onKeyPress(.upArrow) { move(isList ? -1 : -PaperLayout.taskColumns(windowWidth)); return .handled }
      .onKeyPress(.downArrow) { move(isList ? 1 : PaperLayout.taskColumns(windowWidth)); return .handled }
      .onKeyPress(.leftArrow) { guard !isList else { return .ignored }; move(-1); return .handled }
      .onKeyPress(.rightArrow) { guard !isList else { return .ignored }; move(1); return .handled }
      .onKeyPress(.return) { guard let current else { return .ignored }; store.openTask(current.id); return .handled }
      .task {
        await store.refreshAllTasks()
        if store.snapshot && store.allTasks.contains(where: { $0.id == "n4-task-1" }) { selected = "n4-task-1" }
      }
  }
  private func move(_ step: Int) { var cursor = KeyboardSelection(id: selected); cursor.move(step, in: visible.map(\.id)); selected = cursor.id; rowFocus = cursor.id }
  private var controls: some View {
    ViewThatFits(in: .horizontal) {
      HStack(spacing: 8) { scopePicker; Spacer(minLength: 8); filters; layoutPicker }
      VStack(alignment: .leading, spacing: 8) { HStack { scopePicker; Spacer(); layoutPicker }; filters }
    }
  }
  private var scopePicker: some View {
    PaperSegments(values: ["Open", "Ended"], selection: $scope, labels: ["Open": "Open  \(open.count)", "Ended": "Ended  \(ended.count)"]).accessibilityValue("\(open.count) open, \(ended.count) ended tasks")
  }
  private var layoutPicker: some View {
    PaperSegments(values: ["Gallery", "List"], selection: Binding(get: { isList ? "List" : "Gallery" }, set: { layout = $0.lowercased() }))
  }
  private var filters: some View {
    HStack(spacing: 8) {
      Menu {
        Picker("Circle", selection: $circle) { Text("All circles").tag("all"); ForEach(store.circles) { Text($0.name).tag($0.id) }; Text("No circle").tag("none") }
      } label: { Label(circle == "all" ? "All circles" : circle == "none" ? "No circle" : store.circleName(circle), systemImage: "line.3.horizontal.decrease") }.menuStyle(.borderlessButton).fixedSize().padding(.horizontal, 12).frame(height: 28).background(LimmrTheme.surface2, in: RoundedRectangle(cornerRadius: 12))
      Menu {
        Picker("Sort tasks", selection: $sort) { ForEach(["Last updated", "Title", "Progress", "Who", "Circle"], id: \.self) { Text($0) } }
      } label: { Label(sort, systemImage: "arrow.up.arrow.down") }.menuStyle(.borderlessButton).fixedSize().padding(.horizontal, 12).frame(height: 28).background(LimmrTheme.surface2, in: RoundedRectangle(cornerRadius: 12))
    }.font(LimmrTheme.font(12, .semibold))
  }
  private var list: some View {
    VStack(alignment: .leading, spacing: 0) {
      HStack(spacing: 12) {
        Text("Task").frame(maxWidth: .infinity, alignment: .leading)
        Text("Next").frame(maxWidth: .infinity, alignment: .leading)
        if !condensed {
          Text("Who").frame(width: 80, alignment: .leading)
          Text("Progress").frame(width: 76)
          Text("Updated").lineLimit(1).frame(width: 54, alignment: .trailing)
        }
      }.font(LimmrTheme.font(12, .medium)).foregroundStyle(LimmrTheme.ink3).padding(.leading, 70).padding(.trailing, 14).frame(height: 32)
      if scope == "Ended" { taskRows("Ended", tasks: ended) }
      else {
        taskRows("Needs you", tasks: open.filter(\.needsYou))
        taskRows("Working now", tasks: open.filter { !$0.needsYou && $0.status != "quiet" && $0.nextCheckInAt == nil })
        taskRows("Scheduled", tasks: open.filter { !$0.needsYou && $0.nextCheckInAt != nil })
        taskRows("Quiet", tasks: open.filter { !$0.needsYou && $0.status == "quiet" && $0.nextCheckInAt == nil })
        endedRow
      }
    }.n1Card().clipShape(RoundedRectangle(cornerRadius: 16))
  }
  @ViewBuilder private func taskRows(_ title: String, tasks: [TaskSummary]) -> some View {
    if !tasks.isEmpty {
      HStack(spacing: 8) {
        if title == "Needs you" { PaperMarker() }
        if title == "Working now", tasks.contains(where: { store.paperWorkingAgent($0) != nil }) { PaperDot() }
        Text(title + " · \(tasks.count)")
      }.font(LimmrTheme.font(12, .semibold)).foregroundStyle(LimmrTheme.ink3).padding(.horizontal, 14).frame(height: 30)
      ForEach(tasks) { task in
        Button { selected = task.id } label: {
          HStack(spacing: 12) {
            PaperTaskArtwork(store: store, task: task).frame(width: 44, height: 34).clipShape(RoundedRectangle(cornerRadius: 7))
            VStack(alignment: .leading, spacing: 4) {
              Text(task.title).font(LimmrTheme.font(14, .semibold)).lineLimit(2)
              Text(store.taskCircleName(task)).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2).lineLimit(2)
              if condensed {
                Text((task.needsYou ? "You" : store.paperWorkingAgent(task) ?? "Scheduled") + " · " + ((task.lastUpdateAt ?? task.lastActiveAt).map(store.relativeTime) ?? "—"))
                  .font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3).lineLimit(2)
                PaperSteps(progress: task.progress, working: store.paperWorkingAgent(task) != nil).frame(maxWidth: 96)
              }
            }.frame(maxWidth: .infinity, alignment: .leading)
            PaperTaskNext(store: store, summary: task).frame(maxWidth: .infinity, alignment: .leading)
            if !condensed {
            Text(task.needsYou ? "You" : store.paperWorkingAgent(task) ?? "—").font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2).lineLimit(1).frame(width: 80, alignment: .leading)
            // The column keeps its width when a task has no steps, so the other columns stay aligned.
            Color.clear.frame(width: 76, height: 3).overlay { PaperSteps(progress: task.progress, working: store.paperWorkingAgent(task) != nil) }
            Text((task.lastUpdateAt ?? task.lastActiveAt).map(store.relativeTime) ?? "—").font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3).frame(width: 54, alignment: .trailing)
            }
          }.padding(.horizontal, 14).padding(.vertical, 12).frame(minHeight: 84).background(current?.id == task.id ? LimmrTheme.surface2 : .clear).contentShape(Rectangle())
        }.quietCard().focused($rowFocus, equals: task.id).quietKeyboardRow(task.id, selection: rowFocus).modifier(PaperTaskContext(store: store, task: task))
          .accessibilityLabel(TaskPresentation.accessibilityLabel(task)).accessibilityHint("Select task. Press Return to open.")
        Rectangle().fill(LimmrTheme.hairline).frame(height: 1)
      }
    }
  }
  private var endedRow: some View {
    Button { scope = "Ended" } label: {
      HStack(spacing: 12) {
        Image(systemName: "checkmark"); Text("Ended").font(LimmrTheme.font(14, .semibold)); Text("\(ended.count)").foregroundStyle(LimmrTheme.ink3)
        Spacer(); Image(systemName: "chevron.right")
      }.font(LimmrTheme.font(12)).padding(.horizontal, 14).frame(height: 48).frame(maxWidth: .infinity).n1Card()
    }.quietCard()
  }
}

struct PaperTaskInspector: View {
  @Bindable var store: AppStore
  let summary: TaskSummary
  @State private var workspace: TaskWorkspace?
  @Environment(\.limmrWindowHeight) private var windowHeight
  private var task: TrackedTask? { workspace?.task }
  var body: some View {
    VStack(alignment: .leading, spacing: 0) {
      PaperTaskArtwork(store: store, task: summary).frame(height: 150)
      VStack(alignment: .leading, spacing: 12) {
        Text(summary.title).font(LimmrTheme.font(17, .semibold)).lineLimit(2)
        Text(store.taskCircleName(summary) + (task?.people.first.map { " · with " + $0.name } ?? "")).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2)
        if summary.needsYou { PaperChip(text: summary.needsYouReason ?? "Needs you", kind: "need") }
        PaperSteps(progress: summary.progress, working: store.paperWorkingAgent(summary) != nil)
        Text("\(summary.progress.done) of \(summary.progress.total) steps done").font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3)
        if let task {
          Grid(alignment: .leading, horizontalSpacing: 10, verticalSpacing: 8) {
            keyValue("Done when", task.goal)
            if let at = task.endAt.flatMap(Timestamp.date) { keyValue("Ends", at.formatted(.dateTime.weekday(.abbreviated).day().month(.abbreviated)) + ", then archives itself") }
            if let follows = task.follows {
              GridRow { Text("Follows").foregroundStyle(LimmrTheme.ink3); Button(task.followsContext?.title ?? store.allTasks.first { $0.id == follows }?.title ?? "Previous task") { store.openTask(follows) }.buttonStyle(.plain) }
            }
            if !task.people.isEmpty { GridRow { Text("People").foregroundStyle(LimmrTheme.ink3); PaperAvatarStack(store: store, ids: task.people.map(\.id)) } }
          }.font(LimmrTheme.font(12))
          if !task.items.isEmpty {
            Divider().overlay(LimmrTheme.hairline)
            ForEach(task.items.prefix(4)) { item in
              Button { store.openTask(task.id) } label: { Label(item.title, systemImage: "doc.text").lineLimit(1) }.buttonStyle(.plain).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2)
            }
          }
          if let quote = task.latest.first { Divider().overlay(LimmrTheme.hairline); Text("“\(quote.text)” — \(quote.sender)").font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2).lineLimit(4) }
        }
        if let error = workspace?.error { Text(error).font(LimmrTheme.font(12)); Button("Try again") { Task { await workspace?.load(force: true) } } }
        Spacer(minLength: 12)
        HStack { Button("Open task") { store.openTask(summary.id) }.buttonStyle(PaperButtonStyle()); Spacer(); if summary.needsYou { Button("Review") { store.openTask(summary.id) }.buttonStyle(PaperButtonStyle(primary: true)) } }
      }.padding(14).frame(minHeight: max(340, windowHeight - 342), alignment: .topLeading)
    }.n1Card().clipShape(RoundedRectangle(cornerRadius: 16))
      .task(id: summary.id) { let value = store.workspace(summary.id); workspace = value; await value.load() }
  }
  private func keyValue(_ title: String, _ value: String) -> some View {
    GridRow { Text(title).foregroundStyle(LimmrTheme.ink3); Text(value).fixedSize(horizontal: false, vertical: true) }
  }
}

struct PaperTaskContext: ViewModifier {
  @Bindable var store: AppStore
  let task: TaskSummary
  @State private var coverPicker = false
  func body(content: Content) -> some View {
    VStack(alignment: .leading, spacing: 4) {
      content
      if let error = store.taskActionErrors[task.id] { Text(error).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2) }
    }.contextMenu {
      N1TaskActions(store: store, task: task)
      Button("Change cover…") { coverPicker = true }
      if task.cover != nil {
        Button("Remove cover") {
          Task {
            var patch = TaskPatch(); patch.coverRemoved = true
            do { _ = try await TaskClient(backend: store.backend).patch(task.id, patch); await store.refreshTaskHome(); await store.refreshAllTasks() }
            catch { store.taskActionErrors[task.id] = "Couldn’t remove the cover." }
          }
        }
      }
      if task.status == "suggested" { Button("Keep") { Task { _ = await store.surfaceTask(task.id, action: .keep); await store.refreshAllTasks() } } }
      if TaskPresentation.canUndo(task, now: store.presentationNow) { Button("Undo tracking") { Task { _ = await store.surfaceTask(task.id, action: .undo); await store.refreshAllTasks() } } }
      if !["done", "dropped", "dismissed"].contains(task.status) {
        Button("Done") { Task { await store.quietTask(task.id, status: "done") } }
        Button("Not now") { Task { await store.quietTask(task.id, status: "dropped") } }
      }
    }.sheet(isPresented: $coverPicker) { TaskCoverPicker(store: store, taskID: task.id) }
  }
}

private struct PaperTaskNext: View {
  @Bindable var store: AppStore
  let summary: TaskSummary
  @State private var workspace: TaskWorkspace?
  var body: some View {
    Text(workspace?.task?.summary.text.isEmpty == false ? workspace!.task!.summary.text : N1TaskStatus.text(summary))
      .font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2).lineLimit(2)
      .task(id: summary.id) { let value = store.workspace(summary.id); workspace = value; await value.load() }
  }
}
