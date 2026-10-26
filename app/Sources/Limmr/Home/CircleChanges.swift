import LimmrKit
import SwiftUI

struct CircleChangeRow: View {
  @Bindable var store: AppStore
  let change: CircleAutoChange
  let review: () -> Void
  var body: some View {
    HStack(spacing: 14) {
      VStack(alignment: .leading, spacing: 3) {
        Text(change.text).font(.callout)
        Text(store.relativeTime(change.at)).font(.caption).foregroundStyle(AccessibleSecondary())
      }
      Spacer(minLength: 8)
      Button("Review", action: review).limmrButton()
      Button("Undo all") { Task { await store.undoCircleChanges(change.changeIds) } }
        .limmrButton().disabled(store.busy)
    }
  }
}

struct CircleChangeReview: View {
  @Bindable var store: AppStore
  let change: CircleAutoChange
  @Environment(\.dismiss) private var dismiss
  @State private var circle: LimmrKit.Circle?
  @State private var placements: [String: String] = [:]
  @State private var loadError: String?
  var body: some View {
    VStack(alignment: .leading, spacing: 16) {
      HStack {
        Text("People in \(circle?.name ?? change.name)").font(.title2.bold())
        Spacer()
        Button("Done") { dismiss() }
      }
      Text(change.text).font(.callout).foregroundStyle(AccessibleSecondary())
      if let loadError {
        Text(loadError).foregroundStyle(AccessibleSecondary())
        Button("Try again") { Task { await load() } }
      } else if let circle {
        ScrollView {
          LazyVStack(spacing: 12) {
            ForEach(circle.people) { member in
              HStack {
                Button(store.personName(member.personId)) {
                  dismiss()
                  store.selectPage(.circles)
                  store.circlePath = [.circle(change.circleId), .person(member.personId)]
                }.limmrButton()
                Spacer()
                if member.source == "structure" {
                  Text("Placed by Limmr").font(.caption).foregroundStyle(AccessibleSecondary())
                }
                if let id = placements[member.personId] {
                  Button("Undo") {
                    Task {
                      await store.undoCircleChanges([id], single: true)
                      await load()
                    }
                  }.limmrButton().disabled(store.busy)
                }
              }
              Divider()
            }
          }
        }
      } else {
        ProgressView("Loading people…")
      }
      ActionFeedback(store: store, action: "circle-changes")
    }.padding(24).frame(width: 620).frame(minHeight: 300, idealHeight: 520, maxHeight: 520).task { await load() }
  }
  private func load() async {
    do {
      async let value: LimmrKit.Circle = store.backend.get("/v1/circles/\(change.circleId)")
      async let changes: CircleChangesResponse = store.backend.get("/v1/circles/changes")
      let (c, rows) = try await (value, changes)
      circle = c
      placements = [:]
      for row in rows.changes where row.circleId == change.circleId {
        for (person, id) in row.peopleChanges where placements[person] == nil {
          placements[person] = id
        }
      }
      loadError = nil
    } catch { loadError = "These people couldn’t load. Try again." }
  }
}

struct CircleChangesList: View {
  @Bindable var store: AppStore
  @Environment(\.dismiss) private var dismiss
  @State private var changes: [CircleAutoChange] = []
  @State private var reviewing: CircleAutoChange?
  @State private var loadError: String?
  var body: some View {
    VStack(alignment: .leading, spacing: 16) {
      HStack {
        Text("Circle changes").font(.title2.bold())
        Spacer()
        Button("Done") { dismiss() }
      }
      if let loadError {
        Text(loadError)
        Button("Try again") { Task { await load() } }
      }
      ScrollView {
        VStack(spacing: 16) {
          if changes.isEmpty, loadError == nil {
            ContentUnavailableView("No changes to review", systemImage: "person.2",
              description: Text("New circle placements will appear here."))
          }
          ForEach(changes) { change in
            CircleChangeRow(store: store, change: change) { reviewing = change }
          }
        }
      }
      ActionFeedback(store: store, action: "circle-changes")
    }.padding(24).frame(width: 680).frame(minHeight: 300, idealHeight: 520, maxHeight: 520)
      .task { await load() }
      .onChange(of: store.busy) { _, busy in
        if !busy { Task { await load() } }
      }
      .sheet(item: $reviewing) { CircleChangeReview(store: store, change: $0).dismissOnEscape() }
  }
  private func load() async {
    do {
      let response: CircleChangesResponse = try await store.backend.get("/v1/circles/changes")
      changes = response.changes
      loadError = nil
    } catch { loadError = "Circle changes couldn’t load. Try again." }
  }
}

extension AppStore {
  func undoCircleChanges(_ ids: [String], single: Bool = false) async {
    await perform(action: "circle-changes") { [self] in
      if single, let id = ids.first {
        try await backend.post("/v1/circle-changes/\(id)/undo")
      } else {
        try await backend.post("/v1/circles/changes/undo", ["change_ids": ids])
      }
      await refresh()
    }
  }
}
