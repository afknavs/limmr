import Foundation

public enum HomeCircleRectangle: Identifiable, Sendable {
  case effort(CircleEffortUpdate)
  case ask(id: String, text: String, from: String?)
  case event(CircleUpcoming)
  public var id: String {
    switch self {
    case .effort(let value): "effort-" + value.effortId
    case .ask(let id, _, _): "ask-" + id
    case .event(let value): "event-" + value.date + value.text
    }
  }
}
public struct HomeCircleTile: Identifiable, Sendable {
  public let circle: Circle
  public let pinned: Bool
  public let reason: String?
  public let brief: String?
  public let newMessages: Int
  public let rectangles: [HomeCircleRectangle]
  public var updates: [CircleEffortUpdate] {
    rectangles.compactMap { if case .effort(let value) = $0 { value } else { nil } }
  }
  public var id: String { circle.id }
}

/// Combine the server's surfacing evidence with pinned and effort-driven work.
public enum HomeTiles {
  public static func dueSoon(_ due: String?, now: Date) -> Bool {
    guard let date = due.flatMap(Timestamp.date) else { return false }
    return date >= now && date <= now.addingTimeInterval(7 * 86400)
  }
  public static func eventDate(_ value: String) -> Date? {
    Timestamp.date(value) ?? Timestamp.date(value + "T12:00:00Z")
  }
  public static func tiles(circles: [Circle], home: HomeResponse, efforts: [Effort], now: Date)
    -> [HomeCircleTile]
  {
    let all = Dictionary((home.circles.map(\.circle) + circles).map { ($0.id, $0) },
                         uniquingKeysWith: { _, current in current })
    let hidden = Set(efforts.filter(\.hidden).map(\.id))
    return Array(all.values.compactMap { circle -> HomeCircleTile? in
      guard circle.confirmed else { return nil }
      let today = home.circles.first { $0.id == circle.id }
      let pinned = circle.pinned || today?.pinned == true
      guard pinned || circle.kind != "self" else { return nil }
      let work = efforts.filter { EffortPresentation.visible($0) && $0.circleIds.contains(circle.id) }
      let changed = home.circleUpdates.filter { $0.circle.id == circle.id && !hidden.contains($0.effortId) && EffortPresentation.hasTitle($0.title) }
      var updates = Dictionary(changed.map { ($0.effortId, $0) }, uniquingKeysWith: { a, _ in a })
      for effort in work {
        updates[effort.id] = CircleEffortUpdate(circle: .init(id: circle.id, name: circle.name),
          effortId: effort.id, title: effort.title, status: effort.status, line: effort.summary,
          due: effort.due, lastAt: effort.lastAt)
      }
      let sorted = updates.values.sorted { precedes($0, $1, now: now) }
      let brief = today?.brief ?? home.today.first { $0.id == circle.id }?.brief
      let events = (brief?.upcoming ?? []).filter {
        // Date-only events remain visible through their calendar day.
        (Timestamp.date($0.date) ?? Timestamp.date($0.date + "T23:59:59Z") ?? .distantPast) >= now
      }.sorted { $0.date == $1.date ? $0.text < $1.text : $0.date < $1.date }
      let reason: String?
      if sorted.contains(where: { $0.status == "on_you" }) || today?.whyShown == "open_ask" {
        reason = "on you"
      } else if sorted.contains(where: { $0.status != "done" && $0.status != "dormant" && dueSoon($0.due, now: now) }) {
        reason = "due within 7 days"
      } else if today?.whyShown == "upcoming" {
        reason = events.first.flatMap { eventDate($0.date) }.map { "Event " + $0.formatted(.dateTime.weekday(.abbreviated)) } ?? "Upcoming event"
      } else if !changed.isEmpty { reason = "changed since last look" }
      else if today != nil { reason = "Busy" }
      else { reason = nil }
      guard pinned || reason != nil else { return nil }
      var rectangles = sorted.filter { $0.status == "on_you" }.map(HomeCircleRectangle.effort)
      let asks = home.needsReply.filter { $0.circleId == circle.id }
      rectangles += asks.map { .ask(id: $0.id, text: $0.text, from: $0.from?.split(separator: " ").first.map(String.init)) }
      // A brief can carry asks that are not in the actionable reply queue.
      rectangles += (brief?.openAsksForYou ?? []).enumerated().filter { _, text in !asks.contains { $0.text == text } }
        .map { .ask(id: "brief-\($0.offset)", text: $0.element, from: nil) }
      rectangles += events.map(HomeCircleRectangle.event)
      let changedIDs = Set(changed.map(\.effortId))
      rectangles += sorted.filter {
        $0.status != "on_you" && (changedIDs.contains($0.effortId)
          || ($0.status != "done" && $0.status != "dormant" && dueSoon($0.due, now: now)))
      }.map(HomeCircleRectangle.effort)
      return HomeCircleTile(circle: circle, pinned: pinned, reason: reason,
        brief: brief?.summary.isEmpty == false ? brief?.summary : nil,
        newMessages: max(0, today?.newMessages ?? 0), rectangles: Array(rectangles.prefix(3)))
    }.sorted { a, b in
      if a.pinned != b.pinned { return a.pinned }
      func rank(_ tile: HomeCircleTile) -> Int {
        if tile.reason == "on you" { return 0 }
        if tile.reason == "due within 7 days" || tile.reason?.hasPrefix("Event ") == true || tile.reason == "Upcoming event" { return 1 }
        return tile.reason == "changed since last look" ? 2 : 3
      }
      if rank(a) != rank(b) { return rank(a) < rank(b) }
      if rank(a) == 3, a.newMessages != b.newMessages { return a.newMessages > b.newMessages }
      let at = a.updates.first.flatMap { Timestamp.date($0.lastAt) } ?? .distantPast
      let bt = b.updates.first.flatMap { Timestamp.date($0.lastAt) } ?? .distantPast
      return at == bt ? a.id < b.id : at > bt
    }.prefix(6))
  }
  private static func precedes(_ a: CircleEffortUpdate, _ b: CircleEffortUpdate, now: Date) -> Bool {
    if (a.status == "on_you") != (b.status == "on_you") { return a.status == "on_you" }
    let ad = a.status != "done" && a.status != "dormant" && dueSoon(a.due, now: now)
    let bd = b.status != "done" && b.status != "dormant" && dueSoon(b.due, now: now)
    if ad != bd { return ad }
    let adate = a.due.flatMap(Timestamp.date) ?? .distantFuture
    let bdate = b.due.flatMap(Timestamp.date) ?? .distantFuture
    if ad, adate != bdate { return adate < bdate }
    let at = Timestamp.date(a.lastAt) ?? .distantPast
    let bt = Timestamp.date(b.lastAt) ?? .distantPast
    return at == bt ? a.effortId < b.effortId : at > bt
  }
  private static func priorityBand(_ tile: HomeCircleTile) -> Int {
    if tile.pinned { return 0 }
    if tile.reason == "on you" { return 1 }
    if tile.reason == "due within 7 days" || tile.reason?.hasPrefix("Event ") == true || tile.reason == "Upcoming event" { return 2 }
    return tile.reason == "changed since last look" ? 3 : 4
  }
  /// Content footprint breaks ties within priority bands. Actual measurements in the
  /// row layout cap stretching, including text wrapping and taller calendar/ask rows.
  private static func contentHeight(_ tile: HomeCircleTile) -> Double {
    65 + (tile.brief == nil ? 0 : 38) + tile.rectangles.reduce(0) { height, rectangle in
      let rectangleHeight: Double = switch rectangle {
      case .effort: 32.0
      case .ask(_, _, let from): from == nil ? 32.0 : 47.0
      case .event: 50.0
      }
      return height + 12 + rectangleHeight
    }
  }
  public static func pairedTiles(_ tiles: [HomeCircleTile]) -> [HomeCircleTile] {
    var remaining = tiles
    var result: [HomeCircleTile] = []
    while !remaining.isEmpty {
      // Never displace the highest-priority remaining tile as a row leader.
      let leader = remaining.removeFirst()
      result.append(leader)
      guard !remaining.isEmpty else { break }
      func score(_ tile: HomeCircleTile) -> (Int, Double) {
        let difference = abs(contentHeight(leader) - contentHeight(tile))
        let sameBand = priorityBand(leader) == priorityBand(tile)
        let sameKind = leader.rectangles.isEmpty == tile.rectangles.isEmpty
        let rank = difference <= 24 ? (sameBand ? 0 : 1) : sameKind ? (sameBand ? 2 : 3) : 4
        return (rank, difference)
      }
      let partner = remaining.indices.min {
        let a = score(remaining[$0]), b = score(remaining[$1])
        return a == b ? $0 < $1 : a < b
      }!
      result.append(remaining.remove(at: partner))
    }
    return result
  }
  /// Include the standard 16 pt padding: no more than 40 pt below content.
  public static func rowTileHeight(natural: Double, row: Double) -> Double { min(row, natural + 24) }
  public static func needsOtherTile(count: Int) -> Bool { count == 0 || !count.isMultiple(of: 2) }
  public static func otherCircles(circles: [Circle], tiles: [HomeCircleTile]) -> [Circle] {
    let shown = Set(tiles.map(\.id))
    return circles.filter { $0.confirmed && !shown.contains($0.id) }.sorted { $0.name == $1.name ? $0.id < $1.id : $0.name < $1.name }
  }
  public static func rows(count: Int) -> [[Int]] {
    guard count > 0 else { return [] }
    return stride(from: 0, to: count, by: 2).map { Array($0..<min($0 + 2, count)) }
  }
  /// Acknowledge only circles whose entire unread effort slice was actually displayed.
  public static func acknowledgedUpdates(tiles: [HomeCircleTile], unread: [CircleEffortUpdate]) -> [CircleEffortUpdate] {
    tiles.flatMap { tile -> [CircleEffortUpdate] in
      let changed = unread.filter { $0.circle.id == tile.id }
      let shown = Set(tile.updates.map(\.effortId))
      return changed.allSatisfy { shown.contains($0.effortId) } ? changed : []
    }
  }
  public static func stateShape(_ status: String) -> String {
    switch status {
    case "on_you": "filled"
    case "moving", "waiting_on_them": "ring"
    case "done": "check"
    default: "square"
    }
  }
  public static func shortState(_ update: CircleEffortUpdate) -> String {
    let state = update.status == "waiting_on_them" ? "waiting" : update.status.replacingOccurrences(of: "_", with: " ")
    return state + (update.due.flatMap(Timestamp.date).map { " · " + $0.formatted(.dateTime.weekday(.abbreviated)) } ?? "")
  }
}
