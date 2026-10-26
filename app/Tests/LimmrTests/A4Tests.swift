import AppKit
import SwiftUI
import Testing

@testable import Limmr
@testable import LimmrKit

@Suite("A4 watches and compatibility") struct A4Tests {
  @MainActor @Test func editedBodyTravelsUnchangedThroughDemo() async throws {
    let backend = RecordingSuggestionBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    let suggestion = try #require(store.home.suggestions.first)
    let parts = store.suggestionText(suggestion)
    #expect(parts == "I’ll check the train and let you know this evening.")
    await store.decideSuggestion(suggestion, send: true, text: "An edited reply.")
    #expect(store.error == nil)
    #expect(await backend.sentText == "An edited reply.")
    #expect(store.activity.first?.note == "An edited reply.")
    // Demo dispatch preserves the supplied body.
    let demo = DemoBackend()
    let data = try await demo.request(
      "/v1/suggestions/suggestion-1/send", method: "POST", body: Wire.data(["text": "Body only"]))
    let result = try Wire.decode(CircleDraft.self, from: data)
    #expect(result.finalText == "Body only")
  }

  @Test func demoIgnoresCachedSendOptions() async throws {
    for value in ["as_agent", "as_me", "auto", "cached-value"] {
      let demo = DemoBackend()
      let body = "Exact body\n— A caller closing\n"
      let request: [String: Any] = ["text": body, "voice": value, "signed": true]
      let data = try await demo.request("/v1/suggestions/suggestion-1/send", method: "POST",
        body: JSONSerialization.data(withJSONObject: request))
      let result = try Wire.decode(CircleDraft.self, from: data)
      #expect(result.finalText == body && result.agentId == "agent-2")
      let response: ActivityPage = try await demo.get("/v1/activity")
      #expect(response.items.first?.note == body && response.items.first?.agentId == "agent-2")
    }
  }

  @Test func watchWireContractIgnoresFutureFieldsAndNullableValues() throws {
    let json =
      #"{"id":"w_exam","kind":"watch","title":"Exam news","goal":"Tell me when anyone posts about the exam","where":"University","action":"notify","status":"active","created_by":"Codex","created_at":"2026-10-17T09:10:00Z","expires_at":null,"new_hits":2,"latest":{"at":"2026-10-17T12:00:00Z","chat":"University class","sender":"Dr. Morgan","text":"IA date moved to 24 Oct"},"runner":null,"digest":"","instructions":null,"hits":null,"stats":{"runs":0,"replies":0,"last_run_at":null},"future_field":true}"#
    let detail = try Wire.decode(WatchDetail.self, from: Data(json.utf8))
    #expect(detail.summary.where == "University" && detail.summary.latest?.messageId == nil)
    #expect(detail.summary.expiresAt == nil && detail.summary.runner == nil)
    #expect(detail.instructions.isEmpty && detail.hits.isEmpty)
    #expect(try Wire.decode(WatchDetail.self, from: Wire.data(detail)) == detail)
    let noLatest = json.replacingOccurrences(
      of:
        #""latest":{"at":"2026-10-17T12:00:00Z","chat":"University class","sender":"Dr. Morgan","text":"IA date moved to 24 Oct"}"#,
      with: #""latest":null"#)
    #expect(try Wire.decode(WatchSummary.self, from: Data(noLatest.utf8)).latest == nil)
    #expect(try Wire.decode(HomeResponse.self, from: Data("{}".utf8)).watches.isEmpty)
  }

  @Test func demoWatchesImplementEveryPostAndFinalStop() async throws {
    let demo = DemoBackend()
    let list: WatchesResponse = try await demo.get("/v1/watches")
    #expect(list.watches.map(\.title) == ["Exam news", "Atlas updates", "Website changes"])
    let home: HomeResponse = try await demo.get("/v1/home")
    #expect(home.watches == list.watches)
    let handoff: WatchDetail = try await demo.get("/v1/watches/w_website")
    #expect(handoff.summary.runner?.label == "Codex · website" && handoff.stats.replies == 2)
    #expect(handoff.summary.expiresAt != nil)
    let seen = try await demo.request("/v1/watches/w_exam/seen", method: "POST", body: nil)
    #expect(seen.isEmpty)
    var detail: WatchDetail = try await demo.get("/v1/watches/w_exam")
    #expect(detail.summary.newHits == 0)
    let instruction = "Ignore lab batch B posts"
    let updated = try await demo.request(
      "/v1/watches/w_exam/instructions", method: "POST", body: Wire.data(["text": instruction]))
    detail = try Wire.decode(WatchDetail.self, from: updated)
    #expect(detail.instructions.last?.text == instruction)
    for text in ["", "   ", String(repeating: "a", count: 501)] {
      await #expect(throws: (any Error).self) {
        try await demo.request(
          "/v1/watches/w_exam/instructions", method: "POST", body: Wire.data(["text": text]))
      }
    }
    for (route, status) in [("pause", "paused"), ("resume", "active"), ("stop", "stopped")] {
      let data = try await demo.request("/v1/watches/w_exam/" + route, method: "POST", body: nil)
      #expect(try Wire.decode(WatchSummary.self, from: data).status == status)
    }
    for route in ["resume", "pause", "stop", "instructions"] {
      await #expect(throws: (any Error).self) {
        try await demo.request(
          "/v1/watches/w_exam/" + route, method: "POST", body: Wire.data(["text": instruction]))
      }
    }
    let afterStop: HomeResponse = try await demo.get("/v1/home")
    #expect(!afterStop.watches.contains { $0.id == "w_exam" })
    let history: WatchesResponse = try await demo.get("/v1/watches")
    #expect(history.watches.last?.status == "stopped")
    await #expect(throws: (any Error).self) { try await demo.post("/v1/watches/missing/seen") }
    let persisted: WatchDetail = try await demo.get("/v1/watches/w_exam")
    #expect(persisted.instructions.count == 1 && persisted.summary.newHits == 0)
  }

  @MainActor @Test func demoCircleTopicsAndSpecificContributionsPersist() async {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    for circle in store.circles {
      await store.loadCircle(circle.id)
      #expect((1...2).contains(store.circleDetails[circle.id]?.topics.count ?? 0))
      #expect(
        store.circleDetails[circle.id]?.brief?.peopleNotes.allSatisfy {
          !$0.contribution.contains("shared the latest plan")
        } == true)
    }
    await store.circleAction("university", method: "PATCH", values: ["send_rule": "never"])
    #expect(store.circleDetails["university"]?.circle.sendRule == .never)
    await store.useSampleData(false, realBackend: EmptyBackend())
    #expect(store.home.watches.isEmpty)
  }
}

private actor RecordingSuggestionBackend: Backend {
  private let samples = DemoBackend()
  var sentText: String?
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if path.hasPrefix("/v1/suggestions/"), path.hasSuffix("/send"), let body {
      sentText =
        (try JSONSerialization.jsonObject(with: body) as? [String: Any])?["text"] as? String
    }
    return try await samples.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}
