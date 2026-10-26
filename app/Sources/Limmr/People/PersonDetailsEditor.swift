import LimmrKit
import SwiftUI

struct PersonDetailsEditor: View {
  @Bindable var store: AppStore
  let person: Person
  @Environment(\.dismiss) private var dismiss
  @State private var details: PersonDetails
  @State private var name: String
  @State private var description: String
  @State private var otherNames: String
  @State private var saving = false
  @State private var error: String?
  init(store: AppStore, person: Person) {
    self.store = store; self.person = person
    _details = State(initialValue: person.details ?? .init())
    _name = State(initialValue: person.label?.isEmpty == false ? person.label! : person.name)
    _description = State(initialValue: person.howKnown)
    _otherNames = State(initialValue: person.otherNames.joined(separator: "\n"))
  }
  var body: some View {
    VStack(alignment: .leading, spacing: 16) {
      Text("Edit details").font(AppType.blockHeading)
      ScrollView {
        Form {
          TextField("Name", text: $name)
          TextField("Description", text: $description)
          TextField("Other names, one per line", text: $otherNames, axis: .vertical).lineLimit(2...4)
          values("Phone", values: $details.phones)
          values("Email", values: $details.emails)
          values("Handle", values: $details.handles)
          TextField("Birthday (MM-DD or YYYY-MM-DD)", text: $details.birthday)
          TextField("Location", text: $details.location)
          TextField("Organisation", text: $details.org)
          TextField("Job title", text: $details.title)
          TextField("Pronouns", text: $details.pronouns)
        }.textFieldStyle(.roundedBorder).padding(.trailing, 4)
      }
      if let error { Text(error).font(AppType.caption).foregroundStyle(AccessibleSecondary()) }
      HStack {
        Button("Cancel") { dismiss() }.keyboardShortcut(.cancelAction)
        Spacer()
        Button(saving ? "Saving…" : "Save") { Task { await save() } }.keyboardShortcut(.defaultAction)
          .disabled(saving || name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
      }
    }.padding(24).frame(width: 560, height: 640).disabled(saving)
  }
  private func values(_ title: String, values: Binding<[LabeledValue]>) -> some View {
    LabeledContent(title) {
      VStack(alignment: .leading, spacing: 8) {
        ForEach(values.wrappedValue.indices, id: \.self) { index in
          HStack(spacing: 8) {
            TextField("\(title) label", text: values[index].label).labelsHidden().frame(width: 100)
            TextField(title, text: Binding(get: { values.wrappedValue[index].value }, set: { value in
              values.wrappedValue[index].value = value
              values.wrappedValue[index].source = "you"
            })).labelsHidden()
            Button { values.wrappedValue.remove(at: index) } label: { Image(systemName: "minus.circle") }
              .limmrButton().accessibilityLabel("Remove \(title.lowercased()) \(index + 1)")
          }
        }
        Button("Add \(title.lowercased())", systemImage: "plus") {
          values.wrappedValue.append(.init(label: title == "Handle" ? "social" : "personal", value: ""))
        }.limmrButton().font(AppType.caption)
      }
    }
  }
  private func save() async {
    saving = true; defer { saving = false }
    struct Patch: Encodable {
      var label: String; var howKnown: String; var otherNames: [String]; var details: PersonDetails
    }
    var value = details
    value.phones.removeAll { $0.source == "whatsapp" || $0.value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }
    value.emails.removeAll { $0.value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }
    value.handles.removeAll { $0.value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }
    do {
      let patch = Patch(label: name.trimmingCharacters(in: .whitespacesAndNewlines), howKnown: description,
        otherNames: otherNames.split(separator: "\n").map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }.filter { !$0.isEmpty }, details: value)
      _ = try await store.backend.request("/v1/people/\(person.id)", method: "PATCH", body: Wire.data(patch))
      await store.refresh(); await store.loadPerson(); dismiss()
    } catch { self.error = "Couldn’t save these details. Please try again." }
  }
}
