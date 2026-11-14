import Foundation
import Testing
@testable import LimmrKit

struct FirstContactTests {
  @Test func settingsDefaultAndWireKeyPreserveOwnerLimit() throws {
    let legacy = Data(#"{"retention_days":180,"owner":"Alex"}"#.utf8)
    var settings = try Wire.decode(AppSettings.self, from: legacy)
    #expect(settings.firstContactLimit == 5)
    for limit in [0, 5, 20, 100] {
      settings.firstContactLimit = limit
      let encoded = try Wire.data(settings)
      let object = try #require(JSONSerialization.jsonObject(with: encoded) as? [String: Any])
      #expect(object["first_contact_limit"] as? Int == limit)
      #expect(try Wire.decode(AppSettings.self, from: encoded).firstContactLimit == limit)
    }
  }

  @Test func approvalAndActivityKeepBoundedProvenance() throws {
    let data = Data(#"{"id":"first","agent_id":"ai","chat_jid":"15550100901@s.whatsapp.net","kind":"message","text":"Hello.","created_at":"2026-11-13T12:00:00Z","expires_at":"2026-11-13T12:30:00Z","state":"pending","edited":false,"first_contact":true,"first_contact_person":{"id":"maya","name":"Maya Rao","phone":"+15550100901","role":"member"},"first_contact_group":{"id":"invented-group","name":"Design Club"}}"#.utf8)
    var approval = try Wire.decode(Approval.self, from: data)
    #expect(approval.firstContactLine == "New chat with Maya Rao, from Design Club")
    try approval.transition(to: "sent")
    #expect(approval.firstContact == true)
    let roundTrip = try Wire.decode(Approval.self, from: Wire.data(approval))
    #expect(roundTrip.firstContactGroup?.name == "Design Club")
    let activityData = Data(#"{"id":1,"at":"2026-11-13T12:00:00Z","agent_id":"ai","tool":"send_draft","kind":"sent","chat_jids":["15550100901@s.whatsapp.net"],"message_ids":["fake-first"],"count":1,"first_contact":true,"first_contact_person":{"name":"Maya Rao"},"first_contact_group":{"id":"invented-group","name":"Design Club"}}"#.utf8)
    let activity = try Wire.decode(Activity.self, from: activityData)
    #expect(activity.firstContactLine == approval.firstContactLine)
  }

  @Test func ordinaryApprovalHasNoFirstContactLabel() throws {
    let ordinary = DemoData.approvals[0]
    #expect(ordinary.firstContactLine == nil)
    var bare = ordinary
    bare.firstContact = true
    bare.firstContactPerson = try Wire.decode(ContactPerson.self, from: Data(#"{"name":"+1 555 010 0901"}"#.utf8))
    #expect(bare.firstContactLine == "New chat with +1 555 010 0901, at your request")
  }
}
