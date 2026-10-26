import Foundation
import Testing

@testable import Limmr
@testable import LimmrKit

@Suite("W2 Home refresh and permission saves") struct W2Tests {
  @MainActor @Test func homeShowsTriggerCardOnlyForWaitingItemsWithoutDuplicateTiles() async throws {
    for count in [0, 1, 4] {
      let backend = DemoBackend(waitingCount: count)
      let store = AppStore(demo: true, snapshot: true, backend: backend)
      await store.refresh()
      #expect(store.home.waiting.count == count)
      #expect(HomePresentation.mainSections(store.home).contains("triggers") == (count > 0))
      #expect(!HomePresentation.mainSections(store.home).contains("waiting"))
      #expect(!HomePresentation.mainSections(store.home).contains("watches"))
      if count > 0 {
        #expect(
          Array(HomePresentation.mainSections(store.home).prefix(4)) == [
            "greeting", "search", "dashboard", "tasks",
          ])
        let id = try #require(store.home.waiting.first?.id)
        try await backend.post("/v1/watches/\(id)/stop")
        await store.refresh()
        #expect(store.home.waiting.count == count - 1)
        #expect(!store.home.waiting.contains { $0.id == id })
      }
      await store.useSampleData(false, realBackend: EmptyBackend())
      #expect(store.home.waiting.isEmpty)
    }
    // Watches alone no longer create a ghost trigger card.
    var legacy = HomeResponse.empty
    legacy.watches = DemoWatches.details.map(\.summary)
    #expect(!HomePresentation.mainSections(legacy).contains("triggers"))
    #expect(!HomePresentation.mainSections(legacy).contains("watches"))
  }

  @MainActor @Test func sendPermissionSavePreservesOtherPermissions() async throws {
    let backend = DemoBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    store.selectedAgent = "agent-2"
    await store.loadPermissions()
    var expected = store.permissions
    for allowed in [false, true] {
      store.permissionBinding(\.requestSend).wrappedValue = allowed
      await store.waitForPermissionSave()
      expected.requestSend = allowed
      let saved: Permissions = try await backend.get("/v1/agents/agent-2/permissions")
      let agent: Agent = try await backend.get("/v1/agents/agent-2")
      #expect(saved == expected && agent.permissions == expected)
      #expect(store.permissions == expected && store.error == nil)
      await store.loadPermissions()
      #expect(store.permissions.requestSend == allowed)
    }
  }
}
