import Foundation

public enum ConnectionGrace {
  public static func engineWarming(_ status: Status?, now: Date) -> Bool {
    status?.engineStartedAt.flatMap(Timestamp.date).map { now.timeIntervalSince($0) < 90 } ?? false
  }
  public static func reconnecting(_ status: Status?, now: Date, launchedAt: Date?, dropAt: Date? = nil) -> Bool {
    guard let status else { return launchedAt.map { now.timeIntervalSince($0) < 90 } ?? false }
    guard status.linked, !status.connected, status.linkPhase != "logged_out" else { return false }
    if engineWarming(status, now: now) || status.linkPhase == "connecting" { return true }
    if let at = status.linkPhaseAt.flatMap(Timestamp.date) ?? dropAt { return now.timeIntervalSince(at) < 90 }
    return launchedAt.map { now.timeIntervalSince($0) < 90 } ?? false
  }
}
