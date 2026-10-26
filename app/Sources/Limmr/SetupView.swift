import LimmrKit
import SwiftUI

struct SetupView: View {
  @Bindable var store: AppStore
  var initialStep = 0
  @State private var flow = SetupFlow()
  @State private var phoneMode = false
  @State private var phone = ""
  @State private var preparing = false
  @State private var choseQR = false
  @State private var startedAt = Date()
  @State private var compact = false
  @State private var setupChoice = "ai"
  @State private var showingAbout = false
  var initialAbout = false
  var initialPhoneMode = false
  var initialHelperPreparing = false

  var body: some View {
    GeometryReader { geometry in
      ScrollView {
        VStack(spacing: geometry.size.height < 650 ? 16 : 24) {
          HStack(spacing: 8) {
            ForEach(SetupFlow.Step.allCases, id: \.rawValue) { step in
              Circle()
                .fill(step == flow.step ? Color.primary : Color.primary.opacity(0.12))
                .frame(width: 8, height: 8)
            }
          }
          .accessibilityLabel("Step \(flow.step.rawValue + 1) of \(SetupFlow.Step.allCases.count)")
          content
          if !pinnedFooter { footer }
        }
        .multilineTextAlignment(.center)
        .frame(maxWidth: 520)
        .padding(24)
        .frame(
          maxWidth: .infinity, minHeight: max(0, geometry.size.height - (pinnedFooter ? 120 : 48)))
      }
      .safeAreaInset(edge: .top, spacing: 0) {
        HStack {
          Spacer()
          Button("Skip") { store.showSetup = false }.limmrButton()
            .accessibilityLabel("Finish setup later")
        }
        .padding(.horizontal, 24).frame(height: 48)
      }
      .safeAreaInset(edge: .bottom, spacing: 0) {
        if pinnedFooter {
          footer.frame(maxWidth: 520).padding(.horizontal, 32).padding(.bottom, 24)
            .frame(maxWidth: .infinity)
        }
      }
    }
    .onGeometryChange(for: Bool.self) {
      $0.size.height < 650
    } action: {
      compact = $0
    }
    .background { AppBackground() }
    .onAppear {
      phoneMode = initialPhoneMode
      showingAbout = initialAbout
      preparing = initialHelperPreparing
      flow = SetupFlow(step: .init(rawValue: initialStep) ?? .welcome)
      if store.snapshot, !store.demo, initialStep == 1 { flow.helperFailed() }
    }
    .task(id: flow.step) {
      if flow.step == .link, !store.snapshot { await prepareLink() }
    }
    .task(id: flow.step != .welcome && flow.helperError == nil && !preparing) {
      if flow.step != .welcome, flow.helperError == nil, !preparing, !store.snapshot {
        // Keep listening through first sync; changing pages must not cancel its progress.
        await store.linkEvents()
      }
    }
    .onChange(of: store.link.code) { _, _ in startedAt = Date() }
    .onChange(of: store.link.state) { _, value in
      if ["syncing", "ready"].contains(value), flow.step == .link {
        flow.advance(linkReady: true)
      }
      if value == "error", flow.step == .syncing { flow.step = .link }
    }
  }

  @ViewBuilder private var content: some View {
    if showingAbout {
      AboutYouCard(store: store, compact: true)
    } else {
      switch flow.step {
      case .welcome:
        VStack(spacing: 24) {
          Image(systemName: "bubble.left.and.text.bubble.right")
            .font(.system(size: 64, weight: .light)).padding(.vertical, 16)
          Text("Connect WhatsApp").font(.largeTitle.bold())
          Text(
            "Choose which chats your integrations can read, and set a send rule for each circle."
          )
          .font(.title3).foregroundStyle(AccessibleSecondary())
          Label("Your data stays on this Mac.", systemImage: "lock")
            .font(.callout).foregroundStyle(AccessibleSecondary())
        }
      case .link: linking
      case .syncing:
        Text("Syncing your chats").font(.largeTitle.bold())
        ProgressView(
          value: store.link.progress.map { min(1, max(0, $0 / 100)) } ?? (store.demo ? 0.67 : nil))
        HStack(spacing: 32) {
          VStack(spacing: 4) {
            Text((store.link.chats ?? (store.demo ? 128 : 0)).formatted()).font(.title2.weight(.semibold)).monospacedDigit()
            Text("Chats").font(.caption).foregroundStyle(AccessibleSecondary())
          }
          VStack(spacing: 4) {
            Text((store.link.messages ?? (store.demo ? 18420 : 0)).formatted()).font(.title2.weight(.semibold)).monospacedDigit()
            Text("Messages").font(.caption).foregroundStyle(AccessibleSecondary())
          }
        }
        Text("You can keep setting up while your history arrives.").font(.title3).foregroundStyle(
          .secondary)
      case .who:
        Text("Who sets it up?").font(.largeTitle.bold())
        VStack(alignment: .leading, spacing: 12) {
          setupOption(
            "ai", title: "Your AI",
            detail:
              "Connect an AI you use. It suggests circles and adds what it knows about your people.",
            note: "Recommended")
          if AppleBriefModel.unavailableReason == nil {
            setupOption(
              "mac", title: "This Mac",
              detail:
                "Apple’s on-device model writes the first briefs. Private, simpler, and a little slower."
            )
          } else if let reason = AppleBriefModel.unavailableReason {
            Text("This Mac · " + reason).font(.callout).foregroundStyle(AccessibleSecondary()).padding(
              .vertical, 4)
          }
          setupOption(
            "later", title: "Later",
            detail: "Start with your chats. Connect an AI whenever you’re ready.")
        }
        ActionFeedback(store: store, action: "mac-briefs")
      case .connect:
        Text("Connect an integration").font(.largeTitle.bold())
        ConnectTiles(store: store)
        StarterPrompt()
      case .done:
        Image(systemName: "checkmark.bubble").font(.system(size: 64, weight: .light))
        Text("You have the final say").font(.largeTitle.bold())
        Text("Your circle rules decide where integrations can send.").font(.title3).foregroundStyle(
          .secondary)
      }
    }
  }

  private var linking: some View {
    VStack(spacing: compact ? 8 : 16) {
      Text("Link WhatsApp").font(.largeTitle.bold())
      if let message = flow.helperError {
        Text(message).foregroundStyle(AccessibleSecondary())
        Button("Try again") { Task { await prepareLink() } }
          .buttonStyle(NativeGlassButtonStyle(prominent: true)).disabled(preparing)
        Button("Explore with sample data") {
          Task { await store.useSampleData(true) }
        }
        .buttonStyle(NativeGlassButtonStyle())
      } else if store.migrationAvailable && !choseQR {
        Button {
          Task {
            await store.perform {
              try await store.backend.post("/v1/migrate/whatsapp-mcp", ["dry_run": false])
              flow.advance(linkReady: true)
            }
          }
        } label: {
          linkChoice(
            "Move my existing setup", detail: "Keeps your linked device and history, no scan",
            symbol: "arrow.right.arrow.left")
        }
        .buttonStyle(NativeGlassButtonStyle(prominent: true, tile: true))
        Button {
          choseQR = true
          Task { await store.startLink() }
        } label: {
          linkChoice(
            "Link with a QR code instead", detail: "Scan a code with WhatsApp on your phone",
            symbol: "qrcode")
        }
        .buttonStyle(NativeGlassButtonStyle(tile: true))
        ActionFeedback(store: store)
      } else if preparing {
        ProgressView("Setting up the background helper…").frame(height: 180)
          .accessibilityLabel("Registering Limmr’s background helper")
        Text("macOS may ask you to allow Limmr in Login Items.").foregroundStyle(AccessibleSecondary())
      } else if store.link.state == "error" {
        Text(store.link.reason ?? "Your device couldn’t be linked.").foregroundStyle(AccessibleSecondary())
        Button("Try again") { Task { await prepareLink() } }
          .buttonStyle(NativeGlassButtonStyle(prominent: true)).disabled(store.busy)
      } else {
        if phoneMode {
          TextField("Phone number with country code", text: $phone).textFieldStyle(.roundedBorder)
            .accessibilityLabel("Phone number with country code")
          Button("Get pairing code") { Task { await store.startLink(phone: phone) } }
            .buttonStyle(NativeGlassButtonStyle(prominent: true))
            .disabled(phone.count < 8 || store.busy)
          Text(store.link.state == "pair_code" ? (store.link.code ?? "") : "••••  ••••")
            .font(.system(size: 32, weight: .medium, design: .monospaced))
            .padding(.vertical, 24)
        } else {
          ZStack {
            Group {
              if let code = store.link.code, store.link.state == "qr" {
                QRCode(value: code).accessibilityLabel("WhatsApp device linking QR code")
              } else if store.demo {
                QRCode(value: "LIMMR-DEMO-PAIRING-NOT-A-REAL-SESSION")
              } else {
                ProgressView("Waiting for a code…").foregroundStyle(.black)
              }
            }
            .frame(width: 260, height: 260)
            .background(.white, in: RoundedRectangle(cornerRadius: 20))
            TimelineView(.periodic(from: .now, by: 1)) { context in
              let duration = Double(store.link.expiresIn ?? 60)
              let remaining = max(
                0, 1 - context.date.timeIntervalSince(startedAt) / max(1, duration))
              RoundedRectangle(cornerRadius: 26)
                .trim(from: 0, to: store.snapshot ? 0.74 : remaining)
                .stroke(.secondary.opacity(0.35), style: StrokeStyle(lineWidth: 2, lineCap: .round))
                .frame(width: 276, height: 276)
            }
          }
          .frame(height: 280)
        }
        Text("WhatsApp → Settings → Linked devices").font(.headline)
        Text(
          phoneMode
            ? "Choose Link with phone number and enter the code."
            : "Tap Link a device, then scan this code."
        )
        .font(.title3).foregroundStyle(AccessibleSecondary())
        Button(phoneMode ? "Use a QR code instead" : "Link with phone number instead") {
          phoneMode.toggle()
          if !phoneMode { Task { await store.startLink() } }
        }
        .buttonStyle(NativeGlassButtonStyle())
        ActionFeedback(store: store)
        Button("Refresh code") { Task { await store.startLink(phone: phoneMode ? phone : nil) } }
          .limmrButton().disabled(store.busy || (phoneMode && phone.count < 8))
        if store.demo {
          Text("Sample code · No account is linked").font(.caption).foregroundStyle(AccessibleSecondary())
        }
      }
    }
  }

  // The footer scrolls with the content: pinning it over a short window hid errors under it.
  private var pinnedFooter: Bool { false }

  private var footer: some View {
    HStack {
      if flow.step != .welcome {
        Button("Back") {
          if showingAbout {
            showingAbout = false
          } else if flow.step == .link && choseQR && store.migrationAvailable {
            choseQR = false
          } else {
            flow.back()
          }
        }.buttonStyle(NativeGlassButtonStyle())
      }
      // Linking moves on by itself once the phone confirms, so that step has no Continue
      // (except with sample data, where there is no phone).
      if flow.step != .link
        || (store.demo && !preparing && flow.helperError == nil && store.link.state != "error") {
        Button {
          // Welcome advances synchronously. Starting the helper belongs to Link.
          if showingAbout {
            showingAbout = false
            flow.step = .done
          } else if flow.step == .done {
            store.showSetup = false
          } else if flow.step == .who && setupChoice != "ai" {
            if setupChoice == "mac" {
              Task {
                await store.enableMacBriefs(true)
                if store.briefSettings.thisMacEnabled { showingAbout = true }
              }
            } else {
              showingAbout = true
            }
          } else {
            flow.advance(linkReady: store.demo || ["syncing", "ready"].contains(store.link.state))
          }
        } label: {
          Label(continueTitle, systemImage: "arrow.right").frame(maxWidth: .infinity)
        }
        .buttonStyle(NativeGlassButtonStyle(prominent: true)).controlSize(.large)
        .disabled(
          store.busy
            || (flow.step == .link && !store.demo
              && !["syncing", "ready"].contains(store.link.state))
        )
      }
    }
    .padding(.top, 8)
  }

  private func setupOption(_ value: String, title: String, detail: String, note: String? = nil)
    -> some View
  {
    Button {
      setupChoice = value
    } label: {
      HStack(alignment: .top, spacing: 14) {
        Image(systemName: setupChoice == value ? "largecircle.fill.circle" : "circle")
          .foregroundStyle(setupChoice == value ? Color.accentColor : .secondary)
        VStack(alignment: .leading, spacing: 6) {
          HStack {
            Text(title).font(.headline)
            if let note { Text(note).font(.caption).foregroundStyle(AccessibleSecondary()) }
          }
          Text(detail).font(.callout).foregroundStyle(AccessibleSecondary()).fixedSize(
            horizontal: false, vertical: true)
        }
        Spacer(minLength: 0)
      }.padding(16).frame(maxWidth: .infinity, alignment: .leading)
        .background(.primary.opacity(0.025), in: RoundedRectangle(cornerRadius: 10))
        .overlay(
          RoundedRectangle(cornerRadius: 10).stroke(
            .primary.opacity(setupChoice == value ? 0.25 : 0.08), lineWidth: 0.5)
        )
        .multilineTextAlignment(.leading).contentShape(Rectangle())
    }.limmrButton().accessibilityAddTraits(setupChoice == value ? [.isSelected] : [])
  }

  private func linkChoice(_ title: String, detail: String, symbol: String) -> some View {
    HStack(spacing: 16) {
      Image(systemName: symbol).font(.title2).frame(width: 32)
      VStack(alignment: .leading, spacing: 8) {
        Text(title).font(.headline)
        Text(detail).font(.callout)
      }
      Spacer(minLength: 0)
    }
    .multilineTextAlignment(.leading).padding(16)
    .frame(maxWidth: .infinity, minHeight: 84, alignment: .leading)
  }

  private var continueTitle: String {
    if showingAbout { return "Continue" }
    return switch flow.step {
    case .link: "I’ve linked my device"
    case .syncing: "Continue while syncing"
    case .done: "Open Limmr"
    default: "Continue"
    }
  }

  private func prepareLink() async {
    preparing = true
    defer { preparing = false }
    do {
      if !store.demo, store.status == nil {
        try await store.system.startEngine()
        // launchd needs a moment to spawn the helper; give it up to ~8 seconds.
        for _ in 0..<16 where store.status == nil {
          await store.refresh()
          if store.status == nil { try await Task.sleep(for: .milliseconds(500)) }
        }
        guard store.status != nil else { throw BackendError.unavailable("Helper unavailable") }
      }
      flow.helperStarted()
      await store.detectMigration()
      if !store.migrationAvailable { await store.startLink() }
    } catch is CancellationError {
      return
    } catch {
      flow.helperFailed()
    }
  }
}
