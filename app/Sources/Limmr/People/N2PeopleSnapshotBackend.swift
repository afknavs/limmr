import AppKit
import SwiftUI
import LimmrKit

/// Invented, in-memory read-only fixtures for this lane's screenshots.
actor N2PeopleSnapshotBackend: Backend {
  let samples = DemoBackend()
  let page: String
  let portraits: [Data]
  init(page: String, portraits: [Data]) { self.page = page; self.portraits = portraits }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    let route = path.split(separator: "?").first.map(String.init) ?? path
    if method == "GET", route == "/v1/map" {
      let base: CircleMap = try await samples.get("/v1/map")
      var map = page == "n2-map-120" ? Self.network120(base) : N1MapSnapshotBackend.network(base)
      if page == "n2-person-isolated" {
        map.edges.removeAll { $0.source == "person-04" || $0.target == "person-04" }
        if let index = map.nodes.firstIndex(where: { $0.id == "person-04" }) {
          map.nodes[index].circles = []; map.nodes[index].circleId = nil; map.nodes[index].primaryCircleId = nil
        }
      }
      return try Wire.data(map)
    }
    if method == "GET", route == "/v1/people/person-04", page == "n2-person-isolated" {
      var detail: PersonDetail = try await samples.get(path)
      detail.groups = []; detail.relations = []
      detail.person.howKnown = "Met at a sketching workshop"
      detail.person.circle = ""; detail.person.groupsInCommon = []; detail.person.circleAffinity = []; detail.person.primaryCircleId = nil
      return try Wire.data(detail)
    }
    if method == "GET", route == "/v1/circles", page == "n2-person-isolated" {
      var circles: CirclesResponse = try await samples.get(path)
      for i in circles.circles.indices { circles.circles[i].people.removeAll { $0.personId == "person-04" } }
      return try Wire.data(circles)
    }
    if method == "GET", route.hasPrefix("/v1/circles/"), route.split(separator: "/").count == 3, page == "n2-person-isolated" {
      var circle: LimmrKit.Circle = try await samples.get(path)
      circle.people.removeAll { $0.personId == "person-04" }
      return try Wire.data(circle)
    }
    if method == "GET", route.hasSuffix("/avatar"), let id = route.split(separator: "/").dropLast().last {
      let hash = MapSceneMotion.stableHash(String(id))
      guard hash % 4 != 0, !portraits.isEmpty else { throw BackendError.http(404) }
      return portraits[Int(hash % UInt64(portraits.count))]
    }
    return try await samples.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { AsyncThrowingStream { $0.finish() } }
  static func network120(_ base: CircleMap) -> CircleMap {
    var map = DemoPeopleUnderstanding.network400(base)
    let people = Set(map.nodes.filter { $0.kind == "person" }.prefix(120).map(\.id))
    map.nodes = map.nodes.filter { $0.kind != "person" || people.contains($0.id) }
    let ids = Set(map.nodes.map(\.id))
    map.edges = map.edges.filter { ids.contains($0.source) && ids.contains($0.target) }
    return map
  }
}

@MainActor enum N2PeopleSnapshots {
  static let pages = ["n2-people-panel", "n2-people-circles", "n2-person-neighbours", "n2-person-isolated", "n2-person-owner", "n2-map-120", "n2-circle-open"]
  static func content(page: String, store: AppStore) async -> AnyView {
    store.setupStatus = nil
    store.selectPage(.circles)
    for circle in store.circles { await store.loadCircle(circle.id) }
    _ = await NetworkMapLayoutCache.projection(store.circleMap)
    for node in store.circleMap.nodes where node.kind == "person" { await store.loadAvatar(node.id) }
    if page == "n2-people-panel" { store.selectPeopleMapNode(store.circleMap.nodes.first { $0.id == "person-04" }) }
    if page.hasPrefix("n2-person-") {
      let id = page == "n2-person-owner" ? "me" : "person-04"
      store.openPeopleRoute(.person(id)); await store.loadPersonProgressively(id)
    }
    return AnyView(RootView(store: store))
  }
}
