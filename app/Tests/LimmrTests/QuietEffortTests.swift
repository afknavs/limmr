import Foundation
import Testing
@testable import Limmr
@testable import LimmrKit

@Suite("E7 quiet work") @MainActor struct QuietEffortTests {
  @Test func dismissPersistsHiddenAndRefreshesPresentation() async throws {
    let backend = DemoBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    let before: EffortsResponse = try await backend.get("/v1/efforts")
    let quiet = try #require(before.efforts.first { $0.status == "dormant" })
    let revision = store.effortRevision
    let updated = await store.updateEffort(quiet.id, values: ["hidden": true])
    #expect(updated?.hidden == true)
    #expect(store.effortRevision > revision)
    let after: EffortsResponse = try await backend.get("/v1/efforts")
    #expect(!after.efforts.filter(EffortPresentation.visible).contains { $0.id == quiet.id })
    #expect(!store.home.circleUpdates.contains { $0.effortId == quiet.id })
  }
}
