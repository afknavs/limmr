import Foundation
import LimmrKit

/// Snapshot-only invented variants survive the same refresh path as the live view.
actor PeopleSnapshotBackend: Backend {
  let samples = DemoBackend()
  let dense: Bool
  init(dense: Bool) { self.dense = dense }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if dense && path == "/v1/circles" && method == "GET" { return try Wire.data(["circles": P4DenseMapFixture.circles]) }
    if dense && path.hasPrefix("/v1/map") && method == "GET" { return try Wire.data(P4DenseMapFixture.map) }
    let data = try await samples.request(path, method: method, body: body)
    if !dense && path == "/v1/people/person-04" && method == "GET" {
      var profile = try Wire.decode(PersonDetail.self, from: data)
      profile.person.groupsInCommon = profile.groups.map { group in
        GroupInCommon(id: group.jid, name: group.name, circleIds: DemoCircles.circles.filter { $0.chats.contains { $0.chatJid == group.jid } }.map(\.id))
      }
      profile.person.howKnown = ""; profile.person.role = ""
      profile.person.inferredRelationship = RelationshipInference(label: "your project teammate", howKnown: "You work together on the studio renovation and review materials each week.", confidence: 0.91)
      return try Wire.data(profile)
    }
    return data
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { AsyncThrowingStream { $0.finish() } }
}
