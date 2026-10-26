import Foundation
import LimmrKit
import SwiftUI

/// Circles own their update rectangles. The server's unread slice supplies surfacing evidence.
struct CircleUpdatesView: View {
  @Bindable var store: AppStore
  var contentWidth: Double = 760
  private var displayTiles: [HomeCircleTile] { contentWidth >= 620 ? HomeTiles.pairedTiles(tiles) : tiles }
  private var tiles: [HomeCircleTile] {
    HomeTiles.tiles(circles: store.circles, home: store.home, efforts: store.homeEfforts,
                    now: store.demo ? Timestamp.date(DemoData.now)! : Date())
  }
  var body: some View {
    VStack(alignment: .leading, spacing: 16) {
      HStack {
        Text("Circles").font(AppType.blockHeading)
        Spacer()
        Button("All circles") { store.openAllHomeCircles() }
          .limmrButton().font(AppType.caption).foregroundStyle(AccessibleSecondary())
      }
      if store.isLoading("/v1/home") || store.isLoading("/v1/circles") {
        QuietLoadingCards()
      }
      CircleTileLayout {
        ForEach(displayTiles) { tile in
          HomeCircleTileView(store: store, tile: tile)
        }
        if HomeTiles.needsOtherTile(count: tiles.count) {
          OtherCirclesTile(store: store, circles: HomeTiles.otherCircles(circles: store.circles, tiles: tiles))
        }
      }
      if store.loadFailures.keys.contains(where: { $0.hasPrefix("/v1/efforts?") }) {
        HStack {
          Text("Some circle work could not be loaded.").font(AppType.caption).foregroundStyle(AccessibleSecondary())
          Button("Try again") { Task { await store.refreshHomeEfforts() } }.limmrButton().font(AppType.caption)
        }
      }
    }
    // Load when Home is visited or work changes, rather than fetching 500 summaries
    // on every background refresh. The unread slice remains usable during this load.
    .task(id: "\(store.backendGeneration):\(store.home.circleUpdates.map { $0.id + $0.lastAt + $0.status + ($0.due ?? "") }.joined())") {
      await store.refreshHomeEfforts()
    }
    .onDisappear {
      let updates = HomeTiles.acknowledgedUpdates(tiles: tiles, unread: store.home.circleUpdates)
      guard !store.snapshot, !updates.isEmpty else { return }
      let source = store.backend
      Task { try? await source.post("/v1/efforts/seen", CircleEffortSeen(updates)) }
    }
  }
}

struct HomeCircleTileView: View {
  @Bindable var store: AppStore
  let tile: HomeCircleTile
  private var reason: String {
    if tile.reason == "on you" { return "Waiting on you" }
    if tile.pinned { return "Pinned" }
    if tile.reason == "due within 7 days" { return "Due this week" }
    if tile.reason == "changed since last look" { return "New updates" }
    return tile.reason ?? "Pinned"
  }
  var body: some View {
    VStack(alignment: .leading, spacing: 12) {
      Button { store.openCircle(tile.id) } label: {
        HStack(spacing: 10) {
          Image(systemName: tile.circle.symbol).font(.system(size: 14))
            .frame(width: 28, height: 28)
            .background(.primary.opacity(0.055), in: RoundedRectangle(cornerRadius: 7))
          VStack(alignment: .leading, spacing: 3) {
            Text(tile.circle.name).font(.headline).lineLimit(1)
            Text(reason).font(AppType.caption)
              .foregroundStyle(tile.reason == "on you" ? Color.accentColor : .secondary).lineLimit(1)
          }
          Spacer(minLength: 0)
          if tile.newMessages > 0 {
            VStack(spacing: 1) {
              Text(tile.newMessages.formatted()).font(.callout.weight(.semibold)).monospacedDigit()
              Text("new").font(.system(size: 10)).foregroundStyle(AccessibleSecondary())
            }
          }
          if tile.pinned { Image(systemName: "pin.fill").font(.system(size: 9)).foregroundStyle(AccessibleTertiary()) }
        }.contentShape(Rectangle())
      }.limmrButton(.row(radius: 7, bordered: false), id: "circle")
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Open \(tile.circle.name). \(reason). \(tile.pinned ? "Pinned. " : "")\(tile.newMessages) new messages")
        .accessibilityHint("Open this circle")
      ForEach(Array(tile.rectangles.prefix(2))) { rectangle in
        Button { open(rectangle) } label: {
          rectangleContent(rectangle).padding(8).frame(maxWidth: .infinity, alignment: .leading)
            .background(.primary.opacity(0.035), in: RoundedRectangle(cornerRadius: 6))
            .contentShape(Rectangle())
        }.limmrButton(.row(radius: 6, bordered: false), id: "circle-update")
          .accessibilityElement(children: .ignore)
          .accessibilityLabel(label(rectangle))
          .accessibilityHint("Open \(isEffort(rectangle) ? "this work in Activity" : "this circle")")
          .contextMenu {
            if case .effort(let update) = rectangle {
              Button("Not on me") { Task { _ = await store.setEffortStatus(update.effortId, from: update.status, to: "moving") } }
              Button("Done") { Task { _ = await store.setEffortStatus(update.effortId, from: update.status, to: "done") } }
            }
          }
        if case .effort(let update) = rectangle, store.effortUndo[update.effortId] != nil {
          Button("Undo status change") { Task { _ = await store.undoEffortStatus(update.effortId) } }
            .limmrButton().font(AppType.caption).accessibilityLabel("Undo status change for \(update.title)")
        }
      }
    }.padding(16).frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
      .modifier(HomeTileSurface())
      .contextMenu {
        Button(tile.pinned ? "Unpin" : "Pin") { Task { await store.togglePin(tile.circle) } }
      }
  }
  @ViewBuilder private func rectangleContent(_ rectangle: HomeCircleRectangle) -> some View {
    switch rectangle {
    case .effort(let update):
      HStack(spacing: 8) {
        HomeEffortShape(status: update.status).frame(width: 10, height: 12)
        Text(update.title).font(AppType.rowTitle).lineLimit(1).help(update.title)
        Spacer(minLength: 0)
        Text(HomeTiles.shortState(update)).font(.system(size: 10))
          .foregroundStyle(update.status == "on_you" ? Color.accentColor : .secondary)
          .fixedSize()
      }
    case .ask(_, let text, let from):
      HStack(spacing: 8) {
        Image(systemName: "arrowshape.turn.up.left").font(.system(size: 12)).foregroundStyle(AccessibleSecondary())
        VStack(alignment: .leading, spacing: 3) {
          Text(text).font(.callout).lineLimit(1).help(text)
          if let from { Text("from " + from).font(.system(size: 10)).foregroundStyle(AccessibleSecondary()) }
        }
      }
    case .event(let event):
      HStack(spacing: 8) {
        if let date = HomeTiles.eventDate(event.date) {
          VStack(spacing: 1) {
            Text(date.formatted(.dateTime.weekday(.abbreviated)).uppercased()).font(.system(size: 8, weight: .medium))
            Text(date.formatted(.dateTime.day())).font(.system(size: 14, weight: .semibold)).monospacedDigit()
          }.frame(width: 30).padding(.vertical, 3)
            .background(.primary.opacity(0.06), in: RoundedRectangle(cornerRadius: 4))
            .accessibilityHidden(true)
        }
        Text(event.text).font(.callout).lineLimit(2).help(event.text)
      }
    }
  }
  private func isEffort(_ rectangle: HomeCircleRectangle) -> Bool {
    if case .effort = rectangle { true } else { false }
  }
  private func open(_ rectangle: HomeCircleRectangle) {
    if case .effort(let update) = rectangle { store.openEffort(update.effortId) }
    else { store.openCircle(tile.id) }
  }
  private func label(_ rectangle: HomeCircleRectangle) -> String {
    switch rectangle {
    case .effort(let update): "\(update.title). \(HomeTiles.shortState(update)). \(update.line)"
    case .ask(_, let text, let from): "Open ask: \(text)" + (from.map { ". From " + $0 } ?? "")
    case .event(let event): "Upcoming event: \(event.text). \(event.date)"
    }
  }
}
struct HomeTileSurface: ViewModifier {
  @Environment(\.colorSchemeContrast) private var systemContrast
  @Environment(\.snapshotIncreasedContrast) private var snapshotContrast
  func body(content: Content) -> some View {
    let contrast = systemContrast == .increased || snapshotContrast
    content.background(.primary.opacity(0.022), in: RoundedRectangle(cornerRadius: 10))
      .overlay(RoundedRectangle(cornerRadius: 10).stroke(contrast ? Color.primary.opacity(0.65) : Color.secondary.opacity(0.16), lineWidth: contrast ? 1 : 0.5))
  }
}
struct OtherCirclesTile: View {
  @Bindable var store: AppStore
  let circles: [LimmrKit.Circle]
  var body: some View {
    VStack(alignment: .leading, spacing: 12) {
      Text("Other circles").font(.headline)
      if circles.isEmpty {
        Button("All circles") { store.openAllHomeCircles() }.limmrButton().font(.callout)
      } else {
        // Adaptive chips wrap within the same grid edges as the surfaced circles.
        HomeCircleChipsLayout {
          ForEach(circles) { circle in
            Button { store.openCircle(circle.id) } label: {
              Label(circle.name, systemImage: circle.symbol).font(AppType.caption).lineLimit(1)
                .padding(.horizontal, 8).padding(.vertical, 6)
                .background(.primary.opacity(0.04), in: RoundedRectangle(cornerRadius: 6))
            }.limmrButton(.row(radius: 6, bordered: false))
              .accessibilityLabel("Open \(circle.name)")
          }
        }
      }
    }.padding(16).frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
      .modifier(HomeTileSurface())
  }
}

/// Paired rows share an origin; short cards gain at most 24 pt beyond their natural height.
struct CircleTileLayout: Layout {
  private func placements(width: Double, subviews: Subviews) -> (Double, [(CGPoint, CGSize)]) {
    let columns = width >= 620 && subviews.count > 1 ? 2 : 1
    let gap = 16.0
    let cardWidth = max(0, (width - Double(columns - 1) * gap) / Double(columns))
    var y = 0.0
    var placed: [(CGPoint, CGSize)] = []
    for start in stride(from: 0, to: subviews.count, by: columns) {
      let end = min(start + columns, subviews.count)
      let natural = (start..<end).map { subviews[$0].sizeThatFits(ProposedViewSize(width: cardWidth, height: nil)).height }
      let height = natural.max() ?? 0
      for index in start..<end {
        let tileHeight = HomeTiles.rowTileHeight(natural: natural[index - start], row: height)
        placed.append((CGPoint(x: Double(index - start) * (cardWidth + gap), y: y), CGSize(width: cardWidth, height: tileHeight)))
      }
      y += height + gap
    }
    return (max(0, y - gap), placed)
  }
  func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
    let width = proposal.width ?? 760
    return CGSize(width: width, height: placements(width: width, subviews: subviews).0)
  }
  func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
    for (view, placement) in zip(subviews, placements(width: bounds.width, subviews: subviews).1) {
      view.place(at: CGPoint(x: bounds.minX + placement.0.x, y: bounds.minY + placement.0.y), proposal: ProposedViewSize(placement.1))
    }
  }
}
struct HomeEffortShape: View {
  let status: String
  var body: some View {
    Group {
      switch HomeTiles.stateShape(status) {
      case "filled": Circle().fill(Color.accentColor).frame(width: 7, height: 7)
      case "ring": Circle().strokeBorder(.secondary, lineWidth: 1).frame(width: 7, height: 7)
      case "check": Image(systemName: "checkmark").font(.system(size: 9, weight: .medium)).foregroundStyle(AccessibleSecondary())
      default: Rectangle().fill(.secondary).frame(width: 5, height: 5)
      }
    }.accessibilityHidden(true)
  }
}

extension AppStore {
  func openAllHomeCircles() {
    selectPage(.circles)
    mapSelection = MapSelection()
    UserDefaults.standard.set("Circles", forKey: "peopleTab")
  }
}

/// Non-lazy chips provide finite intrinsic measurements inside the row-aligned tile layout.
struct HomeCircleChipsLayout: Layout {
  private func placements(width: Double, subviews: Subviews) -> (Double, [(CGPoint, CGSize)]) {
    var x = 0.0, y = 0.0, rowHeight = 0.0
    var result: [(CGPoint, CGSize)] = []
    for view in subviews {
      let natural = view.sizeThatFits(.unspecified)
      let size = view.sizeThatFits(ProposedViewSize(width: min(width, natural.width), height: nil))
      if x > 0 && x + size.width > width { y += rowHeight + 8; x = 0; rowHeight = 0 }
      result.append((CGPoint(x: x, y: y), size))
      x += size.width + 8; rowHeight = max(rowHeight, size.height)
    }
    return (y + rowHeight, result)
  }
  func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
    let width = proposal.width ?? 300
    return CGSize(width: width, height: placements(width: width, subviews: subviews).0)
  }
  func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
    for (view, placement) in zip(subviews, placements(width: bounds.width, subviews: subviews).1) {
      view.place(at: CGPoint(x: bounds.minX + placement.0.x, y: bounds.minY + placement.0.y), proposal: ProposedViewSize(placement.1))
    }
  }
}
