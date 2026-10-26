import Foundation
import LimmrKit

/// Home-owned snapshot variations; no change to the shared snapshot runner or real backend.
@MainActor enum HomeQuietSnapshot {
  static func prepare(store: AppStore, arguments: [String] = ProcessInfo.processInfo.arguments) {
    if store.snapshot && store.demo && arguments.contains("--a3-loading") {
      store.prepareHomeLoadingSnapshot()
      return
    }
    guard store.snapshot, store.demo,
      arguments.contains("--u6-watches") || arguments.contains("--u6-empty")
    else { return }
    store.home.watches = arguments.contains("--u6-empty") ? [] : DemoHomeTriggers.watches
    store.home.waiting = []
    // Fill the half-year with invented activity, keeping its dates and per-AI totals intact.
    if let history = store.history, let end = history.days.last {
      let formatter = ISO8601DateFormatter()
      let endDate = Timestamp.date(end.date + "T00:00:00Z")!
      store.history = StatsHistory(
        days: (0..<182).map { index in
          var day = history.days[index % history.days.count]
          day.date = String(
            formatter.string(from: endDate.addingTimeInterval(Double(index - 181) * 86400)).prefix(
              10))
          return day
        })
    }
  }
}
