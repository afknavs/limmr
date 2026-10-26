import AppKit
import LimmrKit
import SwiftUI
import UserNotifications

struct ConnectView: View {
  @Bindable var store: AppStore
  let kind: ConnectKind
  @State private var created: CreatedAgent?
  @State private var selectedProfile = "new"
  @State private var finished = false
  var initialAccess = AgentAccess()
  @State private var access = AgentAccess()
  @State private var preset = "own"
  @Environment(\.dismiss) private var dismiss

  var body: some View {
    ScrollView {
      VStack(alignment: .leading, spacing: 12) {
        HStack(spacing: 12) {
          AgentIcon(symbol: kind.symbol)
          Text("Connect \(kind.rawValue)").font(.largeTitle.bold())
          Spacer()
        }
        if kind.remote { remoteSteps } else { localSteps }
        if pendingCode == nil {
          CopySetupPrompt(kind: kind, approved: created != nil, token: created?.token,
                          remoteURL: store.remote?.mcpUrl)
        }
        if pendingCode == nil {
          Divider()
          IntegrationSkill(kind: kind, sample: store.demo || store.snapshot)
          if finished { StarterPrompt() }
        }

      }
      .padding(24)
    }
    .safeAreaInset(edge: .bottom) {
      VStack(alignment: .leading, spacing: 12) {
        ActionFeedback(store: store)
        if kind.remote, let code = pendingCode {
          HStack {
            Button("Deny") { approve(code, allow: false) }.buttonStyle(NativeGlassButtonStyle())
            Button("Approve") { approve(code, allow: true) }
              .buttonStyle(NativeGlassButtonStyle(prominent: true))
            Spacer()
            Button("Close") { dismiss() }.buttonStyle(NativeGlassButtonStyle())
          }
          .disabled(store.busy)
        } else {
          HStack {
            Spacer()
            Button(created == nil && !finished ? "Close" : "Review permissions") {
              if let created {
                store.reviewIntegration(created.agent.id)
              } else if finished {
                store.reviewIntegration(store.selectedAgent)
              }
              dismiss()
            }
            .buttonStyle(NativeGlassButtonStyle())
          }
        }
      }
      .padding(16).background(Color(nsColor: .windowBackgroundColor))
    }
    .frame(width: 580).frame(minHeight: 340, idealHeight: 620, maxHeight: 620)
    .background(Color(nsColor: .windowBackgroundColor))
    .onAppear {
      access = initialAccess
      if initialAccess.circles != .all { preset = "given" }
      selectedProfile =
        store.agents.first { $0.kind == kind.kind && $0.transport == "remote" }?.id ?? "new"
    }
    .sheet(isPresented: $store.showRemote) { RemoteWizard(store: store).dismissOnEscape() }
  }

  private var pendingCode: RemoteCode? {
    let pending =
      store.remote?.pendingCodes.filter {
        ($0.kind == kind.kind || (kind == .chatgpt && $0.kind == "custom")) && $0.state == "waiting" && $0.sync != true
      } ?? []
    return pending.first { $0.code == RemotePairingReview.shared.selectedCode } ?? pending.first
  }

  @ViewBuilder private var remoteSteps: some View {
    if finished {
      Label("Connected", systemImage: "checkmark").font(.headline)
      Text("Your connection is ready. Start a conversation with your AI to set up Limmr.").font(
        .callout
      ).foregroundStyle(AccessibleSecondary())
    } else if store.remote?.enabled != true {
      StepRow(
        number: 1, title: "Turn on remote access",
        detail: "Give \(kind.rawValue) a secure connection to this Mac.")
      Button("Set up remote access") { store.showRemote = true }
        .buttonStyle(NativeGlassButtonStyle(prominent: true))
    } else if let code = pendingCode {
      AIPresetChoice(
        preset: Binding(
          get: { preset },
          set: { kind in
            preset = kind
            if kind == "given" { access = AgentAccess(circles: .only([]), unsorted: false) }
          }))
      Divider()
      Text("\(code.clientName) wants to connect").font(.headline)
      Text("Match this code in your browser. Returning to \(code.returnsTo).").font(.callout)
        .foregroundStyle(AccessibleSecondary())
      Text(code.code.prefix(3) + " " + code.code.suffix(3))
        .font(.system(size: 30, weight: .medium, design: .monospaced)).tracking(4)
      Picker("Connect as", selection: $selectedProfile) {
        ForEach(store.agents.filter { $0.transport == "remote" }) { Text($0.name).tag($0.id) }
        Text("New \(kind.rawValue) profile").tag("new")
      }
      Divider()

      if preset == "given" { AccessControls(access: $access, circles: store.circles) }

    } else {
      StepRow(
        number: 1, title: "Open \(kind.rawValue) settings",
        detail: kind == .chatgpt
          ? "Go to Apps → Advanced and turn on Developer mode."
          : "Go to Connectors → Add custom connector.")
      StepRow(
        number: 2, title: kind == .chatgpt ? "Create an app" : "Add a connector",
        detail: "Name it WhatsApp (Limmr), use the connection address, and choose OAuth.")
      CopyField(title: "Connection address", value: store.remote?.mcpUrl ?? "")
      StepRow(
        number: 3, title: "Match the code, then approve",
        detail: "Check that the six-digit code in \(kind.rawValue) matches this one.")
      if pendingCode != nil {
        Text("Sending stays off until you allow it.").font(.callout).foregroundStyle(AccessibleSecondary())

      } else {
        Text("Waiting for a connection request…").foregroundStyle(AccessibleSecondary())
      }
    }
  }

  @ViewBuilder private var localSteps: some View {
    if finished {
      Label("Connected", systemImage: "checkmark").font(.headline)
      Text("Your connection is ready. Start a conversation with your AI to set up Limmr.").font(
        .callout
      ).foregroundStyle(AccessibleSecondary())
    } else {
      VStack(alignment: .leading, spacing: 16) {
        Text("Your own AI · Full access").font(.headline)
        Text("Sending follows each circle’s rule and the integration’s permission to send.").font(.caption)
          .foregroundStyle(AccessibleSecondary())
        StepRow(
          number: 1, title: "Create its connection",
          detail:
            "Keep \(kind.rawValue)’s access and activity separate from your other integrations."
        )
        if created == nil {
          Button("Create connection") {
            Task {
              await store.perform {
                let data = try await store.backend.request(
                  "/v1/agents", method: "POST",
                  body: Wire.data([
                    "name": kind.rawValue, "kind": kind.kind, "transport": "local",
                  ]))
                created = try Wire.decode(CreatedAgent.self, from: data)
                await store.refresh()
              }
            }
          }
          .buttonStyle(NativeGlassButtonStyle(prominent: true)).disabled(store.busy)
        }
        StepRow(
          number: 2, title: "Add it to \(kind.rawValue)",
          detail: "Connect once, then choose which chats it can see.")
        if let token = created?.token {
          if kind == .codex || kind == .claudeCode {
            Button(finished ? "Connected" : "Connect") {
              Task {
                await store.perform {
                  if kind == .codex {
                    try store.system.installCodex(token: token)
                  } else {
                    try await store.system.installClaude(token: token)
                  }
                  finished = true
                }
              }
            }
            .buttonStyle(NativeGlassButtonStyle(prominent: true)).disabled(store.busy || finished)
          }
          DisclosureGroup("Show details") {
            CopyField(title: "Connection configuration · keep private", value: configuration(token))
          }
        }
        StepRow(
          number: 3, title: "Review its permissions",
          detail: "Choose what it can see, and whether it can send.")
      }
    }
  }

  private func configuration(_ token: String) -> String {
    if kind == .claudeCode {
      return
        "claude mcp add --scope user --transport http limmr http://127.0.0.1:18800/mcp --header \"Authorization: Bearer \(token)\""
    }
    if kind == .codex {
      return
        "[mcp_servers.limmr]\nurl = \"http://127.0.0.1:18800/mcp\"\nhttp_headers = { Authorization = \"Bearer \(token)\" }"
    }
    let object = [
      "mcpServers": [
        "limmr": [
          "url": "http://127.0.0.1:18800/mcp", "headers": ["Authorization": "Bearer \(token)"],
        ] as [String: Any]
      ]
    ]
    guard
      let data = try? JSONSerialization.data(
        withJSONObject: object, options: [.prettyPrinted, .sortedKeys])
    else { return "" }
    return String(decoding: data, as: UTF8.self)
  }

  private func approve(_ code: RemoteCode, allow: Bool) {
    Task {
      await store.perform {
        let choice = RemoteApprovalChoice(
          code: code.code, agentId: selectedProfile == "new" ? nil : selectedProfile,
          newAgent: selectedProfile == "new" ? ["name": kind.rawValue, "kind": kind.kind] : nil,
          access: access, preset: preset)
        let previous = Set(store.agents.map(\.id))
        if allow {
          try await store.backend.post("/v1/remote/approve", choice)
        } else {
          try await store.backend.post("/v1/remote/deny", ["code": code.code])
        }
        await store.refresh()
        if allow {
          let selected =
            selectedProfile == "new"
            ? store.agents.first { !previous.contains($0.id) && $0.transport == "remote" }?.id
            : selectedProfile
          if let selected { store.selectedAgent = selected }
          finished = true
          store.notice = "Connected. Ask your AI to set up Limmr."
        }
      }
    }
  }
}

/// Fed by a dedicated app-lifetime poll as well as refresh.
/// Demo/snapshot refreshes have no notification or window effects.
@MainActor final class RemotePairingReview {
  static let shared = RemotePairingReview()
  private weak var owner: AppStore?
  private var seen = Set<String>()
  private var notified = Set<String>()
  private(set) var selectedCode: String?
  private let isActive: @MainActor () -> Bool
  private let notify: @MainActor (AppStore, String) -> Void
  private let clearNotifications: @MainActor ([String]) -> Void
  var openMainWindow: @MainActor () -> Void = {
    NSApp.windows.first { $0.title == "Limmr" }?.makeKeyAndOrderFront(nil)
    NSApp.activate(ignoringOtherApps: true)
  }
  init(
    isActive: @escaping @MainActor () -> Bool = { NSApp.isActive },
    notify: @escaping @MainActor (AppStore, String) -> Void = RemotePairingReview.notify,
    clearNotifications: @escaping @MainActor ([String]) -> Void = {
      UNUserNotificationCenter.current().removeDeliveredNotifications(withIdentifiers: $0)
    }
  ) {
    self.isActive = isActive
    self.notify = notify
    self.clearNotifications = clearNotifications
  }
  private static func notify(store: AppStore, id: String) {
    Task {
      try? await store.notifications.authorize()
      guard
        store.remote?.pendingCodes.contains(where: {
          $0.state == "waiting" && $0.code + $0.requestedAt == id
        }) == true
      else { return }
      let content = UNMutableNotificationContent()
      content.title = "An AI wants to connect"
      content.body = "Match the six-digit code and review access in Limmr."
      try? await UNUserNotificationCenter.current().add(
        .init(identifier: id, content: content, trigger: nil))
    }
  }
  func select(_ code: String) { selectedCode = code }
  func update(store: AppStore) {
    guard !store.demo, !store.snapshot else { return }
    if owner !== store {
      owner = store
      seen = []
      notified = []
    }
    let pending = (store.remote?.pendingCodes ?? []).filter { $0.state == "waiting" && $0.sync != true }
      .sorted { $0.requestedAt < $1.requestedAt }
    let identifiers = Set(pending.map { $0.code + $0.requestedAt })
    seen.formIntersection(identifiers)
    let removed = Array(notified.subtracting(identifiers))
    if !removed.isEmpty { clearNotifications(removed) }
    notified.formIntersection(identifiers)
    guard let next = pending.first(where: { !seen.contains($0.code + $0.requestedAt) }) else {
      return
    }
    let id = next.code + next.requestedAt
    if !isActive(), !notified.contains(id) {
      notified.insert(id)
      notify(store, id)
    }
    // Don't replace a sheet the owner is already reviewing. Unseen requests are queued.
    guard store.connectKind == nil, !store.showRemote, !store.showConnect else { return }
    selectedCode = next.code
    openMainWindow()
    store.connectKind = next.kind == "claude_ai" ? .claude : .chatgpt
    if !store.showSetup { store.showSettings = true }
    seen.insert(id)
  }
}
