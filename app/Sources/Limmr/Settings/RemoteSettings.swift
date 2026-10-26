import LimmrKit
import SwiftUI

struct RemoteSettings: View {
  @Bindable var store: AppStore
  var body: some View {
    Section {
      Toggle(
        "Allow remote connections",
        isOn: Binding(
          get: { store.remote?.enabled == true },
          set: { on in
            if on {
              store.showRemote = true
            } else {
              Task {
                await store.perform {
                  struct Configuration: Encodable {
                    var enabled: Bool
                    var publicUrl: String
                  }
                  try await store.backend.post(
                    "/v1/remote",
                    Configuration(enabled: false, publicUrl: store.remote?.publicUrl ?? ""))
                  if !store.demo { try await store.system.remoteConnection().remove() }
                  await store.refresh()
                }
              }
            }
          }
        )
      )
      .settingsMatch("Allow remote connections", store: store)
      .toggleStyle(.switch)
      LabeledContent("ChatGPT and Claude") {
        Button("Set up remote access…") { store.showRemote = true }
      }
      if store.remote?.enabled == true {
        CopyField(title: "MCP address", value: store.remote?.mcpUrl ?? "")
      }
      ForEach(store.remote?.pendingCodes ?? []) { code in
        HStack {
          Text("\(code.clientName) is waiting to connect")
          Spacer()
          Button("Review code") {
            RemotePairingReview.shared.select(code.code)
            store.connectKind =
              code.clientName.localizedCaseInsensitiveContains("claude") ? .claude : .chatgpt
          }
          .buttonStyle(NativeGlassButtonStyle())
        }
      }
      ForEach(store.remote?.clients ?? []) { client in
        HStack {
          Text(client.name)
          Spacer()
          Button("Disconnect") {
            Task {
              await store.perform {
                try await store.backend.post("/v1/remote/revoke", ["client_id": client.id])
                await store.refresh()
              }
            }
          }
          .buttonStyle(NativeGlassButtonStyle())
        }
      }
      ActionFeedback(store: store)
    } header: {
      SettingsPaneTitle(title: "Remote access")
    }
  }
}
