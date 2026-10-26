import Foundation

public enum AgentName {
  public static func normalized(_ value: String) throws -> String {
    let name = value.trimmingCharacters(in: .whitespacesAndNewlines)
    guard (1...40).contains(name.count) else {
      throw BackendError.unavailable("Use a name with 1–40 characters.")
    }
    return name
  }
}
