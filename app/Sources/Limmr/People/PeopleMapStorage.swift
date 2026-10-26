import Foundation

/// Retire the old map switch's file without reading it or touching any other setting.
enum PeopleMapStorage {
  static func removeRetiredSetting(in home: URL) {
    try? FileManager.default.removeItem(at: home.appendingPathComponent("map-drift.json"))
  }
}
