import SwiftUI

struct ChatPicker: View {
  @Bindable var store: AppStore
  @State private var query = ""
  @Environment(\.dismiss) private var dismiss
  var body: some View {
    VStack(alignment: .leading, spacing: 16) {
      Text("Choose chats").font(.largeTitle.bold())
      TextField("Search chats", text: $query).textFieldStyle(.roundedBorder)
      List {
        CurrentChatSections(store: store, chats: store.chats, query: query) { chat in
          HStack(spacing: 12) {
            Text(String(chat.name.prefix(1))).font(.headline)
              .frame(width: 32, height: 32).background(.quaternary, in: Circle())
            Toggle(
              chat.name,
              isOn: Binding(
                get: { store.permissions.chats.jids.contains(chat.jid) },
                set: { selected in
                  store.editPermissions {
                    $0.chats.jids.removeAll { $0 == chat.jid }
                    if selected { $0.chats.jids.append(chat.jid) }
                  }
                }
              )
            )
            .toggleStyle(.checkbox)
          }
          .padding(.vertical, 4)
        }
      }
      Text("Hidden chats are always invisible to every integration.").font(.caption)
        .foregroundStyle(
          .secondary)
      HStack {
        Spacer()
        Button("Done") { dismiss() }.buttonStyle(NativeGlassButtonStyle(prominent: true))
      }
    }
    .padding(24).frame(width: 460, height: 460)
  }
}
