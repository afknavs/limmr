import AppKit
import Foundation
import LimmrKit

// Reports are reconstructed from approved facts, never from health detail,
// arbitrary errors, names, identifiers, or verbatim log lines.
enum ConnectionDiagnostics {
  static let checkIDs: Set<String> = ["version", "whatsapp", "phone", "history", "messages_db", "hub_db", "sync_log", "logs", "local_mcp", "remote", "remote_listener", "tasks", "watches", "engine_restarts", "old_bridge"]
  static let warningKinds = ["Engine stopped responding; restart requested", "Storage cleanup saved for later", "Remote access unavailable", "WhatsApp reconnect unavailable", "Engine warning (private details omitted)"]
  static let stages: Set<String> = ["hub.open", "messages.open", "listener", "migration.sync", "migration.sync_storage", "storage.maintenance", "warmup.hub", "warmup.people", "warmup.circles", "remote.start"]
  static func version(_ value: String?) -> String {
    guard let value, value.range(of: #"^\d{1,3}(\.\d{1,3}){1,3}([+-][A-Za-z0-9.]{1,20})?$"#, options: .regularExpression) != nil else { return "unavailable" }
    return value
  }
  static func report(appVersion: String?, engineVersion: String?, checks: [HealthCheck],
                     engineReport: String = "", logTail: String = "", elapsed: TimeInterval = 0) -> String {
    var lines = ["Limmr diagnostics", "App version: \(version(appVersion))", "Engine version: \(version(engineVersion))"]
    for check in checks {
      let id = checkIDs.contains(check.id) ? check.id : check.id.hasPrefix("agent_") ? "ai_app" : "other"
      let state = ["ok", "warn", "fail"].contains(check.state) ? check.state : "unknown"
      lines.append("check \(id): \(state)")
    }
    if checks.isEmpty { lines.append("Health checks: unavailable while the engine reconnects") }
    for line in engineReport.split(separator: "\n") {
      let text = String(line)
      if text.hasPrefix("health_check_ms="), let ms = Double(text.dropFirst("health_check_ms=".count)), ms.isFinite, (0..<3_600_000).contains(ms) {
        lines.append(String(format: "health_check_ms=%.3f", ms))
      }
      if let kind = warningKinds.first(where: { text.hasSuffix($0) }) { lines.append(kind) }
      let fields = text.split(whereSeparator: \.isWhitespace)
      if let stage = fields.first(where: { $0.hasPrefix("stage=") }), stages.contains(String(stage.dropFirst(6))),
         let timing = fields.first(where: { $0.hasPrefix("duration_ms=") }), let ms = Double(timing.dropFirst(12)), ms.isFinite, (0..<3_600_000).contains(ms) {
        lines.append("\(stage) " + String(format: "duration_ms=%.3f", ms))
      }
    }
    for line in logTail.split(separator: "\n").filter({ $0.contains("WARN ") }).suffix(10) {
      if line.contains("engine watchdog:") { lines.append(warningKinds[0]) }
      else if line.contains("sync storage maintenance deferred") { lines.append(warningKinds[1]) }
      else if line.contains("remote listener unavailable") { lines.append(warningKinds[2]) }
      else if line.contains("WhatsApp reconnect unavailable") { lines.append(warningKinds[3]) }
      else { lines.append(warningKinds[4]) }
    }
    lines.append(String(format: "copy_duration_ms=%.3f", max(0, elapsed.isFinite ? elapsed * 1000 : 0)))
    return lines.joined(separator: "\n") + "\n"
  }
  static func logTail(at url: URL) -> String {
    guard let file = try? FileHandle(forReadingFrom: url) else { return "" }
    defer { try? file.close() }
    guard let size = try? file.seekToEnd() else { return "" }
    try? file.seek(toOffset: size > 65536 ? size - 65536 : 0)
    guard let data = try? file.read(upToCount: 65536) else { return "" }
    let text = String(decoding: data, as: UTF8.self)
    return size > 65536 ? text.split(separator: "\n", omittingEmptySubsequences: false).dropFirst().joined(separator: "\n") : text
  }
}
extension AppStore {
  func copyDiagnostics() async {
    let start = Date()
    let data: Data?
    if demo || snapshot { data = try? await backend.request("/v1/diagnostics", method: "GET", body: nil) }
    else { data = try? await system.localData("/v1/diagnostics") }
    let tail = demo || snapshot ? "" : ConnectionDiagnostics.logTail(at: system.paths.home.appending(path: "logs/engine.log"))
    let report = ConnectionDiagnostics.report(
      appVersion: Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "0.1.0",
      engineVersion: status?.engineVersion, checks: checks,
      engineReport: data.map { String(decoding: $0, as: UTF8.self) } ?? "", logTail: tail,
      elapsed: Date().timeIntervalSince(start))
    NSPasteboard.general.clearContents()
    NSPasteboard.general.setString(report, forType: .string)
    notice = "Diagnostics copied."
  }
}
