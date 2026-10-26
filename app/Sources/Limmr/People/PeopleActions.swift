import AppKit
import SwiftUI
import LimmrKit
import UniformTypeIdentifiers

struct PeopleActions: View {
  @Bindable var store: AppStore
  @State private var fill = false
  var body: some View {
    VStack(alignment: .leading, spacing: 16) {
      VStack(alignment: .leading, spacing: 4) {
        Button("Export people…") { Task { await export() } }
          .settingsMatch("Export people…", store: store)
        Text("Save your people and their details as a JSON file.")
          .font(.footnote).foregroundStyle(AccessibleSecondary())
      }
      VStack(alignment: .leading, spacing: 4) {
        Button("Fill in from your AI…") { fill = true }
          .settingsMatch("Fill in from your AI…", store: store)
        Text("Ask your connected AI to suggest details for you to confirm.")
          .font(.footnote).foregroundStyle(AccessibleSecondary())
      }
      ActionFeedback(store: store, action: "people-export")
    }
      .sheet(isPresented: $fill) { FillFromAI() }
  }
  private func export() async {
    await store.perform(action: "people-export") {
      let data = try await store.backend.request("/v1/people/export", method: "GET", body: nil)
      let panel = NSSavePanel()
      panel.title = "Export people"
      panel.nameFieldStringValue = "Limmr People.json"
      panel.allowedContentTypes = [.json]
      guard await panel.begin() == .OK, let url = panel.url else { return }
      try data.write(to: url, options: .atomic)
      store.notice = "People exported."
    }
  }
}
struct FillFromAI: View {
  @Environment(\.dismiss) private var dismiss
  static let prompt = """
  Help me fill in my Limmr people from what you already know. Call whoami first. Use find_person and get_person to match existing people; ask me if a match is ambiguous. Propose only details you actually know, and tell me where each came from. Never invent a phone number, address, birthday or relationship.

  Use update_person with fields.details for phones, emails, birthday (MM-DD or YYYY-MM-DD), location, handles and pronouns. Phones, emails and handles are arrays of {label, value}. All profile details arrive as suggestions for me to confirm. For other knowledge, use add_fact with suggested:true and evidence_message_ids when available. Do not change existing notes or confirmed values. Do not send messages, reactions, typing indicators or read receipts.
  """
  var body: some View {
    VStack(alignment: .leading, spacing: 20) {
      HStack { Text("Fill in from your AI").font(.title2.bold()); Badge(text: "New") }
      Text("Ask ChatGPT, Claude or Codex to add what they already know about your people. Everything arrives as a suggestion you confirm.")
        .fixedSize(horizontal: false, vertical: true)
      CopyField(title: "Copy this prompt to your connected AI", value: Self.prompt)
      HStack { Spacer(); Button("Done") { dismiss() }.keyboardShortcut(.cancelAction) }
    }.padding(28).frame(width: 540)
  }
}
