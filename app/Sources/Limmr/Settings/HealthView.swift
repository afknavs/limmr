import SwiftUI
import LimmrKit

struct HealthView: View {
  @Bindable var store: AppStore
  var body: some View {
    VStack(alignment: .leading, spacing: 12) {
      ForEach(store.checks) { check in
        HStack(alignment: .top, spacing: 10) {
          Image(systemName: check.state == "ok" ? "checkmark.circle" : "exclamationmark.circle")
            .foregroundStyle(AccessibleSecondary())
          VStack(alignment: .leading, spacing: 3) {
            Text(plainTitle(check)).font(.callout.weight(.medium))
            Text(plainDetail(check)).font(.caption).foregroundStyle(AccessibleSecondary())
          }
          Spacer()
          if let fix = check.fixId, check.state != "ok", fix != "migrate_whatsapp_mcp" {
            Button("Repair") { Task { await store.fix(fix) } }.buttonStyle(NativeGlassButtonStyle()).disabled(store.busy)
          }
        }
      }
      Button("Copy diagnostics") { Task { await store.copyDiagnostics() } }
        .buttonStyle(NativeGlassButtonStyle())
    }.padding(.vertical, 8)
  }
  private func plainTitle(_ check: HealthCheck) -> String {
    if check.id == "local_mcp" { return "AI app connection" }
    if check.id == "remote_listener" { return "Remote access" }
    return plain(check.title)
  }
  private func plainDetail(_ check: HealthCheck) -> String { plain(check.detail) }
  private func plain(_ value: String) -> String {
    if ["helper", "listener", "mcp", "loopback", "hlc", "port"].contains(where: { value.localizedCaseInsensitiveContains($0) }) {
      return "See Advanced for technical details."
    }
    return value
  }
}
