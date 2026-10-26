import Foundation
import LimmrKit

/// A connected account with no history or entities, entirely in memory.
struct O1EmptyAccountBackend: Backend {
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    guard method == "GET" else { throw BackendError.unavailable("Read-only empty fixture") }
    switch path.split(separator: "?").first {
    case "/v1/status":
      var status: Status = try await EmptyBackend().get("/v1/status")
      status.linked = true
      status.connected = true
      return try Wire.data(status)
    case "/v1/stats": return try Wire.data(DashboardStats())
    case "/v1/stats/history": return try Wire.data(StatsHistory())
    case "/v1/activity/digests": return try Wire.data(ActivityDigests())
    default: return try await EmptyBackend().request(path, method: method, body: body)
    }
  }
  func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { AsyncThrowingStream { $0.finish() } }
}
