import LimmrKit
import SwiftUI

extension AppStore {
  func currentAttention(_ task: TaskSummary) -> TaskSummary { allTasks.first { $0.id == task.id } ?? home.tasks.first { $0.id == task.id } ?? task }
  func reconcileLaterNotices(_ tasks: [TaskSummary]) {
    for task in tasks where !taskLaterSaving.contains(task.id) {
      if task.laterUntil == nil || task.laterUntil.flatMap(Timestamp.date).map({ $0 <= presentationNow }) == true { taskLaterNotices[task.id] = nil }
    }
  }
  func laterTask(_ id: String, until: Date? = nil) async {
    guard !taskLaterSaving.contains(id) else { return }
    taskLaterSaving.insert(id); defer { taskLaterSaving.remove(id) }
    let generation = backendGeneration
    let predicted = until ?? TaskLaterTime.defaultUntil(now: presentationNow)
    taskLaterNotices[id] = ISO8601DateFormatter().string(from: predicted)
    do {
      let body = until.map { ["until": ISO8601DateFormatter().string(from: $0)] } ?? [:]
      let data = try await backend.request("/v1/tasks/\(id)/later", method: "POST", body: Wire.data(body))
      let value = try Wire.decode(TrackedTask.self, from: data)
      guard generation == backendGeneration else { return }
      taskLaterNotices[id] = value.laterUntil
      applyTaskAttention(value)
      await refreshTaskHome(); await refreshAllTasks()
      taskActionErrors[id] = nil
    } catch { if generation == backendGeneration { taskLaterNotices[id] = nil; taskActionErrors[id] = "Couldn’t leave this for later. Try again." } }
  }
  func bringBackTask(_ id: String) async {
    guard !taskLaterSaving.contains(id) else { return }
    taskLaterSaving.insert(id); defer { taskLaterSaving.remove(id) }
    let generation = backendGeneration
    do {
      let data = try await backend.request("/v1/tasks/\(id)/later", method: "DELETE", body: nil)
      let value = try Wire.decode(TrackedTask.self, from: data)
      guard generation == backendGeneration else { return }
      taskLaterNotices[id] = nil; applyTaskAttention(value)
      await refreshTaskHome(); await refreshAllTasks()
      taskActionErrors[id] = nil
    } catch { if generation == backendGeneration { taskActionErrors[id] = "Couldn’t bring this back. Try again." } }
  }
  private func applyTaskAttention(_ task: TrackedTask) {
    if let i = home.tasks.firstIndex(where: { $0.id == task.id }) { home.tasks[i] = task.compact(now: presentationNow) }
    if let i = allTasks.firstIndex(where: { $0.id == task.id }) { allTasks[i] = task.compact(now: presentationNow) }
    taskWorkspaceCache[task.id]?.task = task
    // Recheck the shared People projection instead of guessing which other tasks need this person.
    Task { @MainActor in
      let generation = backendGeneration
      if let map: CircleMap = try? await backend.get("/v1/map/mesh"), generation == backendGeneration { circleMap = map }
    }
  }
}

struct TaskLaterButton: View {
  @Bindable var store: AppStore
  let id: String
  var body: some View {
    Menu {
      ForEach(["In 3 hours", "Tomorrow morning", "Next Monday"], id: \.self) { option in
        Button(option) { Task { await store.laterTask(id, until: TaskLaterTime.option(option, now: store.presentationNow)) } }
      }
    } label: { Text("Later") } primaryAction: { Task { await store.laterTask(id) } }
      .menuStyle(.borderlessButton).fixedSize().disabled(store.taskLaterSaving.contains(id))
  }
}
struct TaskLaterNotice: View {
  @Bindable var store: AppStore
  let id: String
  let until: String
  var body: some View {
    HStack(spacing: 4) {
      Text(TaskLaterTime.notice(until, now: store.presentationNow) + " ·")
      Button("Undo") { Task { await store.bringBackTask(id) } }.buttonStyle(.plain).disabled(store.taskLaterSaving.contains(id))
    }.font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2).frame(maxWidth: .infinity, alignment: .leading)
  }
}
