import AppKit
import Darwin
import Foundation
import LimmrKit

/// Explicit snapshot-only synthetic fixture; never used by the installed app.
actor P1FixtureBackend: Backend {
  let directory: URL
  let fallback = DemoBackend()
  init(directory: String) { self.directory = URL(fileURLWithPath: directory) }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    let route = String(path.split(separator: "?")[0]).split(separator: "/").last.map(String.init) ?? ""
    if method == "GET", route == "presence" { return Data(#"{"agents":[]}"#.utf8) }
    if method == "GET", ["people", "map", "graph", "chats"].contains(route) {
      return try Data(contentsOf: directory.appendingPathComponent(route + ".json"))
    }
    return try await fallback.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { AsyncThrowingStream { $0.finish() } }
}
@MainActor enum P1Performance {
  static func measure(seconds: Double, output: String) async {
    var before = rusage(), after = rusage()
    getrusage(RUSAGE_SELF, &before)
    let start = Date()
    var last = start, maxDelay = 0.0
    while Date().timeIntervalSince(start) < seconds {
      try? await Task.sleep(for: .milliseconds(20))
      let now = Date()
      maxDelay = max(maxDelay, now.timeIntervalSince(last) - 0.020)
      last = now
    }
    getrusage(RUSAGE_SELF, &after)
    func cpu(_ usage: rusage) -> Double {
      Double(usage.ru_utime.tv_sec + usage.ru_stime.tv_sec) + Double(usage.ru_utime.tv_usec + usage.ru_stime.tv_usec) / 1_000_000
    }
    let elapsed = Date().timeIntervalSince(start)
    let values: [String: Any] = ["seconds": elapsed, "cpu_pct": (cpu(after) - cpu(before)) / elapsed * 100,
      "peak_rss_mb": Double(after.ru_maxrss) / 1_048_576, "main_actor_max_delay_ms": maxDelay * 1000,
      "mode": "offscreen synthetic NSHostingView, not a frontmost linked account"]
    if let data = try? JSONSerialization.data(withJSONObject: values, options: [.sortedKeys, .prettyPrinted]) {
      try? data.write(to: URL(fileURLWithPath: output))
    }
  }
}
