import Foundation

public struct LabeledValue: Codable, Sendable, Equatable {
  public var label: String
  public var value: String
  public var source: String
  public init(label: String, value: String, source: String = "you") {
    self.label = label; self.value = value; self.source = source
  }
}
public struct PersonDetails: Codable, Sendable, Equatable {
  public var phones: [LabeledValue]
  public var emails: [LabeledValue]
  public var birthday: String
  public var handles: [LabeledValue]
  public var location: String
  public var org: String = ""
  public var title: String = ""
  public var sources: [String: String]? = nil
  public var pronouns: String
  public init(phones: [LabeledValue] = [], emails: [LabeledValue] = [], birthday: String = "",
              handles: [LabeledValue] = [], location: String = "", pronouns: String = "") {
    self.phones = phones; self.emails = emails; self.birthday = birthday
    self.handles = handles; self.location = location; self.pronouns = pronouns
  }
  enum CodingKeys: String, CodingKey { case phones, emails, birthday, handles, location, pronouns, org, title, sources }
  /// Tolerates missing or null fields so one odd record can't stop the whole app from refreshing.
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    phones = (try? c.decodeIfPresent([LabeledValue].self, forKey: .phones)) ?? []
    emails = (try? c.decodeIfPresent([LabeledValue].self, forKey: .emails)) ?? []
    birthday = (try? c.decodeIfPresent(String.self, forKey: .birthday)) ?? ""
    handles = (try? c.decodeIfPresent([LabeledValue].self, forKey: .handles)) ?? []
    location = (try? c.decodeIfPresent(String.self, forKey: .location)) ?? ""
    org = (try? c.decodeIfPresent(String.self, forKey: .org)) ?? ""
    title = (try? c.decodeIfPresent(String.self, forKey: .title)) ?? ""
    sources = try? c.decodeIfPresent([String: String].self, forKey: .sources)
    pronouns = (try? c.decodeIfPresent(String.self, forKey: .pronouns)) ?? ""
  }
}
public struct DetailSuggestion: Codable, Identifiable, Sendable, Equatable {
  public var createdAt: String? = nil
  public var evidenceMessageIds: [String]? = nil
  public var id: String
  public var field: String
  public var label: String
  public var value: String
  public var source: String
  public var status: String
  public init(id: String, field: String, label: String = "", value: String, source: String, status: String = "suggested") {
    self.id = id; self.field = field; self.label = label; self.value = value
    self.source = source; self.status = status
  }
}
