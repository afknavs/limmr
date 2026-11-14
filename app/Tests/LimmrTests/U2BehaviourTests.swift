import Foundation
import LimmrKit
import Testing
@testable import Limmr

@MainActor @Suite("U2 behaviour", .serialized) struct U2BehaviourTests {
  @Test func laterSurvivesRefreshPageChangeAndNewStore() async throws {
    let backend = DemoBackend()
    let store = AppStore(demo: true, snapshot: true, fixedClock: true, backend: backend)
    await store.refresh()
    #expect(store.home.tasks.contains { $0.id == "tsk_study" && $0.needsYou })
    await store.laterTask("tsk_study")
    #expect(store.taskActionErrors["tsk_study"] == nil)
    #expect(store.taskLaterNotices["tsk_study"] != nil)
    store.selectPage(.circles)
    await store.refresh()
    store.selectPage(.overview)
    #expect(store.home.tasks.first { $0.id == "tsk_study" }?.needsYou == false)
    #expect(store.home.tasks.first { $0.id == "tsk_study" }?.laterUntil != nil)
    let fresh = AppStore(demo: true, snapshot: true, fixedClock: true, backend: backend)
    await fresh.refresh()
    #expect(fresh.home.tasks.first { $0.id == "tsk_study" }?.needsYou == false)
    await fresh.workspace("tsk_study").load()
    #expect(fresh.workspace("tsk_study").task?.laterUntil != nil)
    await fresh.bringBackTask("tsk_study")
    #expect(fresh.home.tasks.first { $0.id == "tsk_study" }?.needsYou == true)
    #expect(fresh.workspace("tsk_study").task?.laterUntil == nil)
  }
  @Test func archivedNeverEntersLiveStoreAndRestoresFromStaleRoute() async throws {
    let backend = DemoBackend(u2Archived: true)
    let store = AppStore(demo: true, snapshot: true, fixedClock: true, backend: backend)
    await store.refresh()
    #expect(store.archivedCircles.count == 1)
    #expect(store.circles.allSatisfy { $0.isLive })
    let archived = try #require(store.archivedCircles.first)
    store.circles.append(archived)
    #expect(!store.circles.contains { $0.id == archived.id })
    await store.loadCircle(archived.id)
    #expect(store.circleError == nil && store.circleDetails[archived.id] == nil)
    await store.setCircleArchived(archived, archived: false)
    #expect(store.archivedCircles.isEmpty)
    #expect(store.circles.contains { $0.id == archived.id })
    store.openPeopleRoute(.circle(archived.id))
    await store.setCircleArchived(archived, archived: true)
    #expect(store.circlePath.isEmpty)
    #expect(store.archivedCircleUndo?.id == archived.id)
    await store.setCircleArchived(archived, archived: false)
    #expect(store.circles.contains { $0.id == archived.id })
  }
  @Test func startupScriptNeverProducesAnOutageTitle() throws {
    let start = Timestamp.date("2026-11-13T09:00:00Z")!
    var status = try Wire.decode(Status.self, from: Data(#"{"linked":true,"connected":false,"account_name":"Fixture","pending_approvals":0,"agents":[],"health":"ok","engine_started_at":"2026-11-13T09:00:00Z","link_phase":"connecting","link_phase_at":"2026-11-13T09:00:00Z"}"#.utf8))
    let unknown = HealthCheck(id: "remote", title: "Remote", state: "checking", detail: "Checking")
    for second in [0, 1, 8, 30, 91] {
      if second >= 8 { status.connected = true; status.linkPhase = "connected" }
      let card = ConnectionAttention.resolve(state: second == 0 ? .starting : status.connected ? .connected : .offline,
        checks: second < 30 ? [unknown] : [], remoteEnabled: true, sync: nil,
        status: status, now: start.addingTimeInterval(Double(second)), launchedAt: start)
      for phrase in ["disconnected", "offline", "needs attention", "stopped"] {
        #expect(card?.title.localizedCaseInsensitiveContains(phrase) != true)
      }
      if second == 1 { #expect(card?.repair == .wait && card?.button.isEmpty == true) }
    }
    // A real drop gets a bounded grace window; logged-out and missing engine do not.
    status.connected = false; status.linkPhase = "disconnected"; status.linkPhaseAt = status.engineStartedAt
    #expect(ConnectionAttention.resolve(state: .offline, checks: [], remoteEnabled: false, sync: nil, status: status, now: start.addingTimeInterval(91))?.repair == .reconnect)
    #expect(ConnectionAttention.resolve(state: .loggedOut, checks: [], remoteEnabled: false, sync: nil, status: status, now: start)?.repair == .link)
    #expect(ConnectionAttention.resolve(state: .notRunning, checks: [], remoteEnabled: false, sync: nil, status: status, now: start)?.repair == .restart)
    let failed = HealthCheck(id: "remote", title: "Remote", state: "fail", detail: "Unavailable")
    #expect(ConnectionAttention.resolve(state: .connected, checks: [failed], remoteEnabled: true, sync: nil, status: status, now: start.addingTimeInterval(30)) == nil)
    #expect(ConnectionAttention.resolve(state: .connected, checks: [unknown], remoteEnabled: true, sync: nil, status: status, now: start.addingTimeInterval(120)) == nil)
    #expect(ConnectionAttention.resolve(state: .connected, checks: [failed], remoteEnabled: true, sync: nil, status: status, now: start.addingTimeInterval(120))?.repair == .remote("restart_remote"))
  }
  @Test func localSnoozeCalendarOptionsAndLabels() {
    var calendar = Calendar(identifier: .gregorian); calendar.timeZone = TimeZone(identifier: "America/New_York")!
    let saturday = calendar.date(from: DateComponents(year: 2026, month: 3, day: 7, hour: 4))!
    let sunday = TaskLaterTime.defaultUntil(now: saturday, calendar: calendar)
    #expect(calendar.component(.day, from: sunday) == 8 && calendar.component(.hour, from: sunday) == 9)
    let monday = calendar.date(from: DateComponents(year: 2026, month: 3, day: 9, hour: 3))!
    let next = TaskLaterTime.option("Next Monday", now: monday, calendar: calendar)
    #expect(calendar.component(.day, from: next) == 16 && calendar.component(.hour, from: next) == 9)
    #expect(TaskLaterTime.option("In 3 hours", now: saturday, calendar: calendar).timeIntervalSince(saturday) == 10800)
  }
}
