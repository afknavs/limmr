import Foundation
import Testing

@testable import LimmrKit

@Suite("E2 effort presentation") struct EffortQualityTests {
  @Test func titlesAndDormancyStayInternalAndHomeHasFourUniqueRows() {
    var unnamed = DemoEfforts.efforts[0]
    unnamed.title = "  "
    #expect(!EffortPresentation.visible(unnamed))
    for junk in [
      "Image with Maya", "Media omitted", "OTP code", "Shared work", "Verification code",
    ] {
      unnamed.title = junk
      #expect(!EffortPresentation.visible(unnamed))
    }
    let now = Timestamp.date(DemoData.now)!
    var rows = DemoEfforts.updates
    var duplicate = rows[0]
    duplicate.circle.id = "other-circle"
    rows.insert(duplicate, at: 1)
    var quiet = rows[0]
    quiet.effortId = "quiet"
    quiet.status = "dormant"
    rows.insert(quiet, at: 0)
    var junk = rows[0]
    junk.effortId = "junk"
    junk.title = "Photo with Maya"
    rows.insert(junk, at: 0)
    for i in 0..<8 {
      var row = rows[2]
      row.effortId = "extra-\(i)"
      rows.append(row)
    }
    let shown = EffortPresentation.homeUpdates(rows, now: now)
    #expect(shown.count == 4)
    #expect(Set(shown.map(\.effortId)).count == 4)
    #expect(shown.allSatisfy { $0.status != "dormant" && EffortPresentation.hasTitle($0.title) })
    #expect(EffortPresentation.homeUpdates([quiet, junk], now: now).isEmpty)
    #expect(EffortPresentation.homeUpdates([], now: now).isEmpty)
    #expect(DemoEfforts.efforts.first { $0.status == "dormant" }?.summary == "")
  }
  @Test func dueLabelRequiresADateAndSeenUsesOnlyRenderedRows() {
    var row = DemoEfforts.updates[0]
    row.due = nil
    #expect(!row.statusLabel.contains("due"))
    row.due = "not-a-date"
    #expect(!row.statusLabel.contains("due"))
    let rows = EffortPresentation.homeUpdates(
      DemoEfforts.updates, now: Timestamp.date(DemoData.now)!)
    let seen = CircleEffortSeen(rows)
    #expect(seen.seen.count <= rows.count)
  }
}

extension EffortQualityTests {
  @Test func homeEligibilityUsesSeenStateAndSevenDayDeadlineWindow() {
    let now = Timestamp.date(DemoData.now)!
    var row = DemoEfforts.updates[0]
    row.changed = false
    row.status = "moving"
    row.due = nil
    #expect(EffortPresentation.homeUpdates([row], now: now).isEmpty)
    row.changed = true
    #expect(EffortPresentation.homeUpdates([row], now: now).count == 1)
    row.changed = false
    row.status = "on_you"
    #expect(EffortPresentation.homeUpdates([row], now: now).count == 1)
    row.status = "moving"
    row.due = ISO8601DateFormatter().string(from: now.addingTimeInterval(6 * 86400))
    #expect(EffortPresentation.homeUpdates([row], now: now).count == 1)
    row.due = ISO8601DateFormatter().string(from: now.addingTimeInterval(8 * 86400))
    #expect(EffortPresentation.homeUpdates([row], now: now).isEmpty)
    row.due = ISO8601DateFormatter().string(from: now.addingTimeInterval(-86400))
    #expect(EffortPresentation.homeUpdates([row], now: now).isEmpty)
  }
}
