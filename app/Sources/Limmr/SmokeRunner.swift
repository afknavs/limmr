import AppKit
import LimmrKit

/// Exercises the same store actions used by the controls, against the in-memory backend.
/// It never installs a connector, registers a service, asks for notifications or opens a network connection.
@MainActor enum SmokeRunner {
  static func run() async {
    let store = AppStore(demo: true, snapshot: true)
    var checks = 0
    func expect(_ value: Bool, _ label: String) throws {
      guard value else { throw BackendError.unavailable(label) }
      checks += 1
    }
    do {
      await store.refresh()
      try expect(
        store.agents.count == 3 && store.people.count == DemoData.people.count
          && store.chats.count == DemoData.chats.count, "Directory fixtures")
      try expect(
        !store.circleMap.nodes.isEmpty && store.circleMap.nodes.count <= 150
          && store.pending.count == 2, "Initial map and approvals")
      store.selectedAgent = "agent-3"
      await store.loadPermissions()
      try expect(
        !store.permissions.requestSend && !store.permissions.writePeople, "Remote defaults")
      store.selectedAgent = "agent-2"
      await store.loadPermissions()
      store.permissions.chats = .init(mode: "only", jids: [DemoData.groupJid(0)])
      await store.savePermissions()
      await store.loadPermissions()
      try expect(
        store.permissions.chats.summary == "Can read 1 chat", "Permission save and readback")
      await store.loadPerson()
      try expect(
        store.profile?.person.name == "Maya Rao"
          && store.profile?.facts.contains { $0.id == "f1" && $0.status == "active" } == true,
        "Profile and provenance")
      store.selectedActivity = store.activity.first { !$0.messageIds.isEmpty }?.id
      await store.loadActivity()
      try expect(!store.activityMessages.isEmpty, "Message lookup by reference")
      await store.resolve("a1", approve: true, text: "An edited demo message.")
      try expect(
        store.approvals.first { $0.id == "a1" }?.text == "An edited demo message."
          && store.pending.count == 1, "Edited approval")
      await store.resolve("a2", approve: false)
      try expect(store.pending.isEmpty, "Denial")
      await store.revert("f1")
      try expect(
        store.changes.first { $0.id == "f1" }?.reverted == true
          && store.profile?.facts.contains { $0.id == "f1" } == false, "Change reversion")
      store.settings.hiddenChats = [DemoData.personJid(0)]
      await store.saveSettings()
      let settings: AppSettings = try await store.backend.get("/v1/settings")
      try expect(settings.hiddenChats.count == 1, "Hidden chat settings")
      await store.filterActivity(agent: "agent-2", kind: "read", chat: "all")
      try expect(
        !store.activity.isEmpty
          && store.activity.allSatisfy { $0.agentId == "agent-2" && $0.kind == "read" },
        "Server activity filters")
      store.navigate(URL(string: "limmr://people/person-04")!)
      try expect(
        store.page == .circles && store.selectedPerson == "person-04" && store.error == nil,
        "Deep-link navigation")
      print("App smoke test passed: \(checks) checks.")
      exit(0)
    } catch {
      fputs("App smoke test failed: \(error.localizedDescription)\n", stderr)
      exit(1)
    }
  }
}
