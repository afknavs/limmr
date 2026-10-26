import LimmrKit
import SwiftUI

struct WaitingTile: View {
  @Bindable var store: AppStore
  let item: WaitingItem
  @State private var presented = false
  var body: some View {
    Button { presented = true } label: {
      VStack(alignment: .leading, spacing: 7) {
        HStack(spacing: 8) {
          Text(item.agent).font(.callout.weight(.semibold))
          Spacer(minLength: 0)
          Text("\(item.newHits) new").font(.caption.weight(item.newHits > 0 ? .semibold : .regular))
            .foregroundStyle(item.newHits > 0 ? Color.accentColor : .secondary)
        }
        Text(item.sentence).font(.callout).fixedSize(horizontal: false, vertical: true)
          .frame(maxWidth: .infinity, minHeight: 48, alignment: .topLeading)
        Text("\(item.where) · \(store.relativeTime(item.since)) waiting")
          .font(.caption).foregroundStyle(AccessibleSecondary()).lineLimit(1)
      }
      .padding(12).frame(maxWidth: .infinity, alignment: .leading).contentShape(Rectangle())
    }
    .limmrButton()
    .background(.primary.opacity(0.035), in: RoundedRectangle(cornerRadius: 8))
    .accessibilityLabel("\(item.agent), \(item.sentence), \(item.where), \(store.relativeTime(item.since)) waiting, \(item.newHits) new matches")
    .accessibilityValue("\(item.newHits) new matches")
    .accessibilityHint("Open watch details")
    .popover(isPresented: $presented, arrowEdge: .bottom) { WatchCard(store: store, id: item.id).dismissOnEscape() }
  }
}
