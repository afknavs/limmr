import Foundation
import Testing

@testable import Limmr
@testable import LimmrKit

@Suite("A9 access, setup and first briefs") struct A9Tests {
  @Test func legacyPermissionsAndAccessWire() throws {
    let original = try Wire.data(Permissions.defaults(remote: false))
    let legacy = try Wire.decode(Permissions.self, from: original)
    #expect(legacy.access == nil)
    let restricted = try Wire.decode(AgentAccess.self, from: Data(#"{"circles":["work"]}"#.utf8))
    #expect(!restricted.unsorted)
    let all = try Wire.decode(AgentAccess.self, from: Data(#"{"circles":"all"}"#.utf8))
    #expect(all.unsorted)
    let object = try #require(JSONSerialization.jsonObject(with: Wire.data(all)) as? [String: Any])
    #expect(object["until"] is NSNull)
    #expect(object["circles"] as? String == "all")
    let now = try #require(Timestamp.date(DemoData.now))
    let expired = AgentAccess(circles: .only(["work"]), until: "2026-10-16T12:00:00Z")
    #expect(expired.hasEnded(at: now))
    #expect(expired.summary(at: now) == "Ended 16 Oct")
    #expect(restricted.summary(names: ["work": "Work"], at: now) == "Work")
    let agent = try Wire.decode(Agent.self, from: Wire.data(DemoData.agents[0]))
    #expect(agent.access == nil && agent.accessSummary == nil)
    let proposal = try Wire.decode(CircleProposal.self, from: Wire.data(DemoCircles.proposals[0]))
    #expect(proposal.batchId == nil && proposal.createdBy == nil)
  }
  @Test func demoBatchAcceptDismissAndSetupProgress() async throws {
    let backend = DemoBackend()
    let before: HomeResponse = try await backend.get("/v1/home")
    #expect(before.proposals.count == 2)
    #expect(Set(before.proposals.compactMap(\.batchId)).count == 1)
    #expect(try await backend.setupStatus().stage == "sorting")
    try await backend.reviewProposals([before.proposals[0].id], keep: true)
    try await backend.reviewProposals([before.proposals[1].id], keep: false)
    let after: HomeResponse = try await backend.get("/v1/home")
    #expect(after.proposals.isEmpty)
    let circles: CirclesResponse = try await backend.get("/v1/circles")
    #expect(circles.circles.contains { $0.id == before.proposals[0].id && $0.confirmed })
    #expect(!circles.circles.contains { $0.id == before.proposals[1].id })
    #expect(try await backend.setupStatus().stage == "ready")
  }
  @Test @MainActor func accessQuickEditsRenewAndEndReadBack() async throws {
    let backend = DemoBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    store.selectedAgent = "agent-3"
    await store.loadPermissions()
    let restricted = AgentAccess(circles: .only(["internship"]), until: "2026-11-17T12:00:00Z")
    store.updateIntegrationAccess(restricted, id: "agent-3")
    await store.waitForPermissionSave()
    let read: Agent = try await backend.get("/v1/agents/agent-3")
    #expect(read.access == restricted)
    #expect(store.permissions.access == restricted)
    store.editPermissions { $0.search = false }
    await store.waitForPermissionSave()
    let preserved: Agent = try await backend.get("/v1/agents/agent-3")
    #expect(preserved.permissions?.access == restricted)
    #expect(preserved.permissions?.search == false)
    var ended = restricted
    ended.until = "2026-10-16T12:00:00Z"
    try await backend.updateAccess(ended, agentID: "agent-3")
    let endRead: Agent = try await backend.get("/v1/agents/agent-3")
    #expect(endRead.accessSummary == "Ended 16 Oct")
    ended.until = nil
    try await backend.updateAccess(ended, agentID: "agent-3")
    let renewed: Agent = try await backend.get("/v1/agents/agent-3")
    #expect(renewed.access?.until == nil)
    #expect(renewed.access?.circles == .only(["internship"]))
  }
  @Test func remoteApprovalCarriesSelectedAccess() async throws {
    let backend = DemoBackend()
    let choice = RemoteApprovalChoice(
      code: "482916", agentId: nil,
      newAgent: ["name": "Work AI", "kind": "chatgpt"],
      access: AgentAccess(circles: .only(["internship"]), until: "2026-11-17T12:00:00Z"), preset:"given")
    try await backend.post("/v1/remote/approve", choice)
    let response: AgentsResponse = try await backend.get("/v1/agents")
    let agent = try #require(response.agents.first { $0.name == "Work AI" })
    #expect(agent.access == choice.access)
    let remote: RemoteState = try await backend.get("/v1/remote")
    #expect(remote.pendingCodes.isEmpty)
  }
  @Test @MainActor func fakeBriefModelBoundsAndAttribution() async throws {
    let backend = DemoBackend()
    let before: BriefSettings = try await backend.get("/v1/settings/briefs")
    #expect(!before.thisMacEnabled)
    let enabled = try await backend.setThisMacBriefs(true)
    #expect(enabled.thisMacEnabled)
    let fake = FakeBriefModel()
    try await OnDeviceBriefWriter(backend: backend, model: fake, now: Timestamp.date(DemoData.now)!)
      .write(circleID: "atlas")
    #expect(fake.context.count <= OnDeviceBriefWriter.contextLimit)
    #expect(fake.context.contains("newest first"))
    let history: BriefsResponse = try await backend.get("/v1/circles/atlas/briefs")
    let brief = try #require(history.briefs.first)
    #expect(brief.summary.split(separator: " ").count == 120)
    #expect(brief.writtenBy == "this-mac")
    #expect(brief.openAsksForYou == ["Bring samples"])
    #expect(brief.upcoming.first?.text == "Studio visit")
    #expect(!brief.evidenceMessageIds.isEmpty)
    let activity: ActivityPage = try await backend.get("/v1/activity")
    #expect(activity.items.contains { $0.agentId == "this-mac" && $0.kind == "summary" })
    _ = try await backend.setThisMacBriefs(false)
    let disabled: BriefSettings = try await backend.get("/v1/settings/briefs")
    #expect(!disabled.thisMacEnabled)
  }
  @Test @MainActor func modelFailureDoesNotWriteBrief() async throws {
    let backend = DemoBackend()
    let old: BriefsResponse = try await backend.get("/v1/circles/atlas/briefs")
    do {
      try await OnDeviceBriefWriter(
        backend: backend, model: FailingBriefModel(), now: Timestamp.date(DemoData.now)!
      ).write(circleID: "atlas")
      Issue.record("A model failure must propagate")
    } catch {}
    let after: BriefsResponse = try await backend.get("/v1/circles/atlas/briefs")
    #expect(after.briefs == old.briefs)
  }
  @Test @MainActor func sixStepSetupAndSnapshotCoverage() {
    #expect(SetupFlow.Step.allCases.count == 6)
    var flow = SetupFlow(step: .syncing)
    flow.advance()
    #expect(flow.step == .who)
    flow.advance()
    #expect(flow.step == .connect)
    #expect(AppStore.starterPrompt == "Set up Limmr for me.")
    for page in [
      "setup-who", "home-setup", "proposals-review", "remote-approval", "integration-everything",
      "integration-work", "integration-ended", "connect-claude-code", "connect-chatgpt",
    ] { #expect(SnapshotRunner.pages.contains(page)) }
  }
}
@MainActor private final class FakeBriefModel: BriefModel {
  var context = ""
  func generate(context: String) async throws -> MacBrief {
    self.context = context
    return MacBrief(
      summary: Array(repeating: "word", count: 140).joined(separator: " "),
      openAsks: ["Bring samples"], upcoming: [.init(date: "2026-10-21", text: "Studio visit")])
  }
}
@MainActor private struct FailingBriefModel: BriefModel {
  func generate(context: String) async throws -> MacBrief {
    throw BackendError.unavailable("Model unavailable")
  }
}

extension A9Tests {
  @Test @MainActor func unicodeMessagesAreTrimmedWithoutSplittingCharacters() async throws {
    let backend = LongMessagesBackend()
    let model = FakeBriefModel()
    try await OnDeviceBriefWriter(
      backend: backend, model: model, now: Timestamp.date(DemoData.now)!
    ).write(circleID: "atlas")
    #expect(model.context.utf8.count <= OnDeviceBriefWriter.contextLimit)
    #expect(model.context.contains("🌿"))
    #expect(!model.context.contains("�"))
    #expect(model.context.range(of: DemoData.date(1)) != nil)
    let data = try #require(await backend.written)
    let object = try #require(JSONSerialization.jsonObject(with: data) as? [String: Any])
    #expect(object["chat_jids"] == nil)  // Admin write derives circle scope; unknown keys are rejected.
    #expect(object["written_by"] as? String == "this-mac")
    #expect((object["evidence_message_ids"] as? [String])?.count ?? 999 <= 200)
  }
}
private actor LongMessagesBackend: Backend {
  let base = DemoBackend()
  var written: Data?
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if path.hasPrefix("/v1/messages?") {
      return try Wire.data(
        MessagesResponse(
          messages: (1...250).map { i in
            Message(
              id: "message-\(i)", chatJid: DemoData.groupJid(0), sender: "Maya",
              text: String(repeating: "🌿", count: 1000), timestamp: DemoData.date(i))
          }))
    }
    if path.hasSuffix("/brief"), method == "PUT" { written = body }
    return try await base.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}
