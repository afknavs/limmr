import LimmrKit
import SwiftUI

extension AppStore {
  func refreshAllTasks() async {
    let generation = backendGeneration
    do {
      let response: TasksResponse = try await backend.get("/v1/tasks?status=all&limit=500")
      guard generation == backendGeneration else { return }
      allTasks = response.tasks; allTasksError = nil
      await taskCovers.warm(response.tasks, backend: backend, generation: generation)
    } catch { if generation == backendGeneration { allTasksError = "Couldn’t load tasks. Try again." } }
  }
}

struct AllTasksView: View {
  @Bindable var store: AppStore
  var body: some View { PaperTasksPage(store: store) }
}
private struct SnapshotTaskLayoutKey: EnvironmentKey { static let defaultValue: String? = nil }
extension EnvironmentValues {
  var snapshotTaskLayout: String? { get { self[SnapshotTaskLayoutKey.self] } set { self[SnapshotTaskLayoutKey.self] = newValue } }
}

// Circle pages keep compact native disclosure rows; Tasks uses sortable tables.
struct TaskListSections: View {
  @Bindable var store: AppStore
  let tasks: [TaskSummary]
  var collapseQuiet = false
  var body: some View {
    VStack(alignment: .leading, spacing: 20) {
      let active = TaskListPresentation.active(tasks)
      rows("Needs you", active.filter(\.needsYou))
      rows("Active", active.filter { !$0.needsYou })
      if !TaskListPresentation.quiet(tasks).isEmpty {
        DisclosureGroup("Quiet (\(TaskListPresentation.quiet(tasks).count))") { rows("", TaskListPresentation.quiet(tasks)) }
      }
      if !TaskListPresentation.history(tasks).isEmpty {
        DisclosureGroup("History (\(TaskListPresentation.history(tasks).count))") { rows("", TaskListPresentation.history(tasks)) }
      }
    }
  }
  @ViewBuilder private func rows(_ title: String, _ values: [TaskSummary]) -> some View {
    if !values.isEmpty {
      VStack(alignment: .leading, spacing: 8) {
        if !title.isEmpty { Text(title).font(.title3.weight(.semibold)) }
        ForEach(values) { task in N1TaskRow(store: store, task: task) }
      }
    }
  }
}

struct N1TaskTableRecord: Identifiable {
  let task: TaskSummary
  let circle: String
  var id: String { task.id }
  var title: String { task.title }
  var progress: Double { task.progress.fraction }
  var ai: String { task.createdBy.name.isEmpty ? "You" : task.createdBy.name }
  var moved: String { task.lastActiveAt ?? "" }
}
struct N1TaskTables: View {
  @Bindable var store: AppStore
  let tasks: [TaskSummary]
  @State private var selection: String?
  @State private var sortOrder = [KeyPathComparator(\N1TaskTableRecord.moved, order: .reverse)]
  var body: some View {
    VStack(alignment: .leading, spacing: 20) {
      let active = TaskListPresentation.active(tasks)
      group("Needs you", active.filter(\.needsYou))
      group("Active", active.filter { !$0.needsYou })
      if !TaskListPresentation.quiet(tasks).isEmpty {
        DisclosureGroup("Quiet (\(TaskListPresentation.quiet(tasks).count))") { table(TaskListPresentation.quiet(tasks)) }
      }
      if !TaskListPresentation.history(tasks).isEmpty {
        DisclosureGroup("History (\(TaskListPresentation.history(tasks).count))") { table(TaskListPresentation.history(tasks)) }
      }
    }
  }
  @ViewBuilder private func group(_ title: String, _ values: [TaskSummary]) -> some View {
    if !values.isEmpty {
      Text(title).font(.title3.weight(.semibold))
      table(values)
    }
  }
  private func table(_ values: [TaskSummary]) -> some View {
    let records = values.map { N1TaskTableRecord(task: $0, circle: store.taskCircleName($0)) }.sorted(using: sortOrder)
    return Table(records, selection: $selection, sortOrder: $sortOrder) {
      TableColumn("", value: \.title) { record in N1TaskThumbnail(store: store, task: record.task) }.width(38)
      TableColumn("Task", value: \.title) { record in
        VStack(alignment: .leading, spacing: 2) {
          Text(record.title).font(.headline).lineLimit(1)
          Text(N1TaskStatus.text(record.task)).font(.caption).foregroundStyle(record.task.needsYou ? Color.orange : .secondary).lineLimit(1)
        }
      }.width(min: 150, ideal: 240)
      TableColumn("Circle", value: \.circle).width(min: 80, ideal: 100)
      TableColumn("Progress", value: \.progress) { record in
        Text("\(record.task.progress.done)/\(record.task.progress.total)").monospacedDigit()
      }.width(64)
      TableColumn("AI", value: \.ai).width(min: 75, ideal: 90)
      TableColumn("Last moved", value: \.moved) { record in
        Text(record.moved.isEmpty ? "—" : store.relativeTime(record.moved))
      }.width(96)
    }
    .tableStyle(.inset(alternatesRowBackgrounds: true))
    .frame(height: CGFloat(min(values.count, 10)) * 48 + 32)
    .contextMenu(forSelectionType: String.self) { ids in
      if let id = ids.first, let task = values.first(where: { $0.id == id }) { N1TaskActions(store: store, task: task) }
    } primaryAction: { ids in if let id = ids.first { store.openTask(id) } }
    .onKeyPress(.return) {
      guard let selection, values.contains(where: { $0.id == selection }) else { return .ignored }
      store.openTask(selection); return .handled
    }
  }
}

/// Suggestions are available for owner review without joining the open task list.
struct SuggestedTasksRow: View {
  @Bindable var store: AppStore
  let tasks: [TaskSummary]
  var body: some View {
    if !tasks.isEmpty {
      DisclosureGroup("Suggested (\(tasks.count))") {
        ForEach(tasks) { task in
          VStack(alignment: .leading, spacing: 4) {
            HStack {
              N1TaskRow(store: store, task: task)
              Button("Keep") { resolve(task.id, .keep) }
              Button("Dismiss") { resolve(task.id, .dismiss) }
            }.disabled(store.taskActions.contains(task.id))
            if let error = store.taskActionErrors[task.id] { Text(error).font(.caption).foregroundStyle(.secondary) }
          }
        }
      }
    }
  }
  private func resolve(_ id: String, _ action: TaskSurfacingAction) {
    Task { _ = await store.surfaceTask(id, action: action); await store.refreshAllTasks() }
  }
}
