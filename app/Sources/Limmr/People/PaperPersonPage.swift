import LimmrKit
import SwiftUI

struct PaperProfileHero<MenuContent: View>: View {
  @Bindable var store: AppStore
  let person: Person
  var showMap: () -> Void
  @ViewBuilder var menu: MenuContent
  var body: some View {
    ViewThatFits(in: .horizontal) {
      HStack(alignment: .top, spacing: 18) { identity; Spacer(minLength: 4); actions }
      VStack(alignment: .leading, spacing: 16) { identity; actions }
    }.padding(.vertical, 16).padding(.horizontal, 20).frame(maxWidth: .infinity, alignment: .leading).n1Card()
  }
  private var identity: some View {
    HStack(spacing: 18) {
      PersonAvatar(store: store, id: person.id, name: store.personName(person.id), size: 84)
      VStack(alignment: .leading, spacing: 10) {
        Text(store.personName(person.id)).font(LimmrTheme.TypeScale.page).tracking(-0.48).lineLimit(2)
        if !person.howKnown.isEmpty { Text(person.howKnown).font(LimmrTheme.font(14)).foregroundStyle(LimmrTheme.ink2).fixedSize(horizontal: false, vertical: true).frame(maxWidth: 520, alignment: .leading) }
        PaperFlow(spacing: 8) { placements }
      }.frame(maxWidth: .infinity, alignment: .leading)
    }
  }
  @ViewBuilder private var placements: some View {
    ForEach(store.paperCircles(person.id)) { circle in
      Button { store.openPeopleRoute(.circle(circle.id)) } label: { PaperChip(text: circle.name) }.buttonStyle(.plain)
    }
    if store.hiddenChatsBinding(for: store.directChatJIDs(for: person)).wrappedValue { PaperChip(text: "Hidden from your AIs") }
    if let phone = person.details?.phones.first { PaperChip(text: phone.value) }
  }
  private var actions: some View {
    HStack(spacing: 8) {
      Button("Show in map", systemImage: "point.3.connected.trianglepath.dotted", action: showMap).buttonStyle(PaperButtonStyle())
      Button("Open chat") { store.paperOpenChat(person) }.buttonStyle(PaperButtonStyle()).disabled(person.jids.isEmpty)
      Menu { menu } label: { Image(systemName: "ellipsis").frame(width: 28, height: 28) }.menuStyle(.borderlessButton).fixedSize().padding(.horizontal, 6).background(LimmrTheme.surface2, in: RoundedRectangle(cornerRadius: 12)).accessibilityLabel("More options for " + person.name)
    }
  }
}
struct PaperPersonBody: View {
  @Bindable var store: AppStore
  let person: Person
  var saveNotes: (String) -> Void
  @Environment(\.limmrWindowWidth) private var windowWidth
  @Environment(\.limmrWindowHeight) private var windowHeight
  @State private var data = PaperPersonData()
  var body: some View {
    VStack(alignment: .leading, spacing: 12) {
      if let stats = data.profile?.stats {
        ViewThatFits(in: .horizontal) {
          HStack(spacing: 28) { numbers(stats); Spacer(minLength: 0); if let history = data.history, history.weeks.contains(where: { $0.messages > 0 }) { PaperConversationChart(history: history).frame(width: 230) } }
          VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 24) { numbers(stats) }
            if let history = data.history, history.weeks.contains(where: { $0.messages > 0 }) { PaperConversationChart(history: history).frame(maxWidth: 300) }
          }
          VStack(alignment: .leading, spacing: 16) { LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], alignment: .leading, spacing: 16) { numbers(stats) }; if let history = data.history, history.weeks.contains(where: { $0.messages > 0 }) { PaperConversationChart(history: history) } }
        }.padding(.vertical, 16).padding(.horizontal, 20).frame(maxWidth: .infinity, alignment: .leading).n1Card()
      }
      let layout = PaperLayout.sideBySide(windowWidth) ? AnyLayout(HStackLayout(alignment: .top, spacing: 12)) : AnyLayout(VStackLayout(alignment: .leading, spacing: 12))
      layout {
        VStack(alignment: .leading, spacing: 12) {
          if let ask = data.tasks.map({ store.currentAttention($0) }).first(where: { $0.needsYou || store.taskLaterNotices[$0.id] != nil }) { PaperPersonAsk(store: store, task: ask) }
            if let summary = data.summary {
            VStack(alignment: .leading, spacing: 10) {
              HStack { section("Lately"); Spacer(); Button("Refresh") { Task { await data.load(person.id, store: store) } }.buttonStyle(.plain).font(LimmrTheme.font(12)) }
              Text(summary.text).font(LimmrTheme.font(14)).fixedSize(horizontal: false, vertical: true)
              Text("Summary by " + store.agentName(summary.writtenBy) + " · " + store.relativeTime(summary.writtenAt)).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3)
            }.padding(16).n1Card()
          }
          if !data.messages.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
              HStack { section("Recent conversation"); Spacer(); Button("Open chat") { store.paperOpenChat(person) }.buttonStyle(.plain).font(LimmrTheme.font(12)) }
              ScrollView {
              VStack(alignment: .leading, spacing: 8) {
              ForEach(data.messages) { message in
                HStack(alignment: .top, spacing: 10) {
                  PersonAvatar(store: store, id: message.isFromMe ? "me" : person.id, name: message.isFromMe ? "You" : person.name, size: 26)
                  VStack(alignment: .leading, spacing: 4) {
                    HStack(spacing: 4) { Text(message.isFromMe ? "You" : person.name).font(LimmrTheme.font(14, .semibold)); Text("· " + store.relativeTime(message.timestamp)).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3) }
                    Text(message.text).font(LimmrTheme.font(14)).fixedSize(horizontal: false, vertical: true).textSelection(.enabled)
                  }
                }.padding(.vertical, 8)
                if message.id != data.messages.last?.id { Divider().overlay(LimmrTheme.hairline) }
              }
              }.frame(maxWidth: .infinity, alignment: .leading)
              }.frame(height: PaperLayout.conversationHeight(windowHeight))
            }.padding(16).frame(maxWidth: .infinity, alignment: .leading).n1Card()
          }
        }.frame(maxWidth: .infinity, alignment: .topLeading)
        VStack(alignment: .leading, spacing: 12) {
          if !data.tasks.isEmpty { PaperPersonWork(store: store, tasks: data.tasks).padding(16).n1Card() }
          if let groups = data.profile?.groups, !groups.isEmpty { PaperPersonGroups(store: store, groups: groups).padding(16).n1Card() }
          PaperPersonNotes(notes: person.notes, save: saveNotes)
        }.frame(width: PaperLayout.sideBySide(windowWidth) ? PaperLayout.inspectorWidth(windowWidth) : nil).frame(maxWidth: PaperLayout.sideBySide(windowWidth) ? nil : .infinity, alignment: .leading)
      }
      if let error = data.error { Text(error).font(LimmrTheme.font(12)); Button("Try again") { Task { await data.load(person.id, store: store) } }.buttonStyle(PaperButtonStyle()) }
    }.task(id: "\(store.backendGeneration):\(person.id)") { await data.load(person.id, store: store) }
  }
  private func numbers(_ stats: PersonStats) -> some View {
    Group {
      if let at = stats.lastDm { PaperNumber(value: store.relativeTime(at), label: "last spoke") }
      PaperNumber(value: stats.dmMessages30.formatted(), label: "messages this month")
      PaperNumber(value: stats.sharedGroups.count.formatted(), label: "groups in common")
      PaperNumber(value: data.tasks.filter { !["done", "dropped", "dismissed"].contains($0.status) }.count.formatted(), label: "tasks together")
    }
  }
  private func section(_ text: String) -> some View { Text(text).font(LimmrTheme.font(12, .semibold)).foregroundStyle(LimmrTheme.ink3) }
}
private struct PaperPersonNotes: View {
  let notes: String
  var save: (String) -> Void
  @State private var editing = false
  @State private var draft = ""
  var body: some View {
    VStack(alignment: .leading, spacing: 10) {
      HStack { Text("Notes for your AIs").font(LimmrTheme.font(12, .semibold)).foregroundStyle(LimmrTheme.ink3); Spacer(); Button(editing ? "Cancel" : "Edit") { draft = notes; editing.toggle() }.buttonStyle(.plain).font(LimmrTheme.font(12)) }
      if editing {
        TextEditor(text: $draft).font(LimmrTheme.font(14)).frame(minHeight: 80).accessibilityLabel("Notes for your AIs")
        Button("Save") { save(draft); editing = false }.buttonStyle(PaperButtonStyle(primary: true))
      } else { Text(notes.isEmpty ? "Add a note for your AIs" : notes).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2).fixedSize(horizontal: false, vertical: true) }
    }.padding(16).frame(maxWidth: .infinity, alignment: .leading).n1Card()
  }
}
