import LimmrKit
import SwiftUI

struct PaperCircleContents: View {
  @Bindable var store: AppStore
  let detail: CircleDetail
  var more: () -> Void
  @Environment(\.limmrWindowWidth) private var windowWidth
  @Environment(\.limmrWindowHeight) private var windowHeight
  @State private var selected: String?
  @State private var tab = "People"
  @State private var query = ""
  @State private var order = "Recent"
  @State private var staff = false
  @FocusState private var searchFocus: Bool
  private var members: [Person] {
    let ids = Set(detail.people.map(\.personId))
    let matches = store.people.filter { ids.contains($0.id) && (query.isEmpty || $0.name.localizedStandardContains(query) || $0.role.localizedStandardContains(query)) && (!staff || $0.role.localizedCaseInsensitiveContains("staff") || $0.name.hasPrefix("Prof.") || $0.name.hasPrefix("Dr.")) }
    return matches.sorted { a,b in
      switch order {
      case "Closest":
        let aw = store.circleMap.nodes.first { $0.id == a.id }.map { $0.closeness ?? $0.weight ?? 0 } ?? 0
        let bw = store.circleMap.nodes.first { $0.id == b.id }.map { $0.closeness ?? $0.weight ?? 0 } ?? 0
        return aw == bw ? a.id < b.id : aw > bw
      case "Role": return a.role == b.role ? a.name < b.name : a.role < b.role
      case "A–Z": return a.name.localizedStandardCompare(b.name) == .orderedAscending
      default: return store.paperLastSpoke(a) == store.paperLastSpoke(b) ? a.name < b.name : (store.paperLastSpoke(a) ?? "") > (store.paperLastSpoke(b) ?? "")
      }
    }
  }
  private var current: String? { selected ?? members.first(where: { store.paperPersonNeeds($0.id) })?.id ?? members.first?.id }
  private var groupIDs: [String] { detail.chats.map(\.chatJid).filter { $0.hasSuffix("@g.us") } }
  var body: some View {
    VStack(alignment: .leading, spacing: 12) {
      header
      if let brief = detail.brief {
        HStack(spacing: 10) {
          Text("Today").font(LimmrTheme.font(14, .semibold))
          Text(brief.summary).font(LimmrTheme.font(14)).lineLimit(2)
          Spacer(minLength: 4)
          Text(store.agentName(brief.writtenBy) + " · " + store.relativeTime(brief.writtenAt)).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3)
          Button("Full brief", action: more).buttonStyle(.plain).font(LimmrTheme.font(12, .semibold))
        }.padding(.horizontal, 14).padding(.vertical, 10).frame(minHeight: 40).n1Card()
      }
      let layout = PaperLayout.sideBySide(windowWidth) ? AnyLayout(HStackLayout(alignment: .top, spacing: 12)) : AnyLayout(VStackLayout(alignment: .leading, spacing: 12))
      layout {
        list.frame(width: PaperLayout.sideBySide(windowWidth) ? min(372, PaperLayout.contentWidth(windowWidth) * 0.38) : nil).frame(height: PaperLayout.circleListHeight(windowWidth: windowWidth, windowHeight: windowHeight))
        if let current { PaperPersonSelection(store: store, id: current).id(current) }
        else { QuietBlockState(title: "No selection", detail: "Choose a person to see your shared context.") }
      }
    }.task(id: detail.circle.id) {
      for task in store.allTasks.filter(\.needsYou).prefix(20) { await store.workspace(task.id).load() }
    }
    .onChange(of: store.searchRequest) { _, _ in searchFocus = true }
    .onChange(of: store.escapeRequest) { _, _ in query = ""; searchFocus = false }
    .onChange(of: store.rowRequest) { _, _ in move(store.rowStep) }
    .onChange(of: store.rowOpenRequest) { _, _ in if let current { store.openPeopleRoute(.person(current)) } }
  }
  private var header: some View {
    ViewThatFits(in: .horizontal) {
      HStack(spacing: 14) { identity; Spacer(minLength: 8); numbers; actions }
      VStack(alignment: .leading, spacing: 12) { HStack { identity; Spacer(); actions }; numbers }
    }.padding(.bottom, 2)
  }
  private var identity: some View {
    HStack(spacing: 14) {
      CirclePictureButton(store: store, circle: detail.circle, size: 44)
      VStack(alignment: .leading, spacing: 4) { Text(detail.circle.name).font(LimmrTheme.TypeScale.page).tracking(-0.48).lineLimit(2); Text(detail.circle.description).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2).lineLimit(1) }
    }
  }
  private var numbers: some View {
    HStack(spacing: 26) {
      PaperNumber(value: "\(detail.people.count)", label: "people")
      PaperNumber(value: "\(groupIDs.count)", label: "group chats")
      PaperNumber(value: "\(TaskListPresentation.active(TaskListPresentation.inCircle(store.allTasks, id: detail.circle.id)).count)", label: "open tasks")
    }
  }
  private var actions: some View {
    HStack(spacing: 12) {
      Button(detail.circle.pinned ? "Pinned" : "Pin", systemImage: "pin") { Task { await store.togglePin(detail.circle) } }.buttonStyle(PaperButtonStyle())
      Button(action: more) { Image(systemName: "ellipsis") }.buttonStyle(PaperButtonStyle()).accessibilityLabel("Circle details and controls")
    }
  }
  private var list: some View {
    VStack(alignment: .leading, spacing: 0) {
      ViewThatFits(in: .horizontal) {
      HStack(spacing: 8) {
        circleTabs; circleSearch
      }
      VStack(alignment: .leading, spacing: 8) { circleTabs; circleSearch }
      }.padding(10)
      PaperFlow(spacing: 6) {
        ForEach(["Recent", "Closest", "Role", "A–Z"], id: \.self) { value in
          Button(value) { order = value }.font(LimmrTheme.font(12, .medium)).buttonStyle(.plain).padding(.horizontal, 8).frame(height: 22)
            .foregroundStyle(order == value ? LimmrTheme.onPrimary : LimmrTheme.ink2).background(order == value ? LimmrTheme.ink : LimmrTheme.surface2, in: Capsule()).accessibilityAddTraits(order == value ? [.isSelected] : [])
        }
        Toggle("Staff", isOn: $staff).toggleStyle(.button).buttonStyle(.plain).font(LimmrTheme.font(12)).padding(.horizontal, 8).frame(height: 22).background(staff ? LimmrTheme.navOn : LimmrTheme.surface2, in: Capsule())
      }.padding(.horizontal, 10).padding(.bottom, 8)
      ScrollView {
        LazyVStack(alignment: .leading, spacing: 0) {
          if tab == "Groups" {
            ForEach(groupIDs.filter { query.isEmpty || store.chatName($0).localizedStandardContains(query) }, id: \.self) { jid in
              Button { store.openPeopleRoute(.group(jid)) } label: { Label(store.chatName(jid), systemImage: "person.2").font(LimmrTheme.font(14)).frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, 16).frame(minHeight: 48) }.quietCard()
            }
          } else {
            memberSection("Needs you", people: members.filter { store.paperPersonNeeds($0.id) })
            memberSection("Active this week", people: members.filter { !store.paperPersonNeeds($0.id) && active($0) })
            memberSection("Everyone else", people: members.filter { !store.paperPersonNeeds($0.id) && !active($0) })
          }
        }
      }
    }.n1Card().clipShape(RoundedRectangle(cornerRadius: 16))
      .onKeyPress(.downArrow) { move(1); return .handled }
      .onKeyPress(.upArrow) { move(-1); return .handled }
      .onKeyPress(.return) { guard let current else { return .ignored }; store.openPeopleRoute(.person(current)); return .handled }
  }
  private var circleTabs: some View {
        PaperSegments(values: ["People", "Groups"], selection: $tab, labels: ["People": "People  \(detail.people.count)", "Groups": "Groups  \(groupIDs.count)"])
  }
  private var circleSearch: some View {
    HStack(spacing: 6) { Image(systemName: "magnifyingglass").foregroundStyle(LimmrTheme.ink3); TextField("Search", text: $query).textFieldStyle(.plain).focused($searchFocus).accessibilityLabel("Search circle people and groups") }.font(LimmrTheme.font(14)).padding(.horizontal, 10).frame(minWidth: 100).frame(height: 28).background(LimmrTheme.panel, in: RoundedRectangle(cornerRadius: 12))
  }
  private func active(_ person: Person) -> Bool { store.paperLastSpoke(person).flatMap(Timestamp.date).map { store.presentationNow.timeIntervalSince($0) <= 7 * 86400 } ?? false }
  @ViewBuilder private func memberSection(_ title: String, people: [Person]) -> some View {
    if !people.isEmpty {
      HStack(spacing: 6) { Text(title); Text("\(people.count)").foregroundStyle(LimmrTheme.ink3) }.font(LimmrTheme.font(12, .semibold)).foregroundStyle(LimmrTheme.ink2).padding(.horizontal, 16).padding(.top, 8).padding(.bottom, 4)
      ForEach(people) { person in
        Button { selected = person.id } label: {
          HStack(spacing: 12) {
            PersonAvatar(store: store, id: person.id, name: person.name, size: 30)
            VStack(alignment: .leading, spacing: 4) { Text(store.personName(person.id)).font(LimmrTheme.font(14, .semibold)); Text(person.role).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2) }.lineLimit(2)
            Spacer(minLength: 0)
            if store.paperPersonNeeds(person.id) { PaperMarker() }
            if let at = store.paperLastSpoke(person) { Text(store.relativeTime(at)).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3) }
          }.padding(.horizontal, 16).frame(minHeight: 56).frame(maxWidth: .infinity, alignment: .leading).background(current == person.id ? LimmrTheme.surface2 : .clear).contentShape(Rectangle())
        }.quietCard().accessibilityLabel(person.name + ". " + person.role).accessibilityAddTraits(current == person.id ? [.isSelected] : [])
          .contextMenu { Button("Profile") { store.openPeopleRoute(.person(person.id)) }; Button("Open chat") { store.paperOpenChat(person) } }
      }
    }
  }
  private func move(_ step: Int) { var cursor = KeyboardSelection(id: current); cursor.move(step, in: members.map(\.id)); selected = cursor.id }
}
