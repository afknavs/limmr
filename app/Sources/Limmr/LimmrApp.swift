import AppKit
import LimmrKit
import SwiftUI

@main struct LimmrApp: App {
  @State private var store: AppStore
  @Environment(\.openWindow) private var openWindow

  init() {
    let arguments = ProcessInfo.processInfo.arguments
    let updateHelper = arguments.dropFirst().first?.hasPrefix("--limmr-") == true
    let audit = arguments.contains("--decode-audit")
    let snapshot = audit || arguments.contains("--snapshot") || arguments.contains("--smoke-test")
    let initialStore = AppStore(
      demo: updateHelper || snapshot || arguments.contains("--demo"),
      snapshot: updateHelper || snapshot)
    _store = State(initialValue: initialStore)
    if audit {
      NSApplication.shared.setActivationPolicy(.prohibited)
      Task { @MainActor in
        guard let index = arguments.firstIndex(of: "--decode-audit"), arguments.count == index + 3,
          let url = URL(string: arguments[index + 1]) else {
          print("/v1  FAIL  arguments"); exit(2)
        }
        let backend = APIClient(credentialFile: URL(fileURLWithPath: arguments[index + 2]), baseURL: url)
        let ok = await DecodeAudit.run(backend: backend) { print($0) }
        exit(ok ? 0 : 1)
      }
      return
    }
    if UpdateEntrypoint.handle(arguments) { return }
    if !snapshot && !updateHelper && !initialStore.demo {
      // App-lifetime task: closing a window must not stop pairing reviews.
      Task { await initialStore.monitorRemotePairings() }
    }
    if snapshot {
      NSApplication.shared.setActivationPolicy(.prohibited)
      Task { @MainActor in
        if arguments.contains("--smoke-test") {
          await SmokeRunner.run()
        } else {
          await SnapshotRunner.run(arguments: arguments)
        }
      }
    }
  }

  var body: some Scene {
    WindowGroup("Limmr", id: "main") {
      if !store.snapshot {
        RootView(store: store)
          .frame(minWidth: WindowMetrics.minimumWidth, minHeight: WindowMetrics.minimumHeight)
          .onAppear {
            RemotePairingReview.shared.openMainWindow = {
              openWindow(id: "main")
              NSApp.activate(ignoringOtherApps: true)
            }
          }
          .task(id: store.demo) {
            if !store.demo {
              await store.system.startup.recover(using: store.system)
            }
            await store.refresh()
            if store.status?.linked == false
              || (!store.demo && store.system.startup.phase == .newInstall)
            {
              store.showSetup = true
            }
            await store.monitor()
          }
          .task(id: store.demo) {
            if !store.demo && !store.snapshot { UpdateController.shared.start(store: store) }
          }
          .task(id: store.briefSettings.thisMacEnabled) {
            if store.briefSettings.thisMacEnabled { await store.writeMacBriefs() }
          }
      }
    }
    .defaultSize(width: WindowMetrics.defaultWidth, height: WindowMetrics.defaultHeight)
    .windowResizability(.contentMinSize)
    .windowToolbarStyle(.unified(showsTitle: false))
    // Snapshots render into their own offscreen window; an empty main window would flash on screen.
    .defaultLaunchBehavior(store.snapshot ? .suppressed : .automatic)
    .commands {
      LimmrCommands(store: store)
      CommandGroup(replacing: .help) { SetupGuideLink() }
      CommandGroup(after: .appInfo) {
        if !store.setupComplete {
          Button("Set up Limmr…") {
            store.setupStep = 0
            store.showSetup = true
          }
        }
        Button("Refresh") { Task { await store.refresh() } }.keyboardShortcut("r")
      }
    }
    Settings {
      SettingsView(store: store).defaultScrollAnchor(.top)
    }
    .defaultSize(width: 720, height: 540)
    MenuBarExtra {
      if !store.snapshot {
        MenuBarView(store: store) {
          openWindow(id: "main")
          NSApp.activate(ignoringOtherApps: true)
        }
      }
    } label: {
      if !store.snapshot {
        Label {
          Text(store.pending.isEmpty ? "Limmr" : "\(store.pending.count)")
        } icon: {
          Image(systemName: "bubble.left.and.text.bubble.right")
        }
      }
    }
    .menuBarExtraStyle(.window)
  }
}
