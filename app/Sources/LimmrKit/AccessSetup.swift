import Foundation

/// A missing access field preserves the original everything-visible behavior.
public enum AccessCircles: Codable, Equatable, Sendable {
  case all, only([String])
  public init(from decoder: any Decoder) throws {
    let c = try decoder.singleValueContainer()
    if let word = try? c.decode(String.self), word == "all" { self = .all }
    else { self = .only(try c.decode([String].self)) }
  }
  public func encode(to encoder: any Encoder) throws {
    var c = encoder.singleValueContainer()
    switch self { case .all: try c.encode("all"); case .only(let ids): try c.encode(ids) }
  }
}
public struct AgentAccess: Codable, Equatable, Sendable {
  public var circles: AccessCircles
  public var unsorted: Bool
  public var until: String?
  public var ended: Bool?
  public init(circles: AccessCircles = .all, unsorted: Bool? = nil, until: String? = nil) {
    self.circles = circles; self.unsorted = unsorted ?? (circles == .all); self.until = until
  }
  enum CodingKeys: String, CodingKey { case circles, unsorted, until, ended }
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    circles = try c.decodeIfPresent(AccessCircles.self, forKey: .circles) ?? .all
    unsorted = try c.decodeIfPresent(Bool.self, forKey: .unsorted) ?? (circles == .all)
    until = try c.decodeIfPresent(String.self, forKey: .until)
    ended = try c.decodeIfPresent(Bool.self, forKey: .ended)
  }
  // Null explicitly removes an old expiry on PATCH, rather than leaving it unchanged.
  public func encode(to encoder: any Encoder) throws {
    var c = encoder.container(keyedBy: CodingKeys.self)
    try c.encode(circles, forKey: .circles); try c.encode(unsorted, forKey: .unsorted)
    if let until { try c.encode(until, forKey: .until) } else { try c.encodeNil(forKey: .until) }
  }
  public func hasEnded(at now: Date = .now) -> Bool {
    until.flatMap(Timestamp.date).map { $0 <= now } ?? (ended == true)
  }
  public func summary(names: [String: String] = [:], at now: Date = .now) -> String {
    func dateText(_ date: Date) -> String {
      let f = DateFormatter(); f.locale = Locale(identifier: "en_GB"); f.dateFormat = "d MMM"
      return f.string(from: date)
    }
    if hasEnded(at: now) { return until.flatMap(Timestamp.date).map { "Ended " + dateText($0) } ?? "Ended" }
    var text: String
    switch circles {
    case .all: text = "Everything"
    case .only(let ids):
      text = ids.map { names[$0] ?? $0 }.joined(separator: ", ")
      if unsorted { text += (text.isEmpty ? "" : ", ") + "Chats in no circle" }
      if text.isEmpty { text = "No circles" }
    }
    if let date = until.flatMap(Timestamp.date) { text += " · until " + dateText(date) }
    return text
  }
}
public struct SetupStatus: Codable, Sendable {
  public struct CircleCounts: Codable, Sendable { public var confirmed: Int; public var proposed: Int }
  public struct PeopleCounts: Codable, Sendable { public var total: Int; public var withoutDetails: Int }
  public struct NextStep: Codable, Sendable {
    public var `do`: String; public var why: String; public var how: String; public var tools: [String]
  }
  public var ownerProfile: OwnerProfile?
 public var stage: String
  public var owner: String?
  public var linked: Bool?
  public var chats: Int?
  public var circles: CircleCounts?
  public var unsortedActiveChats: Int?
  public var people: PeopleCounts?
  public var circlesWithoutBrief: [String]?
  public var nextSteps: [NextStep]?
  public var sayToOwner: String?
  public init(stage: String) { self.stage = stage }
}
public struct BriefSettings: Codable, Equatable, Sendable {
  public struct OnDevice: Codable, Equatable, Sendable {
    public var enabled: Bool
    public init(enabled: Bool = false) { self.enabled = enabled }
  }
  public var enabled: Bool?
  public var time: String?
  public var onDeviceBriefs: OnDevice?
  public var thisMacEnabled: Bool { onDeviceBriefs?.enabled == true }
  public init(onDeviceEnabled: Bool = false) { onDeviceBriefs = .init(enabled: onDeviceEnabled) }
}
public struct RemoteApprovalChoice: Encodable, Sendable {
  public var code: String
  public var agentId: String?
  public var newAgent: [String: String]?
  public var access: AgentAccess
  /// Lets your own AI set Limmr up: suggest circles, fill in people and write briefs. Sends stay off.
  public var fillIn: Bool
 public var preset: String
  public init(code: String, agentId: String?, newAgent: [String: String]?, access: AgentAccess, fillIn: Bool = false, preset: String = "own") {
    self.code = code; self.agentId = agentId; self.newAgent = newAgent; self.access = access; self.fillIn = fillIn; self.preset = preset
  }
}
public extension Backend {
  func setupStatus() async throws -> SetupStatus { try await get("/v1/setup/status") }
  func updateAccess(_ access: AgentAccess, agentID: String) async throws {
    _ = try await request("/v1/agents/\(agentID)/access", method: "PATCH", body: Wire.data(access))
  }
  func reviewProposals(_ ids: [String], keep: Bool) async throws {
    try await post("/v1/circles/proposals/\(keep ? "accept" : "dismiss")", ["ids": ids])
  }
  func setThisMacBriefs(_ enabled: Bool) async throws -> BriefSettings {
    if enabled { try await post("/v1/agents/this-mac/enable") }
    // Read-modify-write preserves the separate connected-AI brief schedule.
    var settings: BriefSettings = try await get("/v1/settings/briefs")
    settings.onDeviceBriefs = .init(enabled: enabled)
    let data = try await request("/v1/settings/briefs", method: "PUT", body: Wire.data(settings))
    return try Wire.decode(BriefSettings.self, from: data)
  }
}
