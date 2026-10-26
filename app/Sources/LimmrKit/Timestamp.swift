import Foundation

public enum Timestamp {
  public static func date(_ value: String) -> Date? {
    let formatter = ISO8601DateFormatter()
    formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    return formatter.date(from: value) ?? ISO8601DateFormatter().date(from: value)
  }
}

extension Activity {
  /// IDs are cursors, not timestamps. Compare instants, including fractional seconds and offsets.
  public static func newestFirst(_ lhs: Activity, _ rhs: Activity) -> Bool {
    let left = Timestamp.date(lhs.at) ?? .distantPast
    let right = Timestamp.date(rhs.at) ?? .distantPast
    return left == right ? lhs.id > rhs.id : left > right
  }
}
