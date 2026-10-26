import Foundation

/// A row keeps edits per variation. The wire carries the exact message body.
public struct QueuedDraftState: Sendable {
  public private(set) var variation = 0
  public var edits: [Int: String] = [:]
  public let variations: [String]
  public init(_ item: CircleSuggestion, circle: Circle?, fallback: String) {
    variations =
      item.variations.isEmpty ? [item.body ?? fallback] : Array(item.variations.prefix(3))

  }
  public var text: String { edits[variation] ?? variations[variation] }
  public mutating func selectVariation(_ delta: Int) {
    variation = (variation + delta + variations.count) % variations.count
  }
  public var request: SuggestionSendRequest {
    .init(text: edits[variation], variation: variation)
  }
}
public struct SuggestionSendRequest: Codable, Sendable, Equatable {
  public var text: String?
  public var variation: Int
  public init(text: String? = nil, variation: Int) {
    self.text = text
    self.variation = variation
  }
}
public enum HomePresentation {
  public static func usesRecentColumn(contentWidth: Double) -> Bool { contentWidth >= 980 }
  public static func usesRecentColumn(windowWidth: Double, contentWidth: Double) -> Bool {
    windowWidth >= 1150 && contentWidth >= 900
  }
  public static func stacksDashboard(mainWidth: Double) -> Bool { mainWidth < 420 }
  public static func dashboardWidth(mainWidth: Double) -> Double {
    stacksDashboard(mainWidth: mainWidth)
      ? mainWidth : min(dashboardContentWidth + 32, (mainWidth - 16) * 0.48)
  }
  /// Reserve a narrow stack for three totals before choosing calendar weeks.
  /// Below this width the dashboard retains its vertical arrangement.
  public static func fullDashboardGeometry(contentWidth: Double) -> (horizontal: Bool, heatmap: Double, counts: Double) {
    guard contentWidth >= 620 else { return (false, contentWidth, contentWidth) }
    let natural = 36 + Double(maximumHeatmapWeeks) * (heatmapSide + heatmapGap) - heatmapGap
    let heatmap = min(natural, contentWidth - 24 - 120)
    return (true, heatmap, contentWidth - heatmap - 24)
  }
  public static func searchWidth(mainWidth: Double) -> Double { mainWidth }
  public enum ActivitySlot: Sendable { case triggers, replies, history }
  public static func activitySlot(_ home: HomeResponse) -> ActivitySlot {
    !home.waiting.isEmpty ? .triggers : !home.needsReply.isEmpty ? .replies : .history
  }
  public static func mainSections(_ home: HomeResponse) -> [String] {
    let present = Set(sections(home))
    return ["greeting", "search", "dashboard", "tasks", "triggers", "circles", "queued", "needs_reply",
      "proposals", "integrations", "changes"].filter {
      switch $0 {
      case "greeting", "search", "dashboard", "circles": true
      case "tasks": !TaskPresentation.open(home.tasks).isEmpty || !TaskPresentation.suggested(home.tasks).isEmpty
      case "triggers": activitySlot(home) == .triggers
      case "needs_reply": activitySlot(home) == .triggers && !home.needsReply.isEmpty
      case "changes": hasChanges(home)
      default: present.contains($0)
      }
    }
  }
  public static func sections(_ home: HomeResponse) -> [String] {
    CirclesPresentation.homeSections.filter { section in
      switch section {
      case "waiting": !home.waiting.isEmpty
      case "watches": home.waiting.isEmpty && !home.watches.isEmpty
      case "queued": !home.queued.isEmpty
      case "circles": !Self.circles(home.circles).isEmpty
      case "needs_reply": !home.needsReply.isEmpty
      case "recent_activity": !home.recentActivity.isEmpty
      case "proposals": !home.proposals.isEmpty
      default: true
      }
    }
  }
  public static func hasChanges(_ home: HomeResponse) -> Bool {
    !home.circleChanges.isEmpty || home.circleChangesMore > 0 || !home.circleAdditions.isEmpty
      || home.suggestions.contains { $0.kind == "archive" }
  }
  public static func greeting(at date: Date = Date(), calendar: Calendar = .current) -> String {
    switch calendar.component(.hour, from: date) {
    case 5..<12: "Good morning"
    case 12..<17: "Good afternoon"
    case 17..<22: "Good evening"
    default: "Today"
    }
  }
  public static func dashboardFooter(
    lastSyncAt: String?, now: Date = Date(), timeZone: TimeZone = .current
  ) -> String {
    let zone = timeZone.abbreviation(for: now) ?? timeZone.identifier
    guard let at = lastSyncAt.flatMap(Timestamp.date) else { return zone + " · Waiting for sync" }
    let elapsed = max(0, now.timeIntervalSince(at))
    let sync: String
    if elapsed < 60 {
      sync = "Last sync just now"
    } else if elapsed < 3600 {
      sync = "Last sync \(Int(elapsed / 60)) min ago"
    } else if elapsed < 86400 {
      sync = "Last sync \(Int(elapsed / 3600)) hr ago"
    } else {
      sync = "Last sync \(Int(elapsed / 86400)) d ago"
    }
    return zone + " · " + sync
  }
  public static let heatmapWeeks = 26
  public static let maximumHeatmapWeeks = 52
  public static let heatmapSide = 14.0
  public static let heatmapGap = 2.0
  /// Intrinsic strip width, including weekday labels; the card adds 16 pt padding per side.
  public static let dashboardContentWidth =
    36 + Double(heatmapWeeks) * (heatmapSide + heatmapGap) - heatmapGap
  // Weekday labels occupy 36 points. Cells grow from 14 to 16 points.
  public static func heatmapCellSide(contentWidth: Double, columns: Int) -> Double {
    guard columns > 0 else { return 0 }
    return heatmapGeometry(contentWidth: contentWidth, columns: columns).side
  }
  /// Drop the oldest weeks to keep calendar cells readable at every width.
  public static func heatmapGeometry(contentWidth: Double, columns: Int) -> (
    side: Double, visible: Int
  ) {
    guard columns > 0, contentWidth.isFinite, contentWidth >= 50 else { return (0, 0) }
    let visible = max(0, min(columns, maximumHeatmapWeeks, Int(min(Double(maximumHeatmapWeeks), (contentWidth - 34) / 16))))
    guard visible > 0 else { return (0, 0) }
    return (min(16, (contentWidth - 36 - Double(visible - 1) * heatmapGap) / Double(visible)), visible)
  }
  public struct MonthLabel: Equatable, Sendable {
    public var title: String
    public var column: Int
    public var x: Double
  }
  public static func monthLabels(cells: [HistoryDay?], side: Double) -> [MonthLabel] {
    var previous = ""
    var result: [MonthLabel] = []
    for column in 0..<(cells.count / 7) {
      guard let day = cells[(column * 7)..<((column + 1) * 7)].compactMap({ $0 }).last,
        let date = Timestamp.date(day.date + "T00:00:00Z") else { continue }
      let key = String(day.date.prefix(7))
      if key != previous {
        let formatter = DateFormatter(); formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = TimeZone(secondsFromGMT: 0); formatter.dateFormat = "MMM"
        // Avoid overlapping the previous month's label or clipping the right edge.
        if let last = result.last, column - last.column < 3 { result.removeLast() }
        if column <= cells.count / 7 - 2 {
          result.append(.init(title: formatter.string(from: date), column: column, x: 36 + Double(column) * (side + heatmapGap)))
        }
        previous = key
      }
    }
    return result
  }
  public static func dayLabel(_ day: HistoryDay, agents: [Agent]) -> String {
    let formatter = DateFormatter(); formatter.locale = Locale(identifier: "en_US_POSIX")
    formatter.timeZone = TimeZone(secondsFromGMT: 0); formatter.dateFormat = "EEE d MMM"
    let date = Timestamp.date(day.date + "T00:00:00Z").map { formatter.string(from: $0) } ?? day.date
    let counts = agents.compactMap { agent -> String? in
      guard let count = day.agents?[agent.id], count.reads + count.sends > 0 else { return nil }
      return "\(agent.name): \(count.reads) reads, \(count.sends) sends"
    }
    return (["\(date) · \(day.messages) messages · \(day.agentSends) AI sends", "\(day.agentReads) AI reads"] + counts).joined(separator: " · ")
  }
  public static func heatmapPeriod(weeks: Int) -> String {
    if weeks >= 52 { return "Past 12 months" }
    if weeks == 26 { return "Past 6 months" }
    return "Past \(weeks) weeks"
  }
  /// Calendar weeks ending in the latest recorded week. Missing days stay empty.
  public static func stripCells(_ history: StatsHistory, weeks: Int = heatmapWeeks) -> [HistoryDay?] {
    let weeks = max(1, min(maximumHeatmapWeeks, weeks))
    guard let last = history.days.map(\.date).max(),
      let end = Timestamp.date(last + "T00:00:00Z")
    else { return [] }
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = TimeZone(secondsFromGMT: 0)!
    let weekday = (calendar.component(.weekday, from: end) + 5) % 7
    let start = calendar.date(byAdding: .day, value: -((weeks - 1) * 7 + weekday), to: end)!
    let byDate = Dictionary(history.days.map { ($0.date, $0) }, uniquingKeysWith: { _, b in b })
    let formatter = ISO8601DateFormatter()
    return (0..<(weeks * 7)).map { offset in
      let date = calendar.date(byAdding: .day, value: offset, to: start)!
      return byDate[String(formatter.string(from: date).prefix(10))]
    }
  }
  public static func integrationCounts(agents: [Agent], days: [HistoryDay]) -> String {
    agents.map { agent in
      let count = days.reduce(0) {
        $0 + ($1.agents?[agent.id]?.reads ?? 0) + ($1.agents?[agent.id]?.sends ?? 0)
      }
      return "\(agent.name) \(count.formatted())"
    }.joined(separator: " · ")
  }
  public static func changesLine(
    _ home: HomeResponse, now: Date = Date(), calendar: Calendar = .current
  ) -> String {
    let placements = home.circleChanges.filter {
      ($0.byKind == "structure" || ($0.agent == "Limmr" && $0.action == "placed"))
        && Timestamp.date($0.at).map { calendar.isDate($0, inSameDayAs: now) } == true
    }
    let count = placements.reduce(0) { $0 + max(0, $1.count) }
    if count > 0 {
      // Home only carries a recent slice; never imply this is a complete daily total when truncated.
      let prefix = home.circleChangesMore > 0 ? "at least " : ""
      return
        "Limmr placed \(prefix)\(count.formatted()) \(count == 1 ? "person" : "people") into circles today"
    }
    return "Your circles have changes to review"
  }
  public static func circles(_ items: [CircleToday]) -> [CircleToday] {
    items.filter {
      let pinned = $0.pinned || $0.circle.pinned
      return pinned || ($0.circle.kind != "self" && ($0.openAsks > 0 || $0.newMessages > 0))
    }
    .sorted { a, b in
      let ap = a.pinned || a.circle.pinned
      let bp = b.pinned || b.circle.pinned
      if ap != bp { return ap }
      if (a.openAsks > 0) != (b.openAsks > 0) { return a.openAsks > 0 }
      let at = a.brief?.writtenAt ?? a.circle.updatedAt
      let bt = b.brief?.writtenAt ?? b.circle.updatedAt
      return at == bt ? a.id < b.id : at > bt
    }
  }
  /// One sentence at the top of Home: what needs you, most urgent first.
  public static func focus(_ home: HomeResponse) -> String {
    var parts: [String] = []
    let drafts = home.queued.count
    if drafts > 0 {
      parts.append(drafts == 1 ? "1 draft waits for you" : "\(drafts) drafts wait for you")
    }
    let asks = home.needsReply.count
    if asks > 0 {
      parts.append(asks == 1 ? "1 message needs a reply" : "\(asks) messages need a reply")
    }
    let hits = home.watches.filter(\.isOpen).reduce(0) { $0 + $1.newHits }
    if hits > 0 {
      parts.append(
        hits == 1 ? "a watch caught something new" : "your watches caught \(hits) new things")
    }
    guard let last = parts.popLast() else { return "Nothing needs you right now." }
    let sentence =
      parts.isEmpty
      ? last : parts.joined(separator: ", ") + (parts.count > 1 ? ", and " : " and ") + last
    return sentence.prefix(1).uppercased() + sentence.dropFirst() + "."
  }
  public static func shouldDismiss(translation: Double, width: Double) -> Bool {
    translation < -max(180, width * 0.55)
  }
  public static func whatsappURL(jid: String?) -> URL? {
    guard let jid, jid.hasSuffix("@s.whatsapp.net"),
      let number = jid.split(separator: "@").first, number.allSatisfy(\.isNumber)
    else { return nil }
    return URL(string: "whatsapp://send?phone=\(number)")
  }
}
public enum PeoplePresentation {
  public static func circles(_ circles: [Circle], query: String) -> [Circle] {
    circles.filter { query.isEmpty || $0.name.localizedCaseInsensitiveContains(query) }
  }
  /// "Classmate · University, Flatmate · Flatmates". A role that only repeats the person's name
  /// ("Mum" in Family) shows as the circle alone.
  public static func roles(_ id: String, circles: [Circle], name: String = "") -> String {
    circles.compactMap { circle in
      circle.people.first { $0.personId == id }.map { member in
        member.role.isEmpty || member.role.caseInsensitiveCompare(name) == .orderedSame
          ? circle.name : "\(member.role) · \(circle.name)"
      }
    }.joined(separator: ", ")
  }
  /// The owner is a person too; keep the self record visible and first.
  public static func people(_ people: [Person], circles: [Circle], query: String) -> [Person] {
    return people.filter {
      query.isEmpty || $0.name.localizedCaseInsensitiveContains(query)
        || roles($0.id, circles: circles).localizedCaseInsensitiveContains(query)
    }
  }
}
