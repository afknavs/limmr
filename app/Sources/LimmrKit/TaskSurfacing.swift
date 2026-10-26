import Foundation

public enum TaskSurfacingAction: String, Sendable { case keep, dismiss, undo }

public extension TaskClient {
  func surface(_ id: String, action: TaskSurfacingAction) async throws -> TrackedTask {
    try Wire.decode(TrackedTask.self, from: await backend.request(Self.path(id) + "/" + action.rawValue,
      method: "POST", body: Wire.data(Empty())))
  }
}
public extension TaskPresentation {
  static func suggested(_ tasks: [TaskSummary]) -> [TaskSummary] {
    tasks.filter { $0.status == "suggested" }
  }
  static func canUndo(_ task: TaskSummary, now: Date) -> Bool {
    guard ["active", "waiting"].contains(task.status), task.undoChangeId?.isEmpty == false,
      let at = task.autoKeptAt.flatMap(Timestamp.date) else { return false }
    return now >= at && now.timeIntervalSince(at) < 86400
  }
}

public enum ActivityDayFilter {
  /// Activity timestamps carry their offset. Match the calendar day the owner clicked locally.
  public static func matches(_ at: String, day: String?, timeZone: TimeZone = .current) -> Bool {
    guard let day else { return true }
    guard let date = Timestamp.date(at) else { return false }
    let formatter = DateFormatter()
    formatter.locale = Locale(identifier: "en_US_POSIX"); formatter.timeZone = timeZone
    formatter.dateFormat = "yyyy-MM-dd"
    return formatter.string(from: date) == day
  }
}
