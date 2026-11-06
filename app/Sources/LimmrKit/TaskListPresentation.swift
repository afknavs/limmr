import Foundation

public enum TaskListPresentation {
  public static func search(_ tasks: [TaskSummary], title: String) -> [TaskSummary] {
    let query = title.trimmingCharacters(in: .whitespacesAndNewlines)
    return query.isEmpty ? tasks : tasks.filter { $0.title.localizedStandardContains(query) }
  }
  public static func active(_ tasks: [TaskSummary]) -> [TaskSummary] {
    TaskPresentation.open(tasks) + TaskPresentation.suggested(tasks)
  }
  public static func quiet(_ tasks: [TaskSummary]) -> [TaskSummary] {
    tasks.filter { $0.status == "quiet" }.sorted { ($0.lastActiveAt.flatMap(Timestamp.date) ?? .distantPast) > ($1.lastActiveAt.flatMap(Timestamp.date) ?? .distantPast) }
  }
  public static func history(_ tasks: [TaskSummary]) -> [TaskSummary] {
    tasks.filter { ["done", "dropped", "dismissed"].contains($0.status) }.sorted { (($0.closedAt ?? $0.lastActiveAt).flatMap(Timestamp.date) ?? .distantPast) > (($1.closedAt ?? $1.lastActiveAt).flatMap(Timestamp.date) ?? .distantPast) }
  }
  public static func closure(_ task: TaskSummary) -> String {
    let date = task.closedAt.flatMap(Timestamp.date)?.formatted(date: .abbreviated, time: .omitted) ?? "Closed"
    let reason: String
    switch task.closeReason {
    case "agent_done": reason = "Completed by AI"
    case "lifespan": reason = "Closed after inactivity"
    case "not_a_task", "suggestion_expired": reason = "Suggestion dismissed"
    case "owner": reason = "Set aside by you"
    default: reason = "Completed"
    }
    return date + " · " + reason
  }
  public static func inCircle(_ tasks: [TaskSummary], id: String) -> [TaskSummary] { tasks.filter { $0.circleIds.contains(id) } }
  public static func noCircle(_ tasks: [TaskSummary]) -> [TaskSummary] { tasks.filter { $0.circleIds.isEmpty } }
}
