import LimmrKit
import SwiftUI

struct HomeSearchView: View {
  @Bindable var store: AppStore
  @State private var query = ""
  @State private var hits: [HomeSearchHit] = []
  @State private var error: String?
  @State private var loading = false
  @State private var presented = false
  @State private var retry = 0
  @State private var selection = KeyboardSelection()
  @FocusState private var resultFocus: String?
  @FocusState private var focused: Bool
  var body: some View {
    HStack(spacing: 6) {
      Image(systemName: "magnifyingglass").foregroundStyle(AccessibleSecondary())
      TextField("Search people, chats or work", text: $query)
        .textFieldStyle(.plain).font(.callout).focused($focused)
        .onSubmit { openSelection() }
        .accessibilityLabel("Search people, chats or work")
        .accessibilityHint("Use Up and Down to choose a result, then Return to open it")
        .onKeyPress(.downArrow) { move(1); return .handled }
        .onKeyPress(.upArrow) { move(-1); return .handled }
      if !query.isEmpty {
        Button { query = ""; presented = false } label: { Image(systemName: "xmark.circle.fill") }
          .limmrButton().foregroundStyle(AccessibleSecondary()).accessibilityLabel("Clear search")
      }
      Text("⌘F").font(.caption).foregroundStyle(AccessibleTertiary()).accessibilityHidden(true)
    }.padding(.horizontal, 10).frame(height: 36)
      .background(.primary.opacity(0.04), in: RoundedRectangle(cornerRadius: 10))
      .popover(isPresented: $presented, arrowEdge: .bottom) {
        VStack(alignment: .leading, spacing: 4) {
          if loading { ProgressView("Searching…").padding(12) }
          else if let error {
            Text(error).font(.callout).padding(8)
            Button("Try again") { retry += 1 }.padding(8)
          } else if hits.isEmpty {
            Text("No matches. Try a person, chat or topic.").font(.callout).foregroundStyle(AccessibleSecondary()).padding(12)
          } else {
            ForEach(hits) { hit in
              Button { open(hit) } label: {
                HStack(spacing: 10) {
                  Image(systemName: hit.symbol).foregroundStyle(AccessibleSecondary()).frame(width: 18)
                  Text(hit.title).lineLimit(1)
                  Spacer()
                  Text(hit.kind.rawValue).font(.caption).foregroundStyle(AccessibleSecondary())
                }.padding(9).contentShape(Rectangle())
              }.limmrButton(.row(radius: 8))
                .focused($resultFocus, equals: hit.id)
                .keyboardRow(hit.id, selection: selection.id)
                .accessibilityLabel(hit.title)
                .accessibilityValue(hit.kind.rawValue)
                .accessibilityHint("Open this result")
            }
          }
        }.padding(6).frame(width: 360)
          .environment(\.keyboardRowNavigation, true)
          .onKeyPress(.downArrow) { move(1); return .handled }
          .onKeyPress(.upArrow) { move(-1); return .handled }
          .onKeyPress(.return) { openSelection(); return .handled }
          .onExitCommand { clear() }
      }
      .onChange(of: query) { _, value in
        hits = []; error = nil; selection.id = nil
        loading = !value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        if !loading { presented = false }
      }
      .onChange(of: focused) { _, value in if value && !query.isEmpty { presented = true } }
      .onAppear { focusRequestedSearch() }
      .onChange(of: store.searchRequest) { _, _ in focusRequestedSearch() }
      .onChange(of: store.escapeRequest) { _, _ in clear() }
      .onChange(of: store.rowRequest) { _, _ in move(store.rowStep) }
      .onChange(of: store.rowOpenRequest) { _, _ in openSelection() }
      .onExitCommand { clear() }
      .task(id: "\(store.backendGeneration):\(query):\(retry)") { await search() }
  }
  private func focusRequestedSearch() {
    guard store.pendingSearchPage == .overview else { return }
    Task { @MainActor in
      await Task.yield()
      focused = true; store.pendingSearchPage = nil
    }
  }
  private func clear() { query = ""; presented = false; hits = []; selection.id = nil }
  private func move(_ step: Int) {
    selection.move(step, in: hits.map(\.id))
    if !hits.isEmpty { presented = true }
    resultFocus = selection.id
  }
  private func openSelection() {
    guard !loading, let hit = hits.first(where: { $0.id == selection.id }) ?? hits.first else { return }
    open(hit)
  }
  private func search() async {
    guard !query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return }
    let generation = store.backendGeneration
    let source = store.backend
    loading = true; error = nil
    do {
      try await Task.sleep(for: .milliseconds(250))
      let result = try await HomeSearch.find(query, backend: source)
      guard !Task.isCancelled, generation == store.backendGeneration else { return }
      hits = result; loading = false; presented = true
    } catch {
      guard !Task.isCancelled, generation == store.backendGeneration else { return }
      hits = []; loading = false; self.error = "Search could not be loaded."; presented = true
    }
  }
  private func open(_ hit: HomeSearchHit) {
    presented = false; focused = false
    store.openHomeSearchHit(hit)
  }
}

extension AppStore {
  func openHomeSearchHit(_ hit: HomeSearchHit) {
    switch hit.kind {
    case .effort: openEffort(hit.target)
    case .person:
      selectPage(.circles)
      selectedPerson = hit.target
      circlePath = [.person(hit.target)]
    case .chat:
      selectPage(.circles)
      circlePath = [.chat(hit.target, hit.title)]
    }
  }
  func openEffort(_ id: String) {
    openActivityRoute(.effort(id))
  }
}
