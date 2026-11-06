import Foundation
import LimmrKit
import SwiftUI

struct TaskNotesWidget: View {
  @Bindable var model: TaskWorkspace
  let task: TrackedTask
  @State private var draft = ""
  @State private var editing: String?
  @State private var editText = ""
  var body: some View {
    VStack(alignment: .leading, spacing: 10) {
      Text("Notes").font(AppType.blockHeading).foregroundStyle(AccessibleSecondary())
      TextField("Add a note", text: $draft).textFieldStyle(.plain).font(.callout)
        .padding(10).background(.primary.opacity(0.035), in: RoundedRectangle(cornerRadius: 7))
        .onSubmit { if !draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { save(text: draft) } }
        .accessibilityHint("Press Return to save a note for your AIs")
      ForEach(task.ownerNotes.sorted { $0.createdAt > $1.createdAt }) { note in
        VStack(alignment: .leading, spacing: 5) {
          if editing == note.id {
            TextField("Note", text: $editText, axis: .vertical).textFieldStyle(.plain).onSubmit { save(id: note.id, text: editText) }
            HStack {
              Button("Save") { save(id: note.id, text: editText) }.buttonStyle(.bordered).controlSize(.small)
              Button("Cancel") { editing = nil }.buttonStyle(.bordered).controlSize(.small)
            }
          } else { Text(note.text).font(.callout).textSelection(.enabled) }
          Text(Timestamp.date(note.updatedAt)?.formatted(date: .abbreviated, time: .shortened) ?? "")
            .font(AppType.caption).foregroundStyle(AccessibleSecondary())
        }.padding(10).frame(maxWidth: .infinity, alignment: .leading)
          .background(Color.yellow.opacity(0.07), in: RoundedRectangle(cornerRadius: 7))
          .contextMenu {
            Button("Edit") { editing = note.id; editText = note.text }
            Button("Delete", role: .destructive) { save(id: note.id, text: "", remove: true) }
          }
      }
    }.padding(16).frame(maxWidth: .infinity, alignment: .leading).modifier(HomeTileSurface())
  }
  private func save(id: String = "", text: String, remove: Bool = false) {
    struct Write: Encodable, Sendable { var id: String; var text: String; var remove: Bool }
    Task {
      await model.change {
        let data = try await model.client.backend.request(TaskClient.path(task.id) + "/notes", method: "POST", body: Wire.data(Write(id: id, text: text, remove: remove)))
        return try Wire.decode(TrackedTask.self, from: data)
      }
      if model.error == nil { if id.isEmpty { draft = "" }; editing = nil }
    }
  }
}
