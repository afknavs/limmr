import Foundation
import LimmrKit

extension AppStore {
  /// Rows already in memory paint immediately; refresh is independent of purpose work.
  func refreshDetailedActivity(force: Bool = false) async {
    guard force || !snapshot else { return }
    let generation = backendGeneration
    let query = activityQuery
    let suffix = query.isEmpty ? "quiet=true" : query + (query.contains("quiet=true") ? "" : "&quiet=true")
    do {
      let page: ActivityPage = try await backend.get("/v1/activity?limit=50&" + suffix)
      guard generation == backendGeneration, query == activityQuery, !Task.isCancelled else { return }
      let old = activity
      activityQuery = suffix
      let merged = Dictionary((old + page.items).map { ($0.id, $0) }, uniquingKeysWith: { _, new in new })
      activity = merged.values.sorted(by: Activity.newestFirst)
      if old.count <= 50 { activityBefore = page.nextBefore }
    } catch { /* Cached rows stay visible. The next stream hint/appearance retries. */ }
  }
  func handleActivityEvent(_ event: SSEEvent) {
    guard let data = event.data.data(using: .utf8), let row = try? Wire.decode(Activity.self, from: data), row.kind != "refresh" else {
      scheduleDataRefresh(); return
    }
    guard !ActivityPresentation.isEmptyRead(row) else { return }
    if !allRecentActivity.contains(where: { $0.id == row.id }) {
      allRecentActivity = Array(([row] + allRecentActivity).sorted(by: Activity.newestFirst).prefix(50))
    }
    let filters = URLComponents(string: "?" + activityQuery)?.queryItems ?? []
    func value(_ name: String) -> String? { filters.first { $0.name == name }?.value }
    if (value("agent") == nil || row.agentId == value("agent")),
       (value("kind") == nil || row.kind == value("kind")),
       (value("chat") == nil || row.chatJids.contains(value("chat")!)),
       !activity.contains(where: { $0.id == row.id }) {
      activity.append(row); activity.sort(by: Activity.newestFirst)
    }
    // A read changes the log, not Home/tasks/People. Mutations and the engine's
    // revision hints still refresh those screens with the existing 2s coalescing.
    if !ActivityPresentation.isRead(row) { scheduleDataRefresh() }
  }
}
