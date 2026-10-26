import LimmrKit
import SwiftUI

struct SourcesSettings: View {
  @Bindable var store: AppStore
  var body: some View {
    Section {
      HStack {
        Label("WhatsApp", systemImage: "bubble.left.and.bubble.right")
        Spacer()
        Text(
          store.status?.connected == true
            ? "Connected" : store.status?.linked == true ? "Reconnecting" : "Not linked"
        )
        .foregroundStyle(AccessibleSecondary())
      }
      .settingsMatch("WhatsApp", store: store)
    } header: {
      SettingsPaneTitle(title: "Sources")
    }
  }
}

private struct SnapshotAccessKey: EnvironmentKey { static let defaultValue = false }
extension EnvironmentValues {
  var snapshotIntegrationAccess: Bool {
    get { self[SnapshotAccessKey.self] }
    set { self[SnapshotAccessKey.self] = newValue }
  }
}
