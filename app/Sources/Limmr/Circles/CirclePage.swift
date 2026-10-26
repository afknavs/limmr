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
        LazyVStack(alignment: .leading, spacing: 24) {
          Button("Back", systemImage: "chevron.left") { store.leavePeopleRoute() }.buttonStyle(
            .plain
          ).font(.callout)
          if let detail = store.circleDetails[id] {
            header(detail.circle)
            ActionFeedback(store: store, action: "circle-\(id)")
            if let message = store.circleError {
              HStack {
                Text(message).font(.callout).foregroundStyle(AccessibleSecondary())
                Button("Try again") { Task { await store.loadCircle(id) } }
              }
            }
            if let brief = detail.brief {
              briefCard(brief)
            } else {
              QuietBlockState(title: "A brief for this circle", detail: "Your AIs can save a summary of the conversations and what needs you here.")
            }
            if showHistory {
              VStack(alignment: .leading, spacing: 0) {
                VStack(alignment: .leading, spacing: 16) {
                  Text("Brief history").font(AppType.blockHeading)
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
                        Text("Current").font(AppType.caption).foregroundStyle(AccessibleSecondary())
                      }
                    }
                  }
                }
              }
            }
            VStack(alignment: .leading, spacing: 16) {
              Text("Topics").font(AppType.blockHeading)
              ForEach(detail.topics) { topic in
                HStack {
                  Image(systemName: topic.status == "done" ? "checkmark.circle" : "text.bubble")
                    .foregroundStyle(AccessibleSecondary())
                  Text(topic.title).font(AppType.rowTitle).lineLimit(1).truncationMode(.tail)
                  Spacer()
                  Badge(text: topic.status.capitalized)
                }
              }
              if detail.topics.isEmpty { QuietBlockState(title: "Topics take shape here", detail: "Shared plans and questions will appear as the conversation develops.") }
            }
            VStack(alignment: .leading, spacing: 16) {
              Text("People").font(AppType.blockHeading)
              if detail.people.isEmpty {
                QuietBlockState(title: "People in this circle", detail: "People appear here when you or an AI adds them to this circle.")
              }
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
                      Text(store.personName(member.personId)).font(AppType.rowTitle).lineLimit(1).truncationMode(.tail)
                      Text(member.role).font(.callout).foregroundStyle(AccessibleSecondary())
                      if let note = detail.brief?.peopleNotes.first(where: {
                        $0.personId == member.personId
                      }) {
                        Text(note.contribution).font(.callout).foregroundStyle(AccessibleSecondary())
                      }
                    }
                    Spacer()
                    Image(systemName: "chevron.right").font(AppType.caption).foregroundStyle(AccessibleSecondary())
                  }
                }.limmrButton()
              }
            }
            VStack(alignment: .leading, spacing: 16) {
              HStack {
                Text("Chats included").font(AppType.blockHeading)
                Spacer()
                Button(addingChat ? "Cancel" : "Add chat…") { addingChat.toggle() }.limmrButton()
              }
              if detail.chats.isEmpty {
                QuietBlockState(title: "Choose conversations", detail: "Add a chat to give this circle its context.")
              }
              ForEach(detail.chats) { chat in
                HStack(spacing: 16) {
                  Image(
                    systemName: store.chats.first { $0.jid == chat.chatJid }?.isGroup == true
                      ? "person.2" : "person")
                  Text(store.chatName(chat.chatJid)).lineLimit(1).truncationMode(.tail)
                  Spacer()
                  Text(chat.roleInCircle).font(.callout).foregroundStyle(AccessibleSecondary())
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
            Divider()
            VStack(alignment: .leading, spacing: 16) {
              Text("Who can see this").font(AppType.blockHeading)
              let integrations = store.agents.filter { store.canSee($0, circle: detail.circle) }
              if integrations.isEmpty {
                Text("No integration has access to every chat in this circle.").font(.callout)
                  .foregroundStyle(AccessibleSecondary())
              }
              ForEach(integrations) { agent in
                HStack {
                  EntityIconButton(
                    store: store, bucket: "agents", id: agent.id, icon: agent.icon,
                    fallback: agent.symbol)
                  Text(agent.name).font(AppType.rowTitle).lineLimit(1).truncationMode(.tail)
                }
              }
            }
            VStack(alignment: .leading, spacing: 16) {
              Text("Send rule").font(AppType.blockHeading)
              Text(detail.circle.sendRule.explanation).font(.callout).foregroundStyle(AccessibleSecondary())
              Text(
                "A chat in several circles follows the strictest rule. Integration permissions also apply."
              ).font(AppType.caption).foregroundStyle(AccessibleSecondary())

            }
            ActionFeedback(store: store, action: "circle-\(id)")
          } else if let message = store.circleError {
            Text(message)
            Button("Try again") { Task { await store.loadCircle(id) } }
          } else {
            ProgressView("Loading circle…")
          }
        }
        .frame(maxWidth: 760).padding(.horizontal, geometry.size.width < 900 ? 24 : 32).padding(
          .vertical, 24
        ).frame(maxWidth: .infinity)
      }
    }
    .navigationBarBackButtonHidden(true)
    .background { AppBackground() }
    .task(id: id) {
      await store.loadCircle(id)
      if !store.snapshot { store.markCircleSeen(id) }
    }
  }
  private func header(_ circle: LimmrKit.Circle) -> some View {
    VStack(alignment: .leading, spacing: 16) {
      HStack(spacing: 16) {
        EntityIconButton(
          store: store, bucket: "circles", id: circle.id, icon: circle.icon, fallback: circle.symbol
        ).font(.largeTitle.weight(.light))
        Text(circle.name).font(AppType.pageTitle).lineLimit(1).truncationMode(.tail).help(circle.name)
      }
      HStack {
        if circle.kind.caseInsensitiveCompare(circle.name) != .orderedSame {
          Text(circle.kind.capitalized).font(.callout).foregroundStyle(AccessibleSecondary())
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
          Text(circle.sendRule.title).font(AppType.caption)
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
          Text("The brief").font(AppType.blockHeading)
          Spacer()
          Button("History") { showHistory.toggle() }.limmrButton().font(.callout)
        }
        Text(brief.summary).font(.body).textSelection(.enabled)
        VStack(alignment: .leading, spacing: 6) {
          Text("What changed").font(AppType.blockHeading)
          Text(brief.whatChanged).font(.callout)
        }
        if !brief.openAsksForYou.isEmpty {
          VStack(alignment: .leading, spacing: 8) {
            Text("Open asks for you").font(AppType.blockHeading)
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
        ).font(AppType.caption).foregroundStyle(AccessibleSecondary())
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
      LazyVStack(alignment: .leading, spacing: 24) {
        Button("Back to circle", systemImage: "chevron.left") {
          store.leavePeopleRoute()
        }.limmrButton()
        Text("\(store.circleName(circleID)) · Version \(version)").font(.title.bold())
        if let brief = store.circleHistory[circleID]?.first(where: { $0.version == version }) {
          Text(brief.summary).textSelection(.enabled)
          Text(brief.whatChanged).foregroundStyle(AccessibleSecondary())
          Text("Written by \(store.agentName(brief.writtenBy)) · \(store.time(brief.writtenAt))")
            .font(AppType.caption).foregroundStyle(AccessibleSecondary())
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
      .background { AppBackground() }
  }
}
