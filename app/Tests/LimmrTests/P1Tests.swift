import Foundation
import LimmrKit
import Testing
@testable import Limmr

@Suite("P1 bounded refresh and wire compatibility") @MainActor struct P1Tests {
  @Test func missingProfileFieldsAndOwner() throws {
    let person = try Wire.decode(Person.self, from: Data(#"{"id":"me","name":"Alex Example","jids":null,"notes":null}"#.utf8))
    #expect(person.notes.isEmpty && person.circle.isEmpty && person.jids.isEmpty)
    #expect(PeoplePresentation.people([person], circles: [], query: "").first?.id == "me")
  }
  @Test func failedPeopleCallNamesEndpointAndKeepsOtherData() async {
    let backend = P1FailingBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    #expect(store.refreshError?.hasPrefix("Couldn’t refresh") == true)
    #expect(store.loadFailures["/v1/people?limit=50"]?.contains("GET /v1/people") == true)
    #expect(store.loadFailures["/v1/people?limit=50"]?.contains("expected format") == true)
    #expect(!store.agents.isEmpty && store.status?.linked == true)
    #expect(store.people.isEmpty)
  }
  @Test func repeatsAreDiffedAndDoNotFetchLegacyGraph() async {
    let backend = P1CountingBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    await store.refresh()
    let paths = await backend.paths
    #expect(!paths.contains { $0.contains("/graph") || $0.contains("limit=500") })
    #expect(paths.filter { $0.hasPrefix("/v1/people?") }.allSatisfy { $0.contains("limit=50") })
    #expect(store.refreshError == nil)
  }
}
actor P1FailingBackend: Backend {
  let demo = DemoBackend()
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if path.hasPrefix("/v1/people?") { return Data(#"{"people":42}"#.utf8) }
    return try await demo.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { AsyncThrowingStream { $0.finish() } }
}
actor P1CountingBackend: Backend {
  var paths: [String] = []
  let demo = DemoBackend()
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    paths.append(path)
    return try await demo.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { AsyncThrowingStream { $0.finish() } }
}

@Suite("P1 map deltas") struct P1MapTests {
  @Test func deltaReplacesAndRemoves() throws {
    let initial = try Wire.decode(CircleMap.self, from: Data(#"{"nodes":[{"id":"me","kind":"person","title":"Alex"},{"id":"old","kind":"person","title":"Old"}],"edges":[],"pulses":[],"cursor":"p1.first"}"#.utf8))
    let delta = try Wire.decode(CircleMap.self, from: Data(#"{"delta":true,"cursor":"p1.next","nodes":[{"id":"me","kind":"person","title":"Alex Example"}],"removed_nodes":["old"],"edges":[],"pulses":[]}"#.utf8))
    let result = initial.applying(delta)
    #expect(result.nodes.count == 1 && result.nodes[0].title == "Alex Example")
    #expect(result.cursor == "p1.next")
  }
}
