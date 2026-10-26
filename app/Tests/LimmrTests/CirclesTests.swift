import AppKit
import Foundation
import SwiftUI
import Testing

@testable import Limmr
@testable import LimmrKit

@Suite("A3 circles") struct CirclesTests {
  @MainActor @Test func sidebarAlwaysReturnsToPeopleAndNeverPresentsInspector() async {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    store.circlePath = [.circle("atlas"), .person("person-04")]
    store.selectedPerson = "person-04"
    store.selectPage(.overview)
    store.selectPage(.circles)
    #expect(store.circlePath.isEmpty && store.selectedPerson.isEmpty)
    #expect(
      !store.showConnect && store.connectKind == nil && !store.showRemote && !store.showSettings)
    store.openCircle("atlas")
    #expect(store.circlePath == [.circle("atlas")])
    store.circlePath.append(.person("person-04"))
    #expect(store.circlePath.count == 2 && !store.showConnect)
    #expect(Page.allCases.map(\.rawValue) == ["Home", "People", "Activity"])
  }
  @MainActor @Test func integrationDeepLinkOpensSettings() async {
    let store = AppStore(demo: true, snapshot: true)
    store.navigate(URL(string: "limmr://agents/agent-2")!)
    #expect(
      store.showSettings && store.settingsTab == .integrations && store.selectedAgent == "agent-2")
  }
  @MainActor @Test func connectFlowHasOnlyOnePresentingWindow() {
    let store = AppStore(demo: true, snapshot: true)
    store.showSetup = true
    store.connectKind = .codex
    #expect(
      store.setupConnectKind.wrappedValue == .codex && store.settingsConnectKind.wrappedValue == nil
    )
    store.showSetup = false
    #expect(
      store.setupConnectKind.wrappedValue == nil && store.settingsConnectKind.wrappedValue == .codex
    )
    store.reviewIntegration("agent-2")
    #expect(store.connectKind == nil && store.showSettings && store.settingsTab == .integrations)
  }
  @Test func gridBreakpointAndSendRuleCopy() {
    #expect(CirclesPresentation.homeColumns(contentWidth: 899) == 1)
    #expect(CirclesPresentation.homeColumns(contentWidth: 900) == 2)
    #expect(SendRule.allCases.map(\.title) == ["Send freely", "Confirm in chat", "Ask me in Limmr", "Never"])
    #expect(SendRule.confirm.explanation.contains("in your conversation"))
    #expect(SendRule.never.explanation.contains("cannot send"))
  }
  @MainActor @Test func demoHasSevenCirclesOverlappingPeopleAndNoPhantomChats() async throws {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    #expect(
      store.circles.count == 7 && store.home.suggestions.filter { $0.taskId == nil }.count == 3
        && store.home.proposals.count == 2)
    let jids = Set(store.chats.map(\.jid))
    let people = Set(store.people.map(\.id))
    for circle in store.circles {
      await store.loadCircle(circle.id)
      let detail = try #require(store.circleDetails[circle.id])
      #expect(detail.chats.allSatisfy { jids.contains($0.chatJid) })
      #expect(detail.people.allSatisfy { people.contains($0.personId) })
      #expect(detail.brief?.summary.split(separator: " ").count ?? 121 <= 120)
    }
    let maya = store.circles.filter { $0.people.contains { $0.personId == "person-04" } }
    #expect(Set(maya.map(\.id)) == ["friends", "university", "atlas"])
    #expect(
      store.circleDetails["university"]?.people.first { $0.personId == "person-04" }?.role
        != store.circleDetails["atlas"]?.people.first { $0.personId == "person-04" }?.role)
    await store.useSampleData(false, realBackend: EmptyBackend())
    #expect(
      store.circles.isEmpty && store.home.today.isEmpty && store.circleDetails.isEmpty
        && store.circleMap.nodes.isEmpty)
  }
  @MainActor @Test func briefRevertRuleEditSuggestionAndProposalPersist() async throws {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    await store.loadCircle("atlas")
    let old = try #require(store.circleHistory["atlas"]?.first { $0.version == 2 })
    await store.circleAction("atlas", suffix: "/briefs/2/revert")
    #expect(store.circleDetails["atlas"]?.brief?.summary == old.summary)
    #expect(store.circleDetails["atlas"]?.brief?.version == 4)
    #expect(store.circleHistory["atlas"]?.count == 3)
    await store.circleAction("atlas", method: "PATCH", values: ["send_rule": "never"])
    let suggestion = try #require(store.home.suggestions.first { $0.circleId == "atlas" })
    await store.decideSuggestion(suggestion, send: true)
    #expect(store.error != nil && store.home.suggestions.contains { $0.id == suggestion.id })
    await store.circleAction("atlas", method: "PATCH", values: ["send_rule": "confirm"])
    await store.decideSuggestion(suggestion, send: true, text: "Exact edited demo reply")
    #expect(store.error == nil && !store.home.suggestions.contains { $0.id == suggestion.id })
    #expect(
      store.activity.first?.note == "Exact edited demo reply"
        && store.activity.first?.confirmedVia == "mac")
    let proposal = try #require(store.home.proposals.first)
    await store.circleAction(proposal.id, method: "PATCH", values: ["name": "Morning walks"])
    await store.circleAction(proposal.id, suffix: "/confirm")
    #expect(store.circles.contains { $0.name == "Morning walks" && $0.confirmed })
    #expect(!store.home.proposals.contains { $0.id == proposal.id })
    #expect(store.error == nil)
  }
  @MainActor @Test func cardsSizeToTheirContentAndPulsesTrackLooking() async throws {
    _ = NSApplication.shared
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    await store.refreshHomeEfforts()
    for width in [360.0, 572.0] {
      var heights: [Double] = []
      for tile in HomeTiles.tiles(circles: store.circles, home: store.home,
        efforts: store.homeEfforts, now: Timestamp.date(DemoData.now)!) {
        let host = NSHostingView(
          rootView: HomeCircleTileView(store: store, tile: tile).environment(\.snapshotRendering, true)
            .frame(width: width))
        heights.append(host.fittingSize.height)
        if tile.rectangles.isEmpty && tile.brief == nil { #expect(host.fittingSize.height < 100) }
      }
      #expect(!heights.isEmpty && heights.allSatisfy { $0 > 40 && $0 < 500 })
    }
    let chat = try #require(store.circles.first { $0.id == "atlas" }?.chats.first?.chatJid)
    #expect(store.unseenMessages(chat) > 0)
    store.markCircleSeen("atlas")
    #expect(store.unseenMessages(chat) == 0)
  }

  @MainActor @Test func failedProposalEditDoesNotKeepOriginal() async {
    let backend = FailingProposalEdit()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    await store.keepProposal("proposal-1", name: "Morning walks")
    #expect(store.error != nil)
    #expect(await backend.confirmations == 0)
    #expect(store.home.proposals.contains { $0.id == "proposal-1" })
  }

  @MainActor @Test func keepingProposalsPreservesChats() async {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    for proposal in store.home.proposals { await store.keepProposal(proposal.id) }
    #expect(store.circles.count == 9)
    #expect(store.circles.first { $0.id == "proposal-1" }?.chats.count == 2)
    #expect(store.circles.first { $0.id == "proposal-2" }?.chats.count == 2)

  }

  @MainActor @Test func activityBriefLinkCannotJumpToALaterRevert() async throws {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    await store.loadCircle("atlas")
    let brief = try #require(store.circleDetails["atlas"]?.brief)
    let at = ISO8601DateFormatter().string(
      from: try #require(Timestamp.date(brief.writtenAt)).addingTimeInterval(1))
    let row = Activity(
      id: 50, at: at, agentId: "agent-2", tool: "update_brief", kind: "summary",
      chatJids: brief.chatJids, messageIds: brief.evidenceMessageIds, count: 1, note: brief.summary)
    await store.circleAction("atlas", suffix: "/briefs/3/revert")
    #expect(store.circleDetails["atlas"]?.brief?.version == 4)
    await store.openActivityBrief(row)
    #expect(store.error == nil && store.circlePath == [.circle("atlas"), .brief("atlas", 3)])
  }

  @MainActor @Test func failedDeliveryWithHTTP200IsVisibleAndCannotRetry() async throws {
    let backend = FailedSuggestionDelivery()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    let suggestion = try #require(store.home.suggestions.first)
    await store.decideSuggestion(suggestion, send: true)
    #expect(store.error?.contains("Check Activity") == true && !store.canRetry)
    #expect(store.attemptedSuggestionSends.contains(suggestion.id))
    await store.retry()
    await store.decideSuggestion(suggestion, send: true)
    #expect(await backend.sends == 1)
  }

  @MainActor @Test func lostSendResponseNeverOffersARetry() async throws {
    let backend = FailedSuggestionDelivery(loseResponse: true)
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    let suggestion = try #require(store.home.suggestions.first)
    await store.decideSuggestion(suggestion, send: true)
    #expect(store.error?.contains("could not be confirmed") == true && !store.canRetry)
    await store.retry()
    await store.decideSuggestion(suggestion, send: true)
    #expect(await backend.sends == 1)
  }

  @Test func exactEngineWireShapesIncludingNullCollections() throws {
    let circle =
      #"{"id":"atlas","name":"Atlas","kind":"project","symbol":"house","description":"","send_rule":"free","allow_as_me":false,"created_by":"you","confirmed":true,"updated_at":"2026-10-17T12:00:00Z","chats":null,"people":null,"topics":null}"#
    let home = Data(
      "{\"today\":[{\"circle\":\(circle),\"brief\":null,\"new_messages\":8,\"open_asks\":0,\"upcoming\":0}],\"suggestions\":null,\"proposals\":[{\"id\":\"atlas\",\"circle\":\(circle),\"why\":\"Shared members\",\"source\":\"structure\"}],\"integrations\":[]}"
        .utf8)
    let decoded = try Wire.decode(HomeResponse.self, from: home)
    #expect(
      decoded.today.first?.newMessages == 8 && decoded.today.first?.circle.chats.isEmpty == true)
    #expect(decoded.proposals.first?.why == "Shared members")
    let map = try Wire.decode(
      CircleMap.self,
      from: Data(
        #"{"nodes":[{"id":"atlas","kind":"circle","title":"Atlas"}],"edges":[{"source":"atlas","target":"p1","kind":"membership"}],"pulses":[{"id":"atlas","new_messages":4}]}"#
          .utf8))
    #expect(map.nodes.first?.title == "Atlas" && map.pulses.first?.newMessages == 4)
    let encoded = try #require(
      JSONSerialization.jsonObject(with: Wire.data(decoded.today[0].circle)) as? [String: Any])
    #expect(encoded["send_rule"] as? String == "free" && encoded["allow_as_me"] == nil)
  }
}

private actor FailingProposalEdit: Backend {
  let samples = DemoBackend()
  var confirmations = 0
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if method == "PATCH", path == "/v1/circles/proposal-1" { throw BackendError.http(503) }
    if path == "/v1/circles/proposal-1/confirm" { confirmations += 1 }
    return try await samples.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}

private actor FailedSuggestionDelivery: Backend {
  let samples = DemoBackend()
  let loseResponse: Bool
  init(loseResponse: Bool = false) { self.loseResponse = loseResponse }
  var sends = 0
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    let data = try await samples.request(path, method: method, body: body)
    if path.hasPrefix("/v1/suggestions/"), path.hasSuffix("/send") {
      sends += 1
      if loseResponse { throw BackendError.http(500) }
      var result = try Wire.decode(CircleDraft.self, from: data)
      result.state = "failed"
      result.messageId = nil
      return try Wire.data(result)
    }
    return data
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}
