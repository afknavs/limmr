import Foundation
import Testing
@testable import Limmr
@testable import LimmrKit

@Suite("N1 notifications and setup") struct N1Tests {
  @Test @MainActor func readyShapeShowsNoHomeSetupCard() async throws {
    let backend = DemoBackend(n1ReadyShape: true)
    let setup: SetupStatus = try await backend.get("/v1/setup/status")
    let home: HomeResponse = try await backend.get("/v1/home")
    #expect(setup.stage == "ready")
    let circles = try #require(setup.circles)
    let people = try #require(setup.people)
    #expect(circles.confirmed == 7 && circles.proposed == 0)
    #expect((setup.unsortedActiveChats ?? 0) > (setup.chats ?? 0) / 2)
    #expect(people.withoutDetails > people.total / 2)
    let reason = SetupHomeCard.reason(stage: setup.stage, proposals: home.proposals, profile: .sample)
    #expect(reason.isEmpty)
    #expect(!SetupHomeCard.shouldShow(reason: reason, dismissedReason: ""))
    for stage in ["sorting", "filling", "ready"] {
      #expect(SetupHomeCard.reason(stage: stage, proposals: [], profile: .sample).isEmpty)
    }
  }
  @Test @MainActor func dismissalIsTiedToNewReason() throws {
    #expect(SetupHomeCard.reason(stage: "new", proposals: [], profile: .sample) == "new")
    #expect(SetupHomeCard.reason(stage: "ready", proposals: [], profile: .init()) == "about-you-empty")
    let old = "proposals:old-batch"
    #expect(!SetupHomeCard.shouldShow(reason: old, dismissedReason: old))
    #expect(SetupHomeCard.shouldShow(reason: "proposals:new-batch", dismissedReason: old))
    #expect(SetupHomeCard.shouldShow(reason: "about-you-empty", dismissedReason: old))
    var proposals = DemoCircles.proposals
    #expect(!proposals.isEmpty)
    for i in proposals.indices { proposals[i].batchId = "new-batch" }
    #expect(SetupHomeCard.reason(stage: "ready", proposals: proposals, profile: .sample) == "proposals:new-batch")
  }

  @Test func activityVoiceReasonWireCompatibility() throws {
    let activity = try Wire.decode(Activity.self, from: Data(#"{"id":1,"at":"2026-10-18T12:00:00Z","agent_id":"demo-ai","tool":"notify_me","kind":"send_requested","chat_jids":[],"message_ids":[],"count":1,"voice":"as_agent","voice_reason":"Notifying you"}"#.utf8))
    #expect(activity.agentId == "demo-ai" && activity.tool == "notify_me")
    let legacy = try Wire.decode(Activity.self, from: Data(#"{"id":1,"at":"2026-10-18T12:00:00Z","agent_id":"demo-ai","tool":"list_chats","kind":"read","chat_jids":[],"message_ids":[],"count":1}"#.utf8))
    #expect(legacy.agentId == "demo-ai" && legacy.kind == "read")
  }
}
