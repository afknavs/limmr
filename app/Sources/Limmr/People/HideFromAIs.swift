import LimmrKit
import SwiftUI

extension AppStore {
  /// A profile controls every DM alias, but never any shared group.
  func directChatJIDs(for person: Person) -> [String] {
    let aliases = person.jids.filter { $0.hasSuffix("@s.whatsapp.net") || $0.hasSuffix("@lid") }
    guard chats.contains(where: { !$0.isGroup && aliases.contains($0.jid) }) else { return [] }
    return aliases
  }

  /// An older DM may be beyond the first page of the sidebar's chat cache.
  func loadDirectChat(for person: Person) async {
    guard directChatJIDs(for: person).isEmpty else { return }
    let source = backend
    let generation = backendGeneration
    var cursor = chatCursor
    var visited = Set<String>()
    do {
      while let next = cursor, !next.isEmpty, visited.insert(next).inserted {
        var query = URLComponents()
        query.queryItems = [URLQueryItem(name: "limit", value: "100"), URLQueryItem(name: "cursor", value: next)]
        let page: ChatsResponse = try await source.get("/v1/chats?" + (query.percentEncodedQuery ?? ""))
        guard generation == backendGeneration, !Task.isCancelled else { return }
        let matches = page.chats.filter { !$0.isGroup && person.jids.contains($0.jid) }
        chats += matches.filter { match in !chats.contains { $0.jid == match.jid } }
        if !matches.isEmpty { return }
        cursor = page.nextCursor
      }
    } catch {
      if !Task.isCancelled { self.error = "Direct chat could not be loaded. Try opening the profile again." }
    }
  }

  func hiddenChatsBinding(for jids: [String]) -> Binding<Bool> {
    Binding(
      get: { !jids.isEmpty && jids.allSatisfy(self.settings.hiddenChats.contains) },
      set: { hidden in
        guard !jids.isEmpty else { return }
        self.editSettings {
          $0.hiddenChats.removeAll { jids.contains($0) }
          if hidden { $0.hiddenChats.append(contentsOf: Set(jids).sorted()) }
        }
      })
  }
}

struct HideFromAIsSwitch: View {
  @Bindable var store: AppStore
  let chatJIDs: [String]
  let explanation: String

  var body: some View {
    VStack(alignment: .leading, spacing: 8) {
      Toggle("Hide from your AIs", isOn: store.hiddenChatsBinding(for: chatJIDs))
        .toggleStyle(.switch).disabled(chatJIDs.isEmpty)
      Text(chatJIDs.isEmpty ? "No direct chat to hide." : explanation)
        .font(.callout).foregroundStyle(AccessibleSecondary())
      SettingsFeedback(store: store)
    }
  }
}
