import Foundation
import Testing

@testable import Limmr
@testable import LimmrKit

@Suite("F2 personal AIs and About you") struct F2Tests {
  @Test func presetsMatchEngine() throws {
    let graph = DemoPeople.graph(count: 300)
    #expect(Set(graph.nodes.map(\.id)).count == graph.nodes.count)
    #expect(DemoData.people.first?.id == "me")
    let own = Permissions.preset("own")
    #expect(own.access == AgentAccess())
    #expect(own.writePeople && own.saveSummaries && own.downloadMedia && own.requestSend)
    let given = Permissions.preset("given")
    #expect(given.access?.circles == .only([]))
    #expect(given.access?.unsorted == false)
    #expect(!given.writePeople && !given.requestSend && !given.downloadMedia)
  }
  @Test func ownerPatchReadbackAndReview() async throws {
    let b = DemoBackend()
    let initial: OwnerProfile = try await b.get("/v1/owner")
    #expect(initial.name == "Alex" && initial.studies.contains("Northfield"))
    #expect(initial.people.contains { $0.relation == "mum" })
    _ = try await b.request(
      "/v1/owner", method: "PUT", body: Wire.data(["about": "I enjoy reading."]))
    let changed: OwnerProfile = try await b.get("/v1/owner")
    #expect(
      changed.about == "I enjoy reading." && changed.people == initial.people
        && changed.studies == initial.studies)
    let suggestion = try #require(changed.suggestions.first)
    try await b.post("/v1/owner/suggestions/\(suggestion.id)/confirm")
    let reviewed: OwnerProfile = try await b.get("/v1/owner")
    #expect(reviewed.work == "Part-time library assistant" && reviewed.suggestions.isEmpty)
  }
  @Test func ownRemoteIgnoresLimitedAccess() async throws {
    let b = DemoBackend()
    let choice = RemoteApprovalChoice(
      code: "482916", agentId: nil, newAgent: ["name": "Personal ChatGPT", "kind": "chatgpt"],
      access: AgentAccess(circles: .only([])))
    try await b.post("/v1/remote/approve", choice)
    let list: AgentsResponse = try await b.get("/v1/agents")
    let a = try #require(list.agents.first { $0.name == "Personal ChatGPT" })
    #expect(a.preset == "own" && a.permissions == Permissions.preset("own"))
  }
  @Test func integrationPresetSwitchReadBack() async throws {
    let b = DemoBackend()
    for kind in ["given", "own"] {
      try await b.post("/v1/agents/agent-2/preset", ["preset": kind])
      let a: Agent = try await b.get("/v1/agents/agent-2")
      #expect(a.preset == kind && a.permissions == Permissions.preset(kind))
    }
  }
  @Test @MainActor func ownerFlowsHaveSnapshotRoutesAndNoExtraPolling() async throws {
    #expect(SnapshotRunner.pages.contains("about-you"))
    #expect(SnapshotRunner.pages.contains("setup-about-you"))
    #expect(SnapshotRunner.pages.contains("person-you"))
    let b = F2CountingBackend()
    let store = AppStore(demo: true, snapshot: true, backend: b)
    await store.refresh()
    await store.refresh()
    #expect(store.ownerProfile.name == "Alex")
    #expect(await b.paths.filter { $0 == "/v1/owner" }.isEmpty)
  }
  @Test @MainActor func presetWaitsForEarlierCapabilitySave() async throws {
    let backend = DemoBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    store.selectedAgent = "agent-2"
    await store.loadPermissions()
    store.editPermissions { $0.search = false }
    await store.switchPreset("given", agentID: "agent-2")
    let read: Agent = try await backend.get("/v1/agents/agent-2")
    #expect(read.preset == "given" && read.permissions == Permissions.preset("given"))
    #expect(store.permissions == Permissions.preset("given"))
  }
  @Test func ownerWireAndHomeCompatibility() throws {
    let data = try Wire.data(OwnerProfile.sample)
    let decoded = try Wire.decode(OwnerProfile.self, from: data)
    #expect(decoded == OwnerProfile.sample)
    let thin = OwnerProfile()
    #expect(thin.thin)
    let home = try Wire.decode(
      HomeResponse.self,
      from: Data(
        #"{"circle_changes":[{"id":"auto","circle_id":"Design Club 2026","name":"Design Club 2026","agent":"Codex","action":"added","at":"2026-10-17T12:00:00Z"}]}"#
          .utf8))
    #expect(home.circleChanges.first?.name == "Design Club 2026" && home.circleAdditions.isEmpty)
  }
}
actor F2CountingBackend: Backend {
  let demo = DemoBackend()
  var paths: [String] = []
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    paths.append(path)
    return try await demo.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}
