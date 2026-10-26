import Foundation
import Testing

@testable import Limmr
@testable import LimmrKit

@Suite("R1 structural placements") struct R1Tests {
  @Test func groupedRowsAndLegacyDecode() throws {
    let home = try Wire.decode(
      HomeResponse.self,
      from: Data(
        #"{"circle_changes":[{"id":"a","circle_id":"studio","name":"Studio","agent":"Limmr","action":"placed","at":"2026-10-19T12:00:00Z","count":40,"change_ids":["a","b"],"people_ids":["p1","p2"],"people_changes":{"p1":"a","p2":"b"},"by_kind":"structure"}],"circle_changes_more":2}"#
          .utf8))
    let row = try #require(home.circleChanges.first)
    #expect(home.circleChanges.count == 1 && home.circleChangesMore == 2)
    #expect(row.text == "Limmr placed 40 people in Studio")
    #expect(row.changeIds == ["a", "b"] && row.peopleChanges["p1"] == "a")
    let legacy = try Wire.decode(
      CircleAutoChange.self,
      from: Data(
        #"{"id":"old","circle_id":"studio","name":"Studio","agent":"Example AI","action":"updated","at":"2026-10-19T12:00:00Z"}"#
          .utf8))
    #expect(legacy.count == 1 && legacy.changeIds == ["old"] && legacy.peopleChanges.isEmpty)
    #expect(
      try Wire.decode(HomeResponse.self, from: Wire.data(home)).circleChanges.first?.count == 40)
  }

  @Test func structuralActivityText() {
    let activity = Activity(
      id: 1, at: DemoData.now, agentId: "Limmr",
      tool: "circle_structure_placed", kind: "people_write", chatJids: [], messageIds: [],
      count: 40,
      note: "Limmr placed 40 people in Studio")
    #expect(activity.verb == "Limmr placed 40 people in Studio")
    #expect(
      ActivityPresentation.tasks([activity]).first?.title(agent: "Limmr", chatName: { $0 })
        == activity.verb)
  }

  @Test @MainActor func reviewAndBothUndoActionsReadBack() async throws {
    let backend = DemoBackend(structurePlacements: true)
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    let row = try #require(store.home.circleChanges.first)
    #expect(store.home.circleChanges.count == 1 && row.count == 40)
    let people: LimmrKit.Circle = try await backend.get("/v1/circles/\(row.circleId)")
    #expect(people.people.count == 40 && people.people.allSatisfy { $0.source == "structure" })
    let changes: CircleChangesResponse = try await backend.get("/v1/circles/changes")
    #expect(changes.changes.first?.peopleChanges.count == 40)
    await store.undoCircleChanges([row.changeIds[0]], single: true)
    #expect(store.home.circleChanges.first?.count == 39)
    let rest = try #require(store.home.circleChanges.first)
    await store.undoCircleChanges(rest.changeIds)
    #expect(store.home.circleChanges.isEmpty)
    let restored: LimmrKit.Circle = try await backend.get("/v1/circles/\(row.circleId)")
    #expect(restored.people.isEmpty)
    #expect(SnapshotRunner.pages.contains("home-structure"))
    #expect(SnapshotRunner.pages.contains("home-structure-review"))
  }
}
