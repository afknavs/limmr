import Foundation
import Testing
@testable import LimmrKit

struct Z2DecodeTests {
  @Test(arguments: ["", ",\"content\":null", ",\"content\":\"\""])
  func captionlessStickerDecodesInEveryMessageEnvelope(_ content: String) throws {
    let message = #"{"id":"sticker-1","chat_jid":"design@g.us","sender":"maya","timestamp":"2026-10-26T12:00:00Z","is_from_me":false,"media_type":"sticker""# + content + "}"
    let single = try Wire.decode(Message.self, from: Data(message.utf8))
    #expect(single.text.isEmpty && single.displayText == "Sticker")
    let page = try Wire.decode(MessagesResponse.self, from: Data("{\"messages\":[\(message)]}".utf8), strictLists: true)
    #expect(page.messages == [single])
    let group = #"{"group":{"jid":"design@g.us","name":"Design Club","description":"","member_count":2,"i_am_admin":false},"members":[],"facts":[],"recent":["# + message + "]}"
    let detail = try Wire.decode(GroupDetail.self, from: Data(group.utf8), strictLists: true)
    #expect(detail.recent == [single])
    #expect(try Wire.decode(Message.self, from: Wire.data(single)) == single)
  }
  @Test func mediaLabelsPreserveCaptionsAndDocumentNames() throws {
    for (type, label) in [("sticker", "Sticker"), ("image", "Photo"), ("video", "Video"), ("audio", "Voice message"), ("document", "Document")] {
      let data = Data("{\"id\":\"media-1\",\"chat_jid\":\"design@g.us\",\"sender\":\"maya\",\"timestamp\":\"2026-10-26T12:00:00Z\",\"media_type\":\"\(type)\"}".utf8)
      var message = try Wire.decode(Message.self, from: data)
      #expect(message.text.isEmpty && message.displayText == label)
      message.text = "Saturday's plans"
      #expect(message.displayText == "Saturday's plans")
      message.text = " \n "
      message.filename = ""
      #expect(message.displayText == label)
      if type == "document" {
        message.filename = "Workshop.pdf"
        #expect(message.displayText == "Workshop.pdf")
      }
    }
  }
  @Test func invalidMessageContentStillFailsStrictDecoding() {
    let data = Data(#"{"messages":[{"id":"bad","chat_jid":"design@g.us","sender":"maya","timestamp":"2026-10-26T12:00:00Z","content":42}]}"#.utf8)
    #expect(throws: DecodingError.self) {
      try Wire.decode(MessagesResponse.self, from: data, strictLists: true)
    }
  }
  @Test func demoGroupIncludesCaptionlessSticker() async throws {
    let detail: GroupDetail = try await DemoBackend().get("/v1/groups/\(DemoData.groupJid(4))")
    let sticker = try #require(detail.recent?.first { $0.mediaType == "sticker" })
    #expect(sticker.text.isEmpty && sticker.displayText == "Sticker")
  }
  @Test func activityNullAndMissingArrays() throws {
    for suffix in [",\"chat_jids\":null,\"message_ids\":null", ""] {
      let data = Data("{\"items\":[{\"id\":1,\"at\":\"2026-10-26\",\"agent_id\":\"you\",\"tool\":\"circle_to_task\",\"kind\":\"people_write\",\"count\":1\(suffix)}]}".utf8)
      let page = try Wire.decode(ActivityPage.self, from: data)
      #expect(page.items.count == 1)
      #expect(page.items[0].chatJids.isEmpty && page.items[0].messageIds.isEmpty)
    }
  }
  @Test func badRecordDoesNotDiscardNeighbors() throws {
    let data = Data(#"{"chats":[{"jid":"a","name":"Maya","is_group":false},{"jid":42},{"jid":"b","name":"Design Club","is_group":true}]}"#.utf8)
    let page = try Wire.decode(ChatsResponse.self, from: data, route: "/v1/chats")
    #expect(page.chats.map(\.jid) == ["a", "b"])
  }
  @Test func structuredAndPlainBriefAsksCoexist() throws {
    let data = Data(#"{"circle_id":"club","version":1,"summary":"Review","what_changed":"","open_asks_for_you":["Review the deck",{"text":"Choose a time"},42],"written_by":"you","written_at":"","covers_until":""}"#.utf8)
    #expect(try Wire.decode(CircleBrief.self, from: data).openAsksForYou == ["Review the deck", "Choose a time"])
  }
  @Test func envelopesAndNestedArraysDefaultToEmpty() throws {
    #expect(try Wire.decode(AgentsResponse.self, from: Data(#"{"agents":null}"#.utf8)).agents.isEmpty)
    #expect(try Wire.decode(Graph.self, from: Data(#"{}"#.utf8)).nodes.isEmpty)
    #expect(try Wire.decode(ChatScope.self, from: Data(#"{"mode":"all","jids":null}"#.utf8)).jids.isEmpty)
  }
}
