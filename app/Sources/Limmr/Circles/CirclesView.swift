import LimmrKit
import SwiftUI

struct CirclesView: View {
  @Bindable var store: AppStore
  @AppStorage("peopleTab") private var tab = "Circles"
  @State private var query = ""
  @State private var onlySelected = false
  @State private var circleFilter = "all"
  @State private var peopleSort = "Recent"
  @Environment(\.limmrWindowWidth) private var windowWidth
  private var browsingPeople: [Person] {
    let values = store.orderedPeople(query: query, includeUnselected: !onlySelected).filter { person in
      circleFilter == "all" || (circleFilter == "none" ? store.paperCircles(person.id).isEmpty : store.paperCircles(person.id).contains { $0.id == circleFilter })
    }
    if peopleSort == "Closest" {
      return values.sorted { a,b in (store.circleMap.nodes.first { $0.id == a.id }?.weight ?? 0) > (store.circleMap.nodes.first { $0.id == b.id }?.weight ?? 0) }
    }
    return values
  }
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
    let people = browsingPeople
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
    let _ = SpeedTrace.body("People")
    Group {
      ScrollViewReader { proxy in
      ScrollView {
        LazyVStack(alignment: .leading, spacing: 12) {
          PaperPageHeader(title: "People", subtitle: "\(store.circleMap.nodes.filter { $0.kind == "person" && $0.id != "me" }.count) people · \(store.circles.filter { $0.kind != "self" }.count) \(store.circles.filter { $0.kind != "self" }.count == 1 ? "circle" : "circles") · \(PaperMesh.newPeople(store.circleMap,now:store.presentationNow).count) new this week") {
            PaperSearch(store: store, query: $query, prompt: "Find someone").frame(width: windowWidth >= 1200 ? 300 : 220).onSubmit { if let match = browsingPeople.first { store.mapSelection = MapSelection(person:match.id) } }
          }
          if store.isLoading("/v1/circles") || store.isLoading("/v1/people") {
            QuietLoadingCards()
          }
          PaperPeopleNetwork(store: store, sortLoose: { circleFilter = "none"; tab = "People"; store.mapSelection = MapSelection() }, query: query)
          HStack(spacing: 16) {
            PaperSegments(values: ["Circles", "People", "Groups"], selection: Binding(
              get: { store.mapSelection.tab ?? snapshotTab ?? tab },
              set: { tab = $0; store.mapSelection = MapSelection() }))

            Spacer(minLength: 0)
            Menu {
              Picker("Sort people", selection: $peopleSort) { Text("Recent").tag("Recent"); Text("Closest").tag("Closest") }
              Toggle("Only the selected person", isOn: $onlySelected).disabled(store.mapSelection.person == nil)
            } label: { Label(peopleSort, systemImage: "arrow.up.arrow.down") }.menuStyle(.borderlessButton).fixedSize()
            Menu {
              Picker("Circle filter", selection: $circleFilter) { Text("All circles").tag("all"); Text("Not in a circle").tag("none"); ForEach(store.circles) { Text($0.name).tag($0.id) } }
            } label: { Label(circleFilter == "all" ? "All circles" : circleFilter == "none" ? "Not in a circle" : store.circleName(circleFilter), systemImage: "line.3.horizontal.decrease") }.menuStyle(.borderlessButton).fixedSize()

          }
          .id("people-list-start")
          if store.mapSelection.circle != nil || onlySelected {
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
                    CirclePicture(circle: circle, size: 28)
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
                        ? Color.primary.opacity(0.035) : .clear,
                      in: RoundedRectangle(cornerRadius: 8))
                }.quietCard()
                  .focused($rowFocused, equals: "circle:" + circle.id)
                  .quietKeyboardRow("circle:" + circle.id, selection: selection.id)
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
              let matches = browsingPeople
              LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 20), count: PaperLayout.peopleColumns(windowWidth)), spacing: 0) {
                ForEach(matches.filter { store.personIsCurrent($0) }) { person in personRow(person) }
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
        }.padding(.horizontal, 28).padding(.top, 24).padding(.bottom, 20).frame(maxWidth: .infinity).paperReadingWidth()
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

    }
    .onChange(of: store.mapSelection) { _, selection in
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
    Button { store.mapSelection = MapSelection(person:person.id) } label: {
      HStack(spacing:12) {
        PersonAvatar(store:store,id:person.id,name:person.name,size:30)
        VStack(alignment:.leading,spacing:3) {
          Text(person.label?.isEmpty == false ? person.label! : person.name).font(LimmrTheme.font(14,.semibold)).lineLimit(2)
          Text(store.paperCircles(person.id).map(\.name).joined(separator:" · ")).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2).lineLimit(1)
        }
        Spacer(minLength:0)
        if let at = store.paperLastSpoke(person) { Text(store.relativeTime(at)).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3) }
      }.frame(minHeight:60).contentShape(Rectangle()).overlay(alignment:.top) { Rectangle().fill(LimmrTheme.hairline).frame(height:1) }
    }.buttonStyle(.plain).focused($rowFocused,equals:"person:"+person.id)
      .quietKeyboardRow("person:"+person.id,selection:selection.id)
      .contextMenu { Button("Profile") { store.openPeopleRoute(.person(person.id)) } }
      .accessibilityLabel(person.label?.isEmpty == false ? person.label! : person.name)
      .accessibilityValue(store.paperCircles(person.id).map(\.name).joined(separator:" and ")+(store.paperPersonNeeds(person.id) ? ", needs you" : ""))
      .accessibilityHint("Select on the map; Return opens the profile")
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
