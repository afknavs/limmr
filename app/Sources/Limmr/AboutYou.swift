import LimmrKit
import SwiftUI

/// Shared by Settings and People > You. Only onboarding may offer seed editing.
struct AboutYouCard: View {
  @Bindable var store: AppStore
  var compact = false
  @Environment(\.openWindow) private var openWindow
  @State private var loading = true

  var body: some View {
    if compact {
      OwnerSeedCard(store: store, compact: true)
    } else {
      VStack(alignment: .leading, spacing: 16) {
        Text("About you").font(AppType.blockHeading)
        Text("Your AIs’ understanding of you and the contexts in your life.")
          .font(.callout).foregroundStyle(AccessibleSecondary())
        if let brief = store.aboutYouBrief {
          Text(brief.summary).font(.body).textSelection(.enabled)
            .fixedSize(horizontal: false, vertical: true)
          if !brief.whatChanged.isEmpty {
            Text(brief.whatChanged).font(.callout).foregroundStyle(AccessibleSecondary())
              .fixedSize(horizontal: false, vertical: true)
          }
          Text("Last updated by \(store.agentName(brief.writtenBy)) · \(store.relativeTime(brief.writtenAt))")
            .font(AppType.caption).foregroundStyle(AccessibleSecondary())
        } else if loading {
          ProgressView("Loading your brief…")
        } else {
          Text("Your AIs haven’t written a You brief yet.").foregroundStyle(AccessibleSecondary())
        }
        if !store.aboutYouContexts.isEmpty {
          Divider()
          Text("Your contexts").font(AppType.blockHeading)
          ForEach(store.aboutYouContexts) { circle in
            VStack(alignment: .leading, spacing: 4) {
              Label(circle.name, systemImage: circle.symbol).font(AppType.rowTitle).lineLimit(1).truncationMode(.tail)
              if !circle.description.isEmpty {
                Text(circle.description).font(.callout).foregroundStyle(AccessibleSecondary())
              }
            }
          }
        }
        if let error = store.circleError {
          Text(error).font(AppType.caption).foregroundStyle(AccessibleSecondary())
          Button("Try again") { Task { await load() } }
        }
        Button("Open in People", systemImage: "person.crop.circle") {
          store.navigate(URL(string: "limmr://people/me")!)
          openWindow(id: "main")
          NSApp.activate(ignoringOtherApps: true)
        }.limmrButton().foregroundStyle(Color.accentColor)
      }
      .frame(maxWidth: .infinity, alignment: .leading)
      .padding(16).background(.primary.opacity(0.025), in: RoundedRectangle(cornerRadius: 12))
      .multilineTextAlignment(.leading)
      .task(id: "\(store.backendGeneration)-\(store.aboutYouCircle?.id ?? "")") { await load() }
    }
  }
  private func load() async {
    loading = true
    if let id = store.aboutYouCircle?.id { await store.loadCircle(id) }
    loading = false
  }
}

extension AppStore {
  var aboutYouCircle: LimmrKit.Circle? { circles.first { $0.kind == "self" } }
  var aboutYouBrief: CircleBrief? {
    guard let id = aboutYouCircle?.id else { return nil }
    let candidates = [circleDetails[id]?.brief, home.today.first { $0.circle.id == id }?.brief]
      .compactMap { $0 }
    return candidates.max { $0.version < $1.version }
  }
  /// Contexts are derived from the same circle map agents maintain, never the seed form.
  var aboutYouContexts: [LimmrKit.Circle] {
    circles.filter { $0.kind != "self" && $0.confirmed }
      .sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending }
  }
}

private struct OwnerSeedCard: View {
  @Bindable var store: AppStore
  var compact = false
  @State private var draft = OwnerProfile()
  @State private var baseline = OwnerProfile()
  @State private var loaded = false
  var body: some View {
    VStack(alignment: .leading, spacing: compact ? 10 : 14) {
      Text("Optional seed text").font(.title2.bold())
      Text(
        compact
          ? "Optional · give your AIs a starting point for understanding your life."
          : "A little context helps your AIs choose circles that fit your life now. Add only what you want to share."
      ).font(.callout).foregroundStyle(AccessibleSecondary())
      TextField("Name", text: $draft.name).textFieldStyle(.roundedBorder)
      TextField("Studies · current school or university and course", text: $draft.studies)
        .textFieldStyle(.roundedBorder)
      TextField("Work · current role", text: $draft.work).textFieldStyle(.roundedBorder)
      VStack(alignment: .leading, spacing: 8) {
        Text("People close to you").font(.headline)
        ForEach(draft.people.indices, id: \.self) { i in
          HStack {
            TextField("Name", text: $draft.people[i].name)
            TextField("Relation", text: $draft.people[i].relation)
            Button("Remove", systemImage: "minus.circle") { draft.people.remove(at: i) }.labelStyle(
              .iconOnly)
          }
        }
        Button("Add a person", systemImage: "plus") {
          draft.people.append(.init(name: "", relation: ""))
        }
      }.textFieldStyle(.roundedBorder)
      VStack(alignment: .leading, spacing: 8) {
        Text("Active projects").font(.headline)
        ForEach(draft.projects.indices, id: \.self) { i in
          HStack {
            TextField("Project", text: $draft.projects[i].name)
            TextField(
              "With · names, separated by commas",
              text: Binding(
                get: { draft.projects[i].with.joined(separator: ", ") },
                set: {
                  draft.projects[i].with = $0.split(separator: ",").map {
                    $0.trimmingCharacters(in: .whitespaces)
                  }
                }))
            Button("Remove", systemImage: "minus.circle") { draft.projects.remove(at: i) }
              .labelStyle(.iconOnly)
          }
        }
        Button("Add a project", systemImage: "plus") { draft.projects.append(.init(name: "")) }
      }.textFieldStyle(.roundedBorder)
      TextField("Anything else · up to 600 characters", text: $draft.about, axis: .vertical)
        .lineLimit(2...4).textFieldStyle(.roundedBorder)
        .onChange(of: draft.about) { _, text in
          if text.count > 600 { draft.about = String(text.prefix(600)) }
        }
      HStack {
        Button("Save") { Task { await save() } }.buttonStyle(
          NativeGlassButtonStyle(prominent: true)
        ).disabled(store.busy || !loaded || draft == baseline)
        Text("Optional · your words stay yours.").font(.caption).foregroundStyle(AccessibleSecondary())
      }
      if !compact && !store.ownerProfile.suggestions.isEmpty {
        Divider()
        Text("Suggested by your AI").font(.headline)
        ForEach(store.ownerProfile.suggestions) { suggestion in
          VStack(alignment: .leading, spacing: 6) {
            Text(suggestion.field.capitalized + " · " + suggestion.value.display).font(.callout)
            HStack {
              Text(store.agentName(suggestion.source)).font(.caption).foregroundStyle(AccessibleSecondary())
              Spacer()
              Button("Confirm") { Task { await review(suggestion.id, true) } }
              Button("Dismiss") { Task { await review(suggestion.id, false) } }
            }
          }
        }
      }
      ActionFeedback(store: store, action: "owner")
    }.padding(18).background(.primary.opacity(0.025), in: RoundedRectangle(cornerRadius: 12))
      .multilineTextAlignment(.leading)
      .task { await load() }
  }
  private func load() async {
    let generation = store.backendGeneration
    await store.perform(action: "owner") {
      let p: OwnerProfile = try await store.backend.get("/v1/owner")
      guard generation == store.backendGeneration else { return }
      store.ownerProfile = p
      if !p.name.isEmpty { store.settings.owner = p.name }
      draft = p
      baseline = p
      loaded = true
    }
  }
  private func save() async {
    let generation = store.backendGeneration
    // Send only changed fields. An edit elsewhere must not erase an unrelated field.
    var fields: [String: ProfileValue] = [:]
    if draft.name != baseline.name { fields["name"] = .text(draft.name) }
    if draft.studies != baseline.studies { fields["studies"] = .text(draft.studies) }
    if draft.work != baseline.work { fields["work"] = .text(draft.work) }
    if draft.about != baseline.about { fields["about"] = .text(draft.about) }
    if draft.people != baseline.people { fields["people"] = .people(draft.people) }
    if draft.projects != baseline.projects { fields["projects"] = .projects(draft.projects) }
    await store.perform(action: "owner") {
      let data = try await store.backend.request(
        "/v1/owner", method: "PUT", body: Wire.data(fields))
      guard generation == store.backendGeneration else { return }
      let p = try Wire.decode(OwnerProfile.self, from: data)
      store.ownerProfile = p
      if !p.name.isEmpty { store.settings.owner = p.name }
      draft = p
      baseline = p
      store.notice = "About you saved."
    }
  }
  private func review(_ id: String, _ confirm: Bool) async {
    await store.perform(action: "owner") {
      try await store.backend.post("/v1/owner/suggestions/\(id)/\(confirm ? "confirm" : "dismiss")")
    }
    await load()
  }
}

struct AIPresetChoice: View {
  @Binding var preset: String
  var body: some View {
    VStack(alignment: .leading, spacing: 10) {
      Picker("This connection is", selection: $preset) {
        Text("Your own AI").tag("own")
        Text("An AI you were given").tag("given")
      }.pickerStyle(.segmented)
      Text(
        preset == "own"
          ? "Full access, including old chats. Sending follows each circle’s rule and the integration’s permission to send."
          : "Choose circles and an optional end date. Starts with read-only access."
      ).font(.caption).foregroundStyle(AccessibleSecondary())
    }
  }
}
