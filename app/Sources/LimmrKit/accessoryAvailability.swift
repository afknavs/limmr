import Foundation

public enum AccessoryAvailability {
  public static func isAvailable(enabled: Bool, installed: () -> Bool) -> Bool { enabled || installed() }
}
