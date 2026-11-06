import LimmrKit
import SwiftUI

/// One drawing surface instead of 294 native controls in the first log layout.
/// Accessibility exposes the same per-day buttons without laying them all out.
struct ActivityHeatmap: View {
  let history: StatsHistory
  let agents: [Agent]
  let openDay: (String) -> Void
  @State private var hovered: HistoryDay?
  @Environment(\.colorSchemeContrast) private var contrast
  private let weeks = 42
  private func day(at point: CGPoint, cells: [HistoryDay?], width: Double) -> HistoryDay? {
    let side = min(16.0, max(2, (width - 36) / Double(weeks) - 2))
    let column = Int(floor((point.x - 36) / (side + 2)))
    let row = Int(floor((point.y - 20) / (side + 2)))
    guard point.x >= 36, point.y >= 20, column >= 0, column < weeks, row >= 0, row < 7 else { return nil }
    let index = column * 7 + row
    return cells.indices.contains(index) ? cells[index] : nil
  }
  var body: some View {
    let cells = HomePresentation.stripCells(history, weeks: weeks)
    let maximum = max(1, cells.compactMap { $0?.agentActivity }.max() ?? 1)
    GeometryReader { geometry in
      Canvas { context, size in
        let side = min(16.0, max(2, (size.width - 36) / Double(weeks) - 2))
        for label in HomePresentation.monthLabels(cells: cells, side: side) {
          context.draw(Text(label.title).font(.system(size: 10)).foregroundStyle(.secondary), at: CGPoint(x: label.x, y: 0), anchor: .topLeading)
        }
        for row in [0, 2, 4] {
          context.draw(Text(["Mon", "", "Wed", "", "Fri", "", ""][row]).font(.system(size: 9)).foregroundStyle(.secondary), at: CGPoint(x: 0, y: 20 + Double(row) * (side + 2)), anchor: .topLeading)
        }
        for (index, day) in cells.enumerated() {
          guard let day else { continue }
          let count = day.agentActivity
          let intensity = StatsHistory.intensity(count, maximum: maximum)
          let color: Color = count == 0 ? .primary.opacity(contrast == .increased ? 0.16 : 0.07) : .accentColor.opacity(0.3 + 0.7 * intensity)
          let rect = CGRect(x: 36 + Double(index / 7) * (side + 2), y: 20 + Double(index % 7) * (side + 2), width: side, height: side)
          context.fill(Path(roundedRect: rect, cornerRadius: 3), with: .color(color))
        }
      }
      .contentShape(Rectangle())
      .onTapGesture { point in if let day = day(at: point, cells: cells, width: geometry.size.width) { openDay(day.date) } }
      .onContinuousHover { phase in
        switch phase {
        case .active(let point): hovered = day(at: point, cells: cells, width: geometry.size.width)
        case .ended: hovered = nil
        }
      }
      .help(hovered.map { HomePresentation.dayLabel($0, agents: agents) } ?? "AI activity over the last 42 weeks. Choose a day to open its log.")
      .accessibilityLabel("AI activity, last 42 weeks")
      .accessibilityChildren {
        ForEach(cells.compactMap { $0 }, id: \.date) { day in
          Button(HomePresentation.dayLabel(day, agents: agents)) { openDay(day.date) }
        }
      }
    }.frame(height: 148)
  }
}
