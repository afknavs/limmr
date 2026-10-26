import LimmrKit
import SwiftUI

/// Presentation uses observed connection facts; missing phone evidence is not a failure.
enum EngineSurfaceState: String, CaseIterable {
  case starting, restarting, notRunning, updating, offline, loggedOut, phoneUnreachable, connected

  var title: String {
    switch self {
    case .starting: "Starting Limmr"
    case .restarting: "Reconnecting to Limmr"
    case .notRunning: "Limmr isn’t running"
    case .updating: "Updating Limmr"
    case .offline: "WhatsApp is offline"
    case .loggedOut: "Link WhatsApp"
    case .phoneUnreachable: "Check your phone’s connection"
    case .connected: "WhatsApp connected"
    }
  }
  var detail: String {
    switch self {
    case .starting, .restarting: "Your chats will appear when the connection is ready."
    case .notRunning: "Start the background helper to reconnect your chats."
    case .updating: "Your chats will reconnect when the update finishes."
    case .offline: "Limmr will reconnect automatically. Your saved chats are still here."
    case .loggedOut: "Link a device to bring your chats to this Mac."
    case .phoneUnreachable: "Open WhatsApp on your phone and check its internet connection."
    case .connected: "Your linked device is connected."
    }
  }
  var action: String {
    switch self {
    case .loggedOut: "Link WhatsApp"
    case .notRunning: "Start Limmr"
    case .phoneUnreachable: "Check again"
    case .starting, .restarting, .updating, .connected: "Open Limmr"
    case .offline: "Reconnect"
    }
  }
  var inProgress: Bool { [.starting, .restarting, .updating].contains(self) }
}

extension AppStore {
  var engineSurfaceState: EngineSurfaceState {
    if !demo && !snapshot {
      switch system.startup.phase {
      case .starting: return .starting
      case .restarting: return .restarting
      case .updating: return .updating
      case .failed: return .notRunning
      default: break
      }
    }
    // A failed latest status read wins over the last cached connected status.
    if loadFailures.keys.contains(where: { $0.hasPrefix("/v1/status") }) { return .notRunning }
    guard let status else { return .starting }
    guard status.linked else { return .loggedOut }
    if checks.contains(where: {
      $0.id == "phone" && $0.state != "ok"
        && ($0.detail.localizedCaseInsensitiveContains("unreachable")
          || $0.detail.localizedCaseInsensitiveContains("offline"))
    }) { return .phoneUnreachable }
    return status.connected ? .connected : .offline
  }

  func connectionAction(_ state: EngineSurfaceState) async {
    if state == .loggedOut {
      setupStep = 1
      showSetup = true
    } else if state == .offline {
      await perform { [self] in try await backend.post("/v1/fix/reconnect") }
      await refresh()
    } else if state == .notRunning {
      // A cached status can outlive the helper; explicitly recover on this user action.
      if !demo && !snapshot {
        await perform { [self] in try await system.startEngine() }
      }
      await refresh()
    } else {
      await reconnect()
    }
  }
}

struct EngineStatusBanner: View {
  let state: EngineSurfaceState
  var action: () -> Void
  var body: some View {
    HStack(spacing: 12) {
      if state.inProgress { ProgressView().controlSize(.small).accessibilityLabel(state.title) }
      VStack(alignment: .leading, spacing: 4) {
        Text(state.title).font(.callout.weight(.medium))
        Text(state.detail).font(.caption).foregroundStyle(AccessibleSecondary())
          .fixedSize(horizontal: false, vertical: true)
      }
      Spacer(minLength: 0)
      Button(state.action, action: action).buttonStyle(NativeGlassButtonStyle())
        .accessibilityLabel(state.action)
    }
    .padding(14).background(.primary.opacity(0.035), in: RoundedRectangle(cornerRadius: 12))
    .accessibilityElement(children: .contain)
  }
}
