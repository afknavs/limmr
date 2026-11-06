import LimmrKit
import SwiftUI

struct CirclePage: View {
  @Bindable var store: AppStore
  let id: String
  @State private var showHistory = false
  @State private var archivedHere = false
  @State private var addingChat = false
  @State private var selectedChat = ""
  @State private var chatQuery = ""
  var body: some View {
    GeometryReader { geometry in
      ScrollView {
        LazyVStack(alignment: .leading, spacing: 20) {
          Button(store.peopleBackTitle, systemImage: "chevron.left") { store.leavePeopleRoute() }.buttonStyle(
            .plain
          ).font(.callout)
          if let detail = store.circleDetails[id] {
            header(detail.circle)
            ActionFeedback(store: store, action: "circle-\(id)")
            if let message = store.circleError {
              HStack {
                Text(message).font(.callout).foregroundStyle(.secondary)
                Button("Try again") { Task { await store.loadCircle(id) } }
              }
            }
            if let brief = detail.brief {
              briefCard(brief)
            }
            if showHistory {
              VStack(alignment: .leading, spacing: 0) {
                VStack(alignment: .leading, spacing: 16) {
                  Text("Brief history").font(.title3.weight(.semibold))
                  ForEach(store.circleHistory[id] ?? []) { brief in
                    HStack {
                      Button("Version \(brief.version) · \(store.time(brief.writtenAt))") {
                        store.circlePath.append(.brief(id, brief.version))
                      }.limmrButton()
                      Spacer()
                      if brief.version != detail.brief?.version {
                        Button("Revert") {
                          Task {
                            await store.circleAction(id, suffix: "/briefs/\(brief.version)/revert")
                          }
                        }.buttonStyle(NativeGlassButtonStyle())
                      } else {
                        Text("Current").font(.caption).foregroundStyle(.secondary)
                      }
                    }
                  }
                }
              }
            }
            if CirclePageSections.hasTasks(store.allTasks, id: id) {
            VStack(alignment: .leading, spacing: 16) {
              Text("Tasks").font(.title3.weight(.semibold))
              TaskListSections(store: store, tasks: TaskListPresentation.inCircle(store.allTasks, id: id), collapseQuiet: true)
            }
            }
            if !detail.topics.isEmpty {
            VStack(alignment: .leading, spacing: 16) {
              Text("Topics").font(.title3.weight(.semibold))
              ForEach(detail.topics) { topic in
                HStack {
                  Image(systemName: topic.status == "done" ? "checkmark.circle" : "text.bubble")
                    .foregroundStyle(.secondary)
                  Text(topic.title).font(.headline).lineLimit(1).truncationMode(.tail)
                  Spacer()
                  Badge(text: topic.status.capitalized)
                }
              }
            }
            }
            if !detail.people.isEmpty {
            VStack(alignment: .leading, spacing: 16) {
              Text("People").font(.title3.weight(.semibold))
              ForEach(detail.people) { member in
                Button {
                  store.circlePath.append(.person(member.personId))
                } label: {
                  HStack(alignment: .top, spacing: 16) {
                    EntityIconButton(
                      store: store, bucket: "people", id: member.personId,
                      icon: store.people.first { $0.id == member.personId }?.icon,
                      initials: String(
                        store.personName(member.personId).split(separator: " ").prefix(2)
                          .compactMap(\.first)))
                    VStack(alignment: .leading, spacing: 4) {
                      Text(store.personName(member.personId)).font(.headline).lineLimit(1).truncationMode(.tail)
                      Text(member.role).font(.callout).foregroundStyle(.secondary)
                      if let note = detail.brief?.peopleNotes.first(where: {
                        $0.personId == member.personId
                      }) {
                        Text(note.contribution).font(.callout).foregroundStyle(.secondary)
                      }
                    }
                    Spacer()
                    Image(systemName: "chevron.right").font(.caption).foregroundStyle(.secondary)
                  }
                }.quietCard()
              }
            }
            }
            if !detail.chats.isEmpty || addingChat {
            VStack(alignment: .leading, spacing: 16) {
              HStack {
                Text("Chats included").font(.title3.weight(.semibold))
                Spacer()
                Button(addingChat ? "Cancel" : "Add chat…") { addingChat.toggle() }.limmrButton()
              }
              ForEach(detail.chats) { chat in
                HStack(spacing: 16) {
                  Image(
                    systemName: store.chats.first { $0.jid == chat.chatJid }?.isGroup == true
                      ? "person.2" : "person")
                  Text(store.chatName(chat.chatJid)).lineLimit(1).truncationMode(.tail)
                  Spacer()
                  Text(chat.roleInCircle).font(.callout).foregroundStyle(.secondary)
                }
              }
              if addingChat {
                TextField("Search chats", text: $chatQuery).textFieldStyle(.roundedBorder)
                CurrentChatSections(
                  store: store,
                  chats: store.chats.filter { chat in
                    !detail.chats.contains { $0.chatJid == chat.jid }
                  }, query: chatQuery
                ) { chat in
                  Toggle(
                    chat.name,
                    isOn: Binding(
                      get: { selectedChat == chat.jid }, set: { selectedChat = $0 ? chat.jid : "" })
                  ).toggleStyle(.checkbox)
                }
                Button("Add chat") {
                  Task {
                    await store.circleAction(
                      id, suffix: "/chats",
                      values: ["chat_jid": selectedChat, "role_in_circle": "Included chat"])
                    if store.error == nil { addingChat = false }
                  }
                }.disabled(selectedChat.isEmpty).buttonStyle(NativeGlassButtonStyle())
              }
            }
            } else {
              Button("Add chat…", systemImage: "plus") { addingChat = true }.limmrButton()
            }
            Divider()
            VStack(alignment: .leading, spacing: 16) {
              Text("Who can see this").font(.title3.weight(.semibold))
              let integrations = store.agents.filter { store.canSee($0, circle: detail.circle) }
              if integrations.isEmpty {
                Text("No integration has access to every chat in this circle.").font(.callout)
                  .foregroundStyle(.secondary)
              }
              ForEach(integrations) { agent in
                HStack {
                  EntityIconButton(
                    store: store, bucket: "agents", id: agent.id, icon: agent.icon,
                    fallback: agent.symbol)
                  Text(agent.name).font(.headline).lineLimit(1).truncationMode(.tail)
                }
              }
            }
            VStack(alignment: .leading, spacing: 16) {
              Text("Send rule").font(.title3.weight(.semibold))
              Text(detail.circle.sendRule.explanation).font(.callout).foregroundStyle(.secondary)
              Text(
                "A chat in several circles follows the strictest rule. Integration permissions also apply."
              ).font(.caption).foregroundStyle(.secondary)

            }
            ActionFeedback(store: store, action: "circle-\(id)")
          } else if let message = store.circleError {
            Text(message)
            Button("Try again") { Task { await store.loadCircle(id) } }
          } else {
            ProgressView("Loading circle…")
          }
        }
        .frame(maxWidth: 1000).padding(20).frame(maxWidth: .infinity)
      }
    }
    .navigationBarBackButtonHidden(true)
    .background { Color(nsColor: .windowBackgroundColor) }
    .task(id: id) {
      await store.loadCircle(id)
      await store.refreshAllTasks()
      if !store.snapshot { store.markCircleSeen(id) }
    }
  }
  private func header(_ circle: LimmrKit.Circle) -> some View {
    VStack(alignment: .leading, spacing: 16) {
      HStack(spacing: 16) {
        CirclePictureButton(store: store, circle: circle, size: 44)
        Text(circle.name).foregroundStyle(.primary).font(.largeTitle.bold()).lineLimit(1).truncationMode(.tail).help(circle.name)
      }
      HStack {
        if circle.kind.caseInsensitiveCompare(circle.name) != .orderedSame {
          Text(circle.kind.capitalized).font(.callout).foregroundStyle(.secondary)
        }
        Spacer()
        Button(archivedHere ? "Undo archive" : "Archive", systemImage: "archivebox") {
          Task {
            let archive = !archivedHere
            await store.perform(action: "circle-\(id)") {
              _ = try await store.backend.request("/v1/circles/\(id)", method: "PATCH",
                body: Wire.data(["archived": archive]))
              archivedHere = archive
              await store.refresh()
              await store.loadCircle(id)
            }
          }
        }.limmrButton().font(.callout).disabled(store.busy)
        Button(circle.pinned ? "Unpin" : "Pin", systemImage: circle.pinned ? "pin.slash" : "pin") {
          Task { await store.togglePin(circle) }
        }.limmrButton().font(.callout).disabled(store.busy)
        Menu {
          ForEach(SendRule.allCases, id: \.self) { rule in
            Button {
              Task {
                await store.circleAction(id, method: "PATCH", values: ["send_rule": rule.rawValue])
              }
            } label: {
              if circle.sendRule == rule {
                Label(rule.title, systemImage: "checkmark")
              } else {
                Text(rule.title)
              }
            }
          }
        } label: {
          Text(circle.sendRule.title).font(.caption)
        }.menuStyle(.button).buttonStyle(.bordered).controlSize(.small).fixedSize().disabled(
          store.busy
        )
        .accessibilityLabel("Send rule: \(circle.sendRule.title)")
      }
    }
  }
  private func briefCard(_ brief: CircleBrief) -> some View {
    VStack(alignment: .leading, spacing: 0) {
      VStack(alignment: .leading, spacing: 16) {
        HStack {
          Text("The brief").font(.title3.weight(.semibold))
          Spacer()
          Button("History") { showHistory.toggle() }.limmrButton().font(.callout)
        }
        Text(brief.summary).font(.body).textSelection(.enabled)
        VStack(alignment: .leading, spacing: 6) {
          Text("What changed").font(.title3.weight(.semibold))
          Text(brief.whatChanged).font(.callout)
        }
        if !brief.openAsksForYou.isEmpty {
          VStack(alignment: .leading, spacing: 8) {
            Text("Open asks for you").font(.title3.weight(.semibold))
            ForEach(brief.openAsksForYou, id: \.self) { ask in
              Label(ask, systemImage: "arrow.turn.down.right").font(.callout)
            }
          }
        }
        if !brief.upcoming.isEmpty {
          ViewThatFits(in: .horizontal) {
            HStack { ForEach(brief.upcoming, id: \.text) { Badge(text: $0.displayText) } }
            VStack(alignment: .leading) {
              ForEach(brief.upcoming, id: \.text) { Badge(text: $0.displayText) }
            }
          }
        }
        Text(
          "Written by \(store.agentName(brief.writtenBy)) · \(store.time(brief.writtenAt)) · Version \(brief.version)"
        ).font(.caption).foregroundStyle(.secondary)
      }
    }
  }
}

struct BriefVersionPage: View {
  @Bindable var store: AppStore
  let circleID: String
  let version: Int
  var body: some View {
    ScrollView {
      LazyVStack(alignment: .leading, spacing: 20) {
        Button("Back to circle", systemImage: "chevron.left") {
          store.leavePeopleRoute()
        }.limmrButton()
        Text("\(store.circleName(circleID)) · Version \(version)").font(.title.bold())
        if let brief = store.circleHistory[circleID]?.first(where: { $0.version == version }) {
          Text(brief.summary).textSelection(.enabled)
          Text(brief.whatChanged).foregroundStyle(.secondary)
          Text("Written by \(store.agentName(brief.writtenBy)) · \(store.time(brief.writtenAt))")
            .font(.caption).foregroundStyle(.secondary)
          Button("Revert to this version") {
            Task {
              await store.circleAction(circleID, suffix: "/briefs/\(version)/revert")
              if store.error == nil { store.circlePath = [.circle(circleID)] }
            }
          }.buttonStyle(NativeGlassButtonStyle())
          ActionFeedback(store: store, action: "circle-\(circleID)")
        }
      }.frame(maxWidth: 760).padding(32).frame(maxWidth: .infinity, alignment: .leading)
    }.navigationBarBackButtonHidden(true)
      .background { Color(nsColor: .windowBackgroundColor) }
  }
}

enum CirclePageSections {
  static func hasTasks(_ tasks: [TaskSummary], id: String) -> Bool {
    !TaskListPresentation.inCircle(tasks, id: id).isEmpty
  }
}
