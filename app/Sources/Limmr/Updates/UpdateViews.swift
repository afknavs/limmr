import LimmrKit
import SwiftUI

struct UpdateSettingsRow: View {
  @Bindable var store: AppStore
  @State private var sample = UpdateController(sample: true)
  private var updates: UpdateController { store.demo || store.snapshot ? sample : .shared }
  var body: some View {
    @Bindable var updates = updates
    VStack(alignment: .leading, spacing: 10) {
      HStack {
        Picker("Updates", selection: $updates.mode) {
          ForEach(UpdateMode.allCases, id: \.rawValue) { Text($0.title).tag($0) }
        }.disabled(!store.snapshot && (!updates.configured || store.demo))
        Button("Check now") { Task { await updates.check() } }
          .disabled(updates.busy || store.demo)
      }
      if !updates.configured && !store.snapshot {
        Text("Development build — install updates manually.").font(.caption).foregroundStyle(AccessibleSecondary())
      }
      if let feed = updates.available {
        HStack {
          Text("Limmr \(feed.version) is ready").font(.callout)
          Spacer()
          Button("What’s new") { updates.showingNotes = true }
        }
      }
      if let message = updates.message { Text(message).font(.caption).foregroundStyle(AccessibleSecondary()) }
    }
    .sheet(isPresented: $updates.showingNotes) { UpdateNotesSheet(store: store, updates: updates).dismissOnEscape() }
  }
}

struct HomeUpdateLine: View {
  @Bindable var store: AppStore
  @State private var sample = UpdateController(sample: true)
  private var updates: UpdateController {
    store.demo || store.snapshot ? sample : .shared
  }
  var body: some View {
    @Bindable var updates = updates
    if let feed = updates.available,
       (!store.demo && !store.snapshot) || (store.snapshot && ProcessInfo.processInfo.arguments.contains("--update-preview")) {
      HStack(spacing: 4) {
        Text("Limmr \(feed.version) is ready ·")
        Button(updates.workspace != nil ? "Restart to update" : "Review update") {
          updates.showingNotes = true
        }.limmrButton().underline()
      }.font(.callout).foregroundStyle(AccessibleSecondary())
      .sheet(isPresented: $updates.showingNotes) { UpdateNotesSheet(store: store, updates: updates).dismissOnEscape() }
    }
  }
}

struct UpdateNotesSheet: View {
  @Bindable var store: AppStore
  @Bindable var updates: UpdateController
  @Environment(\.dismiss) private var dismiss
  var body: some View {
    VStack(alignment: .leading, spacing: 20) {
      Text("What’s new in Limmr \(updates.available?.version ?? "")").font(.title2.weight(.semibold))
      ScrollView {
        Text((try? AttributedString(markdown: updates.available?.notesMarkdown ?? "")) ?? AttributedString(""))
          .frame(maxWidth: .infinity, alignment: .leading).textSelection(.enabled)
      }
      if let message = updates.message { Text(message).font(.callout).foregroundStyle(AccessibleSecondary()) }
      HStack {
        Text("Limmr restarts after you choose to update.").font(.caption).foregroundStyle(AccessibleSecondary())
        Spacer()
        Button("Later") { dismiss() }
        if updates.workspace == nil {
          Button("Download update") { Task { await updates.download() } }
            .disabled(updates.busy || store.demo || store.snapshot)
        } else {
          Button("Restart to update") { Task { await updates.restart(store: store) } }
            .disabled(updates.busy || store.busy || !store.pending.isEmpty || !store.home.queued.isEmpty)
        }
      }
    }.padding(28).frame(width: 560, height: 360)
  }
}
