import Foundation
import LimmrKit

/// Synthetic Home variations live in the backend so refreshes preserve the captured state.
actor H2SnapshotBackend: Backend {
  let base = DemoBackend(structurePlacements: true)
  let h3: H3SnapshotBackend?
  let odd: Bool
  let emptyTriggers: Bool
  init(odd: Bool = false, emptyTriggers: Bool = false) {
    self.odd = odd; self.emptyTriggers = emptyTriggers
    let arguments = ProcessInfo.processInfo.arguments
    if let index = arguments.firstIndex(of: "--h3-scenario"), arguments.indices.contains(index + 1) {
      h3 = H3SnapshotBackend(scenario: arguments[index + 1])
    } else { h3 = nil }
  }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if let h3 { return try await h3.request(path, method: method, body: body) }
    let route = URLComponents(string: path)?.path ?? path
    if route == "/v1/home" {
      var home: HomeResponse = try await base.get(path)
      home.watches = emptyTriggers ? [] : DemoHomeTriggers.watches
      home.waiting = []; home.queued = []; home.needsReply = []
      home.circleUpdates = DemoEfforts.updates.filter { $0.circle.id == "friends" }
      home.circles = home.circles.filter { ["university", "atlas", "friends", "family"].contains($0.id) }
      return try Wire.data(home)
    }
    if route == "/v1/efforts" {
      var efforts = Array(DemoEfforts.efforts.filter { $0.status != "dormant" })
      if !odd {
        var family = DemoEfforts.efforts[3]
        family.status = "waiting_on_them"; family.title = "Seedling delivery"
        family.summary = "Riley is confirming the delivery window."
        family.due = "2026-10-21T12:00:00Z"
        efforts.append(family)
      }
      let query = URLComponents(string: path)?.queryItems?.first { $0.name == "q" }?.value ?? ""
      return try Wire.data(["efforts": efforts.filter {
        query.isEmpty || ($0.title + " " + $0.summary + " " + $0.meta).localizedCaseInsensitiveContains(query)
      }])
    }
    if route == "/v1/history" || route == "/v1/stats/history" {
      // Use the shared half-year fixture generation below for the quiet strip.
      let history: StatsHistory = try await base.get(path)
      guard let end = history.days.last, let date = Timestamp.date(end.date + "T00:00:00Z"), !history.days.isEmpty else {
        return try Wire.data(history)
      }
      let formatter = ISO8601DateFormatter()
      let days = (0..<182).map { index in
        var day = history.days[index % history.days.count]
        day.date = String(formatter.string(from: date.addingTimeInterval(Double(index - 181) * 86400)).prefix(10))
        return day
      }
      return try Wire.data(StatsHistory(days: days))
    }
    if let watch = DemoHomeTriggers.watches.first(where: { route == "/v1/watches/" + $0.id }) {
      var detail = DemoWaiting.details[0]
      detail.summary = watch
      return try Wire.data(detail)
    }
    return try await base.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}
