import CoreImage.CIFilterBuiltins
import Foundation
import Testing

@testable import Limmr
@testable import LimmrKit

@Suite("B2 setup and presentation")
struct RedesignTests {
  @Test func welcomeAdvancesBeforeHelperStarts() {
    var flow = SetupFlow()
    flow.advance()
    #expect(flow.step == .link)
    flow.helperFailed()
    #expect(flow.step == .link)
    #expect(flow.helperError == "Limmr’s background helper isn’t running.")
    flow.helperStarted()
    #expect(flow.helperError == nil)
  }

  @Test func cannotSkipUnlinkedDevice() {
    var flow = SetupFlow(step: .link)
    flow.advance()
    #expect(flow.step == .link)
    flow.advance(linkReady: true)
    #expect(flow.step == .syncing)
    flow.advance()
    flow.advance()
    flow.advance()
    #expect(flow.step == .done)
  }

  @Test func backStopsAtWelcome() {
    var flow = SetupFlow()
    flow.back()
    #expect(flow.step == .welcome)
  }

  @Test func windowFitsSmallLaptop() {
    #expect(WindowMetrics.minimumWidth == 760)
    #expect(WindowMetrics.minimumHeight == 520)
    #expect(WindowMetrics.defaultWidth == 1100)
    #expect(WindowMetrics.defaultHeight == 740)
  }

  @MainActor @Test func realEmptyBackendNeverUsesSampleEntities() async {
    let store = AppStore(demo: false, snapshot: true, backend: EmptyBackend())
    await store.refresh()
    #expect(!store.demo)
    #expect(store.status?.linked == false)
    #expect(store.agents.isEmpty && store.people.isEmpty && store.chats.isEmpty)
    #expect(store.graph.nodes.isEmpty && store.graph.edges.isEmpty)
    #expect(store.approvals.isEmpty && store.activity.isEmpty)
    #expect(store.settings.owner.isEmpty)
  }

  @MainActor @Test func leavingSamplesClearsEveryEntity() async {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    #expect(!store.people.isEmpty && !store.agents.isEmpty)
    await store.useSampleData(false, realBackend: EmptyBackend())
    #expect(!store.demo && store.status?.linked == false)
    #expect(store.people.isEmpty && store.agents.isEmpty && store.chats.isEmpty)
    #expect(store.graph.nodes.isEmpty && store.activity.isEmpty && store.approvals.isEmpty)
    #expect(store.profile == nil && store.activityMessages.isEmpty)
  }

  @MainActor @Test func staleSampleRefreshCannotRepopulateRealMode() async {
    let gate = TestGate()
    let store = AppStore(demo: true, snapshot: true, backend: SlowSamples(gate: gate))
    let staleRefresh = Task { await store.refresh() }
    await gate.waitUntilEntered()
    await store.useSampleData(false, realBackend: EmptyBackend())
    await gate.release()
    await staleRefresh.value
    #expect(!store.demo && store.status?.linked == false)
    #expect(store.people.isEmpty && store.agents.isEmpty && store.graph.nodes.isEmpty)
  }

  @MainActor @Test func failuresStayVisibleAndRetry() async {
    let store = AppStore(demo: true, snapshot: true)
    var attempts = 0
    await store.perform(action: "test-action") {
      attempts += 1
      if attempts == 1 { throw BackendError.unavailable("The connection could not finish.") }
    }
    #expect(store.action == "test-action")
    #expect(store.error == "The connection could not finish.")
    #expect(!store.busy)
    await store.retry()
    #expect(attempts == 2 && store.error == nil)
  }

  @MainActor @Test func permissionsRenderPersistedState() async {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    store.selectedAgent = "agent-2"
    await store.loadPermissions()
    #expect(store.permissions.readMessages && store.permissions.requestSend)
    #expect(store.permissions.search && store.permissions.readPeople)
    store.permissions.requestSend = false
    await store.savePermissions()
    await store.loadPermissions()
    #expect(!store.permissions.requestSend)
    await store.refresh()
    #expect(store.currentAgent?.permissions?.requestSend == false)
  }

  @MainActor @Test func failedPermissionLoadCannotSavePreviousAISettings() async {
    let backend = FailedPermissionLoad()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    store.selectedAgent = "agent-2"
    await store.loadPermissions()
    #expect(store.permissionsAgentID == "agent-2")
    store.selectedAgent = "agent-3"
    await store.loadPermissions()
    #expect(store.permissionsAgentID == nil && store.error != nil)
    await store.savePermissions()
    #expect(await backend.writes == 0)
  }

  @MainActor @Test func sampleApprovalEditsAndDecisionsUpdateEverySurface() async throws {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    #expect(store.pending.count == 2 && store.status?.pendingApprovals == 2)
    let originalCount = store.activity.count
    let revised = "Let’s meet at ten.\n— Claude Code, on behalf of Alex"
    await store.resolve("a1", approve: true, text: revised)
    #expect(store.error == nil)
    #expect(store.pending.count == 1 && store.status?.pendingApprovals == 1)
    #expect(store.approvals.first { $0.id == "a1" }?.text == revised)
    #expect(store.approvals.first { $0.id == "a1" }?.state == "sent")
    #expect(store.allRecentActivity.first?.kind == "approved")
    #expect(store.allRecentActivity.first?.agentId == "you")
    #expect(store.allRecentActivity.first?.note == revised)
    await store.resolve("a2", approve: false)
    #expect(store.pending.isEmpty && store.status?.pendingApprovals == 0)
    #expect(store.approvals.first { $0.id == "a2" }?.state == "denied")
    #expect(store.activity.count == originalCount + 2)
    #expect(store.activity.first?.kind == "denied")
    #expect(store.todayActivity.contains { $0.note == revised })
    await store.resolve("a1", approve: true)
    #expect(store.activity.count == originalCount + 2)
    await store.filterActivity(agent: "you", kind: "approved", chat: "all")
    #expect(store.activity.first?.note == revised)
    #expect(store.activity.allSatisfy { $0.agentId == "you" && $0.kind == "approved" })
  }

  @MainActor @Test func immediatePermissionChangesPersistInOrder() async {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    store.selectedAgent = "agent-2"
    await store.loadPermissions()
    store.permissionBinding(\.requestSend).wrappedValue = false
    store.permissionBinding(\.search).wrappedValue = false
    store.editPermissions { $0.chats.jids = ["120363000001@g.us"] }
    await store.waitForPermissionSave()
    #expect(store.permissionsSaved && store.error == nil)
    await store.loadPermissions()
    #expect(!store.permissions.requestSend && !store.permissions.search)
    #expect(store.permissions.chats.jids == ["120363000001@g.us"])
  }

  @MainActor @Test func immediateSettingsChangesPersistInOrder() async throws {
    let backend = DemoBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    store.settingsBinding(\.retentionDays).wrappedValue = 30
    store.editSettings { $0.hiddenChats.append("120363000001@g.us") }
    await store.waitForSettingsSave()
    let saved: AppSettings = try await backend.get("/v1/settings")
    #expect(store.settingsSaved && store.error == nil)
    #expect(saved.retentionDays == 30)
    #expect(saved.hiddenChats == ["120363000001@g.us"])
  }

  @MainActor @Test func activitySortsByInstantAcrossAllAIs() async {
    let store = AppStore(demo: true, snapshot: true, backend: UnsortedActivity())
    await store.refresh()
    #expect(store.activity.map(\.id) == [1, 3, 2])
    #expect(store.allRecentActivity.map(\.id) == [1, 3, 2])
    await store.filterActivity(agent: "all", kind: "all", chat: "all")
    #expect(store.activity.map(\.id) == [1, 3, 2])
  }

  @MainActor @Test func longURLQRUsesAutomaticCapacityAndSafeFixture() throws {
    #expect(DemoQRCode.longURL.utf8.count == 272)
    let generator = CIFilter.qrCodeGenerator()
    generator.message = Data(DemoQRCode.longURL.utf8)
    generator.correctionLevel = "M"
    #expect(generator.outputImage?.extent.width == 67)
    let image = try #require(QRCode.image(DemoQRCode.longURL))
    #expect(image.width >= 65 && image.width == image.height)
  }

  @Test func syntheticQRRenderingCanRunConcurrently() async {
    // Exercise concurrent callers inside a serial test run without initializing
    // Core Image's shared render service on a cooperative executor.
    await withTaskGroup(of: Int.self) { group in
      for _ in 0..<16 {
        group.addTask { await MainActor.run { QRCode.image(DemoQRCode.longURL)?.width ?? 0 } }
      }
      for await width in group { #expect(width >= 65) }
    }
  }

  @MainActor @Test func approvalDeepLinkGoesHome() {
    let store = AppStore(demo: true, snapshot: true)
    store.navigate(URL(string: "limmr://approvals/a1")!)
    #expect(store.page == .overview && store.selectedApproval == "a1")
    #expect(Page.allCases.map(\.rawValue) == ["Home", "People", "Activity"])
  }

  @MainActor @Test func healthDeepLinkOpensSettingsHealth() {
    let store = AppStore(demo: true, snapshot: true)
    store.navigate(URL(string: "limmr://health")!)
    #expect(store.showSettings && store.settingsTab == .health)
  }
}

private struct SlowSamples: Backend {
  let samples = DemoBackend()
  let gate: TestGate
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    await gate.enter()
    return try await samples.request(path, method: method, body: body)
  }
  func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}

private actor FailedPermissionLoad: Backend {
  let samples = DemoBackend()
  var writes = 0
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if method != "GET" { writes += 1 }
    if path == "/v1/agents/agent-3" { throw BackendError.http(503) }
    return try await samples.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}

private struct UnsortedActivity: Backend {
  let samples = DemoBackend()
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if path.hasPrefix("/v1/activity?") {
      let rows = zip(
        [Int64(2), 1, 3],
        ["2026-10-16T11:00:00Z", "2026-10-16T12:00:00.500Z", "2026-10-16T17:30:00+05:30"]
      ).map { id, at in
        Activity(
          id: id, at: at, agentId: "agent-\(id)", tool: "list_messages", kind: "read", chatJids: [],
          messageIds: [], count: 14)
      }
      return try Wire.data(ActivityPage(items: rows))
    }
    return try await samples.request(path, method: method, body: body)
  }
  func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}
