import SwiftUI

struct ConnectPicker: View {
  @Bindable var store: AppStore
  @Environment(\.dismiss) private var dismiss
  var body: some View {
    ScrollView {
      VStack(alignment: .leading, spacing: 24) {
        HStack {
          Text("Connect an integration").font(.largeTitle.bold())
          Spacer()
          Button("Close") { dismiss() }.buttonStyle(NativeGlassButtonStyle())
        }
        ConnectTiles(store: store)
        SetupGuideLink()
      }
      .padding(32)
    }
    .frame(width: 540, height: 470)
  }
}

struct ConnectTiles: View {
  @Bindable var store: AppStore
  var body: some View {
    LazyVGrid(columns: [GridItem(.adaptive(minimum: 130), spacing: 16)], spacing: 16) {
      ForEach(ConnectKind.allCases) { kind in
        Button {
          store.showConnect = false
          store.connectKind = kind
        } label: {
          VStack(spacing: 12) {
            AgentIcon(symbol: kind.symbol)
            Text(kind.rawValue).font(.headline)
          }
          .frame(maxWidth: .infinity, minHeight: 92)
        }
        .buttonStyle(NativeGlassButtonStyle(tile: true))
      }
    }
  }
}
