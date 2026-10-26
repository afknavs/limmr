import SwiftUI
import LimmrKit

struct HealthView: View {
  @Bindable var store: AppStore
  var body: some View {
    Section {
      HStack {
        Button("Run all checks") { Task { await store.refresh() } }
          .buttonStyle(NativeGlassButtonStyle(prominent: true))
        Button("Copy diagnostics") {
          Task {
            await store.perform {
              let data = try await store.backend.request(
                "/v1/diagnostics", method: "GET", body: nil)
              NSPasteboard.general.clearContents()
              NSPasteboard.general.setString(
                String(decoding: data, as: UTF8.self), forType: .string)
              store.notice = "Redacted diagnostics copied."
            }
          }
        }
        .buttonStyle(NativeGlassButtonStyle())
      }
      if let failure = store.refreshError {
        Text(failure).foregroundStyle(AccessibleSecondary())
        Button("Try again") { Task { await store.refresh() } }.buttonStyle(NativeGlassButtonStyle())
      }
      if store.status == nil {
        Button("Start background helper") {
          Task {
            await store.perform {
              try await store.system.startEngine()
              await store.refresh()
            }
          }
        }
        .buttonStyle(NativeGlassButtonStyle())
      }
      ActionFeedback(store: store)
      if store.notice == "Redacted diagnostics copied." {
        Text("Diagnostics copied").foregroundStyle(AccessibleSecondary())
      }
      ForEach(store.loadFailures.keys.sorted(), id: \.self) { path in
        Text(store.loadFailures[path] ?? "").font(.callout).textSelection(.enabled)
      }
      ForEach(store.checks) { check in
        HStack(alignment: .top, spacing: 12) {
          Image(systemName: check.state == "ok" ? "checkmark.circle" : "arrow.clockwise.circle")
            .foregroundStyle(AccessibleSecondary()).frame(width: 24)
          VStack(alignment: .leading, spacing: 4) {
            Text(check.title).font(AppType.rowTitle)
            Text(check.detail).font(.callout).foregroundStyle(AccessibleSecondary())
          }
          Spacer()
          if let fix = check.fixId {
            Button("Fix") { Task { await store.fix(fix) } }
              .buttonStyle(NativeGlassButtonStyle()).disabled(store.busy)
          }
        }
        .padding(.vertical, 8)
      }
    } header: { SettingsPaneTitle(title: "Health") }
  }
}
