import LimmrKit
import SwiftUI

struct YourDotSection: View {
  @Bindable var store: AppStore
  @State private var status: DotSyncStatus?
  @State private var pairing = false
  @State private var code = ""
  @State private var allowSync = true
  @State private var error: String?
  @State private var busy = false

  var body: some View {
    Section("Sync with another Limmr") {
      if let status {
        if status.paired {
          HStack {
            Label(status.peer.name.isEmpty ? "Another Limmr" : status.peer.name, systemImage: "desktopcomputer")
              .font(.headline)
            if status.paused { Text("Paused").font(AppType.caption).foregroundStyle(AccessibleSecondary()) }
            Spacer()
            Menu {
              Button(status.paused ? "Resume" : "Pause") { change("/v1/sync/pause", body: ["paused": !status.paused]) }
              Button("Unpair") { change("/v1/sync/pair", method: "DELETE") }
            } label: { Image(systemName: "ellipsis") }
              .menuStyle(.borderlessButton).fixedSize().accessibilityLabel("Sync options")
          }
          Text(lastSync(status)).font(.callout).foregroundStyle(AccessibleSecondary())
          Text("\(status.itemsIn) items received · \(status.itemsOut) items sent")
            .font(.callout).foregroundStyle(AccessibleSecondary())
          Label(status.lease.thisMac ? "This Mac runs reminders and check-ins" : "\(status.peer.name.isEmpty ? "The other Limmr" : status.peer.name) runs reminders and check-ins",
            systemImage: "bell.badge")
          ForEach(status.mergeReviews) { review in
            VStack(alignment: .leading, spacing: 8) {
              Text("Two ‘\(review.name)’ entries").font(.headline)
              Text("Choose whether they belong together.").font(.callout).foregroundStyle(AccessibleSecondary())
              HStack {
                Button("Merge") { change("/v1/sync/reviews/\(review.id)", body: ["action": "merge"]) }
                Button("Keep both") { change("/v1/sync/reviews/\(review.id)", body: ["action": "keep_both"]) }
              }
            }
          }
          Text("Unpairing keeps your tasks, people and circles.").font(.caption).foregroundStyle(AccessibleSecondary())
        } else {
          HStack(alignment: .center, spacing: 16) {
            Text("Keep tasks, people and circles the same on this Mac and another Limmr, such as a dot, a server or another Mac.")
              .font(.callout).foregroundStyle(AccessibleSecondary())
            Spacer(minLength: 0)
            Button("Pair…") { pairing = true }.limmrButton().accessibilityLabel("Pair with another Limmr")
          }
        }
      } else {
        Text("Checking sync…").foregroundStyle(AccessibleSecondary())
      }
      if status != nil {
        Text("Hidden chats and AI permissions are separate on each copy of Limmr. Review them on both.")
          .font(.caption).foregroundStyle(AccessibleSecondary())
      }
      Text("AIs you connect appear under Your AIs. Syncing is for another copy of Limmr.")
        .font(.caption).foregroundStyle(AccessibleSecondary())
      if let error { Text(error).foregroundStyle(AccessibleSecondary()).accessibilityLabel("Limmr sync: \(error)") }
    }
    .disabled(busy)
    .task {
      await load()
      guard !store.snapshot else { return }
      while !Task.isCancelled {
        do { try await Task.sleep(for: .seconds(5)) } catch { return }
        await load()
      }
    }
    .sheet(isPresented: $pairing) {
      VStack(alignment: .leading, spacing: 16) {
        Text("Pair with another Limmr").font(.title2.bold())
        Text("Start pairing on the other Limmr. Enter the six-digit code it shows you.")
        Text("Hidden chats and AI permissions do not sync. Review them separately on the other Limmr before giving its AIs access.")
          .font(.callout).foregroundStyle(AccessibleSecondary())
        if store.remote?.enabled == true {
          DisclosureGroup("Pairing command") {
            CopyField(title: "Run on the other Limmr", value: "limmr-engine sync pair --peer \(store.remote?.publicUrl ?? "")")
          }
          TextField("Six-digit code", text: $code).textFieldStyle(.roundedBorder)
          Toggle("Let this Limmr sync my tasks, people and circles.", isOn: $allowSync)
          HStack {
            Button("Cancel") { pairing = false }
            Button("Pair") {
              change("/v1/sync/pair", body: ["code": code])
            }.disabled(!allowSync || code.count != 6 || !code.allSatisfy(\.isNumber))
          }
        } else {
          Text("Set up remote access so the other Limmr can reach this Mac.")
          Button("Set up remote access") { pairing = false; store.showRemote = true }
          Button("Close") { pairing = false }
        }
        if let error { Text(error).font(.callout) }
      }.padding(24).frame(width: 480).dismissOnEscape()
    }
  }

  private func lastSync(_ status: DotSyncStatus) -> String {
    if !status.peer.lastError.isEmpty { return status.peer.lastError }
    if store.demo || store.snapshot { return "Last synced just now" }
    guard let date = ISO8601DateFormatter().date(from: status.peer.lastSyncAt), date.timeIntervalSince1970 > 0 else { return "Waiting for the first sync" }
    let formatter = RelativeDateTimeFormatter()
    formatter.unitsStyle = .full
    return "Last synced \(formatter.localizedString(for: date, relativeTo: Date()))"
  }
  private func load() async {
    do { status = try await store.backend.get("/v1/sync"); error = nil }
    catch { self.error = "Couldn’t refresh sync. Showing the last available status." }
  }
  private func change<T: Encodable & Sendable>(_ path: String, method: String = "POST", body: T) {
    Task {
      busy = true
      defer { busy = false }
      do {
        _ = try await store.backend.request(path, method: method, body: Wire.data(body))
        await load()
        if path == "/v1/sync/pair" { pairing = false }
      } catch { self.error = "Couldn’t update sync. Check the other Limmr’s connection and try again." }
    }
  }
  private func change(_ path: String, method: String) { change(path, method: method, body: [String: String]()) }
}
