import Foundation
import LimmrKit
import Testing
@testable import Limmr

@Suite("Status and Settings") @MainActor struct StatusSettingsTests {
  @Test func healthyHasNoCardAndOptionalConnectionsAreIgnored() {
    let remote = HealthCheck(id: "remote", title: "Remote", state: "fail", detail: "private", fixId: "restart_remote")
    #expect(ConnectionAttention.resolve(state: .connected, checks: [remote], remoteEnabled: false, sync: .demo("never")) == nil)
    #expect(ConnectionAttention.resolve(state: .connected, checks: [remote], remoteEnabled: true, sync: nil)?.repair == .remote("restart_remote"))
    #expect(ConnectionAttention.resolve(state: .connected, checks: [], remoteEnabled: false, sync: .demo("error"))?.repair == .sync)
  }
  @Test(arguments: [EngineSurfaceState.busy, .stalled, .notRunning, .loggedOut, .offline, .phoneUnreachable, .restarting])
  func attentionHasOneCorrectRepair(state: EngineSurfaceState) {
    let card = ConnectionAttention.resolve(state: state, checks: [], remoteEnabled: false, sync: nil)
    #expect(card != nil)
    switch state {
    case .busy, .stalled, .notRunning: #expect(card?.repair == .restart)
    case .loggedOut: #expect(card?.repair == .link)
    case .offline, .phoneUnreachable: #expect(card?.repair == .reconnect)
    default: #expect(card?.repair == .wait)
    }
  }
  @Test func diagnosticsRebuildsOnlySafeFacts() {
    let report = ConnectionDiagnostics.report(appVersion: "0.1.0", engineVersion: "15550100099",
      checks: [.init(id: "agent_CANARY_TOKEN", title: "CANARY_NAME", state: "fail", detail: "CANARY_MESSAGE +15550100099", fixId: nil)],
      engineReport: "CANARY_TOKEN\nhealth_check_ms=1.25\nCANARY_NAME stage=hub.open duration_ms=2.5 CANARY_MESSAGE\nCANARY_NAME Engine stopped responding; restart requested",
      logTail: "2026/10/28 01:00:00 WARN CANARY_MESSAGE +15550100099 Bearer CANARY_TOKEN")
    for secret in ["CANARY", "15550100099", "Bearer"] { #expect(!report.contains(secret)) }
    #expect(report.contains("App version: 0.1.0"))
    #expect(report.contains("check ai_app: fail"))
    #expect(report.contains("health_check_ms=1.250"))
    #expect(report.contains("duration_ms=2.500"))
  }
  @Test func fourTabsKeepSearchAndAdvancedReachable() {
    #expect(SettingsTab.allCases.map(\.rawValue) == ["General", "Connections", "Integrations", "Privacy"])
    for entry in SettingsSearchEntry.all { #expect(SettingsTab.allCases.contains(entry.tab)) }
    #expect(SettingsSearchEntry.matches("data folder").first?.advanced == true)
    #expect(SettingsSearchEntry.matches("Accessory", showAccessory: true).first?.tab == .general)
    #expect(SettingsSearchEntry.matches("Accessory", showAccessory: false).isEmpty)
    #expect(SettingsSearchEntry.matches("hidden").first?.tab == .privacy)
    #expect(SettingsSearchEntry.matches("sync").first?.tab == .connections)
  }
  @Test func busyStatusKeepsLastPageData() async {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    let chats = store.chats.count, people = store.people.count
    store.loadFailures["/v1/status"] = "Connection timed out"
    #expect(store.connectionAttention?.repair == .restart)
    #expect(store.chats.count == chats && store.people.count == people)
  }
}
