import LimmrKit
import SwiftUI

struct AdvancedSettings: View {
  @Bindable var store: AppStore
  @State private var confirm: String?
  @State private var reset = false
  var body: some View {
    Section {
      Button("Set up Limmr…") {
        store.setupStep = 0
        store.showSetup = true
      }.settingsMatch("Set up Limmr", store: store)
      Toggle(
        "Explore with sample data",
        isOn: Binding(
          get: { store.demo }, set: { on in Task { await store.useSampleData(on) } }
        )
      )
      .settingsMatch("Explore with sample data", store: store)
      UpdateSettingsRow(store: store).settingsMatch("Updates", store: store)
      DisclosureGroup("Data folder") {
        Text(store.system.paths.home.path)
          .font(.system(.caption, design: .monospaced)).textSelection(.enabled)
        Button("Open data folder") {
          if !store.demo { NSWorkspace.shared.open(store.system.paths.home) }
        }
        .buttonStyle(NativeGlassButtonStyle())
      }
      .settingsMatch("Data folder", store: store)
      LabeledContent("Background helper") {
        Button("Stop…") { confirm = "Stop background helper" }
      }
      .settingsMatch("Background helper", store: store)
      LabeledContent("WhatsApp connection") {
        Button("Unlink…") { confirm = "Unlink WhatsApp" }
      }
      .settingsMatch("WhatsApp connection", store: store)
      LabeledContent("Circles") {
        Button("Start circles over…") { reset = true }
      }
      .settingsMatch("Circles", store: store)
      LabeledContent("Previous setup") {
        Button("Undo migration…") { confirm = "Undo migration" }
      }
      .settingsMatch("Previous setup", store: store)
      ActionFeedback(store: store)
    } header: {
      SettingsPaneTitle(title: "Advanced")
    }
    .buttonStyle(NativeGlassButtonStyle())
    .sheet(isPresented: $reset) { ResetCirclesSheet(store: store).dismissOnEscape() }
    .confirmationDialog(
      confirm ?? "Connection controls",
      isPresented: Binding(
        get: { confirm != nil }, set: { if !$0 { confirm = nil } }
      )
    ) {
      Button(confirm ?? "Continue", role: .destructive) {
        let choice = confirm
        confirm = nil
        Task {
          await store.perform {
            if choice == "Stop background helper" {
              try await store.system.stopEngine()
            } else if choice == "Unlink WhatsApp" {
              try await store.backend.post("/v1/unlink", ["confirm": true])
            } else {
              try await store.backend.post("/v1/migrate/undo")
            }
            await store.refresh()
          }
        }
      }
    }
  }
}
