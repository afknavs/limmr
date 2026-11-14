import LimmrKit
import SwiftUI

extension AppStore {
  func setCircleArchived(_ circle: LimmrKit.Circle, archived: Bool) async {
    await perform(action: "circle-\(circle.id)") { [self] in
      _ = try await backend.request("/v1/circles/\(circle.id)", method: "PATCH", body: Wire.data(["archived": archived]))
      circleDetails[circle.id] = nil
      if archived { archivedCircleUndo = circle; leavePeopleRoute() }
      else { archivedCircleUndo = nil; archivedCircles.removeAll { $0.id == circle.id } }
      await refresh()
      if !archived && circlePath.contains(.circle(circle.id)) { await loadCircle(circle.id) }
    }
  }
}
struct ArchivedCirclesSheet: View {
  @Bindable var store: AppStore
  @Environment(\.dismiss) private var dismiss
  var body: some View {
    VStack(alignment: .leading, spacing: 18) {
      HStack { Text("Archived circles").font(.title2.weight(.semibold)); Spacer(); Button("Done") { dismiss() } }
      if store.archivedCircles.isEmpty { Text("No archived circles").foregroundStyle(.secondary) }
      ForEach(store.archivedCircles) { circle in
        HStack {
          Label(circle.name, systemImage: circle.displaySymbol)
          Spacer()
          Button("Restore") { Task { await store.setCircleArchived(circle, archived: false) } }.disabled(store.busy)
        }
        ActionFeedback(store: store, action: "circle-\(circle.id)")
      }
    }.padding(24).frame(width: 440).frame(minHeight: 180)
  }
}
struct ArchivedCircleUndo: View {
  @Bindable var store: AppStore
  var body: some View {
    if let circle = store.archivedCircleUndo {
      HStack {
        Text("Circle archived")
        Button("Undo") { Task { await store.setCircleArchived(circle, archived: false) } }.buttonStyle(.plain)
      }.font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2)
    }
  }
}
