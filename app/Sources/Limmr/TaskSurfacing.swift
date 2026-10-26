import Foundation
import LimmrKit

extension AppStore {
  func surfaceTask(_ id: String, action: TaskSurfacingAction) async -> TaskSummary? {
    guard !taskActions.contains(id) else { return nil }
    taskActions.insert(id)
    defer { taskActions.remove(id) }
    let generation = backendGeneration
    do {
      let updated = try await TaskClient(backend: backend).surface(id, action: action)
      guard generation == backendGeneration else { return nil }
      let summary = updated.compact(now: demo ? Timestamp.date(DemoData.now)! : Date())
      if let index = home.tasks.firstIndex(where: { $0.id == id }) {
        if ["dropped", "done"].contains(summary.status) { home.tasks.remove(at: index) }
        else { home.tasks[index] = summary }
      }
      taskActionErrors[id] = nil
      return summary
    } catch {
      if generation == backendGeneration { taskActionErrors[id] = "Couldn’t save this task. Try again." }
      return nil
    }
  }
  func openActivityDay(_ day: String) {
    clearActivityRoute()
    activityDay = day; activityAgent = "all"; activityFilter = "all"
    selectPage(.activity)
  }
}
