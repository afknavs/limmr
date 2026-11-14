import SwiftUI

/// Point-based rules shared by the shell, page grids and keyboard navigation.
enum PaperLayout {
  static let readingWidth = 1320.0
  static let pageWidth = readingWidth + 56
  static func sidebarWidth(_ window: Double) -> Double {
    window < 960 ? 56 : window <= 1100 ? 200 : 232
  }
  /// Pages use the whole panel: no reading cap, no centring. Grids decide how many columns fit.
  static func contentWidth(_ window: Double) -> Double {
    max(0, window - sidebarWidth(window) - 64)
  }
  /// Columns for a row of cards: as many as fit at `minimum`, never more than there are items,
  /// and never wider than `maximum` (a short row stays left-aligned instead of stretching).
  static func fit(width: Double, minimum: Double, maximum: Double, items: Int, spacing: Double = 12) -> (count: Int, width: Double) {
    let count = max(1, min(max(1, items), Int((width + spacing) / (minimum + spacing))))
    return (count, max(0, min(maximum, (width - spacing * Double(count - 1)) / Double(count))))
  }
  /// Home's side column appears once the main column can still hold two ask cards beside it.
  static func homeRail(_ window: Double) -> Double {
    let width = contentWidth(window)
    return width < 900 ? 0 : min(380, max(280, width * 0.26))
  }
  static func taskColumns(_ window: Double) -> Int {
    let width = contentWidth(window)
    return width < 820 ? 2 : min(5, 3 + Int((width - 820) / 240))
  }
  static func peopleColumns(_ window: Double) -> Int {
    taskColumns(window) - 1
  }
  static func sideBySide(_ window: Double) -> Bool { window >= 1100 }
  static func inspectorWidth(_ window: Double) -> Double {
    min(360, max(300, contentWidth(window) * 0.31))
  }
  static func taskListWidth(_ window: Double) -> Double {
    contentWidth(window) - (sideBySide(window) ? inspectorWidth(window) + 12 : 0)
  }
  static func mapHeight(windowHeight: Double) -> Double { max(280, windowHeight - 338) }
  static func circleListHeight(windowWidth: Double, windowHeight: Double) -> Double {
    sideBySide(windowWidth) ? max(320, windowHeight - 232) : max(200, windowHeight * 0.30)
  }
  static func conversationHeight(_ windowHeight: Double) -> Double { max(220, windowHeight - 572) }
}

/// Greedy wrapping preserves each chip's intrinsic width and VoiceOver order.
struct PaperFlow: Layout {
  var spacing: CGFloat = 8
  func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
    arrange(width: proposal.width ?? .infinity, subviews: subviews).size
  }
  func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
    let result = arrange(width: bounds.width, subviews: subviews)
    for (index, point) in result.points.enumerated() {
      let size = subviews[index].sizeThatFits(.unspecified)
      subviews[index].place(at: CGPoint(x: bounds.minX + point.x, y: bounds.minY + point.y), anchor: .topLeading,
        proposal: ProposedViewSize(width: min(bounds.width, size.width), height: size.height))
    }
  }
  private func arrange(width: CGFloat, subviews: Subviews) -> (size: CGSize, points: [CGPoint]) {
    var x: CGFloat = 0, y: CGFloat = 0, row: CGFloat = 0, used: CGFloat = 0
    var points: [CGPoint] = []
    for view in subviews {
      let size = view.sizeThatFits(.unspecified), w = min(width, size.width)
      if x > 0 && x + w > width { x = 0; y += row + spacing; row = 0 }
      points.append(CGPoint(x: x, y: y)); used = max(used, x + w)
      x += w + spacing; row = max(row, size.height)
    }
    return (CGSize(width: width.isFinite ? width : used, height: y + row), points)
  }
}

extension View {
  /// Kept for call sites: pages are leading-aligned and fill the panel.
  func paperReadingWidth() -> some View { frame(maxWidth: .infinity, alignment: .topLeading) }
}

/// A grid whose columns come from the space it has and the cards it holds.
struct PaperGrid<Item: Identifiable, Cell: View>: View {
  let items: [Item]
  let width: Double
  let minimum: Double
  let maximum: Double
  var rows: Int? = nil
  var spacing: Double = 12
  @ViewBuilder var cell: (Item) -> Cell
  var body: some View {
    let fit = PaperLayout.fit(width: width, minimum: minimum, maximum: maximum, items: items.count, spacing: spacing)
    LazyVGrid(columns: Array(repeating: GridItem(.fixed(fit.width), spacing: spacing, alignment: .top), count: fit.count), alignment: .leading, spacing: spacing) {
      ForEach(rows.map { Array(items.prefix($0 * fit.count)) } ?? items) { cell($0) }
    }.frame(maxWidth: .infinity, alignment: .leading)
  }
}
