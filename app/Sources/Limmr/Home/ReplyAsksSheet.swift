import SwiftUI

struct ReplyAsksSheet: View {
  @Bindable var store: AppStore
  @Environment(\.dismiss) private var dismiss
  var body: some View {
    ScrollView {
      VStack(alignment: .leading, spacing: 16) {
        HStack {
          Text("Needs a reply").font(.title2.bold())
          Spacer()
          Button("Done") { dismiss() }
        }
        ForEach(store.home.needsReply) { ReplyAskRow(store: store, ask: $0) }
      }.padding(24)
    }.frame(width: 640).frame(minHeight: 300, idealHeight: 560, maxHeight: 560)
  }
}
