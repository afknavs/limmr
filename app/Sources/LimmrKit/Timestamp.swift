import Foundation

/// ISO8601DateFormatter construction was dominating log layout/sorts. Share the
/// parsers under a lock and bound parsed values; offset/fraction semantics stay intact.
private final class TimestampParser: @unchecked Sendable {
  let lock = NSLock()
  let fractional: ISO8601DateFormatter
  let basic = ISO8601DateFormatter()
  var dates: [String: Date] = [:]
  init() {
    fractional = ISO8601DateFormatter()
    fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
  }
  func date(_ value: String) -> Date? {
    lock.withLock {
      if let cached = dates[value] { return cached }
      let date = fractional.date(from: value) ?? basic.date(from: value)
      if let date {
        if dates.count >= 4096 { dates.removeAll(keepingCapacity: true) }
        dates[value] = date
      }
      return date
    }
  }
}
public enum Timestamp {
  private static let parser = TimestampParser()
  public static func date(_ value: String) -> Date? { parser.date(value) }
}

extension Activity {
  /// IDs are cursors, not timestamps. Compare instants, including fractional seconds and offsets.
  public static func newestFirst(_ lhs: Activity, _ rhs: Activity) -> Bool {
    let left = Timestamp.date(lhs.at) ?? .distantPast
    let right = Timestamp.date(rhs.at) ?? .distantPast
    return left == right ? lhs.id > rhs.id : left > right
  }
}
