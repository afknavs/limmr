import LimmrKit
import SwiftUI

private struct SnapshotAccessKey: EnvironmentKey { static let defaultValue = false }
extension EnvironmentValues {
  var snapshotIntegrationAccess: Bool {
    get { self[SnapshotAccessKey.self] }
    set { self[SnapshotAccessKey.self] = newValue }
  }
}
