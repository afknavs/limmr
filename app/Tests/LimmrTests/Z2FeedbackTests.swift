import Foundation
import LimmrKit
import Testing
@testable import Limmr

@MainActor struct Z2FeedbackTests {
  @Test func lightDiscoveryAndSearchRespectVisibility() {
    #expect(!AccessoryAvailability.isAvailable(enabled: false, installed: { false }))
    #expect(AccessoryAvailability.isAvailable(enabled: true, installed: { false }))
    #expect(AccessoryAvailability.isAvailable(enabled: false, installed: { true }))
    #expect(SettingsSearchEntry.matches("accessory").isEmpty)
    #expect(SettingsSearchEntry.matches("accessory", showAccessory: true).first?.tab == .integrations)
  }
  @Test func statusChangeAndUndoUpdateHomeAndReadBack() async throws {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh(); await store.refreshHomeEfforts()
    let effort = try #require(store.homeEfforts.first { $0.status == "on_you" })
    let changed = await store.setEffortStatus(effort.id, from: effort.status, to: "moving")
    #expect(changed?.status == "moving")
    #expect(store.homeEfforts.first { $0.id == effort.id }?.status == "moving")
    let readback: EffortDetailResponse = try await store.backend.get("/v1/efforts/\(effort.id)")
    #expect(readback.effort.status == "moving")
    let undone = await store.undoEffortStatus(effort.id)
    #expect(undone?.status == "on_you")
    #expect(store.effortUndo[effort.id] == nil)
  }
  @Test func detailsOnlyContainPopulatedFieldsAndNamesRoundTrip() async throws {
    #expect(ProfileDetailsPresentation.rows(.init()).isEmpty)
    let rows = ProfileDetailsPresentation.rows(.init(phones: [.init(label: "mobile", value: "+1 555-0103", source: "whatsapp")]))
    #expect(rows.count == 1 && rows[0].title == "Phone" && rows[0].source == "whatsapp")
    let backend = DemoBackend()
    _ = try await backend.request("/v1/people/person-04", method: "PATCH", body: Data(#"{"label":"Maya Rao","other_names":["Maya","Mayu"]}"#.utf8))
    let detail: PersonDetail = try await backend.get("/v1/people/person-04")
    #expect(detail.person.otherNames == ["Maya", "Mayu"])
  }
}

private actor Z2RefreshBackend: Backend {
  private let base = DemoBackend()
  private var failing = false
  func failRefresh() { failing = true }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if failing && path.hasPrefix("/v1/people?") { throw BackendError.http(500) }
    return try await base.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { AsyncThrowingStream { $0.finish() } }
}

extension Z2FeedbackTests {
  @Test func failedRefreshKeepsLastGoodPeople() async {
    let backend = Z2RefreshBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    let ids = store.people.map(\.id)
    #expect(!ids.isEmpty)
    await backend.failRefresh()
    await store.refresh()
    #expect(store.people.map(\.id) == ids)
    #expect(store.refreshError?.contains("Couldn’t refresh") == true)
  }
}

extension Z2FeedbackTests {
  @Test func decodeAuditUsesTheAppsAgentReadRoute() async {
    var lines: [String] = []
    _ = await DecodeAudit.run(backend: DemoBackend()) { lines.append($0) }
    #expect(!lines.contains { $0.contains("/permissions") })
    #expect(lines.contains { $0.hasPrefix("/v1/agents/agent-1  ok") })
  }
}
