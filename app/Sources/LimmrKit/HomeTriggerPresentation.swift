import Foundation

/// Home wording and state use only facts carried by the watch, never guesses from its expiry.
public struct HomeTriggerPresentation: Sendable, Equatable {
  public enum State: Sendable { case armed, due }
  public enum SourceGlyph: Sendable { case person, group, time }
  public let condition: String
  public let action: String
  public let timing: String
  public let state: State
  public let sourceGlyph: SourceGlyph
  public let actor: String
  public let waitProgress: Double
  public let firedRecently: Bool
  public let listeningLabel: String
  public let fireTime: String?

  public static let emptyExample = "Example: When Maya replies about the design · then: notify you"
  public static let emptyHint = "Ask any agent: tell me when ... Limmr watches and acts."

  public static func armedWatches(_ watches: [WatchSummary], now: Date) -> [WatchSummary] {
    watches.filter {
      $0.status == "active" && ($0.expiresAt.flatMap(Timestamp.date).map { $0 > now } ?? true)
    }
  }
  /// Paused watches remain reachable so the owner can resume or stop them.
  public static func visibleWatches(_ watches: [WatchSummary], now: Date) -> [WatchSummary] {
    watches.filter {
      $0.isOpen && ($0.expiresAt.flatMap(Timestamp.date).map { $0 > now } ?? true)
    }
  }
  public init(_ watch: WatchSummary, now: Date, sourceIsPerson: Bool = false) {
    let scheduled = watch.nextFireAt.flatMap(Timestamp.date)
    sourceGlyph = scheduled != nil ? .time : sourceIsPerson ? .person : .group
    actor = watch.action == "notify" || watch.action == "remind" ? "You" : watch.runner?.label ?? watch.createdBy
    if let scheduled, let start = Timestamp.date(watch.createdAt), scheduled > start {
      waitProgress = min(1, max(0, now.timeIntervalSince(start) / scheduled.timeIntervalSince(start)))
    } else { waitProgress = 0 }
    fireTime = scheduled?.formatted(.dateTime.weekday(.abbreviated).hour().minute())
    if let checked = watch.lastCheckedAt.flatMap(Timestamp.date), checked <= now {
      listeningLabel = "Listening, checked \(Int(now.timeIntervalSince(checked) / 60)) min ago"
    } else { listeningLabel = "Listening, last check unavailable" }
    let goal = watch.goal.trimmingCharacters(in: .whitespacesAndNewlines)
    if goal.lowercased().hasPrefix("tell me when ") {
      condition = "When " + goal.dropFirst("Tell me when ".count)
    } else if goal.lowercased().hasPrefix("when ") {
      condition = "When " + goal.dropFirst(5)
    } else {
      // Preserve free-form goals rather than manufacturing a person, topic or matching rule.
      condition = goal.isEmpty ? "When there is an update in \(watch.where)" : goal
    }
    let nextAction =
      switch watch.action {
      case "notify": "notify you"
      case "digest": "prepare a digest"
      case "park_suggestion": "prepare a draft for review"
      case "wake_session":
        watch.runner.map { "ask \($0.label) to help" } ?? "ask \(watch.createdBy) to help"
      case "feed": "share updates with \(watch.createdBy)"
      default: "review the update"
      }
    action = "then: " + nextAction
    let next = watch.nextFireAt.flatMap(Timestamp.date)
    let latest = watch.latest.flatMap { Timestamp.date($0.at) }
    let justFired =
      watch.status == "active" && watch.newHits > 0
      && latest.map { now.timeIntervalSince($0) >= 0 && now.timeIntervalSince($0) <= 900 } == true
    firedRecently = justFired
    let soon =
      next.map { $0.timeIntervalSince(now) >= -900 && $0.timeIntervalSince(now) <= 3600 } == true
    state = watch.status == "active" && (justFired || soon) ? .due : .armed
    if watch.status == "paused" {
      timing = "paused"
    } else if justFired, let latest {
      timing = "fired \(Self.age(now.timeIntervalSince(latest))) ago"
    } else if let next, next > now {
      timing = "fires in \(Self.age(next.timeIntervalSince(now)))"
    } else if next != nil && soon {
      timing = "due now"
    } else {
      let armed = watch.createdAt.isEmpty ? nil : Timestamp.date(watch.createdAt)
      var text = armed.map { "armed \(Self.age(now.timeIntervalSince($0)))" } ?? "armed"
      if let checked = watch.lastCheckedAt.flatMap(Timestamp.date), checked <= now {
        text += " · checked \(Self.age(now.timeIntervalSince(checked))) ago"
      }
      timing = text
    }
  }
  private static func age(_ seconds: TimeInterval) -> String {
    let seconds = max(0, seconds)
    if seconds < 60 { return "<1 min" }
    if seconds < 3600 { return "\(Int(seconds / 60)) min" }
    if seconds < 86400 { return "\(Int(seconds / 3600)) hr" }
    return "\(Int(seconds / 86400)) d"
  }
}

/// Invented U6 examples. Scheduling hints deliberately do not use expires_at as a deadline.
public enum DemoHomeTriggers {
  public static var watches: [WatchSummary] {
    let now = Timestamp.date(DemoData.now)!
    let formatter = ISO8601DateFormatter()
    func time(_ seconds: Double) -> String {
      formatter.string(from: now.addingTimeInterval(seconds))
    }
    return [
      WatchSummary(
        id: "u6-reply", kind: "handoff", title: "Design reply",
        goal: "When Maya replies about the design", where: "Maya Rao", action: "wake_session",
        status: "active", createdBy: "Codex", createdAt: time(-172800), newHits: 0,
        runner: WatchRunner(kind: "codex", label: "Codex"), lastCheckedAt: time(-240)),
      WatchSummary(
        id: "u6-keyword", kind: "watch", title: "Workshop news",
        goal: "Tell me when anyone mentions the workshop in Design Club", where: "Design Club",
        action: "notify", status: "active", createdBy: "Claude Code", createdAt: time(-86400),
        newHits: 1,
        latest: WatchHit(
          at: time(-120), chat: "Design Club", sender: "Robin Bell",
          text: "The workshop starts on Friday."), lastCheckedAt: time(-60)),
      WatchSummary(
        id: "u6-deadline", kind: "watch", title: "Design review",
        goal: "When the design review is due", where: "Design Club", action: "park_suggestion",
        status: "active", createdBy: "Codex", createdAt: time(-86400), newHits: 0,
        nextFireAt: time(172800), lastCheckedAt: time(-240)),
    ]
  }
}
