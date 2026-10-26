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
  private var greeting: String {
    HomePresentation.greeting(at: store.demo ? Timestamp.date(DemoData.now)! : Date())
  }
  var body: some View {
    Group {
      if store.status?.linked == false || (store.status == nil && !store.isLoading("/v1/status")) {
        VStack(spacing: 24) {
          Image(systemName: "bubble.left.and.text.bubble.right").font(.largeTitle).foregroundStyle(
            .secondary)
          Text("Link WhatsApp to get started").font(.title2.weight(.semibold))
          Button("Link WhatsApp") {
            store.setupStep = 1
            store.showSetup = true
          }.buttonStyle(.borderedProminent)
          Button("Explore with sample data") { Task { await store.useSampleData(true) } }
            .limmrButton()
          if let error = store.refreshError {
            Text(error).font(.callout).foregroundStyle(AccessibleSecondary())
            Button("Try again") { Task { await store.reconnect() } }
          }
        }.frame(maxWidth: .infinity, maxHeight: .infinity)
      } else {
        ScrollViewReader { proxy in
          GeometryReader { geometry in
            let wide = HomePresentation.usesRecentColumn(windowWidth: windowWidth, contentWidth: geometry.size.width)
            let mainWidth = geometry.size.width - (wide ? 328 : 0) - 48
            HStack(alignment: .top, spacing: 24) {
              ScrollView {
                VStack(alignment: .leading, spacing: 24) {
                  if let setup = store.setupStatus {
                    SetupHomeCard(store: store, setup: setup)
                  }
                  ForEach(
                    HomePresentation.mainSections(store.home).filter {
                      $0 != "changes" && $0 != "integrations" && (snapshotSection == nil || $0 == snapshotSection)
                    }, id: \.self
                  ) { section in
                    block(section, mainWidth: mainWidth).id(section)
                  }
                  if !wide { RecentWidget(store: store) }
                  if !wide, HomePresentation.hasChanges(store.home),
                    snapshotSection == nil || snapshotSection == "changes" { block("changes") }
                  if !wide, snapshotSection == nil || snapshotSection == "integrations" { block("integrations") }
                }
                .padding(24).frame(maxWidth: .infinity)
              }
              if wide {
                RecentWidget(store: store, includesCaptions: true).frame(width: 280).padding(.top, 32).padding(
                  .trailing, 24)
              }
            }
          }
          .onChange(of: store.searchRequest) { _, _ in proxy.scrollTo("greeting", anchor: .top) }
          .onChange(of: store.selectedApproval) { _, id in
            if id != nil { withAnimation(reduceMotion ? nil : .default) { proxy.scrollTo("queued", anchor: .top) } }
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
    .onAppear { HomeQuietSnapshot.prepare(store: store) }
    .onChange(of: store.escapeRequest) { _, _ in showAllAsks = false }
    .sheet(isPresented: $showAllAsks) { ReplyAsksSheet(store: store).dismissOnEscape() }
    .sheet(isPresented: $store.showProposalReview) { ProposalReview(store: store).dismissOnEscape() }
  }
  private var greetingHeader: some View {
    VStack(alignment: .leading, spacing: 8) {
      Text(greeting).font(AppType.pageTitle)
      if let error = store.refreshError {
        Text(error).font(.callout).foregroundStyle(AccessibleSecondary())
        Button("Try again") { Task { await store.reconnect() } }
      }
    }
    .frame(maxWidth: .infinity, alignment: .leading)
  }
  @ViewBuilder private func block(_ section: String, mainWidth: Double = 600) -> some View {
    switch section {
    case "greeting": greetingHeader
    case "search": HomeSearchView(store: store).frame(width: HomePresentation.searchWidth(mainWidth: mainWidth))
    case "dashboard":
      DashboardView(store: store, contentWidth: mainWidth - 32, fullWidth: true)
      if store.home.waiting.isEmpty && !store.home.needsReply.isEmpty {
        DisclosureGroup("Needs a reply (\(store.home.needsReply.count))") { NeedsReplyCard(store: store).padding(.top, 8) }
          .font(.callout).foregroundStyle(AccessibleSecondary())
      }
    case "triggers":
      DisclosureGroup("Waiting for a trigger") { TriggerWaitingCard(store: store).padding(.top, 8) }
        .font(.callout).foregroundStyle(AccessibleSecondary())
    case "tasks": TasksTile(store: store)
    case "queued":
      VStack(alignment: .leading, spacing: 8) {
        CountLabel(value: store.home.queued.count, title: "Queued to send")
        ForEach(store.home.queued) { item in
          QueuedDraftRow(store: store, item: item)
          if item.id != store.home.queued.last?.id { Divider() }
        }
      }
    case "circles":
      CircleUpdatesView(store: store, contentWidth: mainWidth)
    case "needs_reply":
      VStack(alignment: .leading, spacing: 8) {
        BlockHeading(title: "Needs a reply")
        ForEach(store.home.needsReply.prefix(3)) { ask in ReplyAskRow(store: store, ask: ask) }
        if store.home.needsReply.count > 3 {
          Button("\(store.home.needsReply.count - 3) more") { showAllAsks = true }.buttonStyle(
            .plain
          ).font(AppType.caption)
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
            store.home, now: store.demo ? Timestamp.date(DemoData.now)! : Date()))
            .lineLimit(1)
          Text("· Review in Activity").fixedSize()
          Spacer(minLength: 0)
        }
      }.limmrButton().font(AppType.caption).foregroundStyle(AccessibleSecondary())
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
        }.limmrButton()
      }.font(AppType.caption).foregroundStyle(AccessibleSecondary()) }
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
    Text(title).font(AppType.blockHeading).foregroundStyle(AccessibleSecondary())
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
          Text("\(ask.from ?? ask.chat ?? "Someone") · \(ask.circle)").font(AppType.rowTitle)
          Text("“\(ask.text)”").font(.callout).foregroundStyle(AccessibleSecondary()).lineLimit(2)
          Text(ask.at.map { "Waiting \(store.relativeTime($0))" } ?? "Waiting time unavailable")
            .font(AppType.caption).foregroundStyle(AccessibleSecondary())
        }
        Spacer(minLength: 0)
        Button("Mark done") { Task { await store.completeAsk(ask) } }.limmrButton()
      }
      HStack(spacing: 16) {
        if let url = HomePresentation.whatsappURL(jid: ask.chatJid) {
          Button("Open in WhatsApp") { openURL(url) }.limmrButton()
        }
        Menu(store.requestedReplyDrafts.contains(ask.id) ? "Draft requested" : "Ask an AI to draft")
        {
          ForEach(store.agents.filter(\.enabled)) { agent in
            Button(agent.name) { Task { await store.requestReplyDraft(ask, agent: agent) } }
          }
        }.menuStyle(.borderlessButton).fixedSize().disabled(
          store.agents.filter(\.enabled).isEmpty || store.requestedReplyDrafts.contains(ask.id))
      }.font(AppType.caption).foregroundStyle(AccessibleSecondary())
      ActionFeedback(store: store, action: "ask-\(ask.id)")
    }.modifier(QuietRow()).onHover { hover = $0 }
      .accessibilityAction(named: "Done") { Task { await store.completeAsk(ask) } }
  }
}
extension AppStore {
  func relativeTime(_ value: String) -> String {
    guard let date = Timestamp.date(value) else { return "Recently" }
    let elapsed = max(0, (demo ? Timestamp.date(DemoData.now)! : Date()).timeIntervalSince(date))
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
        Label(proposal.circle.name, systemImage: proposal.circle.symbol).font(AppType.rowTitle)
        Text(proposal.why).font(.callout).foregroundStyle(AccessibleSecondary())
        Text("\(proposal.circle.chats.count) chats · \(store.proposalActivity(proposal))").font(
          .caption
        ).foregroundStyle(AccessibleSecondary())
        if editing { TextField("Circle name", text: $name).textFieldStyle(.roundedBorder) }
        HStack(spacing: 16) {
          Button("Keep") {
            Task { await store.keepProposal(proposal.id, name: editing ? name : nil) }
          }.buttonStyle(NativeGlassButtonStyle()).disabled(
            store.busy || (editing && name.trimmingCharacters(in: .whitespaces).isEmpty))
          Button(editing ? "Cancel edit" : "Edit") {
            name = proposal.circle.name
            editing.toggle()
          }.limmrButton()
          Button("Dismiss") { Task { await store.circleAction(proposal.id, method: "DELETE") } }
            .limmrButton().foregroundStyle(AccessibleSecondary())
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
    guard agent.enabled else { return "Paused" }
    guard let at = agent.lastSeenAt.flatMap(Timestamp.date) else { return "Hasn’t connected yet" }
    let now = demo ? Timestamp.date(DemoData.now)! : Date()
    let seconds = max(0, now.timeIntervalSince(at))
    if seconds < 300 { return "Active now" }
    if seconds < 3600 { return "\(Int(seconds / 60))m ago" }
    if seconds < 86400 { return "\(Int(seconds / 3600))h ago" }
    return "\(Int(seconds / 86400))d ago"
  }
}
