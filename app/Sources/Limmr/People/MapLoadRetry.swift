import Foundation
import LimmrKit

/// Normal backoff applies to map/circle reads; startup waiting applies to all reads.
/// Callers retain their last decoded value while a request is pending.
enum MapLoadRetry {
  static let delays: [Duration] = [.milliseconds(500), .seconds(1), .seconds(2), .seconds(4)]
  static func request(_ backend: any Backend, path: String,
    pause: @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }
  ) async throws -> Data {
    let retry = path == "/v1/circles" || path.hasPrefix("/v1/map")
    var attempt = 0
    var progress: String?
    var waited: Duration = .zero
    var progressAt = ContinuousClock.now
    while true {
      try Task.checkCancellation()
      do { return try await backend.request(path, method: "GET", body: nil) }
      catch {
        try Task.checkCancellation()
        if case BackendError.starting(let current) = error {
          if current != progress { progress = current; waited = .zero; progressAt = .now }
          guard waited < .seconds(30), progressAt.duration(to: .now) < .seconds(30) else {
            throw BackendError.unavailable("The background helper stopped making startup progress. Try again.")
          }
          // Keep the store's load pending, preserving its loading state and last good value.
          try await pause(.milliseconds(500))
          waited += .milliseconds(500)
          continue
        }
        guard retry, attempt < delays.count else { throw error }
        if case BackendError.http(let status) = error, (400..<500).contains(status), status != 408, status != 429 { throw error }
        try await pause(delays[attempt])
        attempt += 1
      }
    }
  }
}
