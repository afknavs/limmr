import SwiftUI
import LimmrKit

struct WatchCard: View {
  @Bindable var store: AppStore
  let id: String
  @State private var detail: WatchDetail?
  @State private var instruction = ""
  @State private var confirmingStop = false
  @State private var loading = false
  @State private var failure: String?
  var body: some View {
    ScrollView {
      LazyVStack(alignment: .leading, spacing: 12) {
        if let detail {
          HStack {
            Text(detail.summary.title).font(.headline)
            Spacer()
            Text(detail.summary.status.capitalized).font(.caption).foregroundStyle(AccessibleSecondary())
          }
          Text(detail.summary.goal).font(.caption).foregroundStyle(AccessibleSecondary()).textSelection(
            .enabled)
          if let runner = detail.summary.runner {
            Text(runner.label).font(.callout)
            Text("\(detail.stats.runs) runs · \(detail.stats.replies) replies").font(.caption)
              .foregroundStyle(AccessibleSecondary())
            if let expiry = detail.summary.expiresAt.flatMap(Timestamp.date) {
              Text("Until " + expiry.formatted(.dateTime.month(.abbreviated).day().hour().minute()))
                .font(.caption).foregroundStyle(AccessibleSecondary())
            }
          }
          if !detail.hits.isEmpty {
            ForEach(Array(detail.hits.prefix(5).enumerated()), id: \.offset) { _, hit in
              VStack(alignment: .leading, spacing: 4) {
                Text(hit.text).font(.callout).fixedSize(horizontal: false, vertical: true)
                Text("\(hit.sender) · \(hit.chat) · \(store.time(hit.at))")
                  .font(.caption).foregroundStyle(AccessibleSecondary()).lineLimit(1)
              }
            }
          } else {
            Text("Nothing yet").font(.callout).foregroundStyle(AccessibleSecondary())
          }
          if !detail.digest.isEmpty {
            Divider()
            Text(detail.digest).font(.callout).fixedSize(horizontal: false, vertical: true)
          }
          if !detail.instructions.isEmpty {
            DisclosureGroup("Instructions (\(detail.instructions.count))") {
              ForEach(Array(detail.instructions.enumerated()), id: \.offset) { _, item in
                Text(item.text).font(.caption).frame(maxWidth: .infinity, alignment: .leading)
              }
            }.font(.caption)
          }
          if detail.summary.isOpen {
            HStack(spacing: 8) {
              TextField("Add instruction", text: $instruction).textFieldStyle(.roundedBorder)
                .onSubmit { if canAdd { Task { await act("instructions") } } }
              Button("Add") { Task { await act("instructions") } }.disabled(!canAdd)
            }
            if instruction.count > 500 {
              Text("Keep instructions to 500 characters.").font(.caption).foregroundStyle(
                .secondary)
            }
            HStack(spacing: 12) {
              Button(detail.summary.status == "paused" ? "Resume" : "Pause") {
                Task { await act(detail.summary.status == "paused" ? "resume" : "pause") }
              }.buttonStyle(NativeGlassButtonStyle()).disabled(loading)
              Button("Stop") { confirmingStop = true }.limmrButton().disabled(loading)
            }
            if confirmingStop {
              Text("Stop watching? It can't be restarted.").font(.caption).foregroundStyle(
                .secondary)
              HStack {
                Button("Stop watching") { Task { await act("stop") } }.disabled(loading)
                Button("Cancel") { confirmingStop = false }
              }
            }
          }
        } else if failure == nil {
          ProgressView("Loading watch…")
        }
        if let failure {
          Text(failure).font(.caption).foregroundStyle(AccessibleSecondary())
          Button("Try again") { Task { await open() } }
            .disabled(loading)
        }
      }.padding(16)
    }.frame(width: 336).frame(maxHeight: 430)
      .fixedSize(horizontal: false, vertical: true)
      .task(id: id) { await open() }
  }
  private var canAdd: Bool {
    !loading && !instruction.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
      && instruction.count <= 500
  }
  private func open() async {
    loading = true
    defer { loading = false }
    let source = store.backend
    let generation = store.backendGeneration
    do {
      try await source.post("/v1/watches/\(id)/seen")
      let value: WatchDetail = try await source.get("/v1/watches/\(id)")
      guard generation == store.backendGeneration, !Task.isCancelled else { return }
      detail = value
      if value.summary.isOpen {
        if let i = store.home.watches.firstIndex(where: { $0.id == id }) {
          store.home.watches[i] = value.summary
        }
      } else {
        store.home.watches.removeAll { $0.id == id }
      }
      failure = nil
      await store.refresh()
    } catch {
      guard generation == store.backendGeneration, !Task.isCancelled else { return }
      failure = "This watch could not be loaded."
    }
  }
  private func act(_ action: String) async {
    guard !loading else { return }
    loading = true
    defer { loading = false }
    let source = store.backend
    let generation = store.backendGeneration
    do {
      _ = try await source.request(
        "/v1/watches/\(id)/\(action)", method: "POST",
        body: action == "instructions" ? Wire.data(["text": instruction]) : nil)
      let value: WatchDetail = try await source.get("/v1/watches/\(id)")
      guard generation == store.backendGeneration, !Task.isCancelled else { return }
      detail = value
      if action == "instructions" { instruction = "" }
      confirmingStop = false
      failure = nil
      await store.refresh()
    } catch {
      guard generation == store.backendGeneration, !Task.isCancelled else { return }
      failure = "This change could not be confirmed. Check the watch before trying again."
    }
  }
}

private struct SnapshotOpenWatchKey: EnvironmentKey { static let defaultValue: String? = nil }
extension EnvironmentValues {
  var snapshotOpenWatch: String? {
    get { self[SnapshotOpenWatchKey.self] }
    set { self[SnapshotOpenWatchKey.self] = newValue }
  }
}
