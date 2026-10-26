import Foundation
import LimmrKit

/// A5-only synthetic edge cases. Selected explicitly by the snapshot runner.
actor A5SnapshotBackend: Backend {
  private let base = H2SnapshotBackend()
  let emptySections: Bool
  init(emptySections: Bool) { self.emptySections = emptySections }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    let data = try await base.request(path, method: method, body: body)
    let route = URLComponents(string: path)?.path ?? path
    if emptySections {
      if route == "/v1/circles/atlas" {
        var circle = try Wire.decode(LimmrKit.Circle.self, from: data)
        circle.topics = []; circle.people = []; circle.chats = []
        return try Wire.data(circle)
      }
      if route == "/v1/circles/atlas/briefs" {
        return try Wire.data(["briefs": [CircleBrief]()])
      }
      if route.hasPrefix("/v1/efforts/") {
        var detail = try Wire.decode(EffortDetailResponse.self, from: data)
        detail.timeline = []
        return try Wire.data(detail)
      }
      return data
    }
    guard let object = try? JSONSerialization.jsonObject(with: data, options: .fragmentsAllowed) else { return data }
    return try JSONSerialization.data(withJSONObject: renamed(object), options: .fragmentsAllowed)
  }
  private func renamed(_ object: Any) -> Any {
    if let text = object as? String {
      switch text {
      case "Maya Rao": return "Maya Rao — Research, Architecture and Community Workshop Coordinator"
      case "Atlas renovation": return "Atlas renovation — studio planning and community events"
      case "Pitch deck with Maya": return "Pitch deck with Maya — Community Workshop Research and Design Review"
      default: return text
      }
    }
    if let array = object as? [Any] { return array.map(renamed) }
    if let dictionary = object as? [String: Any] { return dictionary.mapValues(renamed) }
    return object
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}
