import LimmrKit
import SwiftUI

struct HomeToolbarSearch: ViewModifier {
  @Bindable var store: AppStore
  @State private var query = ""
  @State private var hits: [HomeSearchHit] = []
  @State private var error: String?
  @State private var loading = false
  @FocusState private var focused: Bool
  func body(content: Content) -> some View {
    content.searchable(text: $query, placement: .toolbarPrincipal, prompt: "Search people, chats or work")
      .searchFocused($focused)
      .searchSuggestions {
        if loading { Text("Searching…") }
        if let error { Text(error) }
        ForEach(hits) { hit in
          Button { open(hit) } label: { Label(hit.title, systemImage: hit.symbol) }
            .accessibilityHint("Open " + hit.kind.rawValue)
        }
        if !loading && error == nil && hits.isEmpty && !query.isEmpty { Text("No matches") }
      }
      .task {
        // Cmd-K can request Home search before this screen has mounted.
        guard store.pendingSearchPage == .overview else { return }
        await Task.yield()
        focused = true; store.pendingSearchPage = nil
      }
      .onSubmit(of: .search) { if let hit = hits.first { open(hit) } }
      .onChange(of: store.searchRequest) { _, _ in focused = true; store.pendingSearchPage = nil }
      .onChange(of: store.escapeRequest) { _, _ in query = ""; focused = false }
      .task(id: "\(store.backendGeneration):\(query)") {
        hits = []; error = nil
        guard !query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { loading = false; return }
        let generation = store.backendGeneration
        loading = true
        do {
          try await Task.sleep(for: .milliseconds(250))
          let result = try await HomeSearch.find(query, backend: store.backend)
          guard !Task.isCancelled, generation == store.backendGeneration else { return }
          hits = result; loading = false
        } catch {
          guard !Task.isCancelled, generation == store.backendGeneration else { return }
          loading = false; self.error = "Search could not be loaded. Try again."
        }
      }
  }
  private func open(_ hit: HomeSearchHit) {
    query = ""; focused = false; store.openHomeSearchHit(hit)
  }
}

extension AppStore {
  func openHomeSearchHit(_ hit: HomeSearchHit) {
    switch hit.kind {
    case .effort: openEffort(hit.target)
    case .person:
      circlePath = []
      openPeopleRoute(.person(hit.target))
      selectedPerson = hit.target
    case .chat:
      circlePath = []
      openPeopleRoute(.chat(hit.target, hit.title))
    }
  }
  func openEffort(_ id: String) {
    openActivityRoute(.effort(id))
  }
}
