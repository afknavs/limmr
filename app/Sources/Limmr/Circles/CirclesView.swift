import LimmrKit
import SwiftUI

struct CirclesView: View {
  @Bindable var store: AppStore
  @AppStorage("peopleTab") private var tab = "Circles"
  @State private var query = ""
  @State private var selection = KeyboardSelection()
  @State private var olderExpanded = false
  @FocusState private var searchFocused: Bool
  @FocusState private var rowFocused: String?
  private var currentTab: String { store.mapSelection.tab ?? snapshotTab ?? tab }
  private var rowIDs: [String] {
    if currentTab == "Circles" {
      return PeoplePresentation.circles(store.circles, query: query).filter {
        store.mapSelection.circle == nil || store.mapSelection.circle == $0.id
      }.map { "circle:" + $0.id }
    }
    let people = store.orderedPeople(query: query)
    return (people.filter { store.personIsCurrent($0) } + (olderExpanded ? people.filter { !store.personIsCurrent($0) } : [])).map { "person:" + $0.id }
  }
  private func move(_ step: Int) {
    guard store.circlePath.isEmpty else { return }
    selection.move(step, in: rowIDs); rowFocused = selection.id
  }
  private func openSelection() {
    guard store.circlePath.isEmpty, let id = selection.current(in: rowIDs) ?? rowIDs.first else { return }
    if id.hasPrefix("circle:") { store.circlePath.append(.circle(String(id.dropFirst(7)))) }
    else { store.circlePath.append(.person(String(id.dropFirst(7)))) }
  }
  @Environment(\.snapshotPeopleTab) private var snapshotTab
  @Environment(\.snapshotPeopleList) private var snapshotList
  @Environment(\.snapshotOlderPeople) private var snapshotOlder
  var body: some View {
    NavigationStack(path: $store.circlePath) {
      ScrollViewReader { proxy in
      ScrollView {
        LazyVStack(alignment: .leading, spacing: 24) {
          VStack(alignment: .leading, spacing: 6) {
            Text("People").font(AppType.pageTitle)
            Text("Your people, in the circles you share.").font(.callout).foregroundStyle(
              .secondary)
          }
          if store.isLoading("/v1/circles") || store.isLoading("/v1/people") {
            QuietLoadingCards()
          }
          PeopleWebHeader(store: store)
          HStack(spacing: 16) {
            Picker(
              "People view",
              selection: Binding(
                get: { store.mapSelection.tab ?? snapshotTab ?? tab },
                set: {
                  tab = $0
                  store.mapSelection = MapSelection()
                })
            ) {
              Text("Circles").tag("Circles")
              Text("People").tag("People")
              Text("Groups").tag("Groups")
            }.pickerStyle(.segmented).labelsHidden().frame(width: 240)
            Spacer(minLength: 0)
            HStack(spacing: 6) {
              Image(systemName: "magnifyingglass").foregroundStyle(AccessibleSecondary())
              TextField("Search", text: $query).textFieldStyle(.plain).accessibilityLabel(
                "Search circles and people")
                .focused($searchFocused)
                .accessibilityHint("Use arrow keys to choose a row, then Return to open it")
                .onSubmit { openSelection() }
            }.padding(8).frame(maxWidth: 240).background(
              .primary.opacity(0.035), in: RoundedRectangle(cornerRadius: 7))
          }
          .id("people-list-start")
          if store.mapSelection.tab != nil {
            Button("Clear map filter", systemImage: "xmark") { store.mapSelection = MapSelection() }
              .font(AppType.caption).limmrButton().foregroundStyle(AccessibleSecondary())
          }
          if (store.mapSelection.tab ?? snapshotTab ?? tab) == "Circles" {
            LazyVStack(spacing: 0) {
              CircleCountColumns.header
              if let proposal = store.home.proposals.first(where: {
                $0.circle.id == store.mapSelection.circle
              }) {
                CircleProposalCard(store: store, proposal: proposal).padding(.vertical, 16)
              }
              ForEach(
                PeoplePresentation.circles(store.circles, query: query).filter {
                  store.mapSelection.circle == nil || store.mapSelection.circle == $0.id
                }
              ) { circle in
                Button {
                  store.circlePath.append(.circle(circle.id))
                } label: {
                  HStack(alignment: .firstTextBaseline, spacing: 16) {
                    Image(systemName: circle.symbol).font(.title3).foregroundStyle(AccessibleSecondary())
                      .frame(width: 28)
                    VStack(alignment: .leading, spacing: 4) {
                      Text(circle.name).font(AppType.rowTitle).lineLimit(1).truncationMode(.tail)
                      Text(
                        circle.sendRule.title
                      ).font(AppType.caption).foregroundStyle(AccessibleSecondary())
                    }
                    Spacer(minLength: 0)
                    CircleCountColumns(pinned: circle.pinned, members: circle.people.count,
                      new: store.circleMap.pulses.first { $0.id == circle.id }?.newMessages ?? 0)
                    Image(systemName: "chevron.right").font(.caption2).foregroundStyle(AccessibleTertiary())
                  }.contentShape(Rectangle()).modifier(QuietRow())
                    .background(
                      store.mapSelection.circle == circle.id
                        ? Color.accentColor.opacity(0.06) : .clear,
                      in: RoundedRectangle(cornerRadius: 8))
                }.limmrButton(.row(radius: 8), id: "people-circle")
                  .focused($rowFocused, equals: "circle:" + circle.id)
                  .keyboardRow("circle:" + circle.id, selection: selection.id)
                  .accessibilityLabel(circle.name)
                  .accessibilityValue("\(Spoken.count(circle.people.count, "members")). \(Spoken.count(store.circleMap.pulses.first { $0.id == circle.id }?.newMessages ?? 0, "New")). \(circle.sendRule.title)")
                  .accessibilityHint("Open this circle")
                  .contextMenu {
                    Button(circle.pinned ? "Unpin" : "Pin") {
                      Task { await store.togglePin(circle) }
                    }
                  }
                Divider().padding(.leading, 50)
              }
              if store.hasLoaded("/v1/circles") && PeoplePresentation.circles(store.circles, query: query).isEmpty {
                Text(
                  query.isEmpty
                    ? "Keep a suggested circle on Home to get started."
                    : "No circles match your search."
                ).font(.callout).foregroundStyle(AccessibleSecondary()).padding(.vertical, 24)
              }
            }
          } else if (store.mapSelection.tab ?? snapshotTab ?? tab) == "Groups" {
            PeopleGroupsList(store: store, query: query)
          } else {
            LazyVStack(spacing: 0) {
              let matches = store.orderedPeople(query: query)
              ForEach(matches.filter { store.personIsCurrent($0) }) { person in
                personRow(person)
              }
              if store.peopleCursor?.isEmpty == false { Button("Load more people") { Task { await store.loadMorePeople() } } }
              let older = matches.filter { !store.personIsCurrent($0) }
              if !older.isEmpty {
                DisclosureGroup("People you haven't talked to recently (\(older.count))", isExpanded: $olderExpanded) {
                  ForEach(older) { person in personRow(person) }
                }.font(.callout).padding(.vertical, 16)
              }
              if store.hasLoaded("/v1/people") && PeoplePresentation.people(store.people, circles: store.circles, query: query)
                .isEmpty
              {
                Text(
                  query.isEmpty
                    ? "People appear after WhatsApp syncs." : "No people match your search."
                ).font(.callout).foregroundStyle(AccessibleSecondary()).padding(.vertical, 24)
              }
            }
          }
        }.frame(maxWidth: 760).padding(24).frame(maxWidth: .infinity)
      }
      .defaultScrollAnchor(snapshotOlder ? .bottom : .top, for: .initialOffset)
      .task(id: snapshotList) {
        if snapshotList {
          try? await Task.sleep(for: .milliseconds(250))
          proxy.scrollTo("people-list-start", anchor: .top)
        }
      }
      .onChange(of: selection.id) { _, id in if let id { proxy.scrollTo(id, anchor: .center) } }
      }
      .pageSurface()
      .navigationDestination(for: CircleDestination.self) { destination in
        Group {
          switch destination {
          case .circle(let id): CirclePage(store: store, id: id)
          case .person(let id): CirclePersonPage(store: store, id: id)
          case .group(let jid): GroupPage(store: store, jid: jid)
          case .chat(let id, let name):
            if id.hasSuffix("@g.us") { GroupPage(store: store, jid: id) }
            else { HomeChatPage(store: store, id: id, name: name) }
          case .brief(let id, let version):
            BriefVersionPage(store: store, circleID: id, version: version)
          }
        }
        .pageSurface()
      }
    }
    .onChange(of: store.mapSelection) { _, selection in
      query = ""
      if let chosenTab = selection.tab { tab = chosenTab }
    }
    .environment(\.keyboardRowNavigation, true)
    .onChange(of: rowFocused) { _, id in if let id { selection.id = id } }
    .onChange(of: rowIDs) { _, ids in if selection.current(in: ids) == nil { selection.id = nil } }
    .onChange(of: store.searchRequest) { _, _ in Task { @MainActor in await Task.yield(); searchFocused = true; store.pendingSearchPage = nil } }
    .onChange(of: store.escapeRequest) { _, _ in query = ""; selection.id = nil }
    .onChange(of: store.rowRequest) { _, _ in move(store.rowStep) }
    .onChange(of: store.rowOpenRequest) { _, _ in openSelection() }
    .onKeyPress(.downArrow) { guard store.circlePath.isEmpty else { return .ignored }; move(1); return .handled }
    .onKeyPress(.upArrow) { guard store.circlePath.isEmpty else { return .ignored }; move(-1); return .handled }
    .onKeyPress(.return) { guard store.circlePath.isEmpty, rowFocused != nil else { return .ignored }; openSelection(); return .handled }
    .onExitCommand { query = ""; selection.id = nil; store.route(.escape) }
    .task(id: query) { await store.searchPeople(query) }
    .task {
      for circle in store.circles where store.circleDetails[circle.id] == nil {
        await store.loadCircle(circle.id)
      }
    }
  }
  private func personRow(_ person: Person) -> some View {
    LazyVStack(spacing: 0) {
      Button {
        store.circlePath.append(.person(person.id))
      } label: {
        HStack(spacing: 16) {
          PersonAvatar(store: store, id: person.id, name: person.name)
          VStack(alignment: .leading, spacing: 4) {
            HStack { Text(person.label?.isEmpty == false ? person.label! : person.name).font(AppType.rowTitle).lineLimit(1).truncationMode(.tail); if person.id == "me" { Badge(text: "You") } }
            Text(
              PeoplePresentation.roles(
                person.id, circles: store.circles, name: person.name)
            ).font(
              .caption
            ).foregroundStyle(AccessibleSecondary()).lineLimit(2)
          }
          Spacer(minLength: 0)
          if let at = store.chats.filter({ person.jids.contains($0.jid) }).compactMap(
            \.lastMessageTime
          ).max() {
            Text(store.relativeTime(at)).font(AppType.caption).foregroundStyle(AccessibleSecondary())
          }
          Image(systemName: "chevron.right").font(.caption2).foregroundStyle(AccessibleTertiary())
        }.contentShape(Rectangle()).modifier(QuietRow())
      }.limmrButton(.row(radius: 8), id: "people-person")
        .focused($rowFocused, equals: "person:" + person.id)
        .keyboardRow("person:" + person.id, selection: selection.id)
        .accessibilityLabel(person.label?.isEmpty == false ? person.label! : person.name)
        .accessibilityValue(PeoplePresentation.roles(person.id, circles: store.circles, name: person.name))
        .accessibilityHint("Open this person's profile")
      Divider().padding(.leading, 54)
    }
  }
}
private struct SnapshotPeopleTabKey: EnvironmentKey { static let defaultValue: String? = nil }
extension EnvironmentValues {
  var snapshotPeopleTab: String? {
    get { self[SnapshotPeopleTabKey.self] }
    set { self[SnapshotPeopleTabKey.self] = newValue }
  }
}

private struct SnapshotOlderPeopleKey: EnvironmentKey { static let defaultValue = false }
extension EnvironmentValues {
  var snapshotOlderPeople: Bool {
    get { self[SnapshotOlderPeopleKey.self] }
    set { self[SnapshotOlderPeopleKey.self] = newValue }
  }
}

private struct SnapshotPeopleListKey: EnvironmentKey { static let defaultValue = false }
extension EnvironmentValues {
  var snapshotPeopleList: Bool {
    get { self[SnapshotPeopleListKey.self] }
    set { self[SnapshotPeopleListKey.self] = newValue }
  }
}
