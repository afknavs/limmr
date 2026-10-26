import Foundation
import Testing
@testable import LimmrKit

@Suite("W2 waiting and compatibility contracts") struct WaitingContractTests {
  private let waitingJSON = #"{"waiting":[{"id":"watch-1","agent":"Claude Code","kind":"watch","sentence":"Tell me when the date changes","where":"Design Club","since":"2026-10-18T09:00:00Z","new_hits":2,"latest":{"at":"2026-10-18T10:00:00Z","chat":"Design Club","sender":"Maya Rao","text":"Friday works"}},{"id":"handoff-1","agent":"Codex","kind":"handoff","sentence":"Wait for Robin's feedback","where":"Robin Bell","since":"2026-10-18T09:10:00Z","new_hits":0,"latest":null},{"id":"feed-1","agent":"ChatGPT","kind":"feed","sentence":"Keep me up to date","where":"All circles","since":"2026-10-18T09:20:00Z","new_hits":0,"latest":null}],"future_field":true}"#

  @Test func waitingEndpointAndHomeDecodeTheSameAdditiveShape() throws {
    let response = try Wire.decode(WaitingResponse.self, from: Data(waitingJSON.utf8))
    let home = try Wire.decode(HomeResponse.self, from: Data(waitingJSON.utf8))
    #expect(home.waiting == response.waiting)
    #expect(response.waiting.map(\.kind) == ["watch", "handoff", "feed"])
    let watch = try #require(response.waiting.first)
    #expect(watch.agent == "Claude Code" && watch.newHits == 2)
    #expect(watch.since == "2026-10-18T09:00:00Z" && watch.where == "Design Club")
    #expect(watch.latest?.sender == "Maya Rao" && watch.latest?.messageId == nil)
    #expect(response.waiting.last?.latest == nil)
    #expect(try Wire.decode(HomeResponse.self, from: Wire.data(home)).waiting == response.waiting)
    for json in ["{}", #"{"waiting":null}"#, #"{"waiting":[]}"#] {
      #expect(try Wire.decode(HomeResponse.self, from: Data(json.utf8)).waiting.isEmpty)
    }
  }

  @Test func obsoletePermissionFieldsAreIgnoredAndNeverEncoded() throws {
    let permission = Permissions.defaults(remote: false)
    var legacy = try #require(JSONSerialization.jsonObject(with: Wire.data(permission)) as? [String: Any])
    legacy["signature_mode"] = "always"
    legacy["signature"] = "Old template"
    legacy["voices"] = ["as_agent"]
    let decoded = try Wire.decode(Permissions.self, from: JSONSerialization.data(withJSONObject: legacy))
    #expect(decoded == permission)
    let wire = try #require(JSONSerialization.jsonObject(with: Wire.data(decoded)) as? [String: Any])
    #expect(wire["signature_mode"] == nil && wire["signature"] == nil && wire["voices"] == nil)
  }



  @Test func waitingMirrorsHomeAndWatchActionsIncludingFeed() async throws {
    let backend = DemoBackend(waitingCount: 4)
    let initial: WaitingResponse = try await backend.get("/v1/waiting")
    #expect(initial.waiting.count == 4)
    for item in initial.waiting {
      let detail: WatchDetail = try await backend.get("/v1/watches/\(item.id)")
      #expect(detail.summary.goal == item.sentence)
      #expect(detail.summary.where == item.where)
    }
    try await backend.post("/v1/watches/w_exam/seen")
    let seen: WaitingResponse = try await backend.get("/v1/waiting")
    #expect(seen.waiting.first { $0.id == "w_exam" }?.newHits == 0)
    for action in ["pause", "resume"] {
      try await backend.post("/v1/watches/w_feed/\(action)")
      let detail: WatchDetail = try await backend.get("/v1/watches/w_feed")
      #expect(detail.summary.status == (action == "pause" ? "paused" : "active"))
    }
    try await backend.post("/v1/watches/w_feed/stop")
    let stopped: WaitingResponse = try await backend.get("/v1/waiting")
    let home: HomeResponse = try await backend.get("/v1/home")
    #expect(!stopped.waiting.contains { $0.id == "w_feed" })
    #expect(home.waiting == stopped.waiting)
    let empty: WaitingResponse = try await EmptyBackend().get("/v1/waiting")
    #expect(empty.waiting.isEmpty)
  }
}
