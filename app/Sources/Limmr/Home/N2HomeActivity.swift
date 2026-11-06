import LimmrKit
import SwiftUI

/// History belongs in the main reading order at every window width.
struct N2HomeActivity: View {
  let history: StatsHistory
  let now: Date
  var openDay: (String) -> Void
  private var days: [HistoryDay] { N1ActivityWindow.days(history: history, now: now) }
  var body: some View {
    let days = days
    let period = days.suffix(30)
    let maximum = max(1, days.map(N1ActivityWindow.count).max() ?? 1)
    ViewThatFits(in: .horizontal) {
      HStack(alignment: .center, spacing: 24) {
        calendar(days, maximum: maximum)
        counts(period, vertical: true).frame(minWidth: 110, maxWidth: .infinity, alignment: .leading)
      }
      VStack(alignment: .leading, spacing: 12) {
        calendar(days, maximum: maximum)
        counts(period, vertical: false)
      }
    }.padding(16).frame(maxWidth: .infinity, alignment: .leading)
      .background(.quaternary.opacity(0.25), in: RoundedRectangle(cornerRadius: 10))
      .accessibilityElement(children: .contain)
      .accessibilityLabel("Activity history")
  }
  private func calendar(_ days: [HistoryDay], maximum: Int) -> some View {
    VStack(alignment: .leading, spacing: 10) {
      Text("Last 12 weeks").font(.headline)
      HStack(alignment: .top, spacing: 3) {
        ForEach(0..<12, id: \.self) { week in
          VStack(spacing: 3) {
            ForEach(0..<7, id: \.self) { weekday in
              let day = days[week * 7 + weekday]
              let count = N1ActivityWindow.count(day)
              Button { openDay(day.date) } label: {
                RoundedRectangle(cornerRadius: 3)
                  .fill(count == 0 ? Color.primary.opacity(0.07) : Color.accentColor.opacity(N1ActivityWindow.opacity(count, maximum: maximum)))
                  .frame(width: 22, height: 18)
              }.quietCard().help(day.date + " · \(count) activity")
                .accessibilityLabel(HomePresentation.dayLabel(day, agents: []))
                .accessibilityHint("Show Activity for this day")
            }
          }
        }
      }
    }
  }
  private func counts(_ days: ArraySlice<HistoryDay>, vertical: Bool) -> some View {
    VStack(alignment: .leading, spacing: 8) {
      Text("Last 30 days").font(.caption).foregroundStyle(.secondary)
      let layout = vertical ? AnyLayout(VStackLayout(alignment: .leading, spacing: 8)) : AnyLayout(HStackLayout(spacing: 20))
      layout {
        metric("Messages", days.reduce(0) { $0 + $1.messages })
        metric("AI reads", days.reduce(0) { $0 + $1.agentReads })
        metric("AI sends", days.reduce(0) { $0 + $1.agentSends })
      }
    }
  }
  private func metric(_ title: String, _ value: Int) -> some View {
    HStack(alignment: .firstTextBaseline, spacing: 8) {
      Text(value.formatted()).font(.title2.weight(.semibold)).monospacedDigit()
      Text(title).font(.caption).foregroundStyle(.secondary)
    }.accessibilityElement(children: .ignore).accessibilityLabel("\(value) \(title), last 30 days")
  }
}
