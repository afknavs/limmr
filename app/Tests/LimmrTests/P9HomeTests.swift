import Foundation
import LimmrKit
import Testing
@testable import Limmr

@Suite("P9 Home before efforts are warm") @MainActor struct P9HomeTests {
  @Test func homeIsReReadUntilEffortsAreReady() async throws {
    let backend = P9Backend()
    let raw = try await backend.request("/v1/home", method: "GET", body: nil)
    let probe = try Wire.decode(HomeResponse.self, from: raw)
    #expect(probe.effortsReady == false)
    let store = AppStore(demo: false, snapshot: true, backend: backend)
    await store.load("/v1/home", title: "Home") { (v: HomeResponse) in store.home = v }
    #expect(store.loadFailures.isEmpty)
    #expect(store.home.effortsReady == false)
    #expect(store.home.circleUpdates.isEmpty)
    await backend.warm()
    store.homeEffortsRetry?.cancel()
    store.homeEffortsRetry = nil
    store.retryHomeUntilEffortsReady(pause: .milliseconds(10), attempts: 50)
    await store.homeEffortsRetry?.value
    #expect(store.home.effortsReady != false)
    #expect(!store.home.circleUpdates.isEmpty)
  }
}

private actor P9Backend: Backend {
  private var warmed = false
  private let backend = DemoBackend()
  func warm() { warmed = true }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    let data = try await backend.request(path, method: method, body: body)
    guard path == "/v1/home", !warmed,
          var json = try JSONSerialization.jsonObject(with: data) as? [String: Any] else { return data }
    json["circle_updates"] = []
    json["efforts_ready"] = false
    return try JSONSerialization.data(withJSONObject: json)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { AsyncThrowingStream { $0.finish() } }
}
