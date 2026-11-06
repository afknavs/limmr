import LimmrKit
import SwiftUI

struct HomeView: View {
  @Environment(\.accessibilityReduceMotion) private var systemReduceMotion
  @Environment(\.snapshotReducedMotion) private var snapshotReduceMotion
  private var reduceMotion: Bool { systemReduceMotion || snapshotReduceMotion }
  @Bindable var store: AppStore
  @Environment(\.limmrWindowWidth) private var windowWidth
  @Environment(\.openSettings) private var openSettings
  @Environment(\.snapshotOpenWatch) private var snapshotWatch
  @Environment(\.snapshotHomeSection) private var snapshotSection
  @State private var showAllAsks = false
  private var mainSections: [String] {
    var sections = HomePresentation.mainSections(store.home)
    // Keep the last card's Undo notice reachable after the section becomes empty.
    if store.taskQuietNotice != nil && !sections.contains("tasks") {
      sections.insert("tasks", at: min(sections.count, (sections.firstIndex(of: "dashboard") ?? 0) + 1))
    }
    return sections
  }
  private var greeting: String {
    HomePresentation.greeting(at: store.presentationNow)
  }
  var body: some View {
    let _ = SpeedTrace.body("Home")
    Group {
      if store.status?.linked == false && store.chats.isEmpty && store.people.isEmpty && store.homeActivityHistory == nil && store.home.tasks.isEmpty {
        VStack(spacing: 20) {
          Image(systemName: "bubble.left.and.text.bubble.right").font(.largeTitle).foregroundStyle(
            .secondary)
          Text("Link WhatsApp to get started").font(.title2.weight(.semibold))
          Button("Link WhatsApp") {
            store.setupStep = 1
            store.showSetup = true
          }.buttonStyle(.borderedProminent)
          Button("Explore with sample data") { Task { await store.useSampleData(true) } }
            .buttonStyle(.bordered)
        }.frame(maxWidth: .infinity, maxHeight: .infinity)
      } else {
        ScrollViewReader { proxy in
          GeometryReader { geometry in
            let wide = windowWidth >= 1200 && geometry.size.width >= 900
            ScrollView {
              VStack(alignment: .leading, spacing: 20) {
                HStack(alignment: .top, spacing: 20) {
                  LazyVStack(alignment: .leading, spacing: 20) {
                    N1Overview(store: store) { row in openOverview(row, proxy: proxy) }
                    if let history = store.homeActivityHistory {
                      N2HomeActivity(history: history, now: store.presentationNow, openDay: store.openActivityDay)
                    }
                    if let setup = store.setupStatus { SetupHomeCard(store: store, setup: setup) }
                    if !TaskListPresentation.active(store.home.tasks).isEmpty || store.taskQuietNotice != nil { TasksTile(store: store) }
                    N1CirclesGrid(store: store).id("n2-circles")
                    if !wide { N1HomeRail(store: store) }
                    if !store.pending.isEmpty {
                      VStack(alignment: .leading, spacing: 12) {
                        Text("Approvals").font(LimmrTheme.TypeScale.section)
                        ForEach(store.pending) { item in ApprovalCard(store: store, approval: item).id(item.id) }
                      }.id("approvals")
                    }
                    if !store.home.queued.isEmpty { block("queued").id("queued") }
                    if !store.home.needsReply.isEmpty { block("needs_reply") }
                    if !store.home.waiting.isEmpty { block("triggers") }
                    if !store.home.proposals.isEmpty { block("proposals") }
                  }.frame(maxWidth: .infinity, alignment: .leading)
                  if wide { N1HomeRail(store: store).frame(width: 240) }
                }
              }.padding(20).frame(maxWidth: 1320, alignment: .leading).frame(maxWidth: .infinity)
            }
          }
          .onChange(of: store.searchRequest) { _, _ in proxy.scrollTo("greeting", anchor: .top) }
          .onChange(of: store.selectedApproval) { _, id in
            if id != nil { withAnimation(reduceMotion ? nil : .default) { proxy.scrollTo(id, anchor: .center) } }
          }
          .task {
            if let snapshotSection {
              try? await Task.sleep(for: .milliseconds(250))
              proxy.scrollTo(snapshotSection, anchor: .top)
            }
          }
        }
        // Print capture has one window. Stage the same popover on its own floating surface,
        // with a scrim separating it from the page instead of laying it over the next block inline.
        .overlay {
          if let id = snapshotWatch {
            ZStack {
              Color(nsColor: .windowBackgroundColor).opacity(0.88)
              WatchCard(store: store, id: id)
                .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 16))
                .overlay(
                  RoundedRectangle(cornerRadius: 16).stroke(
                    .secondary.opacity(0.18), lineWidth: 0.5))
            }
          }
        }
      }
    }
    .navigationTitle("Home")
    .modifier(HomeToolbarSearch(store: store))
    .onAppear { HomeQuietSnapshot.prepare(store: store) }
    .onChange(of: store.escapeRequest) { _, _ in showAllAsks = false }
    .sheet(isPresented: $showAllAsks) { ReplyAsksSheet(store: store).dismissOnEscape() }
    .sheet(isPresented: $store.showProposalReview) { ProposalReview(store: store).dismissOnEscape() }
  }
  private func openOverview(_ row: OverviewRow, proxy: ScrollViewProxy) {
    switch row.kind {
    case "task": store.openTask(row.target)
    case "person": store.openPeopleRoute(.person(row.target))
    case "approval": store.selectedApproval = row.target; proxy.scrollTo(row.target, anchor: .center)
    case "queued": store.selectedApproval = row.target; proxy.scrollTo(row.target, anchor: .center)
    default: break
    }
  }
  private var greetingHeader: some View {
    VStack(alignment: .leading, spacing: 8) {
      Text(greeting).font(.largeTitle.bold())
    }
    .frame(maxWidth: .infinity, alignment: .leading)
  }
  @ViewBuilder private func block(_ section: String, mainWidth: Double = 600) -> some View {
    switch section {
    case "greeting": greetingHeader

    case "dashboard":
      DashboardView(store: store, contentWidth: mainWidth - 32, fullWidth: true)
      if store.home.waiting.isEmpty && !store.home.needsReply.isEmpty {
        DisclosureGroup("Needs a reply (\(store.home.needsReply.count))") { NeedsReplyCard(store: store).padding(.top, 8) }
          .font(.callout).foregroundStyle(.secondary)
      }
    case "triggers":
      DisclosureGroup("Waiting for a trigger") { TriggerWaitingCard(store: store).padding(.top, 8) }
        .font(.callout).foregroundStyle(.secondary)
    case "tasks": TasksTile(store: store)
    case "queued":
      VStack(alignment: .leading, spacing: 8) {
        CountLabel(value: store.home.queued.count, title: "Queued to send")
        ForEach(store.home.queued) { item in
          QueuedDraftRow(store: store, item: item).id(item.id)
          if item.id != store.home.queued.last?.id { Divider() }
        }
      }
    case "circles":
      CircleUpdatesView(store: store, contentWidth: min(mainWidth, 620))
        .frame(maxWidth: 620, alignment: .leading)
    case "needs_reply":
      VStack(alignment: .leading, spacing: 8) {
        BlockHeading(title: "Needs a reply")
        ForEach(store.home.needsReply.prefix(3)) { ask in ReplyAskRow(store: store, ask: ask) }
        if store.home.needsReply.count > 3 {
          Button("\(store.home.needsReply.count - 3) more") { showAllAsks = true }.buttonStyle(
            .plain
          ).font(.caption)
        }
      }
    case "changes":
      Button {
        store.activityAgent = "all"
        store.activityFilter = "all"
        store.selectPage(.activity)
      } label: {
        HStack(spacing: 4) {
          Text(HomePresentation.changesLine(
            store.home, now: store.presentationNow))
            .lineLimit(1)
          Text("· Review in Activity").fixedSize()
          Spacer(minLength: 0)
        }
      }.buttonStyle(.bordered).font(.caption).foregroundStyle(.secondary)
    case "proposals":
      VStack(alignment: .leading, spacing: 16) {
        BlockHeading(title: "New circles suggested")
        ForEach(store.home.proposals) { CircleProposalCard(store: store, proposal: $0) }
      }
    case "integrations":
      if !store.isLoading("/v1/home") && store.hasLoaded("/v1/home") { HStack(spacing: 6) {
        Text(
          store.home.integrations.isEmpty
            ? "No integrations connected"
            : store.home.integrations.map(\.name).formatted(.list(type: .and)) + " connected"
        )
        .lineLimit(2)
        Text("·")
        Button("Manage") {
          store.settingsTab = .integrations
          openSettings()
        }.buttonStyle(.bordered)
      }.font(.caption).foregroundStyle(.secondary) }
    default: EmptyView()
    }
  }
  @ViewBuilder private func activityCompanion(_ slot: HomePresentation.ActivitySlot, mainWidth: Double) -> some View {
    if slot == .triggers { TriggerWaitingCard(store: store, compact: mainWidth < 340) }
    else { NeedsReplyCard(store: store) }
  }

}

struct BlockHeading: View {
  let title: String
  var body: some View {
    Text(title).font(.title3.weight(.semibold)).foregroundStyle(.secondary)
      .frame(maxWidth: .infinity, alignment: .leading)
  }
}
struct QuietRow: ViewModifier {
  @State private var hovering = false
  func body(content: Content) -> some View {
    content.padding(8).background(
      hovering ? Color.primary.opacity(0.035) : .clear, in: RoundedRectangle(cornerRadius: 8)
    )
    .onHover { hovering = $0 }
  }
}
struct ReplyAskRow: View {
  @Bindable var store: AppStore
  let ask: ReplyAsk
  @State private var hover = false
  @Environment(\.openURL) private var openURL
  var body: some View {
    VStack(alignment: .leading, spacing: 8) {
      HStack(alignment: .top, spacing: 16) {
        if let id = store.personID(jid: ask.chatJid, name: ask.from) {
          EntityIconButton(
            store: store, bucket: "people", id: id, icon: store.people.first { $0.id == id }?.icon,
            initials: String(
              store.personName(id).split(separator: " ").prefix(2).compactMap(\.first)))
        }
        VStack(alignment: .leading, spacing: 4) {
          Text("\(ask.from ?? ask.chat ?? "Someone") · \(ask.circle)").font(.headline)
          Text("“\(ask.text)”").font(.callout).foregroundStyle(.secondary).lineLimit(2)
          Text(ask.at.map { "Waiting \(store.relativeTime($0))" } ?? "Waiting time unavailable")
            .font(.caption).foregroundStyle(.secondary)
        }
        Spacer(minLength: 0)
        Button("Mark done") { Task { await store.completeAsk(ask) } }.buttonStyle(.bordered)
      }
      HStack(spacing: 16) {
        if let url = HomePresentation.whatsappURL(jid: ask.chatJid) {
          Button("Open in WhatsApp") { openURL(url) }.buttonStyle(.bordered)
        }
        Menu(store.requestedReplyDrafts.contains(ask.id) ? "Draft requested" : "Ask an AI to draft")
        {
          ForEach(store.agents.filter(\.enabled)) { agent in
            Button(agent.name) { Task { await store.requestReplyDraft(ask, agent: agent) } }
          }
        }.menuStyle(.borderlessButton).fixedSize().disabled(
          store.agents.filter(\.enabled).isEmpty || store.requestedReplyDrafts.contains(ask.id))
      }.font(.caption).foregroundStyle(.secondary)
      ActionFeedback(store: store, action: "ask-\(ask.id)")
    }.modifier(QuietRow()).onHover { hover = $0 }
      .accessibilityAction(named: "Done") { Task { await store.completeAsk(ask) } }
  }
}
extension AppStore {
  func relativeTime(_ value: String) -> String {
    guard let date = Timestamp.date(value) else { return "Recently" }
    let elapsed = max(0, (presentationNow).timeIntervalSince(date))
    if elapsed < 60 { return "now" }
    if elapsed < 3600 { return "\(Int(elapsed / 60)) min" }
    if elapsed < 86400 { return "\(Int(elapsed / 3600)) hr" }
    return "\(Int(elapsed / 86400)) d"
  }
}
struct CircleProposalCard: View {
  @Bindable var store: AppStore
  let proposal: CircleProposal
  @State private var editing = false
  @State private var name = ""
  var body: some View {
    VStack(alignment: .leading, spacing: 0) {
      VStack(alignment: .leading, spacing: 16) {
        Label(proposal.circle.name, systemImage: proposal.circle.displaySymbol).font(.headline)
        Text(proposal.why).font(.callout).foregroundStyle(.secondary)
        Text("\(proposal.circle.chats.count) chats · \(store.proposalActivity(proposal))").font(
          .caption
        ).foregroundStyle(.secondary)
        if editing { TextField("Circle name", text: $name).textFieldStyle(.roundedBorder) }
        HStack(spacing: 16) {
          Button("Keep") {
            Task { await store.keepProposal(proposal.id, name: editing ? name : nil) }
          }.buttonStyle(NativeGlassButtonStyle()).disabled(
            store.busy || (editing && name.trimmingCharacters(in: .whitespaces).isEmpty))
          Button(editing ? "Cancel edit" : "Edit") {
            name = proposal.circle.name
            editing.toggle()
          }.buttonStyle(.bordered)
          Button("Dismiss") { Task { await store.circleAction(proposal.id, method: "DELETE") } }
            .buttonStyle(.bordered).foregroundStyle(.secondary)
        }
        ActionFeedback(store: store, action: "circle-\(proposal.id)")
      }
    }
  }
}
extension AppStore {
  func scopeSummary(_ agent: Agent) -> String {
    if let access = agent.access ?? agent.permissions?.access { return accessSummary(access) }
    if let summary = agent.accessSummary { return summary }
    guard let permission = agent.permissions else { return "Choose chats" }
    guard permission.readMessages || permission.search else {
      return permission.readPeople ? "People only" : "No chat access"
    }
    let available = chats.filter { !settings.hiddenChats.contains($0.jid) }
    let count = available.filter { chat in
      switch permission.chats.mode {
      case "only": permission.chats.jids.contains(chat.jid)
      case "all_except": !permission.chats.jids.contains(chat.jid)
      default: true
      }
    }.count
    return "Can see \(count) chat\(count == 1 ? "" : "s")"
  }

  func lastSeen(_ agent: Agent) -> String {
    if let state = AIIdentity.syncState(agent, sync: connectionSync) { return state }
    guard agent.enabled else { return "Paused" }
    guard let at = agent.lastSeenAt.flatMap(Timestamp.date) else { return "Hasn’t connected yet" }
    let now = presentationNow
    let seconds = max(0, now.timeIntervalSince(at))
    if seconds < 300 { return "Active now" }
    if seconds < 3600 { return "\(Int(seconds / 60))m ago" }
    if seconds < 86400 { return "\(Int(seconds / 3600))h ago" }
    return "\(Int(seconds / 86400))d ago"
  }
}
