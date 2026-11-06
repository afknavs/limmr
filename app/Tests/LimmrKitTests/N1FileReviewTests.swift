import Foundation
import Testing
@testable import LimmrKit

struct N1FileReviewTests {
  @Test func fileReviewMetadataSurvivesWireRoundTrip() throws {
    for image in [false, true] {
      let original = DemoData.fileSuggestion(image: image)
      let decoded = try Wire.decode(CircleSuggestion.self, from: Wire.data(original))
      #expect(decoded.attachment == original.attachment)
      #expect(decoded.attachment?.name == (image ? "Design Club poster.png" : "Design Club guide.pdf"))
      #expect(decoded.chatJid == original.chatJid)
      #expect(decoded.draftText == original.draftText)
    }
    let old = Data(#"{"id":"text-only","circle_id":"university","kind":"reply","chat_jid":"design@g.us","draft_text":"Hello","why":"Review","created_by":"ai","expires_at":"2030-01-01T00:00:00Z"}"#.utf8)
    #expect(try Wire.decode(CircleSuggestion.self, from: old).attachment == nil)
  }
}
