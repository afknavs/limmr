import LimmrKit
import SwiftUI

/// One drawing surface instead of hundreds of native controls.
/// It fills the width it is given: the number of weeks comes from that width (and from how much history there is),
/// and the cells stretch so the grid ends at the card's edge. Accessibility exposes per-day buttons.
struct ActivityHeatmap: View {
  let history: StatsHistory
  let agents: [Agent]
  var width: Double
  let openDay: (String) -> Void
  @State private var hovered: HistoryDay?
  @Environment(\.colorSchemeContrast) private var contrast
  static let gap = 3.0, labels = 34.0, top = 18.0, cellHeight = 14.0, cell = 22.0
  /// Whole weeks that fit at a comfortable cell size, never more than there is history for.
  static func weeks(width: Double, days: Int) -> Int {
    let fit = Int((width - labels + gap) / (cell + gap))
    return max(1, min(HomePresentation.maximumHeatmapWeeks, min(max(8, fit), (days + 12) / 7)))
  }
  private var weeks: Int { Self.weeks(width: width, days: history.days.count) }
  private var side: Double { min(36, max(2, (width - Self.labels - Self.gap * Double(weeks - 1)) / Double(weeks))) }
  private func day(at point: CGPoint, cells: [HistoryDay?]) -> HistoryDay? {
    let column = Int(floor((point.x - Self.labels) / (side + Self.gap)))
    let row = Int(floor((point.y - Self.top) / (Self.cellHeight + Self.gap)))
    guard point.x >= Self.labels, point.y >= Self.top, column >= 0, column < weeks, row >= 0, row < 7 else { return nil }
    let index = column * 7 + row
    return cells.indices.contains(index) ? cells[index] : nil
  }
  var body: some View {
    let cells = HomePresentation.stripCells(history, weeks: weeks)
    let maximum = max(1, cells.compactMap { $0?.agentActivity }.max() ?? 1)
    let side = side
    Canvas { context, _ in
      for label in HomePresentation.monthLabels(cells: cells, side: side) {
        context.draw(Text(label.title).font(LimmrTheme.font(11)).foregroundStyle(LimmrTheme.ink3), at: CGPoint(x: Self.labels + Double(label.column) * (side + Self.gap), y: 0), anchor: .topLeading)
      }
      for row in [0, 2, 4] {
        context.draw(Text(["Mon", "", "Wed", "", "Fri"][row]).font(LimmrTheme.font(11)).foregroundStyle(LimmrTheme.ink3), at: CGPoint(x: 0, y: Self.top + Double(row) * (Self.cellHeight + Self.gap)), anchor: .topLeading)
      }
      for (index, day) in cells.enumerated() {
        guard let day else { continue }
        let count = day.agentActivity
        let color = LimmrTheme.ink.opacity(count == 0 ? (contrast == .increased ? 0.16 : 0.07) : 0.16 + 0.66 * StatsHistory.intensity(count, maximum: maximum))
        let rect = CGRect(x: Self.labels + Double(index / 7) * (side + Self.gap), y: Self.top + Double(index % 7) * (Self.cellHeight + Self.gap), width: side, height: Self.cellHeight)
        context.fill(Path(roundedRect: rect, cornerRadius: 3), with: .color(color))
      }
    }
    .frame(width: width, height: Self.top + 7 * (Self.cellHeight + Self.gap) - Self.gap)
    .contentShape(Rectangle())
    .onTapGesture { point in if let day = day(at: point, cells: cells) { openDay(day.date) } }
    .onContinuousHover { phase in
      switch phase {
      case .active(let point): hovered = day(at: point, cells: cells)
      case .ended: hovered = nil
      }
    }
    .help(hovered.map { HomePresentation.dayLabel($0, agents: agents) } ?? "AI activity over the last \(weeks) weeks. Choose a day to open its log.")
    .accessibilityLabel("AI activity, last \(weeks) weeks")
    .accessibilityChildren {
      ForEach(cells.compactMap { $0 }, id: \.date) { day in
        Button(HomePresentation.dayLabel(day, agents: agents)) { openDay(day.date) }
      }
    }
  }
}
