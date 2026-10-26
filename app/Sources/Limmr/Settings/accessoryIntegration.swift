import AppKit
import LimmrKit
import SwiftUI

extension AppStore {
  var accessoryAvailable: Bool {
    AccessoryAvailability.isAvailable(enabled: settings.accessoryCue.enabled, installed: {
      FileManager.default.fileExists(atPath: "/Applications/Accessory.app") ||
      FileManager.default.fileExists(atPath: NSHomeDirectory() + "/Applications/Accessory.app")
    })
  }
}

struct AccessoryCueSettings: View {
  @Bindable var store: AppStore
  @State private var tested = false
  private let colors = ["#5e5ce6", "#64a8d1", "#75ad91"]
  var body: some View {
    Section("Accessory light") {
      Toggle("Soft light on Accessory when something needs you", isOn: Binding(
        get: { store.settings.accessoryCue.enabled },
        set: { enabled in store.editAccessoryCue { $0.enabled = enabled } }))
        .settingsMatch("Accessory light", store: store)
      HStack(spacing: 12) {
        Text("Color")
        ForEach(Array(colors.enumerated()), id: \.offset) { index, hex in
          Button {
            store.editAccessoryCue { $0.color = hex }
          } label: {
            Circle().fill(swatch(index)).frame(width: 20, height: 20)
              .padding(4).overlay(Circle().stroke(store.settings.accessoryCue.color == hex ? Color.primary : Color.clear, lineWidth: 1))
          }.buttonStyle(.plain)
            .accessibilityLabel(["Violet", "Blue", "Sage"][index])
            .accessibilityValue(store.settings.accessoryCue.color == hex ? "Selected" : "")
        }
        Spacer()
        Button("Test") {
          tested = false
          Task { tested = await store.testAccessoryCue() }
        }.disabled(store.busy).accessibilityLabel("Test Accessory light")
      }
      Text("A quiet breathing light, without sound. Quiet hours: \(store.settings.accessoryCue.quietHours?.start ?? "23:00")–\(store.settings.accessoryCue.quietHours?.end ?? "08:00").")
        .font(.footnote).foregroundStyle(AccessibleSecondary())
      if tested { Text(store.demo ? "Sample light tested" : "Test requested").font(.callout).foregroundStyle(AccessibleSecondary()) }
      ActionFeedback(store: store, action: "accessory-test")
      ActionFeedback(store: store, action: "accessory-settings")
    }
  }
  private func swatch(_ index: Int) -> Color {
    switch index {
    case 1: Color(red: 100/255, green: 168/255, blue: 209/255)
    case 2: Color(red: 117/255, green: 173/255, blue: 145/255)
    default: Color(red: 94/255, green: 92/255, blue: 230/255)
    }
  }
}


extension AppStore {
  func testAccessoryCue() async -> Bool {
    await waitForSettingsSave()
    var requested = false
    await perform(action: "accessory-test") {
      struct Result: Decodable { var ok: Bool; var running: Bool }
      do {
        let result = try Wire.decode(Result.self, from: await self.backend.request(
          "/v1/settings/accessory-cue/test", method: "POST", body: nil))
        guard result.ok && result.running else { throw BackendError.unavailable("Accessory isn’t running.") }
      }
      catch BackendError.http(404) {
        throw BackendError.unavailable("This engine does not support testing the Accessory light yet.")
      }
      requested = true
    }
    return requested
  }
  func editAccessoryCue(_ edit: (inout AccessoryCue) -> Void) {
    var cue = settings.accessoryCue; edit(&cue)
    editSettings { $0.accessoryCue = cue }
    let expected = cue
    let source = backend
    let generation = backendGeneration
    Task {
      await waitForSettingsSave()
      guard generation == backendGeneration, settings.accessoryCue == expected else { return }
      await perform(action: "accessory-settings") {
        let readback: AppSettings = try await source.get("/v1/settings")
        guard generation == self.backendGeneration, self.settings.accessoryCue == expected else { return }
        self.settings.accessoryCue = readback.accessoryCue
        guard readback.accessoryCue == expected else {
          throw BackendError.unavailable("The light setting did not save. Try again.")
        }
      }
    }
  }
}
