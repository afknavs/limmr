import Foundation

public struct OwnerProfile: Codable, Equatable, Sendable {
  public struct ClosePerson: Codable, Equatable, Identifiable, Sendable {
    public var name: String
    public var relation: String
    public var personId: String?
    public var id: String { name + relation }
    public init(name: String, relation: String, personId: String? = nil) {
      self.name = name
      self.relation = relation
      self.personId = personId
    }
  }
  public struct Project: Codable, Equatable, Identifiable, Sendable {
    public var name: String
    public var with: [String]
    public var id: String { name }
    public init(name: String, with: [String] = []) {
      self.name = name
      self.with = with
    }
  }
  public struct Suggestion: Codable, Equatable, Identifiable, Sendable {
    public var id: String
    public var field: String
    public var value: ProfileValue
    public var source: String
    public var at: String
  }
  public var name = ""
  public var about = ""
  public var studies = ""
  public var work = ""
  public var people: [ClosePerson] = []
  public var projects: [Project] = []
  public var updatedBy: String?
  public var updatedAt: String?
  public var suggestions: [Suggestion] = []
  public init() {}
  public var thin: Bool { (studies.isEmpty && work.isEmpty) || people.isEmpty }
  public static var sample: Self {
    var p = Self()
    p.name = "Alex"
    p.studies = "Northfield University, BSc Psychology"
    p.about = "Psychology student who enjoys design and community projects."
    p.people = [
      .init(name: "Mum", relation: "mum", personId: "person-02"),
      .init(name: "Noor Chen", relation: "sister", personId: "person-05"),
      .init(name: "Maya Rao", relation: "best friend", personId: "person-04"),
    ]
    p.projects = [.init(name: "Design Club 2026", with: ["Maya Rao"])]
    p.suggestions = [
      .init(
        id: "owner-suggestion", field: "work", value: .text("Part-time library assistant"),
        source: "agent-2", at: DemoData.now)
    ]
    return p
  }
}
public enum ProfileValue: Codable, Equatable, Sendable {
  case text(String)
  case people([OwnerProfile.ClosePerson])
  case projects([OwnerProfile.Project])
  public init(from decoder: any Decoder) throws {
    let c = try decoder.singleValueContainer()
    if let s = try? c.decode(String.self) {
      self = .text(s)
    } else if let p = try? c.decode([OwnerProfile.ClosePerson].self) {
      self = .people(p)
    } else {
      self = .projects(try c.decode([OwnerProfile.Project].self))
    }
  }
  public func encode(to encoder: any Encoder) throws {
    var c = encoder.singleValueContainer()
    switch self {
    case .text(let s): try c.encode(s)
    case .people(let p): try c.encode(p)
    case .projects(let p): try c.encode(p)
    }
  }
  public var display: String {
    switch self {
    case .text(let s): s
    case .people(let p): p.map { $0.name + " · " + $0.relation }.joined(separator: ", ")
    case .projects(let p): p.map(\.name).joined(separator: ", ")
    }
  }
}
public struct CircleAutoChange: Codable, Identifiable, Sendable {
  public var id: String
  public var circleId: String
  public var name: String
  public var agent: String
  public var action: String
  public var at: String
  public var count: Int
  public var changeIds: [String]
  public var peopleIds: [String]
  public var peopleChanges: [String: String]
  public var runId: String?
  public var byKind: String?
  public var text: String {
    byKind == "structure" || (agent == "Limmr" && action == "placed")
      ? "Limmr placed \(count) people in \(name)" : "\(agent) \(action) \(name)"
  }
  public init(id: String, circleId: String, name: String, agent: String, action: String,
              at: String, count: Int = 1, changeIds: [String]? = nil,
              peopleIds: [String] = [], peopleChanges: [String: String] = [:],
              runId: String? = nil, byKind: String? = nil) {
    self.id = id; self.circleId = circleId; self.name = name; self.agent = agent
    self.action = action; self.at = at; self.count = count; self.changeIds = changeIds ?? [id]
    self.peopleIds = peopleIds; self.peopleChanges = peopleChanges
    self.runId = runId; self.byKind = byKind
  }
  enum CodingKeys: String, CodingKey {
    case id, circleId, name, agent, action, at, count, changeIds, peopleIds, peopleChanges, runId, byKind
  }
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    self.init(id: try c.decode(String.self, forKey: .id),
      circleId: try c.decode(String.self, forKey: .circleId),
      name: try c.decode(String.self, forKey: .name), agent: try c.decode(String.self, forKey: .agent),
      action: try c.decode(String.self, forKey: .action), at: try c.decode(String.self, forKey: .at),
      count: try c.decodeIfPresent(Int.self, forKey: .count) ?? 1,
      changeIds: try c.decodeIfPresent([String].self, forKey: .changeIds),
      peopleIds: try c.decodeIfPresent([String].self, forKey: .peopleIds) ?? [],
      peopleChanges: try c.decodeIfPresent([String: String].self, forKey: .peopleChanges) ?? [:],
      runId: try c.decodeIfPresent(String.self, forKey: .runId),
      byKind: try c.decodeIfPresent(String.self, forKey: .byKind))
  }
}
public struct CircleChangesResponse: Codable, Sendable {
  public var changes: [CircleAutoChange]
}
public struct CircleAddition: Codable, Identifiable, Sendable {
  public var id: String
  public var circleId: String
  public var chats: [String]
  public var people: [String]
  public var why: String
  public var agentId: String
}
