import LimmrKit
import SwiftUI

/// A compact queue in the activity row; full reply actions remain in the existing sheet.
struct NeedsReplyCard: View {
  @Bindable var store: AppStore
  @State private var expanded = false
  var body: some View {
    VStack(alignment: .leading, spacing: 12) {
      Button { expanded = true } label: {
        HStack {
          BlockHeading(title: "Needs a reply")
          Image(systemName: "chevron.right").font(.system(size: 9)).foregroundStyle(AccessibleTertiary())
        }
      }.limmrButton().accessibilityHint("Review messages and reply actions")
      ForEach(store.home.needsReply.prefix(3)) { ask in
        Button { expanded = true } label: {
          HStack(alignment: .top, spacing: 8) {
            InitialGlyph(name: ask.from ?? ask.chat ?? ask.circle, round: true).frame(width: 28, height: 28)
            VStack(alignment: .leading, spacing: 4) {
              HStack(alignment: .firstTextBaseline, spacing: 4) {
                Text(ask.chat ?? ask.from ?? ask.circle).font(AppType.rowTitle).lineLimit(1)
                Spacer(minLength: 0)
                if let at = ask.at { Text(store.relativeTime(at)).font(.system(size: 10)).foregroundStyle(AccessibleSecondary()).fixedSize() }
              }
              Text("“\(ask.text)”").font(.callout).foregroundStyle(AccessibleSecondary()).lineLimit(1).help(ask.text)
            }
          }.frame(maxWidth: .infinity, alignment: .leading).contentShape(Rectangle())
        }.limmrButton(.row(radius: 6, bordered: false))
          .accessibilityElement(children: .ignore)
          .accessibilityLabel("\(ask.from ?? ask.chat ?? ask.circle). \(ask.text). \(ask.at.map { store.relativeTime($0) + " ago" } ?? "")")
          .accessibilityHint("Review this message and reply actions")
      }
      if store.home.needsReply.count > 3 {
        Button("\(store.home.needsReply.count - 3) more") { expanded = true }
          .limmrButton().font(AppType.caption)
      }
    }.padding(16).frame(maxWidth: .infinity, alignment: .topLeading)
      .modifier(HomeTileSurface())
      .sheet(isPresented: $expanded) { ReplyAsksSheet(store: store).dismissOnEscape() }
  }
}
