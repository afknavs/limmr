import SwiftUI
import LimmrKit

struct SidebarPresence: View {
  @Bindable var store: AppStore
  @Environment(\.colorScheme) private var colorScheme
  @Environment(\.snapshotIncreasedContrast) private var snapshotContrast
  @Environment(\.colorSchemeContrast) private var systemContrast
  private var ink: Color {
    let strong = snapshotContrast || systemContrast == .increased
    return colorScheme == .dark ? Color(white: strong ? 0.95 : 0.76) : Color(white: strong ? 0.10 : 0.32)
  }
  var body: some View {
    VStack(alignment: .leading, spacing: 10) {
      HStack(spacing: 16) {
        ForEach(Array(store.agents.filter(\.enabled).prefix(3))) { agent in
          EntityIconButton(store: store, bucket: "agents", id: agent.id, icon: agent.icon, fallback: agent.symbol)
            .foregroundStyle(ink)
            .help(store.presence.first { $0.id == agent.id }?.line ?? "\(agent.name) · idle")
            .contextMenu {
              Button("Show activity") { store.activityAgent = agent.id; store.selectPage(.activity )}
            }
        }
      }
      if let status = store.status {
        Label(status.connected ? "WhatsApp connected" : "Reconnecting WhatsApp",
          systemImage: status.connected ? "circle.fill" : "circle")
          .font(.system(size: 10)).foregroundStyle(ink)
      }
      if store.demo {
        Text("Sample data").font(.system(size: 10)).foregroundStyle(ink)
          .padding(.horizontal, 6).padding(.vertical, 3)
          .background(.primary.opacity(0.035), in: Capsule())
      }
    }.frame(maxWidth: .infinity, alignment: .leading).padding(16)
  }
}
