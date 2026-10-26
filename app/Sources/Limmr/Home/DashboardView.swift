import LimmrKit
import SwiftUI

struct DashboardView: View {
  @Bindable var store: AppStore
  var contentWidth: Double = HomePresentation.dashboardContentWidth
  var fullWidth = false
  private var geometry: (horizontal: Bool, heatmap: Double, counts: Double) {
    HomePresentation.fullDashboardGeometry(contentWidth: contentWidth)
  }
  private var weeks: Int { HomePresentation.heatmapGeometry(contentWidth: geometry.heatmap, columns: 52).visible }
  private var period: [HistoryDay] { store.history?.period(30) ?? [] }
  var body: some View {
    VStack(alignment: .leading, spacing: 12) {
      Text(HomePresentation.heatmapPeriod(weeks: weeks)).font(AppType.blockHeading).foregroundStyle(AccessibleSecondary())
      if let history = store.history {
        if geometry.horizontal {
          HStack(alignment: .center, spacing: 24) {
            calendar(history).frame(width: geometry.heatmap)
            counts(vertical: true).frame(width: geometry.counts)
          }
        } else {
          calendar(history)
          counts(vertical: false)
        }
      } else if store.isLoading("/v1/stats/history") {
        QuietPlaceholder(height: 155).accessibilityLabel("Loading message history")
      } else {
        Text("Message history isn't available yet.").font(.callout).foregroundStyle(AccessibleSecondary())
        Button("Try again") { Task { await store.refreshHistory() } }.limmrButton()
      }
    }.frame(maxWidth: .infinity, alignment: .leading)
      .padding(16).background(.primary.opacity(0.025), in: RoundedRectangle(cornerRadius: 8))
      .help(HomePresentation.dashboardFooter(lastSyncAt: store.dashboard?.lastSyncAt,
        now: store.demo ? Timestamp.date(DemoData.now)! : Date()))
  }
  private func calendar(_ history: StatsHistory) -> some View {
    HeatmapView(history: history, agents: false, weeks: weeks, ai: store.agents,
      width: geometry.heatmap, openDay: store.openActivityDay)
  }
  private func counts(vertical: Bool) -> some View {
    let layout = vertical ? AnyLayout(VStackLayout(alignment: .leading, spacing: 12)) : AnyLayout(HStackLayout(spacing: 24))
    return VStack(alignment: .leading, spacing: 6) {
      Text("Last 30 days").font(.system(size: 10)).foregroundStyle(AccessibleSecondary())
      layout {
      metric("Messages", period.reduce(0) { $0 + $1.messages })
      metric("AI reads", period.reduce(0) { $0 + $1.agentReads })
      metric("AI sends", period.reduce(0) { $0 + $1.agentSends })
      }
    }.frame(maxWidth: .infinity, alignment: .leading)
  }
  private func metric(_ title: String, _ value: Int) -> some View {
    VStack(alignment: .leading, spacing: 2) {
      Text(value.formatted()).font(.system(size: 26, weight: .semibold, design: .rounded)).monospacedDigit()
      Text(title).font(AppType.caption).foregroundStyle(AccessibleSecondary())
    }.accessibilityElement(children: .ignore).accessibilityLabel("\(value) \(title), last 30 days")
      .frame(maxWidth: .infinity, alignment: .leading)
  }
}

struct HeatmapView: View {
  let history: StatsHistory
  let agents: Bool
  var weeks: Int = HomePresentation.heatmapWeeks
  var ai: [Agent] = []
  var width: Double = HomePresentation.dashboardContentWidth
  var openDay: (String) -> Void = { _ in }
  @Environment(\.colorSchemeContrast) private var systemContrast
  @Environment(\.snapshotIncreasedContrast) private var snapshotContrast
  private var contrast: Bool { systemContrast == .increased || snapshotContrast }
  var body: some View {
    let cells = HomePresentation.stripCells(history, weeks: weeks)
    let side = HomePresentation.heatmapGeometry(contentWidth: width, columns: max(1, cells.count / 7)).side
    let maximum = cells.compactMap { $0 }.map { agents ? $0.agentActivity : $0.messages }.max() ?? 1
    VStack(alignment: .leading, spacing: 6) {
      ZStack(alignment: .topLeading) {
        ForEach(HomePresentation.monthLabels(cells: cells, side: side), id: \.column) { label in
          Text(label.title).font(.system(size: 10)).foregroundStyle(AccessibleSecondary())
            .offset(x: label.x)
        }
      }.frame(width: width, height: 14, alignment: .topLeading).accessibilityHidden(true)
      HStack(alignment: .top, spacing: 0) {
        VStack(alignment: .leading, spacing: 2) {
          ForEach(0..<7, id: \.self) { row in
            Text(["Mon", "", "Wed", "", "Fri", "", ""][row])
              .font(.system(size: 9)).foregroundStyle(AccessibleSecondary()).frame(height: side)
          }
        }.frame(width: 36, alignment: .leading).accessibilityHidden(true)
        HStack(alignment: .top, spacing: 2) {
          ForEach(0..<(cells.count / 7), id: \.self) { column in
            VStack(spacing: 2) {
              ForEach(0..<7, id: \.self) { row in
                let day = cells[column * 7 + row]
                if let day {
                  let count = agents ? day.agentActivity : day.messages
                  let label = HomePresentation.dayLabel(day, agents: ai)
                  Button { openDay(day.date) } label: {
                    square(count == 0 ? 0 : StatsHistory.intensity(count, maximum: maximum))
                  }.buttonStyle(.plain).frame(width: side, height: side)
                    .help(label).accessibilityLabel(label).accessibilityHint("Show Activity for this day")
                } else {
                  Color.clear.frame(width: side, height: side).accessibilityHidden(true)
                }
              }
            }
          }
        }
      }
      HStack(spacing: 4) {
        Spacer(minLength: 0)
        Text("Less")
        ForEach([0.0, 0.3, 0.6, 1.0], id: \.self) { square($0).frame(width: 12, height: 12) }
        Text("More")
      }.font(.system(size: 10)).foregroundStyle(AccessibleSecondary())
        .accessibilityElement(children: .ignore).accessibilityLabel("Less to more messages")
    }.accessibilityElement(children: .contain).accessibilityLabel("Message history, \(HomePresentation.heatmapPeriod(weeks: weeks).lowercased())")
  }
  private func square(_ intensity: Double) -> some View {
    RoundedRectangle(cornerRadius: 3)
      .fill(intensity == 0 ? Color.primary.opacity(contrast ? 0.13 : 0.07) : Color.accentColor.opacity(contrast ? 0.45 + 0.55 * intensity : intensity))
      .overlay(RoundedRectangle(cornerRadius: 3).strokeBorder(Color.primary.opacity(contrast ? 0.65 : 0), lineWidth: contrast ? 0.7 : 0))
  }
}
