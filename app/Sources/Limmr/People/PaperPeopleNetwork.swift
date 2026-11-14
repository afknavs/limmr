import LimmrKit
import SwiftUI

struct PaperPeopleNetwork: View {
  @Bindable var store: AppStore
  var sortLoose: () -> Void
  @Environment(\.limmrWindowWidth) private var windowWidth
  @Environment(\.limmrWindowHeight) private var windowHeight
  nonisolated static func mapHeight(windowHeight: Double) -> Double { PaperLayout.mapHeight(windowHeight: windowHeight) }
  private var mapHeight: Double { Self.mapHeight(windowHeight:windowHeight) }
  @State private var scope = "Everyone"
  var query = ""
  private var map: CircleMap { store.circleMap }
  var body: some View {
    VStack(alignment: .leading, spacing: 12) {
      PaperFlow(spacing: 12) {
        PaperSegments(values: ["Everyone", "Close", "This week"], selection: $scope)
        let loose = map.nodes.filter { $0.kind == "person" && $0.id != "me" && UnderstandingLayout.memberships($0,map:map).isEmpty }.count
        if loose > 0 {
          Text("\(loose) people are not in a circle yet").font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2)
          Button("Sort them", action: sortLoose).buttonStyle(PaperButtonStyle())
        }
        Menu {
          if !store.archivedCircles.isEmpty { Button("Archived circles") { store.showArchivedCircles = true } }
          Text("\(store.circleMap.nodes.filter { $0.kind == "person" }.count) people shown")
          Text("\(store.circleMap.summary?.connections ?? store.circleMap.edges.count) recorded links")
          Text("\(NetworkMapMetrics.aiThisWeek(store.circleMap, now: store.presentationNow)) AI-found this week")
          if let summary = store.circleMap.summary { Text(summary.text) }
          if store.mapSelection.person != nil { Button("Clear highlight") { store.mapSelection = MapSelection() } }
        } label: { Image(systemName: "ellipsis").frame(width: 28, height: 28) }.menuStyle(.borderlessButton).fixedSize().accessibilityLabel("Map details and controls")
      }
      let layout = PaperLayout.sideBySide(windowWidth) ? AnyLayout(HStackLayout(alignment: .top, spacing: 16)) : AnyLayout(VStackLayout(alignment: .leading, spacing: 16))
      layout {
        VStack(alignment: .leading, spacing: 8) {
          PeopleNetworkReplay(store: store, map: map, paper: true, paperScope: scope, paperQuery: query) { node in
            store.mapSelection = MapSelection(person: node?.kind == "person" ? node?.id : nil, circle: node?.kind == "circle" ? node?.id : nil)
          }.frame(maxWidth:.infinity,maxHeight:.infinity)
          ViewThatFits(in:.horizontal) {
            HStack(spacing:10) { legend }
            VStack(alignment:.leading,spacing:4) { HStack(spacing:8) { Text("A line = you share a group chat"); Text("islands = your circles") }; HStack(spacing:8) { PaperMarker(); Text("needs you"); PaperDot(); Text("new") } }
          }.font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3)
        }.frame(maxWidth:.infinity).frame(height:mapHeight)
        ScrollView {
        Group {
          if let id = store.mapSelection.person { PaperMapPersonInspector(store:store,id:id).id(id) }
          else if let id = store.mapSelection.circle, let circle = store.circles.first(where: { $0.id == id }) { PaperMapCircleInspector(store:store,circle:circle) }
          else { PaperMapOverview(store:store,sortLoose:sortLoose) }
        }.frame(minHeight: PaperLayout.sideBySide(windowWidth) ? mapHeight : nil)
        }.frame(width: PaperLayout.inspectorWidth(windowWidth))
          .frame(height: PaperLayout.sideBySide(windowWidth) ? mapHeight : min(440, windowHeight * 0.55))
      }
    }.sheet(isPresented: $store.showArchivedCircles) { ArchivedCirclesSheet(store: store) }
      .onExitCommand { store.mapSelection = MapSelection() }
      .onChange(of: store.escapeRequest) { _, _ in store.mapSelection = MapSelection() }
      .task { for task in store.home.tasks.filter(\.needsYou).prefix(20) { await store.workspace(task.id).load() } }
  }
  @ViewBuilder private var legend: some View {
    Text("A line = you share a group chat"); Text("islands = your circles")
    HStack(spacing: 6) { PaperMarker(); Text("needs you") }; HStack(spacing: 6) { PaperDot(); Text("new") }
  }
}
private struct PaperMapPersonInspector: View {
  @Bindable var store: AppStore
  let id: String
  @State private var data = PaperPersonData()
  @ViewBuilder private var placements: some View { ForEach(store.paperCircles(id)) { circle in Button { store.openPeopleRoute(.circle(circle.id)) } label: { PaperChip(text: circle.name) }.buttonStyle(.plain) } }
  var body: some View {
    VStack(alignment: .leading, spacing: 12) {
      HStack(spacing: 12) {
        PersonAvatar(store: store, id: id, name: store.personName(id), size: 48)
        VStack(alignment: .leading, spacing: 4) { Text(data.profile?.person.name ?? store.circleMap.nodes.first { $0.id == id }?.title ?? store.personName(id)).font(LimmrTheme.font(17, .semibold)); Text(data.profile?.person.role ?? "").font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2) }
        Spacer(minLength: 0)
        Button { store.mapSelection = MapSelection() } label: { Image(systemName: "xmark") }.buttonStyle(.plain).accessibilityLabel("Close person panel")
      }
      PaperFlow(spacing: 6) { placements }
      if let stats = data.profile?.stats {
        HStack(spacing: 20) { if let at = stats.lastDm { PaperNumber(value: store.relativeTime(at), label: "last spoke") }; PaperNumber(value: stats.dmMessages30.formatted(), label: "messages this month") }
      }
      if let ask = data.tasks.map({ store.currentAttention($0) }).first(where: \.needsYou) {
        VStack(alignment: .leading, spacing: 12) {
          HStack(alignment: .firstTextBaseline, spacing: 8) { PaperMarker(); Text(ask.needsYouReason ?? "Needs you").font(LimmrTheme.font(14, .semibold)).fixedSize(horizontal: false, vertical: true) }
          HStack { Text(ask.title).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2); Spacer(minLength:4); Button("Review") { store.openTask(ask.id) }.buttonStyle(PaperButtonStyle(primary:true)) }
        }.padding(12).background(LimmrTheme.needBackground, in: RoundedRectangle(cornerRadius: 12))
      }
      let names = store.paperCircles(id).map(\.name).joined(separator:" and ")
      let shared = data.profile?.groups.count
      if !names.isEmpty { Text("In "+names+"."+(shared.map { " You share \($0) group chats." } ?? "")).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2).fixedSize(horizontal:false,vertical:true) }
      Spacer(minLength: 8)
      HStack { Button { if let person = data.profile?.person { store.paperOpenChat(person) } } label: { Text("Open chat").frame(maxWidth:.infinity) }.buttonStyle(PaperButtonStyle()); Button { store.openPeopleRoute(.person(id)) } label: { Text("Profile").frame(maxWidth:.infinity) }.buttonStyle(PaperButtonStyle()) }
      if let error = data.error { Text(error).font(LimmrTheme.font(12)) }
    }.padding(16).frame(maxWidth: .infinity, alignment: .leading).frame(maxHeight:.infinity, alignment:.topLeading).n1Card()
      .task(id: "\(store.backendGeneration):\(id)") { await data.load(id, store: store) }
  }
}

private struct PaperMapCircleInspector: View {
  @Bindable var store: AppStore
  let circle: LimmrKit.Circle
  private var members: [CircleMapNode] { store.circleMap.nodes.filter { ($0.circles ?? []).contains(circle.id) }.sorted { ($0.closeness ?? $0.weight ?? 0) > ($1.closeness ?? $1.weight ?? 0) } }
  var body: some View {
    VStack(alignment:.leading,spacing:12) {
      Image(systemName:circle.displaySymbol).font(.system(size:28)).foregroundStyle(LimmrTheme.ink2)
      Text(circle.name).font(LimmrTheme.font(17,.semibold))
      Text(circle.description).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2)
      HStack(spacing:16) {
        PaperNumber(value:"\(circle.people.count)",label:"people")
        PaperNumber(value:"\(circle.chats.filter { $0.chatJid.hasSuffix("@g.us") }.count)",label:"groups")
        PaperNumber(value:"\(TaskListPresentation.active(TaskListPresentation.inCircle(store.allTasks,id:circle.id)).count)",label:"open tasks")
      }
      HStack(spacing:-6) { ForEach(Array(members.prefix(5))) { node in PersonAvatar(store:store,id:node.id,name:node.title,size:30) } }
      Spacer(minLength:8)
      Button("Open circle") { store.openPeopleRoute(.circle(circle.id)) }.buttonStyle(PaperButtonStyle()).frame(maxWidth:.infinity)
    }.padding(16).frame(maxWidth:.infinity,maxHeight:.infinity,alignment:.topLeading).n1Card()
  }
}
private struct PaperMapOverview: View {
  @Bindable var store: AppStore
  var sortLoose: () -> Void
  private var people: [CircleMapNode] { store.circleMap.nodes.filter { $0.kind == "person" && $0.id != "me" } }
  var body: some View {
    VStack(alignment:.leading,spacing:12) {
      section("Needs you",values:people.filter { $0.needsYou == true || store.paperPersonNeeds($0.id) })
      section("New this week",values:people.filter { PaperMesh.newPeople(store.circleMap,now:store.presentationNow).contains($0.id) })
      Spacer(minLength:8)
      let loose = people.filter { UnderstandingLayout.memberships($0,map:store.circleMap).isEmpty }.count
      if loose > 0 { Text("\(loose) people are not in a circle yet").font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2); Button("Sort them",action:sortLoose).buttonStyle(PaperButtonStyle()) }
    }.padding(16).frame(maxWidth:.infinity,maxHeight:.infinity,alignment:.topLeading).n1Card()
  }
  private func section(_ title:String,values:[CircleMapNode]) -> some View {
    VStack(alignment:.leading,spacing:8) {
      Text(title).font(LimmrTheme.font(14,.semibold))
      if values.isEmpty { Text("None right now").font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3) }
      ForEach(values.prefix(3)) { node in
        Button { store.mapSelection = MapSelection(person:node.id) } label: {
          HStack(spacing:8) { PersonAvatar(store:store,id:node.id,name:node.title,size:26); Text(node.title).font(LimmrTheme.font(12,.medium)); Spacer(minLength:0) }
        }.buttonStyle(.plain)
      }
    }
  }
}
