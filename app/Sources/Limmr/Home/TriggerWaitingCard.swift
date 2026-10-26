import LimmrKit
import SwiftUI

struct TriggerWaitingCard: View {
  @Bindable var store: AppStore
  @Environment(\.colorSchemeContrast) private var systemContrast
  @Environment(\.snapshotIncreasedContrast) private var snapshotContrast
  private var increasedContrast: Bool { systemContrast == .increased || snapshotContrast }
  var compact = false
  var body: some View {
    TimelineView(.periodic(from: .now, by: 60)) { context in
      let now = store.triggerDate(at: context.date)
      let watches = HomeTriggerPresentation.visibleWatches(store.home.watches, now: now)
      VStack(alignment: .leading, spacing: 16) {
        BlockHeading(title: "Waiting for a trigger")
        ForEach(watches.filter { watch in store.home.waiting.contains { $0.id == watch.id } }) { watch in
          TriggerWaitingRow(store: store, watch: watch, now: now, compact: compact)
        }
        ForEach(store.home.waiting.filter { item in !watches.contains { $0.id == item.id } }) { item in
          WaitingTile(store: store, item: item)
        }
      }.padding(16).frame(maxWidth: .infinity, alignment: .topLeading)
        .background(.primary.opacity(0.045), in: RoundedRectangle(cornerRadius: 10))
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(increasedContrast ? Color.primary.opacity(0.65) : Color.secondary.opacity(0.35),
          style: StrokeStyle(lineWidth: increasedContrast ? 1 : 0.5, dash: increasedContrast ? [] : [4, 4])))
    }
  }
}

extension AppStore {
  func triggerDate(at now: Date) -> Date {
    guard demo else { return now }
    // Snapshots stay deterministic; the interactive sample clock keeps moving across visits.
    return Timestamp.date(DemoData.now)!.addingTimeInterval(snapshot ? 0 : max(0, now.timeIntervalSince(demoClockStartedAt)))
  }
}

struct TriggerWaitingRow: View {
  @Bindable var store: AppStore
  let watch: WatchSummary
  let now: Date
  var compact = false
  @State private var presented = false
  var body: some View {
    let isPerson = store.people.contains { $0.name == watch.where || $0.label == watch.where }
      || store.chats.contains { !$0.isGroup && $0.name == watch.where }
    let row = HomeTriggerPresentation(watch, now: now, sourceIsPerson: isPerson)
    Button { presented = true } label: {
      HStack(alignment: .top, spacing: 8) {
        TriggerChain(source: row.sourceGlyph, name: watch.where, actor: row.actor,
                     progress: row.waitProgress, fired: watch.status == "active" && row.firedRecently,
                     small: compact)
          .padding(.top, 4)
        VStack(alignment: .leading, spacing: 4) {
          Text(row.condition).font(AppType.rowTitle).fixedSize(horizontal: false, vertical: true)
          if compact {
            Text(row.action).font(AppType.caption).foregroundStyle(AccessibleSecondary())
              .fixedSize(horizontal: false, vertical: true)
            timing(row).frame(maxWidth: .infinity, alignment: .trailing)
          } else {
            Text(row.action).font(AppType.caption).foregroundStyle(AccessibleSecondary())
              .fixedSize(horizontal: false, vertical: true)
          }
        }.frame(maxWidth: .infinity, alignment: .leading)
        if !compact { timing(row).frame(maxWidth: 72, alignment: .trailing) }
      }.frame(maxWidth: .infinity, alignment: .leading).contentShape(Rectangle())
    }.limmrButton(.row(radius: 10, horizontal: 8, vertical: 6, bordered: false), id: "trigger")
      .accessibilityElement(children: .ignore)
      .accessibilityLabel("\(watch.where) → \(row.actor). \(row.condition), \(row.action). \(watch.status == "paused" ? "Paused" : row.fireTime ?? row.listeningLabel). \(row.timing)")
      .accessibilityValue(watch.status == "paused" ? "Paused" : row.fireTime ?? row.listeningLabel)
      .accessibilityHint("Open trigger details")
      .popover(isPresented: $presented, arrowEdge: .bottom) { WatchCard(store: store, id: watch.id).dismissOnEscape() }
  }
  @ViewBuilder private func timing(_ row: HomeTriggerPresentation) -> some View {
    if watch.status == "paused" {
      Text("Paused").font(AppType.caption).foregroundStyle(AccessibleSecondary())
    } else if let fireTime = row.fireTime {
      Text(fireTime).font(AppType.caption).foregroundStyle(Color.accentColor).fixedSize(horizontal: false, vertical: true)
    } else {
      ListeningGlyph().accessibilityLabel(row.listeningLabel).help(row.listeningLabel)
    }
  }

}

struct TriggerChain: View {
  let source: HomeTriggerPresentation.SourceGlyph
  let name: String
  let actor: String
  let progress: Double
  let fired: Bool
  var small = false
  var body: some View {
    HStack(spacing: 0) {
      Group {
        if source == .time {
          ZStack {
            Circle().stroke(.secondary.opacity(0.2), lineWidth: 2)
            Circle().trim(from: 0, to: progress).stroke(Color.accentColor, style: StrokeStyle(lineWidth: 2, lineCap: .round))
              .rotationEffect(.degrees(-90))
            Image(systemName: "clock").font(.system(size: 12)).foregroundStyle(AccessibleSecondary())
          }
        } else {
          InitialGlyph(name: name, round: source == .person)
        }
      }.frame(width: small ? 24 : 32, height: small ? 24 : 32)
      TriggerLink().stroke(fired ? Color.accentColor : Color.secondary.opacity(0.45),
                          style: StrokeStyle(lineWidth: 1, dash: fired ? [] : [3, 3]))
        .frame(width: small ? 16 : 24, height: 1)
      InitialGlyph(name: actor, round: false).frame(width: small ? 24 : 32, height: small ? 24 : 32)
    }.frame(width: small ? 64 : 88, height: small ? 24 : 32).accessibilityHidden(true)
  }
}
struct InitialGlyph: View {
  let name: String
  var round = false
  var body: some View {
    Text(name == "You" ? "You" : String(name.split(separator: " ").prefix(2).compactMap(\.first)).uppercased())
      .font(.system(size: 10, weight: .semibold, design: .rounded)).foregroundStyle(AccessibleSecondary())
      .frame(maxWidth: .infinity, maxHeight: .infinity)
      .background(.primary.opacity(0.07), in: RoundedRectangle(cornerRadius: round ? 100 : 7))
  }
}
private struct TriggerLink: Shape {
  func path(in rect: CGRect) -> Path {
    Path { $0.move(to: CGPoint(x: rect.minX, y: rect.midY)); $0.addLine(to: CGPoint(x: rect.maxX, y: rect.midY)) }
  }
}
private struct ListeningGlyph: View {
  var body: some View {
    HStack(alignment: .center, spacing: 2) {
      ForEach(0..<4) { i in
        Capsule().fill(.secondary.opacity(0.6)).frame(width: 2, height: [5.0, 10, 14, 7][i])
      }
    }.frame(width: 18, height: 16)
  }
}
