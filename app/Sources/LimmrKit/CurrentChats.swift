import Foundation

public enum CurrentChats {
  public static func isCurrent(_ chat: Chat, days: Int, now: Date) -> Bool {
    guard let at = chat.lastMessageTime.flatMap(Timestamp.date) else { return false }
    return at >= now.addingTimeInterval(-Double(days) * 86400)
  }
  public static func ordered(_ chats: [Chat], query: String = "") -> [Chat] {
    chats.filter { query.isEmpty || $0.name.localizedCaseInsensitiveContains(query) }.sorted {
      let a = $0.lastMessageTime.flatMap(Timestamp.date) ?? .distantPast
      let b = $1.lastMessageTime.flatMap(Timestamp.date) ?? .distantPast
      return a == b ? $0.jid < $1.jid : a > b
    }
  }
}
