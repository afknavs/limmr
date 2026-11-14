import Foundation
import LimmrKit
import Testing
@testable import Limmr

@MainActor @Suite("U2 integration", .serialized) struct U2IntegrateTests {
  @Test func archiveAndLiveLoadsKeepIndependentFailuresAndLastGoodData() async {
    for failingPath in ["/v1/circles", "/v1/circles?archived=1"] {
      let backend = U2CircleFailureBackend(failingPath: failingPath)
      let store = AppStore(demo: true, snapshot: true, backend: backend)
      store.circles = [DemoCircles.details[0].circle]
      let previous = store.circles
      await store.load(failingPath, title: "Circles") { (_: CirclesResponse) in
        Issue.record("Failed load applied data")
      }
      let healthyPath = failingPath == "/v1/circles" ? "/v1/circles?archived=1" : "/v1/circles"
      await store.load(healthyPath, title: "Circles") { (_: CirclesResponse) in }
      #expect(store.loadFailures[failingPath] != nil)
      #expect(store.loadFailures[healthyPath] == nil)
      #expect(store.loadedRoutes.contains(healthyPath))
      #expect(!store.loadedRoutes.contains(failingPath))
      #expect(store.refreshError != nil && store.circles == previous)
      await backend.recover()
      await store.load(failingPath, title: "Circles") { (_: CirclesResponse) in }
      #expect(store.loadFailures.isEmpty && store.refreshError == nil)
    }
  }

  @Test func firstContactSettingSavesThroughTheExistingBinding() async throws {
    let backend = DemoBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    #expect(store.settings.firstContactLimit == 5)
    for limit in [0, 20, 7] {
      store.settingsBinding(\.firstContactLimit).wrappedValue = limit
      await store.waitForSettingsSave()
      let saved: AppSettings = try await store.read("/v1/settings")
      #expect(saved.firstContactLimit == limit)
      #expect(store.error == nil)
    }
    let fresh = AppStore(demo: true, snapshot: true, backend: backend)
    await fresh.refresh()
    #expect(fresh.settings.firstContactLimit == 7)
    #expect(SettingsSearchEntry.matches("first contact").contains { $0.tab == .privacy })
  }
}

private actor U2CircleFailureBackend: Backend {
  let demo = DemoBackend()
  let failingPath: String
  var failing = true
  init(failingPath: String) { self.failingPath = failingPath }
  func recover() { failing = false }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if failing && path == failingPath { throw BackendError.http(503) }
    return try await demo.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}
