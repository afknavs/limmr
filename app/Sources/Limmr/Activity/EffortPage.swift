import LimmrKit
import SwiftUI

struct EffortPage: View {
  @Bindable var store: AppStore
  let id: String
  @State private var detail: EffortDetailResponse?
  @State private var error: String?
  var body: some View {
    VStack(alignment: .leading, spacing: 0) {
      Button(store.activityBackTitle, systemImage: "chevron.left") { store.leaveActivityRoute() }
        .limmrButton().font(.callout).padding(.horizontal, 24).padding(.top, 20)
      Group {
        if let detail {
          VStack(alignment: .leading, spacing: 8) {
            if error != nil { Text("Couldn’t refresh").font(AppType.caption).foregroundStyle(AccessibleSecondary()).padding(.horizontal, 24) }
            EffortDetailView(store: store, detail: detail)
          }
        }
        else if let error {
          VStack(spacing: 16) {
            Text(error).foregroundStyle(AccessibleSecondary())
            Button("Try again") { Task { await load() } }
          }.frame(maxWidth: .infinity, maxHeight: .infinity)
        } else { ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity) }
      }
    }.navigationBarBackButtonHidden(true).background { AppBackground() }
      .task(id: id) { await load() }
  }
  private func load() async {
    error = nil
    do {
      let result: EffortDetailResponse = try await store.backend.get("/v1/efforts/\(id.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed.subtracting(CharacterSet(charactersIn: "/?#%"))) ?? "")")
      try Task.checkCancellation()
      detail = result
    } catch {
      guard !Task.isCancelled else { return }
      self.error = "This effort could not be loaded."
    }
  }
}
