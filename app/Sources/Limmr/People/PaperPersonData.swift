import LimmrKit
import Observation
import SwiftUI

@MainActor @Observable final class PaperPersonData {
  var profile: PersonDetail?
  var tasks: [TaskSummary] = []
  var messages: [Message] = []
  var history: PersonConversationHistory?
  var summary: PersonSummary?
  var error: String?
  func load(_ id: String, store: AppStore) async {
    let generation = store.backendGeneration
    do {
      let profile: PersonDetail = try await store.backend.get("/v1/people/" + id)
      try Task.checkCancellation()
      guard store.backendGeneration == generation else { return }
      self.profile = profile; PersonPageCache.save(profile, store: store)
      let tasks: TasksResponse = try await store.backend.get("/v1/tasks?person=\(id)&status=all")
      guard store.backendGeneration == generation else { return }
      self.tasks = tasks.tasks
      let summary: PersonSummaryResponse? = try? await store.backend.get("/v1/people/\(id)/summary")
      self.summary = summary?.summary
      history = try? await store.backend.get("/v1/people/\(id)/conversation-history")
      if let jid = profile.person.jids.first(where: { !$0.hasSuffix("@g.us") }) {
        var query = URLComponents(); query.queryItems = [.init(name: "chat_jid", value: jid), .init(name: "limit", value: "40")]
        let response: MessagesResponse = try await store.backend.get("/v1/messages?" + (query.percentEncodedQuery ?? ""))
        try Task.checkCancellation()
        guard store.backendGeneration == generation else { return }
        messages = response.messages.filter { !$0.text.isEmpty && $0.deletedAt == nil }
      }
      error = nil
    } catch { if !Task.isCancelled { self.error = "Couldn’t load this person’s context. Try again." } }
  }
}
extension AppStore {
  func paperCircles(_ id: String) -> [LimmrKit.Circle] { circles.filter { $0.people.contains { $0.personId == id } || circleDetails[$0.id]?.people.contains { $0.personId == id } == true } }
  func paperLastSpoke(_ person: Person) -> String? { chats.filter { person.jids.contains($0.jid) }.compactMap(\.lastMessageTime).max() }
  func paperPersonNeeds(_ id: String) -> Bool {
    circleMap.nodes.contains { $0.id == id && $0.needsYou == true } || home.needsReply.contains { ask in people.first { $0.id == id }?.jids.contains(ask.chatJid ?? "") == true } || taskWorkspaceCache.values.contains { work in work.task?.needsYou == true && work.task?.people.contains { $0.id == id } == true }
  }
  func paperOpenChat(_ person: Person) { if let jid = person.jids.first { openPeopleRoute(.chat(jid, person.label ?? person.name)) } }
}

struct PaperPersonSelection: View {
  @Bindable var store: AppStore
  let id: String
  @State private var data = PaperPersonData()
  var body: some View {
    VStack(alignment: .leading, spacing: 16) {
      if let person = data.profile?.person ?? store.peoplePreview(id) {
        HStack(spacing: 14) {
          PersonAvatar(store: store, id: id, name: store.personName(id), size: 56)
          VStack(alignment: .leading, spacing: 4) {
            Text(store.personName(id)).font(LimmrTheme.font(20, .semibold))
            Text(person.role + (store.paperCircles(id).count > 1 ? " · also in " + store.paperCircles(id).map(\.name).joined(separator: ", ") : "")).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2).lineLimit(2)
          }.frame(maxWidth: .infinity, alignment: .leading)
          Button("Open chat") { store.paperOpenChat(person) }.buttonStyle(PaperButtonStyle()).disabled(person.jids.isEmpty)
          Button("Profile", systemImage: "chevron.right") { store.openPeopleRoute(.person(id)) }.buttonStyle(PaperButtonStyle())
        }
        if let ask = data.tasks.map({ store.currentAttention($0) }).first(where: { $0.needsYou || store.taskLaterNotices[$0.id] != nil }) { PaperPersonAsk(store: store, task: ask) }
        HStack(alignment: .top, spacing: 24) {
          VStack(alignment: .leading, spacing: 8) {
            section("Latest from " + person.name.split(separator: " ").prefix(1).joined())
            if let quote = data.messages.first {
              Text("“\(quote.text)”").font(LimmrTheme.font(14)).fixedSize(horizontal: false, vertical: true)
              Text(store.relativeTime(quote.timestamp)).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3)
            } else { Text("No recent conversation available.").font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3) }
          }.frame(maxWidth: .infinity, alignment: .leading)
          if let history = data.history, history.weeks.contains(where: { $0.messages > 0 }) {
            PaperConversationChart(history: history, headingAbove: true).frame(maxWidth: .infinity, alignment: .leading)
          } else if let stats = data.profile?.stats {
            HStack(spacing: 24) {
              PaperNumber(value: stats.dmMessages30.formatted(), label: "messages · 30 days")
              PaperNumber(value: stats.sharedGroups.count.formatted(), label: "shared groups")
            }.frame(maxWidth: .infinity, alignment: .leading)
          }
        }.padding(.top, 16).overlay(alignment: .top) { Rectangle().fill(LimmrTheme.hairline).frame(height: 1) }
        HStack(alignment: .top, spacing: 24) {
          PaperPersonGroups(store: store, groups: data.profile?.groups ?? []).frame(maxWidth: .infinity, alignment: .leading)
          PaperPersonWork(store: store, tasks: data.tasks).frame(maxWidth: .infinity, alignment: .leading)
        }.padding(.top, 16).overlay(alignment: .top) { Rectangle().fill(LimmrTheme.hairline).frame(height: 1) }
      }
      if let history = data.history, history.sharedFilesTotal > 0 { Divider().overlay(LimmrTheme.hairline); PaperSharedFiles(store: store, history: history) }
      if let error = data.error { Text(error).font(LimmrTheme.font(12)); Button("Try again") { Task { await data.load(id, store: store) } }.buttonStyle(PaperButtonStyle()) }
    }.padding(20).frame(maxWidth: .infinity, alignment: .topLeading).n1Card()
      .task(id: "\(store.backendGeneration):\(id)") { await data.load(id, store: store) }
  }
  private func section(_ text: String) -> some View { Text(text).font(LimmrTheme.font(12, .semibold)).foregroundStyle(LimmrTheme.ink3) }
}
struct PaperPersonAsk: View {
  @Bindable var store: AppStore
  let task: TaskSummary
  var body: some View {
    Group {
      if let until = store.taskLaterNotices[task.id] { TaskLaterNotice(store: store, id: task.id, until: until) }
      else { ViewThatFits(in: .horizontal) {
        HStack(spacing: 12) { text; Spacer(minLength: 0); actions }
        VStack(alignment: .leading, spacing: 12) { text; HStack { Spacer(); actions } }
      } }
      if let error = store.taskActionErrors[task.id] { Text(error).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2) }
    }.padding(.vertical, 12).padding(.horizontal, 16).background(LimmrTheme.needBackground, in: RoundedRectangle(cornerRadius: 12))
  }
  private var text: some View {
    VStack(alignment: .leading, spacing: 4) {
      HStack(alignment: .firstTextBaseline, spacing: 8) { PaperMarker(); Text(task.needsYouReason ?? "Needs you").font(LimmrTheme.font(14, .semibold)).fixedSize(horizontal: false, vertical: true) }
      Text(task.title).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2)
    }
  }
  private var actions: some View { HStack(spacing: 12) { TaskLaterButton(store: store, id: task.id); Button("Review") { store.openTask(task.id) }.buttonStyle(PaperButtonStyle(primary: true)) } }
}
struct PaperPersonGroups: View {
  @Bindable var store: AppStore
  let groups: [PeopleGroup]
  var body: some View {
    VStack(alignment: .leading, spacing: 8) {
      Text("Groups in common").font(LimmrTheme.font(12, .semibold)).foregroundStyle(LimmrTheme.ink3)
      ForEach(groups) { group in
        Button { store.openPeopleRoute(.group(group.jid)) } label: {
          HStack(spacing: 10) {
            Image(systemName: "person.2").foregroundStyle(LimmrTheme.ink2)
            VStack(alignment: .leading, spacing: 4) { Text(group.name).font(LimmrTheme.font(14, .semibold)); Text("\(group.memberCount) members").font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2) }
            Spacer(minLength: 0); Image(systemName: "chevron.right").font(.system(size: 12)).foregroundStyle(LimmrTheme.ink3)
          }.padding(.vertical, 6).contentShape(Rectangle())
        }.quietCard()
      }
    }
  }
}
struct PaperPersonWork: View {
  @Bindable var store: AppStore
  let tasks: [TaskSummary]
  @State private var ended = false
  var body: some View {
    VStack(alignment: .leading, spacing: 8) {
      Text("Work you share").font(LimmrTheme.font(12, .semibold)).foregroundStyle(LimmrTheme.ink3)
      ForEach(tasks.filter { ended ? ["done", "dropped", "dismissed"].contains($0.status) : !["done", "dropped", "dismissed"].contains($0.status) }) { task in
        Button { store.openTask(task.id) } label: {
          HStack(spacing: 10) {
            PaperTaskArtwork(store: store, task: task).frame(width: 44, height: 32).clipShape(RoundedRectangle(cornerRadius: 6))
            Text(task.title).font(LimmrTheme.font(14, .semibold)).lineLimit(2).frame(maxWidth: .infinity, alignment: .leading)
            if task.needsYou { PaperChip(text: "Needs you", kind: "need") }
            else if let name = store.paperWorkingAgent(task) { PaperChip(text: name, kind: "working") }
          }.padding(.vertical, 6).contentShape(Rectangle())
        }.quietCard().modifier(PaperTaskContext(store: store, task: task))
      }
      let history = TaskListPresentation.history(tasks)
      if !history.isEmpty { Button(ended ? "Open tasks" : "\(history.count) ended tasks · History") { ended.toggle() }.buttonStyle(.plain).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3) }
    }
  }
}
