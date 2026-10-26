import LimmrKit
import SwiftUI

struct PeopleGroupsList: View {
  @Bindable var store: AppStore
  let query: String
  @State private var groups: [PeopleGroup] = []
  @State private var error: String?
  private var matches: [PeopleGroup] {
    let hits = groups.filter { query.isEmpty || $0.name.localizedCaseInsensitiveContains(query) }
    let ids = Set(hits.map(\.jid)); let parents = Set(hits.compactMap(\.parentJid))
    return groups.filter { ids.contains($0.jid) || parents.contains($0.jid) || $0.parentJid.map(ids.contains) == true }
  }
  private func current(_ g: PeopleGroup) -> Bool {
    guard let at = ([g] + groups.filter { $0.parentJid == g.jid }).compactMap({ $0.lastActivity.flatMap(Timestamp.date) }).max() else { return false }
    return at >= Date().addingTimeInterval(-Double(store.settings.currentDays) * 86400)
  }
  var body: some View {
    LazyVStack(alignment: .leading, spacing: 0) {
      if let error { Text(error).foregroundStyle(AccessibleSecondary()); Button("Try again") { Task { await load() } }.limmrButton() }
      ForEach(matches.filter { $0.parentJid == nil && current($0) }) { group in row(group) }
      let older = matches.filter { $0.parentJid == nil && !current($0) }
      if !older.isEmpty {
        DisclosureGroup("Groups you haven't talked in recently (\(older.count))") {
          ForEach(older) { group in row(group) }
        }.font(.callout).padding(.vertical, 16)
      }
      if groups.isEmpty && error == nil { Text("Groups appear after WhatsApp syncs.").foregroundStyle(AccessibleSecondary()).padding(.vertical, 24) }
      if !groups.isEmpty && matches.isEmpty { Text("No groups match your search.").foregroundStyle(AccessibleSecondary()).padding(.vertical, 24) }
    }.task { await load() }
  }
  private func row(_ group: PeopleGroup) -> some View {
    VStack(alignment: .leading, spacing: 0) {
      Button { store.circlePath.append(.group(group.jid)) } label: {
        HStack(spacing: 16) {
          PersonAvatar(store: store, id: group.jid, name: group.name, icon: group.icon)
          VStack(alignment: .leading, spacing: 4) {
            Text(group.name).font(AppType.rowTitle).lineLimit(1)
            Text("\(group.memberCount) members · \(group.messages30 ?? 0) messages this month").font(AppType.caption).foregroundStyle(AccessibleSecondary())
          }
          Spacer()
          if let last = group.lastActivity { Text(store.relativeTime(last)).font(AppType.caption).foregroundStyle(AccessibleSecondary()) }
          Image(systemName: "chevron.right").font(.caption2).foregroundStyle(AccessibleTertiary())
        }.modifier(QuietRow()).contentShape(Rectangle())
      }.limmrButton(.row(radius: 8), id: "people-group")
      if group.isCommunity == true {
        ForEach(matches.filter { $0.parentJid == group.jid }) { child in
          Button { store.circlePath.append(.group(child.jid)) } label: {
            Label(child.name, systemImage: "person.3").font(.callout).padding(.vertical, 10).padding(.leading, 48)
          }.limmrButton()
        }
      }
      Divider().padding(.leading, 48)
    }
  }
  private func load() async {
    do { let response: GroupsResponse = try await store.backend.get("/v1/groups"); try Task.checkCancellation(); groups = response.groups; error = nil }
    catch { if !Task.isCancelled { self.error = "Groups could not be loaded." } }
  }
}

struct GroupPage: View {
  @Bindable var store: AppStore
  let jid: String
  @State private var detail: GroupDetail?
  @State private var error: String?
  var body: some View {
    ScrollView {
      LazyVStack(alignment: .leading, spacing: 24) {
        Button("Back", systemImage: "chevron.left") { store.leavePeopleRoute() }.limmrButton()
        if let detail {
          HStack(spacing: 16) {
            PersonAvatar(store: store, id: jid, name: detail.group.name, size: 72, icon: detail.group.icon)
            VStack(alignment: .leading, spacing: 6) {
              Text(detail.group.name).font(AppType.pageTitle)
              Text("\(detail.group.memberCount) members").font(.callout).foregroundStyle(AccessibleSecondary())
              HStack { ForEach(store.circles.filter { (detail.group.circleIds ?? []).contains($0.id) }) { circle in
                Button(circle.name) { store.circlePath.append(.circle(circle.id)) }.limmrButton().font(AppType.caption)
              } }
            }
            Spacer()
            EntityIconButton(store: store, bucket: "groups", id: jid, icon: detail.group.icon, title: "Icon")
          }.padding(20).background(.primary.opacity(0.035), in: RoundedRectangle(cornerRadius: 16))
          VStack(alignment: .leading, spacing: 12) {
            BlockHeading(title: "AI access")
            HideFromAIsSwitch(store: store, chatJIDs: [jid], explanation: "When hidden, your AIs can't read this group.")
            ForEach(detail.group.agentAccess ?? []) { access in
              HStack {
                Text(access.name).font(.callout).frame(maxWidth: .infinity, alignment: .leading)
                Toggle("Can read", isOn: Binding(get: { access.read && !store.settings.hiddenChats.contains(jid) }, set: { value in Task { await update(access.agentId, read: value) } })).toggleStyle(.checkbox).disabled(store.settings.hiddenChats.contains(jid))
                Picker("Send", selection: Binding(get: { access.send }, set: { value in Task { await update(access.agentId, send: value) } })) {
                  Text("Freely within circle rules").tag("free")
                  Text("With approval").tag("approval")
                  Text("Never").tag("never")
                }.frame(width: 240)
              }
            }
          }
          VStack(alignment: .leading, spacing: 12) {
            BlockHeading(title: "Members")
            ForEach(detail.members, id: \.person.id) { member in
              Button { store.circlePath.append(.person(member.person.id)) } label: {
                HStack(spacing: 12) {
                  PersonAvatar(store: store, id: member.person.id, name: member.person.name)
                  Text(member.person.name).font(.callout)
                  Spacer()
                  if member.admin { Text("Admin").font(AppType.caption).foregroundStyle(AccessibleSecondary()) }
                }.contentShape(Rectangle())
              }.limmrButton()
            }
          }
          VStack(alignment: .leading, spacing: 12) {
            BlockHeading(title: "Tasks here")
            ForEach(detail.group.taskIds ?? [], id: \.self) { id in
              if let task = store.home.tasks.first(where: { $0.id == id }) {
                SurfacedTaskRow(store: store, task: task)
              } else {
                Button { store.openTask(id) } label: {
                  Text("Open task").frame(maxWidth: .infinity, alignment: .leading).padding(12).contentShape(Rectangle())
                }.limmrButton(.row(radius: 8, bordered: false)).accessibilityLabel("Open task workspace")
              }
            }
            if (detail.group.taskIds ?? []).isEmpty { Text("No open tasks in this group.").font(.callout).foregroundStyle(AccessibleSecondary()) }
          }
          if let summary = detail.summary {
            VStack(alignment: .leading, spacing: 8) {
              BlockHeading(title: "Lately")
              Text(summary.text).font(.callout)
              Text("\(store.agentName(summary.agentId)) · \(store.relativeTime(summary.createdAt))").font(AppType.caption).foregroundStyle(AccessibleSecondary())
            }
          }
          VStack(alignment: .leading, spacing: 12) {
            BlockHeading(title: "Recent messages")
            ForEach(detail.recent ?? []) { message in GroupMessageRow(store: store, message: message, members: detail.members.map(\.person), senders: detail.senders ?? []); Divider() }
            if (detail.recent ?? []).isEmpty { Text("No messages stored here yet.").foregroundStyle(AccessibleSecondary()) }
          }
        } else if error == nil { ProgressView("Loading group…") }
        if let error { Text(error).foregroundStyle(AccessibleSecondary()); Button("Try again") { Task { await load() } }.limmrButton() }
        ActionFeedback(store: store, action: "group-access")
      }.frame(maxWidth: 760).padding(24).frame(maxWidth: .infinity)
    }.navigationBarBackButtonHidden(true).background { AppBackground() }.task(id: jid) { await load() }
      .onChange(of: store.settings.hiddenChats) { _, _ in
        Task { await store.waitForSettingsSave(); await load() }
      }
  }
  private func load() async {
    do { let value: GroupDetail = try await store.backend.get("/v1/groups/\(jid)"); try Task.checkCancellation(); detail = value; error = nil }
    catch { if !Task.isCancelled { self.error = "This group could not be loaded." } }
  }
  private func update(_ agent: String, read: Bool? = nil, send: String? = nil) async {
    struct Edit: Encodable { var agentId: String; var read: Bool?; var send: String? }
    await store.perform(action: "group-access") {
      _ = try await store.backend.request("/v1/groups/\(jid)/access", method: "PATCH", body: Wire.data(Edit(agentId: agent, read: read, send: send)))
      await load()
    }
  }
}

// Same read-only row grammar as HomeChatPage; opening never marks messages read.
struct GroupMessageRow: View {
  @Bindable var store: AppStore
  let message: Message
  var members: [Person] = []
  var senders: [GroupSender] = []

  static func memberName(sender: String, members: [Person]) -> String? {
    guard let person = members.first(where: { $0.jids.contains(sender) }) else { return nil }
    if let label = person.label, !label.isEmpty { return label }
    return person.name.isEmpty ? nil : person.name
  }

  var body: some View {
    VStack(alignment: .leading, spacing: 6) {
      HStack {
        Text(message.isFromMe ? "You" : (Self.memberName(sender: message.sender, members: members) ?? senders.first { $0.jid == message.sender }?.name ?? store.senderName(message.sender))).font(.callout.weight(.medium))
        Spacer()
        Text(store.relativeTime(message.timestamp)).font(.caption).foregroundStyle(AccessibleSecondary())
      }
      Text(message.displayText).font(.callout).textSelection(.enabled)
    }
  }
}
