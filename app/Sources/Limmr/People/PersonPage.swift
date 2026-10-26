import SwiftUI
import UniformTypeIdentifiers
import LimmrKit

struct CirclePersonPage: View {
  @Bindable var store: AppStore
  let id: String
  var snapshotMenu = false
  @State private var forget = false
  @State private var editing = false
  @State private var accessConfirmation = false
  private var profile: PersonDetail? { store.profile?.person.id == id ? store.profile : nil }
  private var person: Person? { profile?.person ?? store.people.first { $0.id == id } }
  private var circles: [LimmrKit.Circle] {
    store.circles.filter { (store.circleDetails[$0.id]?.people ?? $0.people).contains { $0.personId == id } }
  }
  private var hidden: Bool {
    guard let person else { return false }
    return store.hiddenChatsBinding(for: store.directChatJIDs(for: person)).wrappedValue
  }
  private var otherNames: [String] {
    guard let person else { return [] }
    var seen = Set([store.personName(id)])
    return (person.otherNames + [person.name, person.pushName ?? ""]).filter { !$0.isEmpty && seen.insert($0).inserted }
  }
  var body: some View {
    ScrollView {
      LazyVStack(alignment: .leading, spacing: 24) {
        Button("Back", systemImage: "chevron.left") { store.leavePeopleRoute() }.limmrButton()
        if let person {
          header(person).zIndex(snapshotMenu ? 1 : 0)
          PersonLately(store: store, id: id, profile: profile)
          if id == "me" { AboutYouCard(store: store) }
          suggestions(person)
          if let profile { learned(profile.facts.filter { $0.status != "suggested" }) }
          if !circles.isEmpty {
            VStack(alignment: .leading, spacing: 12) {
              BlockHeading(title: "Across your circles")
              ForEach(circles) { circle in
                Button { store.circlePath.append(.circle(circle.id)) } label: {
                  HStack(spacing: 12) {
                    Image(systemName: circle.symbol)
                    VStack(alignment: .leading, spacing: 4) {
                      Text(circle.name).font(AppType.rowTitle)
                      if let member = (store.circleDetails[circle.id]?.people ?? circle.people).first(where: { $0.personId == id }), !member.role.isEmpty {
                        Text(member.role).font(AppType.caption).foregroundStyle(AccessibleSecondary())
                      }
                    }
                    Spacer()
                    Image(systemName: "chevron.right").font(AppType.caption).foregroundStyle(AccessibleSecondary())
                  }.modifier(QuietRow()).contentShape(Rectangle())
                }.limmrButton(.row(radius: 8, bordered: false)).accessibilityLabel("Open \(circle.name)")
              }
            }
          }
          DisclosureGroup("Notes for your AIs") {
            ProfileEntry(title: "", value: person.notes, placeholder: "Add a note for your AIs", source: "from you") { _, value in
              Task { await patch(["notes": value]) }
            }.padding(.top, 8)
          }.font(.callout)
        } else { ProgressView("Loading profile…") }
        ActionFeedback(store: store, action: "profile")
      }.frame(maxWidth: 760).padding(24).frame(maxWidth: .infinity)
    }.navigationBarBackButtonHidden(true).background { AppBackground() }
      .task(id: id) {
        store.selectedPerson = id
        await store.loadPerson()
        if id != "me", let person { await store.loadDirectChat(for: person) }
        for circle in store.circles where store.circleDetails[circle.id] == nil { await store.loadCircle(circle.id) }
      }
      .fileImporter(isPresented: $photoPicker, allowedContentTypes: [.jpeg, .png, .heic]) { result in
        if case .success(let url) = result { Task { await importPhoto(url) } }
      }
      .sheet(isPresented: $editing) {
        if let person { PersonDetailsEditor(store: store, person: person).dismissOnEscape() }
      }
      .sheet(isPresented: $accessConfirmation) {
        VStack(alignment: .leading, spacing: 20) {
          Text(hidden ? "Show to your AIs?" : "Hide from your AIs?").font(AppType.blockHeading)
          Text("When hidden, your AIs can’t read your chat with \(store.personName(id)). They still see what \(store.personName(id)) says in groups you haven’t hidden.").font(.callout)
          HStack {
            Button("Cancel") { accessConfirmation = false }.keyboardShortcut(.cancelAction)
            Spacer()
            Button(hidden ? "Show to your AIs" : "Hide from your AIs") {
              if let person { store.hiddenChatsBinding(for: store.directChatJIDs(for: person)).wrappedValue = !hidden }
              accessConfirmation = false
            }.keyboardShortcut(.defaultAction)
          }
        }.padding(24).frame(width: 440)
      }
      .sheet(isPresented: $forget) { ForgetPersonPrompt(store: store, id: id) { forget = false }.dismissOnEscape() }
  }
  private func header(_ person: Person) -> some View {
    VStack(alignment: .leading, spacing: 18) {
      HStack(alignment: .top, spacing: 16) {
        PersonAvatar(store: store, id: id, name: store.personName(id), size: 72)
        VStack(alignment: .leading, spacing: 6) {
          Text(store.personName(id)).font(AppType.pageTitle).lineLimit(2)
          if !otherNames.isEmpty { Text("Also called " + otherNames.joined(separator: ", ")).font(AppType.caption).foregroundStyle(AccessibleSecondary()) }
          if !person.howKnown.isEmpty { Text(person.howKnown).font(.callout).foregroundStyle(AccessibleSecondary()) }
          if hidden {
            Button { accessConfirmation = true } label: { Label("Hidden from your AIs", systemImage: "eye.slash") }
              .limmrButton().font(AppType.caption).foregroundStyle(AccessibleSecondary())
          }
          if !circles.isEmpty {
            ViewThatFits {
              HStack(spacing: 6) { ForEach(circles) { circle in
                Button(circle.name) { store.circlePath.append(.circle(circle.id)) }.limmrButton().font(AppType.caption)
                  .padding(.horizontal, 8).padding(.vertical, 4).background(.primary.opacity(0.05), in: Capsule())
              } }
              Text(circles.map(\.name).joined(separator: " · ")).font(AppType.caption).foregroundStyle(AccessibleSecondary())
            }
          }
        }
        Spacer(minLength: 8)
        Menu { menuItems(person) } label: { Image(systemName: "ellipsis").frame(width: 28, height: 28) }
          .menuStyle(.borderlessButton).fixedSize().accessibilityLabel("More options for \(store.personName(id))")
      }
      let fields = ProfileDetailsPresentation.rows(person.details ?? .init())
      if !fields.isEmpty {
        Divider()
        Grid(alignment: .leading, horizontalSpacing: 24, verticalSpacing: 10) {
          ForEach(Array(fields.enumerated()), id: \.offset) { _, field in
            GridRow(alignment: .firstTextBaseline) {
              Text(field.title).font(.callout).foregroundStyle(AccessibleSecondary()).frame(width: 92, alignment: .leading)
              VStack(alignment: .leading, spacing: 2) {
                Text(field.value).font(.callout).textSelection(.enabled)
                Text(source(field.source)).font(AppType.caption).foregroundStyle(AccessibleSecondary())
              }.frame(maxWidth: .infinity, alignment: .leading)
            }
          }
        }
      }
    }.padding(20).modifier(HomeTileSurface())
      .overlay(alignment: .topTrailing) {
        if snapshotMenu {
          VStack(alignment: .leading, spacing: 12) { menuItems(person) }.limmrButton().font(.callout)
            .padding(16).frame(width: 220, alignment: .leading).background(.regularMaterial, in: RoundedRectangle(cornerRadius: 10))
            .shadow(color: .black.opacity(0.12), radius: 12, y: 4).offset(x: -20, y: 48)
        }
      }
      .dropDestination(for: URL.self) { urls, _ in
        guard let url = urls.first else { return false }; Task { await importPhoto(url) }; return true
      }
  }
  @ViewBuilder private func menuItems(_ person: Person) -> some View {
    Button("Edit details…") { editing = true }
    Button("Change photo…") { photoPicker = true }
    if person.photo?.source == "custom" { Button("Remove photo") { Task { await changePhoto(nil) } } }
    Button("Message history") {
      if let jid = person.jids.first { store.circlePath.append(.chat(jid, store.personName(id))) }
    }.disabled(person.jids.isEmpty)
    if id != "me" {
      Button(hidden ? "Show to your AIs…" : "Hide from your AIs…") { accessConfirmation = true }
        .disabled(store.directChatJIDs(for: person).isEmpty)
      Divider()
      Button("Forget this person…", role: .destructive) { forget = true }
    }
  }
  @ViewBuilder private func suggestions(_ person: Person) -> some View {
    let details = (person.detailSuggestions ?? []).filter { $0.status == "suggested" }
    let facts = (profile?.facts ?? []).filter { $0.status == "suggested" }
    if !details.isEmpty || !facts.isEmpty || person.inferredRelationship != nil {
      VStack(alignment: .leading, spacing: 12) {
        BlockHeading(title: "Suggestions")
        ForEach(details) { suggestion in
          SuggestionLine {
            Text(suggestion.value).font(.callout)
            Text("\(suggestion.field.capitalized) · \(store.agentName(suggestion.source))").font(AppType.caption).foregroundStyle(AccessibleSecondary())
          } confirm: { review(suggestion.id, confirm: true) } dismiss: { review(suggestion.id, confirm: false) }
        }
        ForEach(facts) { fact in
          SuggestionLine { Text(fact.text).font(.callout) } confirm: { factAction(fact.id, confirm: true) } dismiss: { factAction(fact.id, confirm: false) }
        }
        if let inference = person.inferredRelationship {
          SuggestionLine {
            Text(inference.howKnown).font(.callout)
            Text("How you know them · This Mac").font(AppType.caption).foregroundStyle(AccessibleSecondary())
          } confirm: { Task { await inferenceAction("confirm") } } dismiss: { Task { await inferenceAction("dismiss") } }
        }
      }
    }
  }
  @ViewBuilder private func learned(_ facts: [Fact]) -> some View {
    if !facts.isEmpty {
      VStack(alignment: .leading, spacing: 12) {
        BlockHeading(title: "What Limmr learned")
        ForEach(facts.sorted { $0.createdAt > $1.createdAt }) { fact in
          VStack(alignment: .leading, spacing: 4) {
            Text(fact.text).font(.callout)
            Text(factSource(fact)).font(AppType.caption).foregroundStyle(AccessibleSecondary())
          }
        }
      }
    }
  }
  @State private var photoPicker = false
  private func importPhoto(_ url: URL) async {
    let access = url.startAccessingSecurityScopedResource(); defer { if access { url.stopAccessingSecurityScopedResource() } }
    await store.perform(action: "profile") {
      let size = try url.resourceValues(forKeys: [.fileSizeKey]).fileSize ?? 0
      guard size > 0, size <= 4 * 1024 * 1024 else { throw BackendError.unavailable("Choose a photo smaller than 4 MiB.") }
      try await uploadPhoto(Data(contentsOf: url))
    }
  }
  private func uploadPhoto(_ data: Data?) async throws {
    _ = try await store.backend.request("/v1/people/\(id)/photo", method: data == nil ? "DELETE" : "PUT", body: data)
    store.avatarData[id] = nil; store.avatarAttempts.remove(id); await store.loadAvatar(id)
  }
  private func changePhoto(_ data: Data?) async { await store.perform(action: "profile") { try await uploadPhoto(data) } }

  private func source(_ id: String) -> String {
    switch id { case "you": return "from you"; case "contacts": return "from Contacts"; case "whatsapp": return "from WhatsApp"; case "gmail": return "from Gmail · sample"; default: return "linked by \(store.agentName(id))" }
  }
  private func saveDetails(_ details: PersonDetails) {
    var value = details
    value.phones.removeAll { $0.source == "whatsapp" }
    struct Patch: Encodable { var details: PersonDetails }
    Task {
      await store.perform(action: "profile") {
        _ = try await store.backend.request("/v1/people/\(id)", method: "PATCH", body: Wire.data(Patch(details: value)))
        await store.refresh(); await store.loadPerson()
      }
    }
  }
  private func patch(_ fields: [String: String]) async {
    await store.perform(action: "profile") {
      _ = try await store.backend.request("/v1/people/\(id)", method: "PATCH", body: Wire.data(fields))
      await store.refresh(); await store.loadPerson()
    }
  }
  private func inferenceAction(_ action: String) async {
    await store.perform(action: "profile") {
      try await store.backend.post("/v1/people/\(id)/inference/\(action)")
      await store.refresh(); await store.loadPerson()
    }
  }
  private func review(_ suggestion: String, confirm: Bool) {
    Task {
      await store.perform(action: "profile") {
        try await store.backend.post("/v1/people/\(id)/suggestions/\(suggestion)/\(confirm ? "confirm" : "dismiss")")
        await store.refresh(); await store.loadPerson()
      }
    }
  }
  private func factSource(_ fact: Fact) -> String {
    if fact.sourceAgent == "gmail" { return "from Gmail · sample" }
    if fact.sourceAgent == "you" { return "confirmed by you" }
    if let chat = fact.evidenceChatJids.first {
      return "from \(store.chatName(chat)) · \(Timestamp.date(fact.createdAt)?.formatted(.dateTime.day().month(.abbreviated)) ?? "")"
    }
    return "\(fact.status == "suggested" ? "suggested" : "linked") by \(store.agentName(fact.sourceAgent))"
  }
  private func factAction(_ id: String, confirm: Bool) {
    Task {
      await store.perform(action: "profile") {
        try await store.backend.post("/v1/facts/\(id)/\(confirm ? "confirm" : "revert")")
        await store.refresh(); await store.loadPerson()
      }
    }
  }
}
struct ProfileEntry: View {
  var title: String
  var value: String
  var label = ""
  var placeholder: String
  var source: String
  var save: (String, String) -> Void
  @State private var editing = false
  @State private var draft = ""
  @State private var draftLabel = ""
  var body: some View {
    HStack(alignment: .firstTextBaseline, spacing: 16) {
      if !title.isEmpty { Text(title).font(.callout).foregroundStyle(AccessibleSecondary()).frame(width: 72, alignment: .leading) }
      if editing {
        if !label.isEmpty { TextField("Label", text: $draftLabel).frame(width: 76) }
        TextField(title == "Birthday" ? "MM-DD or YYYY-MM-DD" : placeholder, text: $draft).onSubmit(commit)
        Button("Save", action: commit).limmrButton()
        Button("Cancel") { editing = false }.limmrButton().foregroundStyle(AccessibleSecondary())
      } else {
        Button {
          draft = value; draftLabel = label; editing = true
        } label: {
          HStack {
            Text(value.isEmpty ? placeholder : value).foregroundStyle(value.isEmpty ? .secondary : .primary)
              .lineLimit(1).help(value)
            if !label.isEmpty && !value.isEmpty { Text(label).font(AppType.caption).foregroundStyle(AccessibleTertiary()) }
            Spacer(minLength: 0)
          }.contentShape(Rectangle())
        }.limmrButton()
        if !source.isEmpty { Text(source).font(AppType.caption).foregroundStyle(AccessibleSecondary()) }
      }
    }.font(.callout).padding(.vertical, 2)
  }
  private func commit() { save(draftLabel, draft.trimmingCharacters(in: .whitespacesAndNewlines)); editing = false }
}
struct SuggestionLine<Content: View>: View {
  var suggested = true
  @ViewBuilder var content: () -> Content
  var confirm: () -> Void
  var dismiss: () -> Void
  var body: some View {
    VStack(alignment: .leading, spacing: 6) {
      content()
      if suggested {
        HStack(spacing: 16) {
          Button("Confirm", action: confirm)
          Button("Dismiss", action: dismiss).foregroundStyle(AccessibleSecondary())
        }.limmrButton().font(AppType.caption)

      }
    }
      .accessibilityActions {
        if suggested {
          Button("Confirm suggestion", action: confirm)
          Button("Dismiss suggestion", action: dismiss)
        }
      }
  }
}
struct AddProfileValue: View {
  let title: String
  var save: (String, String) -> Void
  @State private var label = "personal"
  @State private var value = ""
  @Environment(\.dismiss) private var dismiss
  var body: some View {
    VStack(alignment: .leading, spacing: 16) {
      Text("Add \(title.lowercased())").font(.title2.bold())
      TextField("Label", text: $label)
      TextField(title, text: $value)
      HStack { Spacer(); Button("Cancel") { dismiss() }; Button("Add") { save(label, value) }.disabled(value.trimmingCharacters(in: .whitespaces).isEmpty) }
    }.padding(24).frame(width: 400)
  }
}

struct ForgetPersonPrompt: View {
  @Bindable var store: AppStore
  let id: String
  var close: () -> Void
  var body: some View {
    VStack(alignment: .leading, spacing: 24) {
      Text("Forget \(store.personName(id))?").font(.title2.bold())
      Text("This removes their profile, facts, relations and circle memberships. Chats stay in WhatsApp.")
        .fixedSize(horizontal: false, vertical: true)
      HStack {
        Spacer()
        Button("Cancel", action: close).keyboardShortcut(.cancelAction)
        Button("Forget this person", role: .destructive) {
          Task {
            await store.perform(action: "profile") {
              _ = try await store.backend.request("/v1/people/\(id)", method: "DELETE", body: nil)
              close(); store.circlePath = []; store.profile = nil
              await store.refresh()
            }
          }
        }.disabled(store.busy)
      }
      ActionFeedback(store: store, action: "profile")
    }.padding(24).frame(width: 430)
  }
}

private struct SnapshotProfileReviewKey: EnvironmentKey { static let defaultValue = false }
extension EnvironmentValues {
  var snapshotProfileReview: Bool {
    get { self[SnapshotProfileReviewKey.self] }
    set { self[SnapshotProfileReviewKey.self] = newValue }
  }
}
