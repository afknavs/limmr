import Foundation

public struct CircleAffinity: Codable, Sendable, Equatable {
  public var circleId: String
  public var score: Double
  public var reasons: [String]
  public init(circleId: String, score: Double, reasons: [String]) {
    self.circleId = circleId; self.score = score; self.reasons = reasons
  }
}
public struct RelationshipInference: Codable, Sendable, Equatable {
  public var label: String
  public var howKnown: String
  public var confidence: Double
  public var sensitive: Bool
  public var status = "inferred"
  public init(label: String, howKnown: String, confidence: Double, sensitive: Bool = false) {
    self.label = label; self.howKnown = howKnown; self.confidence = confidence; self.sensitive = sensitive
  }
  enum CodingKeys: String, CodingKey { case label, howKnown, confidence, sensitive, status }
  public init(from decoder: any Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    label = try c.decode(String.self, forKey: .label)
    howKnown = try c.decode(String.self, forKey: .howKnown)
    confidence = try c.decode(Double.self, forKey: .confidence)
    sensitive = try c.decodeIfPresent(Bool.self, forKey: .sensitive) ?? false
    status = try c.decodeIfPresent(String.self, forKey: .status) ?? "inferred"
  }
  /// Invalid/uncertain output produces no field. There is no remote-model fallback.
  public static func parse(_ data: Data) -> Self? {
    guard let value = try? Wire.decode(Self.self, from: data), value.confidence.isFinite,
      (0.65...1).contains(value.confidence), !value.label.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
      !value.howKnown.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
      value.label.utf8.count <= 120, value.howKnown.utf8.count <= 500 else { return nil }
    return value
  }
}
public struct UnderstandingContact: Codable, Sendable {
  public var id: String
  public var name: String
  public var jids: [String]?
}
public struct UnderstandingContacts: Codable, Sendable { public var people: [UnderstandingContact] }

public struct GroupInCommon: Codable, Sendable, Equatable, Identifiable {
  public var id: String
  public var name: String
  public var circleIds: [String]
  public var lastActiveAt: String?
  public init(id: String, name: String, circleIds: [String] = [], lastActiveAt: String? = nil) {
    self.id = id; self.name = name; self.circleIds = circleIds; self.lastActiveAt = lastActiveAt
  }
}
