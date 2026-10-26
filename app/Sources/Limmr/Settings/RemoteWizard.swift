import SwiftUI
import LimmrKit

struct RemoteWizard: View {
  @Bindable var store: AppStore
  @State private var ownURL = false
  @State private var address = ""
  @State private var port = 443
  @State private var routes: [FunnelPort] = []
  @State private var showDetails = false
  // Inject both effects for offline tests. Defaults run only after an explicit click.
  var setup: RemoteSetup?
  @State private var detected = false
  @State private var verifying = false
  @State private var reachable = false
  @State private var progress = ""
  @State private var warningSnapshot: RemoteWizardStatus?
  @State private var showAddressWarning = false
  @State private var offerTurnOff = false
  @State private var verificationTask: Task<Void, Never>?
  @Environment(\.dismiss) private var dismiss
  var body: some View {
    ScrollView {
      VStack(alignment: .leading, spacing: 16) {
        HStack {
          Text("Remote access").font(.largeTitle.bold())
          Spacer()
          Button {
            dismiss()
          } label: {
            Image(systemName: "xmark").frame(width: 20, height: 20)
          }.buttonStyle(NativeGlassButtonStyle())
        }
        Text("Tailscale Funnel gives this Mac a secure address for ChatGPT and Claude.")
          .font(.callout).foregroundStyle(AccessibleSecondary())
        if !reachable {
          VStack(alignment: .leading, spacing: 16) {
            Picker("Connection", selection: $ownURL) {
              Text("Use Tailscale").tag(false)
              Text("I have my own HTTPS URL").tag(true)
            }.pickerStyle(.segmented)
            if ownURL {
              TextField("https://your-address.example", text: $address).textFieldStyle(.roundedBorder)
              Text(
                "Point your tunnel to Limmr’s remote listener. Verify the address before adding your AI connector."
              ).font(.system(size: 13)).foregroundStyle(AccessibleSecondary())
              DisclosureGroup("Details", isExpanded: $showDetails) {
                Text("Remote listener: http://127.0.0.1:18801").font(
                  .system(size: 12, design: .monospaced))
              }
            } else {
              StepRow(
                number: 1, title: "Check Tailscale",
                detail:
                  "Check which Funnel ports are already in use."
              )
              Button("Check available ports") { Task { await detect() } }.buttonStyle(
                NativeGlassButtonStyle()
              ).padding(.leading, 40)
              if detected {
                Picker("Use port", selection: $port) {
                  ForEach([443, 8443, 10000], id: \.self) { p in
                    Text(portLabel(p)).tag(p)
                      .disabled(p != 443 && routes.first { $0.port == p }?.protected == true)
                  }
                }.padding(.leading, 40)
              }
              if port == 443, FunnelPorts.shares443(selectedRoute) {
                Text("Share port 443 (recommended for ChatGPT)").font(.headline).padding(.leading, 40)
                Text("Limmr uses /limmr. Your other apps keep their existing addresses.")
                  .font(.callout).foregroundStyle(AccessibleSecondary()).padding(.leading, 40)
              } else if port != 443 {
                Text("May not work with ChatGPT").font(.callout).foregroundStyle(AccessibleSecondary()).padding(.leading, 40)
              }
              DisclosureGroup("Details", isExpanded: $showDetails) {
                ForEach(routes) { route in
                  Text(":\(String(route.port)) · " + route.targetDescription)
                    .font(.caption).textSelection(.enabled)
                }
                Text(commandDetails).font(.system(size: 11, design: .monospaced)).textSelection(.enabled)
              }
              StepRow(
                number: 2, title: "Open your private connection",
                detail:
                  "New integrations need your approval. Discovery can take 10–15 minutes."
              )
            }
            if verifying {
              HStack {
                ProgressView().controlSize(.small)
                Text(progress).font(.system(size: 12)).foregroundStyle(AccessibleSecondary())
              }
            }
          }.disabled(verifying)
        }
        if reachable {
          Label("Ready for ChatGPT", systemImage: "checkmark.circle").font(.headline)
          CopyField(title: "MCP address", value: address + "/mcp")
        }
        if offerTurnOff {
          Text("Verification didn’t finish. Remote access is still on because an approved AI may be using it.")
            .font(.callout)
          Button("Turn off") {
            Task {
              await store.perform {
                try await RemoteWizardTransaction(backend: store.backend).turnOff()
                offerTurnOff = false
                await store.refresh()
              }
            }
          }.buttonStyle(NativeGlassButtonStyle())
        }
        ActionFeedback(store: store)

      }.padding(24)
    }
    .safeAreaInset(edge: .bottom) {
      HStack {
        Text("Connect your AI after verification.").font(.caption).foregroundStyle(AccessibleSecondary())
        Spacer()
        Button(reachable ? "Done" : ownURL ? "Verify address" : port == 443 && FunnelPorts.shares443(selectedRoute) ? "Share port 443" : "Enable Funnel") {
          if reachable { dismiss() } else { verificationTask = Task { await prepare() } }
        }
        .buttonStyle(NativeGlassButtonStyle(prominent: true))
        .disabled(
          verifying || (!ownURL && (!detected || !canUsePort))
            || (ownURL && !validAddress))
      }
      .padding(16).background(Color(nsColor: .windowBackgroundColor))
    }
    .frame(width: 640).frame(minHeight: 340, idealHeight: 600, maxHeight: 600)
    .background(Color(nsColor: .windowBackgroundColor))
    .alert("Your AI will need to reconnect", isPresented: $showAddressWarning) {
      Button("Change address") {
        if let snapshot = warningSnapshot {
          verificationTask = Task { await enable(snapshot: snapshot) }
        }
      }
      Button("Cancel", role: .cancel) { warningSnapshot = nil }
    } message: {
      Text("ChatGPT will need to reconnect at the new address. Restoring the old address restores its unexpired credentials.")
    }
    .onDisappear { verificationTask?.cancel() }
    .onChange(of: port) { _, _ in reachable = false }
    .onChange(of: address) { _, _ in if !verifying { reachable = false } }
    .onChange(of: ownURL) { _, _ in if !verifying { reachable = false } }
  }
  init(store: AppStore, setup: RemoteSetup? = nil) {
    self.store = store
    self.setup = setup
    if store.snapshot {
      _routes = State(initialValue: FunnelPorts.routes(Data(Self.demoRoutes.utf8)))
      _port = State(initialValue: 443)
      _detected = State(initialValue: true)
      if ProcessInfo.processInfo.arguments.contains("--remote-ready") {
        _address = State(initialValue: "https://limmr.example/limmr")
        _reachable = State(initialValue: true)
      }
    }
  }

  var validAddress: Bool {
    RemoteSetup.validAddress(address.trimmingCharacters(in: .whitespacesAndNewlines).trimmingCharacters(in: CharacterSet(charactersIn: "/")))
  }
  static let demoRoutes = #"{"Web":{"limmr.example:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:18771"}}}}}"#
  var selectedRoute: FunnelPort? { routes.first { $0.port == port } }
  var canUsePort: Bool { port == 443 || selectedRoute == nil || selectedRoute?.protected == false }
  func portLabel(_ p: Int) -> String {
    if p == 443 { return FunnelPorts.shares443(routes.first { $0.port == 443 }) ? "Share port 443 (recommended for ChatGPT)" : "Port 443 · Recommended for ChatGPT" }
    return ":\(p) · May not work with ChatGPT" + (routes.first { $0.port == p }?.protected == true ? " · In use" : "")
  }
  var commandDetails: String {
    FunnelPorts.plan(port: port, sharing: port == 443 && FunnelPorts.shares443(selectedRoute))
      .map { "tailscale " + FunnelPorts.command($0).joined(separator: " ") }.joined(separator: "\n")
  }
  func connection() throws -> RemoteSetup {
    if let setup { return setup }
    return try store.system.remoteConnection()
  }
  func detect() async {
    await store.perform {
      routes = store.demo ? FunnelPorts.routes(Data(Self.demoRoutes.utf8)) : try await connection().inspect()
      detected = true
      port = 443
    }
  }
  func prepare() async {
    verifying = true
    defer { verifying = false }
    await store.perform {
      let snapshot = try await RemoteWizardTransaction(backend: store.backend).snapshot()
      let target = address.trimmingCharacters(in: .whitespacesAndNewlines)
        .trimmingCharacters(in: CharacterSet(charactersIn: "/"))
      if snapshot.needsAddressWarning(target: ownURL ? target : nil) {
        warningSnapshot = snapshot
        showAddressWarning = true
      } else {
        await enable(snapshot: snapshot)
      }
    }
  }
  func enable(snapshot: RemoteWizardStatus) async {
    verifying = true
    offerTurnOff = false
    defer { verifying = false }
    await store.perform {
      var url = address.trimmingCharacters(in: .whitespacesAndNewlines).trimmingCharacters(in: CharacterSet(charactersIn: "/"))
      if !ownURL && !store.demo {
        url = try await connection().enable(port: port, inspected: routes)
        ownURL = true
      }
      if store.demo { url = "https://limmr.example/limmr" }
      address = url
      progress = "Waiting for public discovery. This can take 10–15 minutes…"
      let result = await RemoteWizardTransaction(backend: store.backend).run(before: snapshot, url: url) {
        if store.demo { return true }
        let verifier = try connection()
        for _ in 0..<90 {
          try Task.checkCancellation()
          if (try? await verifier.verify(url)) == true {
            try Task.checkCancellation()
            return true
          }
          try await Task.sleep(for: .seconds(10))
        }
        return false
      }
      reachable = result == .ready
      offerTurnOff = result == .clients
      if result != .ready {
        store.notice = result.message
      }
      await store.refresh()
    }
  }
}

struct RemoteWizardConfig: Codable, Equatable, Sendable {
  var enabled: Bool
  var publicUrl: String
}
struct RemoteWizardStatus: Decodable, Sendable {
  var enabled: Bool
  var publicUrl: String
  var clients: [Client]
  var clientsOnOtherUrl: Int?
  var clientsWithTokens: Int?
  struct Client: Decodable, Sendable { var name: String }
  var config: RemoteWizardConfig { .init(enabled: enabled, publicUrl: publicUrl) }
  func needsAddressWarning(target: String?) -> Bool {
    guard target == nil || target != publicUrl else { return false }
    // Older engines lack token counts; conservatively warn for approved clients.
    return (clientsOnOtherUrl ?? 0) > 0 || (clientsWithTokens ?? clients.count) > 0
  }
}

/// All failure paths, including sheet dismissal, use the same guarded restore.
/// The engine rechecks the configuration and client list atomically with the write.
struct RemoteWizardTransaction: Sendable {
  let backend: any Backend
  enum Result: Equatable, Sendable {
    case ready, restored, changed, clients, unknown
    var message: String {
      switch self {
      case .ready: "Ready for ChatGPT."
      case .restored: "Verification didn’t finish. Your previous remote access settings were restored."
      case .changed: "Remote access changed during verification. Those settings were left alone."
      case .clients: "Verification didn’t finish. Remote access was kept on for your approved AI connections. You can turn it off here."
      case .unknown: "Verification didn’t finish. Remote access couldn’t be safely restored. Check Remote access before trying again."
      }
    }
  }
  struct Write: Encodable, Sendable {
    var enabled: Bool
    var publicUrl: String
    var onlyIf: RemoteWizardConfig
    var requireNoClients: Bool = false
  }
  func snapshot() async throws -> RemoteWizardStatus { try await backend.get("/v1/remote") }
  @MainActor func run(before: RemoteWizardStatus, url: String,
    verify: () async throws -> Bool) async -> Result {
    let intended = RemoteWizardConfig(enabled: true, publicUrl: url)
    do {
      try Task.checkCancellation()
      try await backend.post("/v1/remote", Write(enabled: true, publicUrl: url, onlyIf: before.config))
      if try await verify() {
        try Task.checkCancellation()
        return .ready
      }
    } catch { /* A failed response may still have applied the write. Check below. */ }
    // A cancelled URLSession cannot perform cleanup. This task has an independent
    // cancellation lifetime and is awaited before reporting the outcome.
    return await Task.detached { await restore(before: before.config, intended: intended) }.value
  }
  private func restore(before: RemoteWizardConfig, intended: RemoteWizardConfig) async -> Result {
    do {
      let current = try await snapshot()
      guard current.config == intended else { return .changed }
      guard current.clients.isEmpty else { return .clients }
      try await backend.post("/v1/remote", Write(enabled: before.enabled, publicUrl: before.publicUrl,
        onlyIf: intended, requireNoClients: true))
      return .restored
    } catch {
      // A client may have approved or another wizard may have changed the URL
      // between GET and POST. Read back the reason; never retry the mutation.
      if let current = try? await snapshot() {
        if current.config != intended { return .changed }
        if !current.clients.isEmpty { return .clients }
      }
      return .unknown
    }
  }
  func turnOff() async throws {
    let current = try await snapshot()
    try await backend.post("/v1/remote", Write(enabled: false, publicUrl: current.publicUrl,
      onlyIf: current.config))
  }
}
