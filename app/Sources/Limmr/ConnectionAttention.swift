import LimmrKit
import SwiftUI

struct ConnectionAttention: Equatable {
  enum Repair: Equatable { case restart, reconnect, link, remote(String), sync, refresh, wait }
  let title: String
  let detail: String
  let button: String
  let repair: Repair

  static func resolve(state: EngineSurfaceState, checks: [HealthCheck], remoteEnabled: Bool,
                      sync: DotSyncStatus?, refreshFailed: Bool = false) -> Self? {
    switch state {
    case .busy, .stalled, .notRunning:
      return .init(title: state == .notRunning ? "Limmr’s engine stopped" : "Limmr’s engine is busy",
        detail: "Your saved information stays here while the engine reconnects.", button: "Restart engine", repair: .restart)
    case .starting, .restarting, .updating:
      return .init(title: state.title, detail: "Your saved information stays here. Limmr reconnects automatically.",
        button: "Reconnecting…", repair: .wait)
    case .loggedOut:
      return .init(title: "WhatsApp needs linking", detail: "Scan a new code with WhatsApp on your phone.", button: "Reconnect WhatsApp", repair: .link)
    case .phoneUnreachable:
      return .init(title: "Your phone is offline", detail: "Open WhatsApp on your phone and check its internet connection.", button: "Reconnect WhatsApp", repair: .reconnect)
    case .offline:
      return .init(title: "WhatsApp disconnected", detail: "Your saved chats are still here.", button: "Reconnect WhatsApp", repair: .reconnect)
    case .connected: break
    }
    if remoteEnabled, let check = checks.first(where: { ["remote", "remote_listener"].contains($0.id) && $0.state != "ok" }) {
      return .init(title: "Remote access needs attention", detail: "AI apps outside this Mac can’t connect yet.",
        button: "Restart remote access", repair: .remote(check.fixId ?? "restart_remote"))
    }
    if let sync, sync.paired, !sync.peer.lastError.isEmpty {
      return .init(title: "Sync with another Limmr needs attention", detail: "Your information is saved on this Mac.", button: "Try sync again", repair: .sync)
    }
    if checks.contains(where: { $0.id == "engine_restarts" && $0.state != "ok" }) {
      return .init(title: "Limmr’s engine is taking a break", detail: "It restarted several times. Automatic restarts will resume shortly.", button: "Restart engine", repair: .restart)
    }
    if refreshFailed { return .init(title: "Some information couldn’t refresh", detail: "Showing the last available information.", button: "Try again", repair: .refresh) }
    return nil
  }
}

extension AppStore {
  var connectionAttention: ConnectionAttention? {
    .resolve(state: engineSurfaceState, checks: checks, remoteEnabled: remote?.enabled == true,
      sync: connectionSync, refreshFailed: refreshError != nil)
  }
  func restartEngine() async {
    await perform { [self] in
      if !demo && !snapshot {
        let readiness = await system.readiness()
        // A removed job can leave its saved credential behind. Readiness also
        // inspects the exact launchd job; start it if there is nothing to restart.
        if system.registration == .absent || readiness == .unavailable { try await system.startEngine() }
        else {
          try await system.restart()
          await system.startup.recover(using: system)
        }
      }
    }
    await refresh()
  }
  func repairConnection(_ repair: ConnectionAttention.Repair) async {
    switch repair {
    case .restart: await restartEngine()
    case .link: await connectionAction(.loggedOut)
    case .reconnect: await connectionAction(.offline)
    case .remote(let fix): await self.fix(fix)
    case .sync:
      await perform { [self] in try await backend.post("/v1/sync/pause", ["paused": false]) }
      await refresh()
    case .refresh: await refresh()
    case .wait: break
    }
  }
}

struct ConnectionStatusCard: View {
  @Bindable var store: AppStore
  var stateOverride: EngineSurfaceState? = nil
  private var attention: ConnectionAttention? {
    guard let stateOverride else { return store.connectionAttention }
    return .resolve(state: stateOverride, checks: store.checks, remoteEnabled: store.remote?.enabled == true, sync: store.connectionSync)
  }
  var body: some View {
    if let attention {
      HStack(spacing: 14) {
        if attention.repair == .wait { ProgressView().controlSize(.small) }
        VStack(alignment: .leading, spacing: 4) {
          Text(attention.title).font(.callout.weight(.medium))
          Text(attention.detail).font(.caption).foregroundStyle(AccessibleSecondary())
            .fixedSize(horizontal: false, vertical: true)
        }
        Spacer(minLength: 0)
        VStack(alignment: .trailing, spacing: 6) {
          Button(attention.button) { Task { await store.repairConnection(attention.repair) } }
            .buttonStyle(NativeGlassButtonStyle(prominent: true)).disabled(attention.repair == .wait || store.busy)
          Button("Copy diagnostics") { Task { await store.copyDiagnostics() } }
            .buttonStyle(.plain).font(.caption).foregroundStyle(AccessibleSecondary())
        }
      }
      .padding(14).background(.primary.opacity(0.035), in: RoundedRectangle(cornerRadius: 12))
      .accessibilityElement(children: .contain)
    }
  }
}
