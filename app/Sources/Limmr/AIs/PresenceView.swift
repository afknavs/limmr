import SwiftUI
import LimmrKit

struct SidebarPresence: View {
  @Bindable var store: AppStore
  @Environment(\.openSettings) private var openSettings
  private func open(_ agent: Agent) {
    store.selectSidebar("ai:" + agent.id); openSettings()
  }
  var body: some View {
    if !AIIdentity.rows(store.agents, sync: store.connectionSync).isEmpty {
      Section("Your AIs") {
        ForEach(AIIdentity.rows(store.agents, sync: store.connectionSync)) { agent in
          Button { open(agent) } label: {
            TimelineView(.periodic(from: .now, by: 60)) { timeline in
              HStack(spacing: 8) {
                Image(systemName: AIIdentity.symbol(agent, sync: store.connectionSync))
                  .symbolRenderingMode(.hierarchical)
                VStack(alignment: .leading, spacing: 2) {
                  Text(AIIdentity.name(agent, sync: store.connectionSync)).lineLimit(1)
                  Text(store.lastSeen(agent)).font(.caption).foregroundStyle(.secondary)
                }
                Spacer(minLength: 0)
                let now = store.usesFixtureClock ? store.presentationNow : timeline.date
                Circle().fill((AIIdentity.isLinked(agent, sync: store.connectionSync) ? store.connectionSync?.paused == false && store.connectionSync?.peer.lastError.isEmpty == true : AIIdentity.isPresent(agent, now: now)) ? Color.accentColor : Color.secondary.opacity(0.4))
                  .frame(width: 5, height: 5)
              }.contentShape(Rectangle())
            }
          }.buttonStyle(.plain)
            .accessibilityLabel(AIIdentity.name(agent, sync: store.connectionSync) + ". " + store.lastSeen(agent))
            .accessibilityHint("Open this AI in Integrations")
            .contextMenu {
              Button("Open integration") { open(agent) }
              Button("Show activity") { store.activityAgent = agent.id; store.selectPage(.activity) }
              Button("Change symbol…") { open(agent) }
            }
        }
      }
    }
  }
}
