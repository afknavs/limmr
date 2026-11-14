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
      Text("Notes").font(LimmrTheme.TypeScale.section)
      VStack(alignment: .leading, spacing: 10) {
        TextField("Add a note for your AIs", text: $draft).textFieldStyle(.plain).font(LimmrTheme.font(14))
          .padding(.horizontal, 12).frame(height: 32).background(LimmrTheme.surface2, in: RoundedRectangle(cornerRadius: 12))
          .onSubmit { if !draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { save(text: draft) } }
          .accessibilityLabel("Add a note").accessibilityHint("Press Return to save a note for your AIs")
        ForEach(task.ownerNotes.sorted { $0.createdAt > $1.createdAt }) { note in
          VStack(alignment: .leading, spacing: 5) {
            if editing == note.id {
              TextField("Note", text: $editText, axis: .vertical).textFieldStyle(.plain).font(LimmrTheme.font(14)).onSubmit { save(id: note.id, text: editText) }
              HStack {
                Button("Save") { save(id: note.id, text: editText) }.buttonStyle(PaperButtonStyle(primary: true))
                Button("Cancel") { editing = nil }.buttonStyle(PaperButtonStyle())
              }
            } else { Text(note.text).font(LimmrTheme.font(14)).textSelection(.enabled) }
            Text(Timestamp.date(note.updatedAt)?.formatted(date: .abbreviated, time: .shortened) ?? "")
              .font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3)
          }.padding(12).frame(maxWidth: .infinity, alignment: .leading)
            .background(LimmrTheme.surface2, in: RoundedRectangle(cornerRadius: 12))
            .contextMenu {
              Button("Edit") { editing = note.id; editText = note.text }
              Button("Delete", role: .destructive) { save(id: note.id, text: "", remove: true) }
            }
        }
      }.padding(16).frame(maxWidth: .infinity, alignment: .leading).n1Card()
    }.foregroundStyle(LimmrTheme.ink)
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
