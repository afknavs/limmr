import LimmrKit
import SwiftUI

struct BriefsSettings: View {
  @Bindable var store: AppStore
  var body: some View {
    Section {
      Toggle(
        "First briefs from This Mac",
        isOn: Binding(
          get: { store.briefSettings.thisMacEnabled },
          set: { enabled in Task { await store.enableMacBriefs(enabled) } }
        )
      ).settingsMatch("First briefs from This Mac", store: store).toggleStyle(.switch).disabled(
        store.busy
          || (!store.briefSettings.thisMacEnabled && AppleBriefModel.unavailableReason != nil))
      Text(
        "Apple’s on-device model writes first circle briefs and names waiting work in the background while this Mac is idle. Your connected AIs can build on it later."
      )
      .font(.callout).foregroundStyle(AccessibleSecondary())
      if let reason = AppleBriefModel.unavailableReason {
        Text(reason).font(.callout).foregroundStyle(AccessibleSecondary())
      }
      if store.briefSettings.thisMacEnabled {
        Button(store.writingMacBriefs ? "Writing first briefs…" : "Write missing briefs") {
          Task { await store.writeMacBriefs() }
        }
        .disabled(store.busy || store.writingMacBriefs)
      }
      if let notice = store.macBriefNotice {
        Text(notice).font(.callout).foregroundStyle(AccessibleSecondary())
      }
      ActionFeedback(store: store, action: "mac-briefs")
    } header: {
      SettingsPaneTitle(title: "Briefs")
    }
  }
}
