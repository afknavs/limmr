import Foundation
import LimmrKit
import Testing
@testable import Limmr

@Suite("Q2 launch at login") @MainActor struct Q2LaunchAtLoginTests {
  @Test func toggleRegistersOnceWithoutAnEngineSave() async {
    let service = Q2LoginService()
    let backend = Q2SettingsBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend, launchAtLoginService: service)
    store.launchAtLoginBinding.wrappedValue = true
    #expect(store.system.login.isChanging && store.launchAtLoginBinding.wrappedValue)
    await store.system.login.waitForChange()
    #expect(service.registerCalls == 1 && service.unregisterCalls == 0)
    #expect(store.system.login.status == .enabled)
    #expect(await backend.writes == 0)
    await store.refresh()
    #expect(store.launchAtLoginBinding.wrappedValue && store.settings.launchAtLogin)
    #expect(service.registerCalls == 1)
  }

  @Test func blockedThenFailedEnginePutCannotDelayRegistration() async {
    let gate = TestGate()
    let service = Q2LoginService()
    let backend = Q2SettingsBackend(gate: gate)
    let store = AppStore(demo: true, snapshot: true, backend: backend, launchAtLoginService: service)
    store.settingsBinding(\.retentionDays).wrappedValue = 30
    await gate.waitUntilEntered()
    store.launchAtLoginBinding.wrappedValue = true
    await store.system.login.waitForChange()
    #expect(service.registerCalls == 1 && store.system.login.status == .enabled)
    await gate.release()
    await store.waitForSettingsSave()
    #expect(store.error != nil) // The separate engine save really failed.
    #expect(store.system.login.error == nil && store.launchAtLoginBinding.wrappedValue)
    await store.refresh()
    #expect(store.launchAtLoginBinding.wrappedValue && service.registerCalls == 1)
  }

  @Test func registrationErrorIsVisibleAndRetryDoesNotNeedEngine() async {
    let service = Q2LoginService()
    service.fail = true
    let setting = LaunchAtLoginSetting(service: service)
    setting.setEnabled(true)
    await setting.waitForChange()
    #expect(service.registerCalls == 1)
    #expect(setting.error?.contains("Couldn’t turn launch at login on") == true)
    #expect(setting.error?.contains("private") == false)
    #expect(!setting.isOn && !setting.isChanging)
    service.fail = false
    setting.setEnabled(true)
    await setting.waitForChange()
    #expect(service.registerCalls == 2 && setting.isOn && setting.error == nil)
  }

  @Test func approvalIsDistinctFromOffAndOpensLoginItems() async {
    let service = Q2LoginService()
    service.registrationResult = .requiresApproval
    let setting = LaunchAtLoginSetting(service: service)
    setting.setEnabled(true)
    await setting.waitForChange()
    #expect(service.registerCalls == 1 && setting.isOn)
    #expect(setting.status == .requiresApproval)
    #expect(setting.status.detail == "Needs your OK in System Settings → Login Items")
    setting.openLoginItems()
    #expect(service.openCalls == 1)
    service.status = .enabled // Approval while System Settings was in front.
    setting.refresh()
    #expect(setting.status == .enabled && setting.isOn)
  }

  @Test func refreshCannotOverwriteAnInFlightUnregisterOrDuplicateIt() async {
    let gate = TestGate()
    let service = Q2LoginService(status: .enabled)
    service.unregisterGate = gate
    let store = AppStore(demo: true, snapshot: true, launchAtLoginService: service)
    store.launchAtLoginBinding.wrappedValue = false
    await gate.waitUntilEntered()
    #expect(service.unregisterCalls == 1 && service.status == .enabled)
    await store.refresh()
    #expect(store.system.login.isChanging)
    #expect(!store.launchAtLoginBinding.wrappedValue && !store.settings.launchAtLogin)
    store.launchAtLoginBinding.wrappedValue = false
    #expect(service.unregisterCalls == 1)
    await gate.release()
    await store.system.login.waitForChange()
    #expect(store.system.login.status == .notRegistered && !store.system.login.isChanging)
    #expect(service.unregisterCalls == 1 && service.registerCalls == 0)
  }

  @Test func unregisterFailureKeepsActualStateAndShowsError() async {
    let service = Q2LoginService(status: .requiresApproval)
    service.fail = true
    let setting = LaunchAtLoginSetting(service: service)
    setting.setEnabled(false)
    await setting.waitForChange()
    #expect(service.unregisterCalls == 1 && setting.isOn)
    #expect(setting.status == .requiresApproval)
    #expect(setting.error?.contains("Couldn’t turn launch at login off") == true)
  }

  @Test func missingAppIsOffWithActionableDetailAndRefreshWorksWithoutEngine() {
    let service = Q2LoginService(status: .notFound)
    let setting = LaunchAtLoginSetting(service: service)
    #expect(!setting.isOn && setting.status.detail.contains("Install it in Applications"))
    service.status = .requiresApproval
    setting.refresh()
    #expect(setting.isOn && setting.status == .requiresApproval)
  }
}

@MainActor private final class Q2LoginService: LaunchAtLoginManaging {
  var status: LaunchAtLoginStatus
  var registrationResult: LaunchAtLoginStatus = .enabled
  var fail = false
  var registerCalls = 0
  var unregisterCalls = 0
  var openCalls = 0
  var unregisterGate: TestGate?
  init(status: LaunchAtLoginStatus = .notRegistered) { self.status = status }
  func register() throws {
    registerCalls += 1
    if fail { throw NSError(domain: "Q2", code: 1, userInfo: [NSLocalizedDescriptionKey: "private path"]) }
    status = registrationResult
  }
  func unregister() async throws {
    unregisterCalls += 1
    if let unregisterGate { await unregisterGate.enter() }
    if fail { throw NSError(domain: "Q2", code: 2) }
    status = .notRegistered
  }
  func openLoginItems() { openCalls += 1 }
}

private actor Q2SettingsBackend: Backend {
  let base = DemoBackend()
  let gate: TestGate?
  var writes = 0
  init(gate: TestGate? = nil) { self.gate = gate }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if path == "/v1/settings" && method == "PUT" {
      writes += 1
      if let gate { await gate.enter() }
      throw BackendError.http(503)
    }
    return try await base.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}
