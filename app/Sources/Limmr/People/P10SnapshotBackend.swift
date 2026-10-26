import Foundation
import LimmrKit

/// Invented directory sizes and a twelve-member preview leave shared demo data unchanged.
actor P10SnapshotBackend: Backend {
  let demo = DemoBackend()
  let hover: Bool
  init(hover: Bool = false) { self.hover = hover }
  static let counts = [1, 123, 1234, 3, 42, 0]
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    let data = try await demo.request(path, method: method, body: body)
    if method == "GET", path == "/v1/circles" {
      var response = try Wire.decode(CirclesResponse.self, from: data)
      if hover {
        if let index = response.circles.firstIndex(where: { $0.id == "family" }) {
          response.circles[index].people = DemoData.people.prefix(12).map { person in
            var member = response.circles[index].people[0]
            member.personId = person.id
            return member
          }
        }
        return try Wire.data(response)
      }
      for index in response.circles.indices {
        let count = Self.counts[index % Self.counts.count]
        response.circles[index].people = (0..<count).map {
          var member = DemoCircles.circles[0].people[0]
          member.personId = "p10-\(index)-\($0)"
          return member
        }
        response.circles[index].pinned = index % 2 == 0
      }
      return try Wire.data(response)
    }
    if method == "GET", path.hasPrefix("/v1/map") {
      var map = try Wire.decode(CircleMap.self, from: data)
      if hover {
        for person in map.nodes.filter({ $0.kind == "person" }) {
          if !map.edges.contains(where: { $0.source == "family" && $0.target == person.id }) {
            var edge = map.edges[0]
            edge.id = "p10-family-" + person.id
            edge.source = "family"; edge.target = person.id; edge.state = "confirmed"
            map.edges.append(edge)
          }
        }
        return try Wire.data(map)
      }
      for index in map.pulses.indices { map.pulses[index].newMessages = Self.counts[index % Self.counts.count] }
      return try Wire.data(map)
    }
    return data
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}
