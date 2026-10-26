import Foundation
import LimmrKit

/// Opt-in, invented cases carried by the existing Home snapshot entry point.
actor H3SnapshotBackend: Backend {
  let base = DemoBackend(structurePlacements: true)
  let scenario: String
  init(scenario: String) { self.scenario = scenario }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    let route = URLComponents(string: path)?.path ?? path
    if route == "/v1/home" {
      var home: HomeResponse = try await base.get(path)
      let ids = scenario == "odd" ? ["university", "atlas", "friends"] : ["university", "atlas", "friends", "family"]
      home.circles = ids.enumerated().compactMap { index, id -> CircleToday? in
        guard let detail = DemoCircles.details.first(where: { $0.circle.id == id }) else { return nil }
        var circle = detail.circle
        circle.pinned = scenario == "demo" && index < 2
        var brief = detail.brief
        brief?.openAsksForYou = index == 0 ? ["Can you confirm the room for Thursday?"] : []
        brief?.upcoming = index == 2 ? [.init(date: "2026-10-22", text: "Art workshop session")] : []
        guard var item = home.today.first(where: { $0.id == id }) else { return nil }
        item.circle = circle; item.brief = brief; item.newMessages = [12, 38, 7, 6][index]
        item.openAsks = index == 0 ? 1 : 0; item.upcoming = index == 2 ? 1 : 0
        item.pinned = circle.pinned
        item.whyShown = index == 0 ? "open_ask" : index == 2 ? "upcoming" : "busy"
        return item
      }
      home.queued = []; home.proposals = []
      home.waiting = scenario == "demo" ? try DemoHomeTriggers.watches.map {
        try Wire.decode(WaitingItem.self, from: JSONSerialization.data(withJSONObject: [
          "id": $0.id, "agent": $0.createdBy, "kind": $0.kind, "sentence": $0.goal,
          "where": $0.where, "since": $0.createdAt, "new_hits": $0.newHits]))
      } : []
      home.watches = scenario == "demo" ? DemoHomeTriggers.watches : []
      if scenario == "history" || scenario == "server" || scenario == "odd" || scenario == "empty" { home.needsReply = [] }
      else if let ask = home.needsReply.first {
        home.needsReply = (0..<4).map { index in
          var item = ask; item.id = "h3-ask-\(index)"
          item.from = ["Maya Rao", "Robin Bell", "Noor Park", "Alex Lane"][index]
          item.chat = ["Design Club", "Workshop team", "Garden group", "Study room"][index]
          item.text = ["Can you confirm the room for Thursday?", "Which version should we print?", "Shall we meet by the gate?", "Can you check the slides?"][index]
          return item
        }
      }
      if scenario == "empty" { home.circles = []; home.today = [] }
      home.circleUpdates = scenario == "server" || scenario == "empty" ? [] : DemoEfforts.updates.filter { ids.contains($0.circle.id) }
      return try Wire.data(home)
    }
    if route == "/v1/circles" {
      var response: CirclesResponse = try await base.get(path)
      // Keep pin state consistent with the server Home cards.
      response.circles = response.circles.map { value in
        var circle = value
        circle.pinned = scenario == "demo" && ["university", "atlas"].contains(circle.id)
        return circle
      }
      return try Wire.data(response)
    }
    if route == "/v1/efforts" {
      let efforts = scenario == "server" || scenario == "empty" ? [] : DemoEfforts.efforts.filter {
        scenario != "odd" || $0.circleIds.contains(where: ["university", "atlas", "friends"].contains)
      }
      return try Wire.data(["efforts": efforts])
    }
    if route == "/v1/stats/history" || route == "/v1/history" {
      let history: StatsHistory = try await base.get(path)
      guard let end = history.days.last, let date = Timestamp.date(end.date + "T00:00:00Z"), !history.days.isEmpty else { return try Wire.data(history) }
      let formatter = ISO8601DateFormatter()
      return try Wire.data(StatsHistory(days: (0..<364).map { index in
        var day = history.days[index % history.days.count]
        day.date = String(formatter.string(from: date.addingTimeInterval(Double(index - 363) * 86400)).prefix(10))
        return day
      }))
    }
    return try await base.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}
