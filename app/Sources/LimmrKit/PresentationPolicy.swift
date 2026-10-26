import Foundation

/// Shared by the live window, snapshots and regression tests.
public enum WindowMetrics {
  public static let minimumWidth: Double = 760
  public static let minimumHeight: Double = 520
  public static let defaultWidth: Double = 1100
  public static let defaultHeight: Double = 740
  public static let contentWidth: Double = 880
}

public struct SetupFlow: Equatable, Sendable {
  public enum Step: Int, CaseIterable, Sendable {
    case welcome, link, syncing, who, connect, done
  }
  public var step: Step
  public var helperError: String?
  public init(step: Step = .welcome) { self.step = step }
  public mutating func advance(linkReady: Bool = false) {
    guard step != .link || linkReady else { return }
    step = Step(rawValue: min(step.rawValue + 1, Step.done.rawValue)) ?? .done
  }
  public mutating func back() {
    step = Step(rawValue: max(0, step.rawValue - 1)) ?? .welcome
  }
  public mutating func helperFailed() {
    step = .link
    helperError = "Limmr’s background helper isn’t running."
  }
  public mutating func helperStarted() { helperError = nil }
}
