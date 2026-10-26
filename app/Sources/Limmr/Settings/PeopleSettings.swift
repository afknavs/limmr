import SwiftUI
import LimmrKit

struct PeopleSettings: View {
  @Bindable var store: AppStore
  @State private var enabled = AppleBriefModel.unavailableReason == nil
  @State private var loaded = false
  @State private var running = false
  var body: some View {
    Section {
      PeopleActions(store: store)
      ContactsImportSettings(store: store)
    } header: { SettingsPaneTitle(title: "Your people") }
    Section {
      Toggle("Infer how I know frequent contacts on this Mac", isOn: Binding(get: { enabled }, set: { on in
        enabled = on
        if !on { store.peopleInferenceTask?.cancel() }
        Task { await store.perform(action: "people-inference") {
          _ = try await store.backend.request("/v1/settings/people", method: "PUT", body: Wire.data(PeopleInferenceSettings(enabled: on)))
        } }
      })).disabled(!loaded || AppleBriefModel.unavailableReason != nil)
        .settingsMatch("Infer how I know frequent contacts", store: store)
      Text("Uses a bounded window of recent direct chats on this Mac. Inferred relationships stay private until you confirm them.")
        .font(.footnote).foregroundStyle(AccessibleSecondary())
      if let reason = AppleBriefModel.unavailableReason { Text(reason).font(.footnote).foregroundStyle(AccessibleSecondary()) }
      Button(running ? "Understanding people…" : "Re-run now") {
        store.peopleInferenceTask?.cancel()
        store.peopleInferenceTask = Task {
          running = true; defer { running = false }
          await store.perform(action: "people-inference") {
            _ = try await OnDevicePeopleWriter(backend: store.backend, model: AppleRelationshipModel()).run()
            try await store.backend.post("/v1/people/affinity/suggest")
            await store.refresh()
          }
        }
      }.disabled(running || !enabled || AppleBriefModel.unavailableReason != nil || store.demo)
      ActionFeedback(store: store, action: "people-inference")
    } header: { SettingsPaneTitle(title: "On this Mac") }
    .task {
      do {
        let value: PeopleInferenceSettings = try await store.backend.get("/v1/settings/people")
        enabled = value.enabled ?? (AppleBriefModel.unavailableReason == nil)
        loaded = true
      } catch { loaded = false }
    }
  }
}
extension AppStore {
  func schedulePeopleInference() {
    guard !demo, !snapshot, status?.linked == true, !peopleInferenceStarted else { return }
    peopleInferenceStarted = true
    let backend = backend
    peopleInferenceTask = Task { [weak self] in
      do {
        try await backend.post("/v1/people/affinity/suggest")
        guard AppleBriefModel.unavailableReason == nil else { return }
        let settings: PeopleInferenceSettings = try await backend.get("/v1/settings/people")
        guard settings.enabled ?? true else { return }
        _ = try await OnDevicePeopleWriter(backend: backend, model: AppleRelationshipModel()).run()
        try Task.checkCancellation()
        await self?.refresh()
      } catch is CancellationError { } catch {
        guard !Task.isCancelled else { return }
        self?.error = "Couldn't infer relationships on this Mac. Re-run in Settings → People."
      }
    }
  }
}
