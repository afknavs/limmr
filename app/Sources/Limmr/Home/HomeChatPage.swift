import LimmrKit
import SwiftUI

/// Read-only local history. Opening a search hit never marks messages read on WhatsApp.
struct HomeChatPage: View {
  @Bindable var store: AppStore
  let id: String
  let name: String
  @State private var messages: [Message] = []
  @State private var loading = true
  @State private var error: String?
  var body: some View {
    ScrollView {
      VStack(alignment: .leading, spacing: 18) {
        Button("People", systemImage: "chevron.left") { store.circlePath = [] }
          .limmrButton().font(.callout)
        Text(name).font(.largeTitle.bold())
        Text("Recent messages").font(.callout).foregroundStyle(AccessibleSecondary())
        if loading { ProgressView() }
        if let error {
          Text(error).foregroundStyle(AccessibleSecondary())
          Button("Try again") { Task { await load() } }
        }
        ForEach(messages) { message in
          VStack(alignment: .leading, spacing: 6) {
            HStack {
              Text(message.isFromMe ? "You" : store.senderName(message.sender)).font(.callout.weight(.medium))
              Spacer()
              Text(store.relativeTime(message.timestamp)).font(.caption).foregroundStyle(AccessibleSecondary())
            }
            Text(message.displayText)
              .font(.callout).textSelection(.enabled)
          }
          Divider()
        }
        if !loading && error == nil && messages.isEmpty {
          Text("No messages stored here yet.").foregroundStyle(AccessibleSecondary())
        }
      }.padding(28).frame(maxWidth: 820, alignment: .leading).frame(maxWidth: .infinity)
    }.navigationBarBackButtonHidden(true).background { AppBackground() }
      .task(id: id) { await load() }
  }
  private func load() async {
    loading = true; error = nil
    do {
      let response: MessagesResponse = try await store.backend.get(HomeSearch.path("/v1/messages", query: id, key: "chat_jid"))
      try Task.checkCancellation()
      messages = response.messages; loading = false
    } catch {
      guard !Task.isCancelled else { return }
      self.error = "Messages could not be loaded."; loading = false
    }
  }
}
