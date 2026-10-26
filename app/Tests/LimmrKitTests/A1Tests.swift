import Foundation
import Testing
@testable import LimmrKit

@Suite struct A1Tests {
  @Test func decodesInstructionAuditAndConversationDraft() throws {
    let data = Data(#"{"draft_id":"demo","agent_id":"ai","chat_jid":"demo-chat","final_text":"Outline ready.","rule":"confirm","needs_confirmation":false,"created_at":"2026-10-18T12:00:00Z","expires_at":"2026-10-18T12:30:00Z","state":"sent","confirmed_via":"owner_instruction","owner_instruction":"Send the outline now.","queue_for_approval":false}"#.utf8)
    let draft = try Wire.decode(CircleDraft.self, from: data)
    #expect(draft.ownerInstruction == "Send the outline now.")
    #expect(draft.queueForApproval == false && draft.rule == .confirm)
    let row = try Wire.decode(Activity.self, from: Data(#"{"id":1,"at":"2026-10-18T12:00:00Z","agent_id":"ai","tool":"send_draft","kind":"sent","chat_jids":[],"message_ids":[],"count":1,"owner_instruction":"Send the outline now."}"#.utf8))
    #expect(row.instructionReason == "Because you said: Send the outline now.")
    let old = try Wire.decode(Activity.self, from: Data(#"{"id":2,"at":"2026-10-18T12:00:00Z","agent_id":"ai","tool":"send_draft","kind":"sent","chat_jids":[],"message_ids":[],"count":1}"#.utf8))
    #expect(old.instructionReason == nil)
  }
  @Test func explicitReviewRuleAndExistingRulesDecode() throws {
    #expect(try Wire.decode(SendRule.self, from: Data(#""ask_in_limmr""#.utf8)) == .askInLimmr)
    #expect(SendRule.askInLimmr.title == "Ask me in Limmr")
    #expect(try Wire.decode(SendRule.self, from: Data(#""confirm""#.utf8)) == .confirm)
    #expect(try Wire.decode(SendRule.self, from: Data(#""free""#.utf8)) == .free)
  }
}
