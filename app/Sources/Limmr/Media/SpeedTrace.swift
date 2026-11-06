import Foundation
import os

/// Opt-in, synthetic runs only. Never records payloads, URLs, names or credentials.
@MainActor enum SpeedTrace {
  static var enabled = ProcessInfo.processInfo.environment["LIMMR_SPEED_TRACE"] == "1"
  static var bodies: [String: Int] = [:]
  nonisolated static let log = OSLog(subsystem: "dev.limmr.performance", category: .pointsOfInterest)
  static func body(_ screen: String) {
    guard enabled else { return }
    bodies[screen, default: 0] += 1
    os_signpost(.event, log: log, name: "View body", "%{public}@", screen as NSString)
  }
}
