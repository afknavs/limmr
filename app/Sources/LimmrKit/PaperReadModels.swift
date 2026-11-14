import Foundation

public struct ConversationWeek: Codable, Sendable, Equatable {
  public var start: String
  public var messages: Int
}
public struct PersonSharedFile: Codable, Sendable, Equatable {
  public var id: String
  public var chatJid: String
  public var filename: String
  public var mediaType: String
  public var at: String
  public var thumbnail: Data?
  public var key: String { chatJid + ":" + id }
}
public struct PersonConversationHistory: Codable, Sendable {
  public var weeks: [ConversationWeek]
  public var sharedFiles: [PersonSharedFile]
  public var sharedFilesTotal: Int
}
