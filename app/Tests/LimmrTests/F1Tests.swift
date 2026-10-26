import Foundation
import Testing

@testable import Limmr
@testable import LimmrKit

@Suite("F1 current chats and circle reset") struct F1Tests {
  @Test func legacySettingsDefaultAndWindowBoundary() throws {
    let settings = try Wire.decode(
      AppSettings.self, from: Data(#"{"owner":"Alex","hidden_chats":[],"retention_days":180}"#.utf8)
    )
    #expect(settings.currentDays == 180)
    let now = try #require(Timestamp.date(DemoData.now))
    let at = ISO8601DateFormatter().string(from: now.addingTimeInterval(-180 * 86400))
    let boundary = Chat(jid: "boundary", name: "Boundary", lastMessageTime: at)
    #expect(CurrentChats.isCurrent(boundary, days: 180, now: now))
    #expect(!CurrentChats.isCurrent(boundary, days: 179, now: now))
    #expect(!CurrentChats.isCurrent(Chat(jid: "unknown", name: "Unknown"), days: 180, now: now))
    let old = try #require(DemoData.chats.first { $0.name == "Former school batch" })
    #expect(!CurrentChats.isCurrent(old, days: 180, now: now))
    #expect(CurrentChats.ordered(DemoData.chats, query: "Former school").map(\.jid) == [old.jid])
  }
  @Test @MainActor func currentPeopleAndProposalActivity() async throws {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    let people = store.orderedPeople(query: "")
    let older = people.filter { !store.personIsCurrent($0) }
    #expect(older.map(\.name) == ["Avery Lane"])
    let current = people.filter { store.personIsCurrent($0) }
    #expect(current.count == 12)
    #expect(store.personLastChat(current[0]) >= store.personLastChat(current[1]))
    #expect(store.proposalActivity(store.home.proposals[0]).contains("2 current"))
    #expect(store.proposalActivity(store.home.proposals[0]).contains("active"))
    #expect(store.chats.filter { !store.chatIsCurrent($0) }.count == 4)
  }
  @Test @MainActor func resetReadbackKeepsDirectoryAndActivity() async throws {
    let backend = DemoBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    let people = store.people
    let chats = store.chats
    let before: ActivityPage = try await backend.get("/v1/activity")
    do {
      try await backend.post("/v1/circles/reset", ["confirm": false])
      Issue.record("Reset accepted without confirmation")
    } catch {}
    #expect(!store.circles.isEmpty)
    await store.resetCircles()
    #expect(store.error == nil)
    #expect(store.circles.isEmpty && store.home.proposals.isEmpty && store.home.queued.isEmpty)
    #expect(store.people == people && store.chats == chats)
    #expect(!store.circleMap.nodes.contains { $0.kind == "circle" })
    #expect(store.circleMap.events.isEmpty)
    #expect(store.home.needsReply.isEmpty)
    let after: ActivityPage = try await backend.get("/v1/activity")
    #expect(after.items.count == before.items.count + 1)
    #expect(after.items.first?.tool == "reset_circles")
    #expect(try await backend.setupStatus().stage == "new")
  }
}
