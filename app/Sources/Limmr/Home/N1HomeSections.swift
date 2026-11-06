import LimmrKit
import SwiftUI

struct N1CirclesGrid: View {
  @Bindable var store: AppStore
  var body: some View {
    if !store.circles.isEmpty {
      VStack(alignment: .leading, spacing: 12) {
        HStack {
          Text("Circles").font(LimmrTheme.TypeScale.section)
          Spacer()
          Button("People map", systemImage: "person.2") { store.selectPage(.circles) }.quietCard().foregroundStyle(LimmrTheme.accent)
        }
        LazyVGrid(columns: [GridItem(.adaptive(minimum: 190), spacing: 12)], spacing: 12) {
          ForEach(store.circles) { circle in
            Button { store.openCircle(circle.id) } label: {
              VStack(alignment: .leading, spacing: 12) {
                HStack { HomeCirclePicture(store: store, circle: circle, size: 34); Text(circle.name).font(LimmrTheme.TypeScale.card).lineLimit(1); Spacer(minLength: 0) }
                HStack(spacing: -6) {
                  ForEach(Array(circle.people.prefix(4)), id: \.personId) { member in
                    Text(String(store.personName(member.personId).prefix(1))).font(.caption.weight(.semibold))
                      .frame(width: 24, height: 24).background(LimmrTheme.surface2, in: Circle()).overlay(Circle().stroke(LimmrTheme.surface, lineWidth: 2))
                  }
                  Spacer(minLength: 8)
                  Text("\(circle.people.count) \(circle.people.count == 1 ? "person" : "people")").font(LimmrTheme.TypeScale.meta).foregroundStyle(LimmrTheme.ink2)
                }
              }.foregroundStyle(LimmrTheme.ink).padding(14).frame(maxWidth: .infinity, alignment: .leading).n1Card()
            }.quietCard().modifier(QuietRow())
              .contextMenu { CircleIdentityMenu(store: store, circle: circle) }
          }
        }
      }
    }
  }
}
struct N1HomeRail: View {
  @Bindable var store: AppStore
  private var actions: [ActivityTask] { Array(ActivityPresentation.tasks(ActivityPresentation.visible(store.allRecentActivity + store.home.recentActivity)).prefix(3)) }
  var body: some View {
    let actions = actions
    VStack(alignment: .leading, spacing: 20) {
      if !actions.isEmpty {
        VStack(alignment: .leading, spacing: 14) {
          HStack { Text("What your AIs did").font(LimmrTheme.TypeScale.card); Spacer(); Button("Log") { store.selectPage(.activity) }.quietCard().foregroundStyle(LimmrTheme.accent) }
          ForEach(actions) { action in
            Button {
              store.activityAgent = action.agentId; store.selectedTask = action.id; store.selectPage(.activity)
            } label: {
              HStack(alignment: .top, spacing: 10) {
                Image(systemName: "checkmark.circle").foregroundStyle(.secondary).symbolRenderingMode(.hierarchical)
                VStack(alignment: .leading, spacing: 5) {
                  Text(action.title(agent: store.agentName(action.agentId), chatName: store.chatName)).font(LimmrTheme.TypeScale.body).lineLimit(3)
                  Text("by " + store.agentName(action.agentId) + " · " + store.relativeTime(action.end)).font(LimmrTheme.TypeScale.meta).foregroundStyle(LimmrTheme.ink2)
                }.frame(maxWidth: .infinity, alignment: .leading)
              }.foregroundStyle(LimmrTheme.ink)
            }.quietCard()
            if action.id != actions.last?.id { Divider() }
          }
        }
      }

    }
  }
}
struct N1ActivitySquares: View {
  let history: StatsHistory
  let now: Date
  private var days: [HistoryDay] { N1ActivityWindow.days(history: history, now: now) }
  var body: some View {
    let days = days
    VStack(alignment: .leading, spacing: 8) {
      Text("Last 12 weeks").font(.headline)
      let maximum = max(1, days.map(N1ActivityWindow.count).max() ?? 1)
      HStack(alignment: .top, spacing: 3) {
        ForEach(0..<12, id: \.self) { week in
          VStack(spacing: 3) {
            ForEach(0..<7, id: \.self) { weekday in
              let day = days[week * 7 + weekday]
              let count = N1ActivityWindow.count(day)
              RoundedRectangle(cornerRadius: 2)
                .fill(count == 0 ? Color.primary.opacity(0.06) : Color.accentColor.opacity(N1ActivityWindow.opacity(count, maximum: maximum)))
                .frame(width: 10, height: 10)
                .help("\(day.date) · \(count) activity")
            }
          }
        }
      }.frame(height: 88)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Activity in the last 12 weeks, \(days.reduce(0) { $0 + N1ActivityWindow.count($1) }) messages and AI actions")
      HStack(spacing: 4) {
        Text("Less")
        ForEach([0.25, 0.5, 0.75, 1.0], id: \.self) { opacity in
          RoundedRectangle(cornerRadius: 2).fill(Color.accentColor.opacity(opacity)).frame(width: 8, height: 8)
        }
        Text("More")
      }.font(.caption).foregroundStyle(.secondary).accessibilityHidden(true)
    }
  }
}

extension AppStore {
  var homeActivityHistory: StatsHistory? {
    if let recent = homeRailHistory { return recent }
    // Retained older activity still warrants the calendar, even when its most
    // recent twelve weeks are quiet.
    if let history, history.days.contains(where: { N1ActivityWindow.count($0) > 0 }) { return history }
    return nil
  }
  var homeRailHistory: StatsHistory? {
    // Already received audit actions are evidence even while the history request is pending.
    let rows = ActivityPresentation.detailed(allRecentActivity + home.recentActivity)
    let grouped = Dictionary(grouping: rows) { String($0.at.prefix(10)) }
    var days = Dictionary((history?.days ?? []).map { ($0.date, $0) }, uniquingKeysWith: { first, _ in first })
    for (date, rows) in grouped {
      let count = rows.reduce(0) { $0 + max(1, $1.count) }
      var day = days[date] ?? HistoryDay(date: date)
      day.agentReads = max(day.agentReads, max(0, count - day.agentSends))
      days[date] = day
    }
    let value = StatsHistory(days: Array(days.values))
    return N1ActivityWindow.days(history: value, now: presentationNow).contains { N1ActivityWindow.count($0) > 0 } ? value : nil
  }
}

enum N1ActivityWindow {
  static func count(_ day: HistoryDay) -> Int { max(day.messages, day.agentActivity) }
  static func days(history: StatsHistory, now: Date) -> [HistoryDay] {
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = TimeZone(secondsFromGMT: 0)!
    let end = calendar.startOfDay(for: now)
    let lookup = Dictionary(history.days.map { ($0.date, $0) }, uniquingKeysWith: { first, _ in first })
    return (0..<84).map { offset in
      let date = calendar.date(byAdding: .day, value: offset - 83, to: end)!.ISO8601Format().prefix(10)
      return lookup[String(date)] ?? HistoryDay(date: String(date))
    }
  }
  static func opacity(_ count: Int, maximum: Int) -> Double {
    Double(max(1, min(4, Int(ceil(4 * Double(count) / Double(max(1, maximum))))))) / 4
  }
}
