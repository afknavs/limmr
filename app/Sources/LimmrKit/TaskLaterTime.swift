import Foundation

public enum TaskLaterTime {
  public static func defaultUntil(now: Date, calendar: Calendar = .current) -> Date {
    let day = calendar.startOfDay(for: now)
    let offset = calendar.component(.hour, from: now) < 4 ? 0 : 1
    return calendar.date(bySettingHour: 9, minute: 0, second: 0, of: calendar.date(byAdding: .day, value: offset, to: day)!)!
  }
  public static func option(_ name: String, now: Date, calendar: Calendar = .current) -> Date {
    if name == "In 3 hours" { return now.addingTimeInterval(3 * 3600) }
    let tomorrow = calendar.date(byAdding: .day, value: 1, to: now)!
    if name == "Tomorrow morning" { return calendar.date(bySettingHour: 9, minute: 0, second: 0, of: tomorrow)! }
    let days = (2 - calendar.component(.weekday, from: now) + 7) % 7
    let monday = calendar.date(byAdding: .day, value: days == 0 ? 7 : days, to: now)!
    return calendar.date(bySettingHour: 9, minute: 0, second: 0, of: monday)!
  }
  public static func status(_ value: String) -> String {
    guard let at = Timestamp.date(value) else { return value }
    let f = DateFormatter(); f.locale = Locale(identifier: "en_GB"); f.dateFormat = "EEE H:mm"
    return f.string(from: at)
  }
  public static func notice(_ value: String, now: Date) -> String {
    guard let at = Timestamp.date(value) else { return "Back " + value }
    let f = DateFormatter(); f.locale = Locale(identifier: "en_GB"); f.dateFormat = "H:mm"
    if Calendar.current.isDate(at, inSameDayAs: Calendar.current.date(byAdding: .day, value: 1, to: now)!) { return "Back tomorrow at " + f.string(from: at) }
    if Calendar.current.isDate(at, inSameDayAs: now) { return "Back today at " + f.string(from: at) }
    return "Back " + status(value)
  }
}
