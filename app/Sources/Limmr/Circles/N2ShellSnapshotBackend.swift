import AppKit
import LimmrKit

/// Invented images and retained history, including an unlinked preview status.
struct N2ShellSnapshotBackend: Backend {
  let base: N1LookSnapshotBackend
  let portraits: [Data]
  @MainActor static func make() throws -> Self { .init(base: try N1LookSnapshotBackend.make(), portraits: N1MapPortraits.make()) }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    let route = URLComponents(string: path)?.path ?? path
    if route == "/v1/status" {
      let data = try await base.request(path, method: method, body: body)
      var value = try JSONSerialization.jsonObject(with: data) as! [String: Any]
      value["linked"] = false
      return try JSONSerialization.data(withJSONObject: value)
    }
    if route.hasSuffix("/photo"), route.hasPrefix("/v1/groups/"), let first = portraits.first { return first }
    if route.contains("/cover/thumbnail") {
      let parts = route.split(separator: "/")
      if parts.count >= 3, let task = base.tasks.first(where: { $0.id == String(parts[2]) }), let item = task.cover, let url = base.previews[item.id] {
        return try Wire.data(["path": url.path])
      }
      throw BackendError.http(404)
    }
    func candidate(_ task: TaskSummary) -> TaskSummary {
      var value = task
      if let item = task.cover { value.coverCandidate = .init(itemId: item.id, title: item.title, thumbnailUrl: "/v1/tasks/" + task.id + "/cover/thumbnail?revision=fixture") }
      return value
    }
    if route == "/v1/tasks" {
      let response: TasksResponse = try await base.get(path)
      return try Wire.data(TasksResponse(tasks: response.tasks.map(candidate)))
    }
    if route == "/v1/home" {
      var home: HomeResponse = try await base.get(path)
      home.tasks = home.tasks.map(candidate)
      return try Wire.data(home)
    }
    return try await base.request(path, method: method, body: body)
  }
  func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { .init { $0.finish() } }
}
