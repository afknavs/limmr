import LimmrKit
import SwiftUI

extension AppStore {
  var currentNow: Date { demo ? Timestamp.date(DemoData.now)! : .now }
  func chatIsCurrent(_ chat: Chat) -> Bool {
    CurrentChats.isCurrent(chat, days: settings.currentDays, now: currentNow)
  }
  func personLastChat(_ person: Person) -> Date {
    chats.filter { person.jids.contains($0.jid) }.compactMap {
      $0.lastMessageTime.flatMap(Timestamp.date)
    }.max() ?? .distantPast
  }
  func personIsCurrent(_ person: Person) -> Bool {
    person.id == "me" || chats.contains { person.jids.contains($0.jid) && chatIsCurrent($0) }
  }
  func orderedPeople(query: String) -> [Person] {
    PeoplePresentation.people(query.isEmpty ? people : (peopleSearchResults ?? people), circles: circles, query: query).filter {
      mapSelection.person == nil || mapSelection.person == $0.id
    }.sorted {
      if $0.id == "me" { return $1.id != "me" }; if $1.id == "me" { return false }
      let a = personLastChat($0)
      let b = personLastChat($1)
      return a == b ? $0.name.localizedStandardCompare($1.name) == .orderedAscending : a > b
    }
  }
  func proposalActivity(_ proposal: CircleProposal) -> String {
    let included = chats.filter { chat in proposal.circle.chats.contains { $0.chatJid == chat.jid }
    }
    let count = included.filter(chatIsCurrent).count
    let latest = included.compactMap(\.lastMessageTime).max()
    return "\(count) current"
      + (latest.map { " · active " + relativeTime($0) } ?? " · no messages yet")
  }
  func resetCircles() async {
    await perform(action: "reset-circles") { [self] in
      try await backend.post("/v1/circles/reset", ["confirm": true])
      circleDetails = [:]
      circleHistory = [:]
      circlePath = []
      mapSelection = MapSelection()
      await refresh()
    }
  }
}
struct CurrentChatSections<Row: View>: View {
  let store: AppStore
  let chats: [Chat]
  var query = ""
  @ViewBuilder var row: (Chat) -> Row
  var body: some View {
    Group {
      let ordered = CurrentChats.ordered(chats, query: query)
      ForEach(ordered.filter { store.chatIsCurrent($0) }, content: row)
      let older = ordered.filter { !store.chatIsCurrent($0) }
      if !older.isEmpty {
        DisclosureGroup("Older chats (\(older.count))") { ForEach(older, content: row) }
      }
      if store.chatCursor?.isEmpty == false {
        Button("Load more chats") { Task { await store.loadMoreChats() } }.disabled(store.loadingMore)
      }
    }.task(id: query) { await store.searchChats(query) }
  }
}
struct ResetCirclesSheet: View {
  @Bindable var store: AppStore
  @Environment(\.dismiss) private var dismiss
  var body: some View {
    VStack(alignment: .leading, spacing: 18) {
      Text("Start circles over?").font(.title2.bold())
      Text("Removes all circles and their briefs. Chats and people stay.").font(.body)
      HStack {
        Spacer()
        Button("Cancel") { dismiss() }.keyboardShortcut(.cancelAction)
        Button("Start over", role: .destructive) {
          Task {
            await store.resetCircles()
            if store.error == nil { dismiss() }
          }
        }.buttonStyle(.borderedProminent).disabled(store.busy)
      }
      ActionFeedback(store: store, action: "reset-circles")
    }.padding(24).frame(width: 430).background(Color(nsColor: .windowBackgroundColor)).clipShape(
      RoundedRectangle(cornerRadius: 16))
  }
}
