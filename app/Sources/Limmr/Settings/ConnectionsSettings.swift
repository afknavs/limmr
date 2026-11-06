import LimmrKit
import SwiftUI

struct ConnectionsSettings: View {
  @Bindable var store: AppStore
  var body: some View {
    Section {
      connectionRow("Limmr’s engine", state: engineState, healthy: ![EngineSurfaceState.busy, .stalled, .notRunning, .starting, .restarting, .updating].contains(store.engineSurfaceState),
        action: "Restart engine") { Task { await store.restartEngine() } }
      connectionRow("WhatsApp", state: whatsappState, healthy: store.status?.connected == true,
        action: store.status?.linked == true ? "Reconnect" : "Link WhatsApp") {
          Task { await store.connectionAction(store.status?.linked == true ? .offline : .loggedOut) }
        }.settingsMatch("WhatsApp", store: store)
      connectionRow("Remote access", state: store.remote?.enabled == true ? remoteState : "Off",
        healthy: store.remote?.enabled == true && remoteState == "Connected",
        action: store.remote?.enabled == true ? "Manage" : "Set up") {
          if store.remote?.enabled == true { store.connectionRemote.toggle() }
          else { store.showRemote = true }
        }.settingsMatch("Allow remote connections", store: store)
      YourDotSection(store: store).settingsMatch("Sync with another Limmr", store: store)
    } header: { SettingsPaneTitle(title: "Connections") }
    if store.connectionRemote { RemoteSettings(store: store) }
    Section {
      DisclosureGroup("Details", isExpanded: $store.connectionDetails) {
        HealthView(store: store).settingsMatch("Health", store: store)
      }
    }
    Section {
      DisclosureGroup("Advanced", isExpanded: $store.connectionAdvanced) {
        HStack {
          Button("Run all checks") { Task { await store.refresh() } }
            .settingsMatch("Run all checks", store: store)
          Spacer()
        }
        AdvancedSettings(store: store)
        DisclosureGroup("Technical checks") {
          ForEach(store.checks) { check in
            VStack(alignment: .leading, spacing: 4) {
              Text(check.title).font(.callout.weight(.medium))
              Text(check.detail).font(.caption).foregroundStyle(AccessibleSecondary())
            }.textSelection(.enabled)
          }
        }
      }
    }
    ActionFeedback(store: store)
  }
  private var engineState: String {
    switch store.engineSurfaceState {
    case .busy, .stalled: "Busy"
    case .notRunning: "Stopped"
    case .starting: "Starting"
    case .restarting: "Restarting"
    case .updating: "Updating"
    default: "Running"
    }
  }
  private var whatsappState: String {
    store.status?.connected == true ? "Connected" : store.status?.linked == true ? "Reconnecting" : "Needs linking"
  }
  private var remoteState: String {
    store.checks.contains { ["remote", "remote_listener"].contains($0.id) && $0.state != "ok" } ? "Needs attention" : "Connected"
  }
  private func connectionRow(_ name: String, state: String, healthy: Bool, action: String,
                             perform: @escaping () -> Void) -> some View {
    HStack(spacing: 10) {
      Circle().fill(healthy ? Color.green : Color.secondary).frame(width: 6, height: 6).accessibilityHidden(true)
      Text(name)
      Spacer()
      Text(state).font(.callout).foregroundStyle(AccessibleSecondary())
      Button(action, action: perform).buttonStyle(NativeGlassButtonStyle())
    }.padding(.vertical, 5)
  }
}
