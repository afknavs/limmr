import Foundation

public enum ProfileDetailsPresentation {
  public struct Row: Sendable, Equatable {
    public let title: String
    public let value: String
    public let source: String
  }
  public static func rows(_ details: PersonDetails) -> [Row] {
    var result: [Row] = []
    for (title, values) in [("Phone", details.phones), ("Email", details.emails), ("Handle", details.handles)] {
      for value in values where !value.value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
        result.append(.init(title: title, value: value.value, source: value.source))
      }
    }
    for (title, key, value) in [("Birthday", "birthday", details.birthday), ("Location", "location", details.location),
                              ("Organisation", "org", details.org), ("Job title", "title", details.title), ("Pronouns", "pronouns", details.pronouns)] {
      if !value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
        result.append(.init(title: title, value: value, source: details.sources?[key] ?? "you"))
      }
    }
    return result
  }
}
