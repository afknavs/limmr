import AppKit
import LimmrKit
import SwiftUI

@MainActor enum SnapshotRunner {
  static let pages = [
    "z2-profile-full", "z2-profile-minimal", "z2-profile-hidden", "z2-profile-menu", "z2-profile-edit",
    "z2-task-latest", "z2-task-surfaced", "z2-task-history", "z2-effort-status", "z2-sync-unpaired", "z2-sync-paired", "z2-accessory",
    "y7-dot-never", "y7-dot-healthy", "y7-dot-error", "y7-dot-review",
    "y2-heatmap-hover", "y2-heatmap-day",
    "y2-home-circles", "y2-home-open", "y2-home-suggestions", "y2-activity-suggested", "y2-home-kept", "y2-home-dismissed", "y2-home-undone",
    "y45-groups", "y45-group", "y45-custom", "y45-initials", "y45-contacts", "y45-lately",
    "y45-hide-person", "y45-hide-group", "y45-hide-no-dm",
    "y1-task-open", "y1-navigation-stress",
    "q4-task-study", "q4-task-short", "q4-task-waiting", "q4-activity", "q4-home", "q4-accessory", "q4-drafts", "q4-effort",
    "n1-naming-activity", "n1-naming-unavailable",
    "a6-unnamed-activity", "a6-named-effort",
    "o1-helper-registering", "o1-link-error", "o1-empty-linked-home", "o1-empty-linked-circles",
    "o1-empty-linked-activity",
    "replies-sheet", "brief-version",
    "people-focus", "people-loading", "settings-people", "approval", "chat-picker", "effort-sheet",
    "activity-detail", "circle-changes", "update-notes", "empty-people", "empty-activity", "empty-log",
    "empty-integrations", "empty-menubar", "setup-phone",
    "h2-home", "h2-empty-triggers", "h2-odd", "h2-effort", "h2-chat",
    "e1-activity", "e1-search", "e1-detail", "e1-log", "e1-home",
    "u5-home", "u5-home-quiet", "home-structure", "home-structure-review", "u4-home", "u4-dashboard", "u4-circles", "u4-replies", "u4-activity", "u4-activity-expanded",
    "u4-activity-log", "u4-icon-picker", "u4-settings-search",
    "people-dense", "people-dense-hover", "people-circle-hover", "people-counts", "person-inferred",
    "engine-updating", "agent-renaming", "agent-rename-error", "agent-renamed",
    "activity-sends", "activity-reads", "home-dashboard", "home-waiting-0", "home-waiting-1",
    "home-waiting-4", "n1-home", "about-you", "setup-about-you", "person-you",
    "settings-advanced-reset", "people-current", "remote-approval-work", "setup-who", "home-setup",
    "proposals-review", "remote-approval", "integration-everything", "integration-work",
    "integration-ended", "connect-claude-code", "settings-briefs",
    "settings-sources", "settings-integrations-access", "people-fill", "person-full",
    "person-review", "person-forget", "person-add",
    "people-circles", "people-people", "people-map-hover", "people-replay", "settings-general",
    "home-today", "home-watch",
    "home-suggested", "home-proposals", "circles", "circle", "person",
    "settings-integrations", "integration-detail", "home", "overview", "empty-home", "people",
    "agents", "agent-detail", "permissions", "activity",
    "setup-welcome", "setup-link", "setup-qr", "setup-qr-long", "setup-syncing", "setup-connect",
    "setup-done",
    "setup-helper-error", "connect-picker", "connect-chatgpt", "connect-codex", "menubar",
    "settings", "settings-hidden", "settings-remote", "settings-health", "settings-advanced",
    "s2-sidebar", "s2-settings", "s2-about-settings", "s2-about-people", "s2-hidden-people", "s2-hidden-groups",
    "remote",
  ]
  static func run(arguments: [String]) async {
    func argument(_ name: String) -> String? {
      guard let i = arguments.firstIndex(of: name), arguments.indices.contains(i + 1) else {
        return nil
      }
      return arguments[i + 1]
    }
      guard let path = argument("--snapshot"), let page = argument("--page"),
        pages.contains(page) || o1State(page) != nil
    else {
      fputs(
        "Usage: Limmr --snapshot <out.png> --page <page> [--titled-window] [--dark] [--increase-contrast] [--focus-control <id>] [--size 1440x900]\n", stderr)
      exit(2)
    }
    let dimensions = (argument("--size") ?? "1100x740").split(separator: "x").compactMap {
      Double($0)
    }
    guard dimensions.count == 2, dimensions[0] >= WindowMetrics.minimumWidth,
      dimensions[1] >= WindowMetrics.minimumHeight, dimensions.allSatisfy({ $0 <= 4096 })
    else {
      fputs("Snapshot size must be between 760x520 and 4096x4096.\n", stderr)
      exit(2)
    }
    let waitingCount =
      page.hasPrefix("home-waiting-") ? Int(page.split(separator: "-").last ?? "") : nil
    let backend: (any Backend)?
    if page.hasPrefix("y7-dot-") {
      backend = DemoBackend(dotState: String(page.dropFirst("y7-dot-".count)))
    } else if page == "a6-unnamed-activity" {
      backend = DemoBackend(unnamedEffortsOnly: true)
    } else if arguments.contains("--a5-long-names") || arguments.contains("--a5-empty-sections") {
      backend = A5SnapshotBackend(emptySections: arguments.contains("--a5-empty-sections"))
    } else if page.hasPrefix("h2-") {
      backend = H2SnapshotBackend(odd: page == "h2-odd", emptyTriggers: page == "h2-empty-triggers")
    } else if page.hasPrefix("e1-") {
      backend = DemoBackend(structurePlacements: true)
    } else if page == "people-counts" || page == "people-circle-hover" {
      backend = P10SnapshotBackend(hover: page == "people-circle-hover")
    } else if page.hasPrefix("people-dense") || page == "person-inferred" {
      backend = PeopleSnapshotBackend(dense: page.hasPrefix("people-dense"))
    } else if let directory = argument("--perf-fixture") {
      backend = P1FixtureBackend(directory: directory)
    } else if page.hasPrefix("home-structure") || page.hasPrefix("u5-home") {
      backend = DemoBackend(structurePlacements: true)
    } else if let waitingCount {
      backend = DemoBackend(n1ReadyShape: true, waitingCount: waitingCount)
    } else if page == "n1-home" {
      backend = DemoBackend(n1ReadyShape: true)
    } else if page.hasPrefix("z2-profile-") || page == "z2-task-surfaced" || page == "z2-task-latest" {
      backend = Z2SnapshotBackend(minimal: page == "z2-profile-minimal", taskPreview: page == "z2-task-surfaced", taskLatest: page == "z2-task-latest")
    } else if page.hasPrefix("o1-empty-") {
      backend = O1EmptyAccountBackend()
    } else if page.hasPrefix("empty-") || page == "setup-helper-error" {
      backend = EmptyBackend()
    } else { backend = nil }
    let store = AppStore(
      demo: !page.hasPrefix("empty-") && !page.hasPrefix("o1-empty-") && page != "setup-helper-error", snapshot: true, backend: backend)
    await store.refresh()
    if page.hasPrefix("h2-") { await store.refreshHomeEfforts() }
    let dark = arguments.contains("--dark")
    NSApp.appearance = NSAppearance(named: dark ? .darkAqua : .aqua)
    var content: AnyView
    switch page {
    case "z2-profile-full", "z2-profile-minimal", "z2-profile-hidden", "z2-profile-menu", "z2-profile-edit":
      store.selectPage(.circles)
      store.selectedPerson = "person-04"
      await store.loadPerson()
      for circle in store.circles { await store.loadCircle(circle.id) }
      if page == "z2-profile-hidden", let person = store.profile?.person { store.settings.hiddenChats = person.jids }
      if page == "z2-profile-edit", let person = store.profile?.person {
        content = AnyView(snapshotStage { PersonDetailsEditor(store: store, person: person) })
      } else {
        content = AnyView(CirclePersonPage(store: store, id: "person-04", snapshotMenu: page == "z2-profile-menu"))
      }
    case "z2-task-latest", "z2-task-surfaced", "z2-task-history":
      content = AnyView(TaskPage(store: store, id: page != "z2-task-history" ? "tsk_cv" : "tsk_study", startAtHistory: page == "z2-task-history"))
    case "z2-effort-status":
      await store.refreshHomeEfforts()
      _ = await store.setEffortStatus("effort-slides", from: "on_you", to: "moving")
      let detail: EffortDetailResponse = (try? await store.backend.get("/v1/efforts/effort-slides")) ?? DemoEfforts.detail(DemoEfforts.efforts[0])
      content = AnyView(EffortDetailView(store: store, detail: detail))
    case "z2-sync-unpaired", "z2-sync-paired":
      _ = try? await store.backend.request("/v1/sync/pair", method: page == "z2-sync-unpaired" ? "DELETE" : "POST", body: nil)
      content = AnyView(Form { YourDotSection(store: store) }.formStyle(.grouped).background { AppBackground() })
    case "z2-accessory":
      store.settings.accessoryCue.enabled = true
      content = AnyView(Form { AccessoryCueSettings(store: store) }.formStyle(.grouped).background { AppBackground() })
    case "y2-home-circles", "y2-home-open", "y2-home-suggestions", "y2-activity-suggested", "y2-home-kept", "y2-home-dismissed", "y2-home-undone":
      store.selectPage(.overview)
      if page == "y2-home-open" {
        _ = await store.surfaceTask("tsk_cv", action: .dismiss)
        _ = await store.surfaceTask("tsk_print", action: .dismiss)
      } else if page == "y2-home-kept" { _ = await store.surfaceTask("tsk_cv", action: .keep) }
      else if page == "y2-home-dismissed" { _ = await store.surfaceTask("tsk_cv", action: .dismiss) }
      else if page == "y2-home-undone" { _ = await store.surfaceTask("tsk_appointment", action: .undo) }
      if page == "y2-activity-suggested" { store.selectPage(.activity) }
      content = AnyView(RootView(store: store))
    case "y2-heatmap-hover":
      guard let counts = try? JSONDecoder().decode(HistoryAgent.self, from: Data(#"{"reads":5,"sends":3}"#.utf8)) else { exit(1) }
      let day = HistoryDay(date: "2026-10-21", messagesIn: 48, agentSends: 3, agentReads: 5,
        agents: [DemoData.agents[0].id: counts])
      let label = HomePresentation.dayLabel(day, agents: DemoData.agents)
      guard label.hasPrefix("Wed 21 Oct · 48 messages · 3 AI sends"),
        label.contains(DemoData.agents[0].name + ": 5 reads, 3 sends") else { exit(1) }
      content = AnyView(VStack(alignment: .leading, spacing: 24) {
        Text("Message history").font(.title2.bold())
        HeatmapView(history: StatsHistory(days: [day]), agents: false, weeks: 4, ai: DemoData.agents,
          width: 420, openDay: store.openActivityDay)
        Text("Hover description · synthetic verification").font(.headline)
        Text(label).font(.callout)
      }.padding(32).frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading).pageSurface())
      print("Y2: heatmap help/accessibility label contains date, message count, AI totals and per-AI counts.")
    case "y2-heatmap-day":
      content = AnyView(RootView(store: store))
    case "y45-hide-person", "y45-hide-no-dm":
      if let person = store.people.first(where: { $0.id == "person-04" }) {
        if page == "y45-hide-no-dm" { store.chats.removeAll { person.jids.contains($0.jid) } }
        else {
          store.hiddenChatsBinding(for: store.directChatJIDs(for: person)).wrappedValue = true
          await store.waitForSettingsSave()
        }
      }
      content = AnyView(CirclePersonPage(store: store, id: "person-04"))
    case "y45-hide-group":
      store.hiddenChatsBinding(for: [DemoData.groupJid(4)]).wrappedValue = true
      await store.waitForSettingsSave()
      content = AnyView(GroupPage(store: store, jid: DemoData.groupJid(4)))
    case "y45-groups": content = AnyView(CirclesView(store: store).environment(\.snapshotPeopleTab, "Groups").environment(\.snapshotPeopleList, true))
    case "y45-group": content = AnyView(GroupPage(store: store, jid: DemoData.groupJid(4)))
    case "y45-custom", "y45-initials", "y45-contacts", "y45-lately":
      content = AnyView(CirclePersonPage(store: store, id: page == "y45-initials" ? "person-06" : page == "y45-contacts" ? "person-05" : "person-04"))
    case "y1-task-open", "y1-navigation-stress":
      content = AnyView(RootView(store: store))
    case "q4-task-study", "q4-task-short", "q4-task-waiting":
      let index = page == "q4-task-short" ? 1 : page == "q4-task-waiting" ? 2 : 0
      content = AnyView(ScrollView {
        TaskWorkspaceView(store: store, id: DemoTasks.tasks[index].id, initial: DemoTasks.tasks[index])
      }.background { AppBackground() })
    case "q4-activity":
      content = AnyView(ActivityView(store: store).background { AppBackground() })
    case "q4-home":
      content = AnyView(ScrollView {
        VStack(alignment: .leading, spacing: 28) {
          Text("Home").font(AppType.pageTitle)
          TasksTile(store: store)
        }.frame(maxWidth: 760).padding(28).frame(maxWidth: .infinity)
      }.background { AppBackground() })
    case "q4-drafts":
      content = AnyView(ScrollView {
        VStack(alignment: .leading, spacing: 16) {
          Text("Queued to send").font(AppType.pageTitle)
          ForEach(store.home.queued.filter { $0.taskId != nil }) { QueuedDraftRow(store: store, item: $0) }
        }.frame(maxWidth: 760).padding(28).frame(maxWidth: .infinity)
      }.background { AppBackground() })
    case "q4-accessory":
      content = AnyView(Form { AccessoryCueSettings(store: store) }.formStyle(.grouped)
        .frame(maxWidth: 760).frame(maxWidth: .infinity).background { AppBackground() })
    case "q4-effort":
      var detail = DemoEfforts.detail(DemoEfforts.efforts[0])
      detail.taskId = "tsk_syllabus"
      content = AnyView(EffortDetailView(store: store, detail: detail).background { AppBackground() })
    case "n1-naming-activity", "n1-naming-unavailable":
      let unavailable = page == "n1-naming-unavailable"
      let namer = store.makeEffortNamer(environment: .init(
        unavailableReason: { unavailable ? "Turn on Apple Intelligence in System Settings to name work on This Mac." : nil },
        paused: { false }))
      namer.enabled = true; namer.ready = true
      if unavailable {
        await namer.step()
      } else {
        if var queue: EffortNamingResponse = try? await store.backend.get("/v1/efforts/unnamed"),
           let sample = queue.efforts.first {
          queue.efforts = (1...12).map {
            var candidate = sample; candidate.id = "sample-\($0)"; return candidate
          }
          queue.total = queue.efforts.count
          namer.updateQueue(queue)
        }
      }
      store.effortNamer = namer
      store.setupStatus = nil
      store.selectPage(.activity)
      content = AnyView(RootView(store: store))
    case "a6-unnamed-activity":
      store.setupStatus = nil
      store.selectPage(.activity)
      content = AnyView(RootView(store: store))
    case "a6-named-effort":
      let queue: EffortNamingResponse? = try? await store.backend.get("/v1/efforts/unnamed")
      if let candidate = queue?.efforts.first {
        try? await OnDeviceEffortWriter(backend: store.backend, model: DemoEffortNamingModel(),
          session: store.effortNamingSession).name(candidate)
        store.openEffort(candidate.id)
      }
      store.setupStatus = nil
      content = AnyView(RootView(store: store))
    case let page where o1State(page) != nil:
      let state = o1State(page)!
      if page.hasPrefix("o1-menu-") {
        content = AnyView(snapshotStage { MenuBarView(store: store, stateOverride: state) })
      } else {
        content = AnyView(EngineStatusBanner(state: state) {}.frame(maxWidth: 650).padding(24)
          .frame(maxWidth: .infinity, maxHeight: .infinity).background { AppBackground() })
      }
    case "o1-helper-registering":
      content = AnyView(SetupView(store: store, initialStep: 1, initialHelperPreparing: true))
    case "o1-link-error":
      store.link = LinkEvent(state: "error")
      store.link.reason = "Pairing was interrupted. Try linking again."
      content = AnyView(SetupView(store: store, initialStep: 1))
    case "o1-empty-linked-home", "o1-empty-linked-circles", "o1-empty-linked-activity":
      store.setupStatus = nil
      store.selectPage(page.hasSuffix("circles") ? .circles : page.hasSuffix("activity") ? .activity : .overview)
      content = AnyView(RootView(store: store))
    case "replies-sheet": content = AnyView(snapshotStage { ReplyAsksSheet(store: store) })
    case "brief-version":
      await store.loadCircle("atlas")
      store.selectPage(.circles)
      store.circlePath = [.brief("atlas", 2)]
      content = AnyView(RootView(store: store))
    case "approval":
      content = AnyView(snapshotStage {
        ScrollView { ForEach(store.pending) { ApprovalCard(store: store, approval: $0) } }
          .frame(width: 620, height: 420).padding(20).background { AppBackground() }
      })
    case "chat-picker": content = AnyView(snapshotStage { ChatPicker(store: store) })
    case "effort-sheet": content = AnyView(snapshotStage { EffortSheet(store: store, id: DemoEfforts.efforts[0].id) })
    case "circle-changes": content = AnyView(snapshotStage { CircleChangesList(store: store) })
    case "update-notes": content = AnyView(snapshotStage { UpdateNotesSheet(store: store, updates: UpdateController(sample: true)) })
    case "activity-detail":
      store.selectedActivity = store.activity.first?.id
      content = AnyView(ActivityDetail(store: store).frame(maxWidth: 680).padding(28)
        .frame(maxWidth: .infinity, maxHeight: .infinity).background { AppBackground() })
    case "settings-people":
      store.settingsTab = .people
      content = AnyView(SettingsView(store: store))
    case "empty-integrations":
      store.settingsTab = .integrations
      content = AnyView(SettingsView(store: store))
    case "empty-menubar": content = AnyView(snapshotStage { MenuBarView(store: store) })
    case "empty-activity", "empty-log":
      content = AnyView(ActivityView(store: store, detailedLog: page == "empty-log").background { AppBackground() })
    case "empty-people", "people-focus", "people-loading":
      store.showSetup = false
      store.selectPage(.circles)
      if page == "people-focus" { store.mapSelection.circle = "university" }
      content = AnyView(RootView(store: store))
    case "setup-phone":
      store.link = LinkEvent(state: "pair_code")
      store.link.code = "ABCD1234"
      content = AnyView(SetupView(store: store, initialStep: 1, initialPhoneMode: true))
    case "h2-home", "h2-empty-triggers", "h2-odd", "h2-effort", "h2-chat":
      store.setupStatus = nil
      if page == "h2-effort" { store.openEffort("effort-slides") }
      else if page == "h2-chat" {
        store.selectPage(.circles)
        store.circlePath = [.chat(DemoData.groupJid(2), "University class")]
      } else { store.selectPage(.overview) }
      content = AnyView(RootView(store: store))
    case "e1-activity", "e1-search", "e1-log":
      content = AnyView(ActivityView(store: store, detailedLog: page == "e1-log", query: page == "e1-search" ? "Maya" : "").background { AppBackground() })
    case "e1-detail":
      content = AnyView(EffortDetailView(store: store, detail: DemoEfforts.detail(DemoEfforts.efforts[0])).background { AppBackground() })
    case "e1-home":
      content = AnyView(ScrollView { CircleUpdatesView(store: store).frame(maxWidth: 820).padding(32).frame(maxWidth: .infinity) }.background { AppBackground() })
    case "u5-home", "u5-home-quiet":
      store.setupStatus = nil
      store.selectPage(.overview)
      if page == "u5-home-quiet" {
        store.allRecentActivity = []
        store.home.recentActivity = []
        store.dashboard?.recentSends = []
      }
      // Dense invented Home, including the self-chat and read noise U5 must suppress.
      if var card = store.home.today.first(where: { $0.circle.kind == "self" }) {
        card.circle.pinned = false
        card.pinned = false
        card.newMessages = 120
        card.openAsks = 8
        store.home.circles.append(card)
      }
      for (index, tool) in ["list_messages", "search_messages", "list_chats", "list_groups"].enumerated() {
        var row = DemoData.activity[0]
        row.id = Int64(9000 + index)
        row.at = DemoData.now
        row.agentId = "agent-2"
        row.tool = tool
        row.kind = "read"
        row.chatJids = []
        row.messageIds = []
        row.count = tool == "list_chats" || tool == "list_groups" ? 200 : 0
        row.note = nil
        store.allRecentActivity.append(row)
      }
      content = AnyView(RootView(store: store))
    case "home-structure", "home-structure-review":
      store.setupStatus = nil
      store.selectPage(.overview)
      if page == "home-structure-review", let change = store.home.circleChanges.first {
        content = AnyView(snapshotStage { CircleChangeReview(store: store, change: change) })
      } else {
        content = AnyView(RootView(store: store))
      }
    case "u4-activity", "u4-activity-expanded", "u4-activity-log":
      store.activityFilter = "all"
      content = AnyView(
        ActivityView(
          store: store, detailedLog: page == "u4-activity-log"
        ).background { AppBackground() })
    case "u4-icon-picker":
      content = AnyView(
        snapshotStage {
          IconPickerView(recent: [
            .init(kind: "emoji", value: "🎓"), .init(kind: "symbol", value: "book"),
          ]) { _ in }
        })
    case "u4-settings-search":
      store.settingsQuery = "retention"
      store.settingsSearchTarget = "Keep activity for"
      store.settingsTab = .general
      content = AnyView(SettingsView(store: store))
    case "u4-dashboard":
      content = AnyView(snapshotStage { DashboardView(store: store).frame(width: 704) })
    case "u4-home", "u4-circles", "u4-replies":
      store.selectPage(.overview)
      store.setupStatus = nil
      content = AnyView(RootView(store: store))

    case "engine-updating":
      content = AnyView(EngineStartupView(phase: .updating) {})
    case "agent-renaming", "agent-rename-error", "agent-renamed":
      store.selectedAgent = "agent-2"
      await store.loadPermissions()
      let editor = AgentNameEditor()
      if page == "agent-renamed" {
        try? await store.renameAgent("agent-2", name: "Studio AI")
      } else {
        editor.begin("Studio AI")
        if page == "agent-rename-error" {
          await editor.save { _ in throw BackendError.http(404) }
        }
      }
      content = AnyView(
        AgentDetail(store: store, agent: store.currentAgent!, narrow: true, nameEditor: editor)
          .padding(28).frame(maxWidth: .infinity, maxHeight: .infinity).background {
            AppBackground()
          })
    case "s2-sidebar":
      store.selectPage(.circles)
      store.setupStatus = nil
      content = AnyView(RootView(store: store))
    case "s2-settings":
      content = AnyView(SettingsView(store: store))
    case "s2-about-settings":
      if let id = store.aboutYouCircle?.id { await store.loadCircle(id) }
      content = AnyView(SettingsView(store: store))
    case "s2-about-people":
      if let id = store.aboutYouCircle?.id { await store.loadCircle(id) }
      content = AnyView(CirclePersonPage(store: store, id: "me"))
    case "s2-hidden-people", "s2-hidden-groups":
      store.settingsTab = .hiddenChats
      content = AnyView(SettingsView(store: store, hiddenKind: page == "s2-hidden-groups" ? .groups : .people))
    case "about-you":
      content = AnyView(
        ScrollView { AboutYouCard(store: store).frame(maxWidth: 680).padding(28) }.frame(
          maxWidth: .infinity, maxHeight: .infinity
        ).background { AppBackground() })
    case "setup-about-you":
      content = AnyView(SetupView(store: store, initialStep: 3, initialAbout: true))
    case "person-you": content = AnyView(CirclePersonPage(store: store, id: "me"))
    case "setup-welcome", "setup-link", "setup-qr", "setup-qr-long", "setup-syncing",
      "setup-who", "setup-connect", "setup-done",
      "setup-helper-error":
      let steps = [
        "setup-welcome": 0, "setup-link": 1, "setup-qr": 1, "setup-qr-long": 1, "setup-syncing": 2,
        "setup-who": 3, "setup-connect": 4, "setup-done": 5, "setup-helper-error": 1,
      ]
      if page == "setup-link" { store.migrationAvailable = true }
      if page == "setup-qr-long" {
        store.link = LinkEvent(state: "qr")
        store.link.code = DemoQRCode.longURL
      }
      content = AnyView(SetupView(store: store, initialStep: steps[page] ?? 0))
    case "settings-advanced-reset":
      store.settingsTab = .advanced
      content = AnyView(
        SettingsView(store: store).overlay {
          Color.black.opacity(0.18).overlay { ResetCirclesSheet(store: store) }
        })
    case "person-forget":
      content = AnyView(snapshotStage { ForgetPersonPrompt(store: store, id: "person-04") {} })
    case "person-add":
      content = AnyView(snapshotStage { AddProfileValue(title: "Email") { _, _ in } })
    case "people-fill": content = AnyView(snapshotStage { FillFromAI() })
    case "connect-picker": content = AnyView(snapshotStage { ConnectPicker(store: store) })
    case "proposals-review": content = AnyView(snapshotStage { ProposalReview(store: store) })
    case "connect-claude-code":
      content = AnyView(snapshotStage { ConnectView(store: store, kind: .claudeCode) })
    case "remote-approval-work":
      _ = try? await store.backend.request(
        "/v1/circles/internship", method: "PATCH", body: Wire.data(["name": "Work"]))
      await store.refresh()
      content = AnyView(
        snapshotStage {
          ConnectView(
            store: store, kind: .chatgpt,
            initialAccess: AgentAccess(
              circles: .only(["internship"]), until: "2026-11-17T12:00:00Z"))
        })
    case "remote-approval":
      content = AnyView(snapshotStage { ConnectView(store: store, kind: .chatgpt) })
    case "connect-chatgpt":
      store.remote?.pendingCodes = []
      content = AnyView(snapshotStage { ConnectView(store: store, kind: .chatgpt) })
    case "connect-codex":
      content = AnyView(snapshotStage { ConnectView(store: store, kind: .codex) })
    case "menubar": content = AnyView(snapshotStage { MenuBarView(store: store) })
    case "remote": content = AnyView(snapshotStage { RemoteWizard(store: store) })
    case "y7-dot-never", "y7-dot-healthy", "y7-dot-error", "y7-dot-review", "integration-everything", "integration-work", "integration-ended", "settings-briefs",
      "settings-sources", "settings-integrations-access", "settings-general",
      "settings-integrations", "integration-detail", "settings",
      "settings-hidden",
      "settings-remote", "settings-health", "settings-advanced":
      switch page {
      case "settings-briefs": store.settingsTab = .briefs
      case "integration-everything", "integration-work", "integration-ended":
        store.settingsTab = .integrations
        store.selectedAgent = "agent-3"
        _ = try? await store.backend.request(
          "/v1/circles/internship", method: "PATCH", body: Wire.data(["name": "Work"]))
        let access = AgentAccess(
          circles: page == "integration-everything" ? .all : .only(["internship"]),
          until: page == "integration-everything"
            ? nil : page == "integration-ended" ? "2026-10-16T12:00:00Z" : "2026-11-17T12:00:00Z")
        try? await store.backend.updateAccess(access, agentID: "agent-3")
        await store.refresh()
        await store.loadPermissions()
      case "settings-sources": store.settingsTab = .sources
      case "y7-dot-never", "y7-dot-healthy", "y7-dot-error", "y7-dot-review", "settings-integrations": store.settingsTab = .integrations
      case "integration-detail", "settings-integrations-access":
        store.settingsTab = .integrations
        store.selectedAgent = "agent-2"
        await store.loadPermissions()
      case "settings-hidden": store.settingsTab = .hiddenChats
      case "settings-remote": store.settingsTab = .remote
      case "settings-health": store.settingsTab = .health
      case "settings-advanced": store.settingsTab = .advanced
      default: store.settingsTab = .general
      }
      content = AnyView(SettingsView(store: store))
    default:
      switch page {
      case "people-circle-hover", "people-counts", "people-dense", "people-dense-hover", "people-current", "people-circles", "people-people", "people-map-hover", "people-replay",
        "people",
        "circles":
        store.selectPage(.circles)
        if let circle = argument("--select-circle") { store.mapSelection.circle = circle }
        for circle in store.circles { await store.loadCircle(circle.id) }
        if let selected = argument("--select") {
          store.selectedPerson = selected
          await store.loadPerson()
        } else {
          store.selectedPerson = ""
        }
      case "circle", "person", "person-full", "person-review", "person-inferred":
        store.selectPage(.circles)
        for circle in store.circles { await store.loadCircle(circle.id) }
        if page != "circle" {
          store.selectedPerson = "person-04"
          await store.loadPerson()
        }
        store.circlePath =
          page == "circle" ? [.circle(argument("--select") ?? "atlas")] : [.person("person-04")]
      case "agents":
        store.settingsTab = .integrations
        content = AnyView(SettingsView(store: store))
      case "agent-detail", "permissions":
        store.settingsTab = .integrations
        store.selectedAgent = "agent-2"
        await store.loadPermissions()
        store.permissions.chats = .init(
          mode: "only",
          jids: ["120363000001@g.us", "15550100001@s.whatsapp.net", "120363000004@g.us"])
        do {
          _ = try await store.backend.request(
            "/v1/agents/agent-2/permissions", method: "PUT", body: Wire.data(store.permissions))
          await store.refresh()
        } catch {
          fputs("Could not prepare demo permissions.\n", stderr)
          exit(1)
        }
      case "activity", "activity-sends", "activity-reads":
        store.selectPage(.activity)
        store.activityFilter = page == "activity-reads" ? "reads" : "sends"
        store.selectedActivity = nil
        await store.loadActivity()
      default: store.selectPage(.overview)
      }
      content =
        ["agents", "agent-detail", "permissions"].contains(page)
        ? AnyView(SettingsView(store: store)) : AnyView(RootView(store: store))
    }
    content = AnyView(
      content.preferredColorScheme(dark ? .dark : .light).environment(
        \.colorScheme, dark ? .dark : .light
      ).environment(\.snapshotRendering, argument("--perf-seconds") == nil).environment(
        \.snapshotOpenWatch, page == "home-watch" ? (argument("--watch") ?? "w_exam") : nil
      ).environment(
        \.snapshotExpandedOptions, page == "permissions"
      ).environment(
        \.snapshotHomeSection,
        page == "y2-home-suggestions" ? "tasks" : page == "y2-home-circles" ? "circles" : page == "u4-replies"
          ? "needs_reply"
          : page == "u4-circles"
            ? "circles"
            : page == "home-dashboard"
              ? "dashboard"
              : page == "home-today"
                ? "circles"
                : page == "home-suggested" ? "queued" : page == "home-proposals" ? "proposals" : nil
      ).environment(
        \.snapshotMapFrame,
        page == "people-circle-hover" ? "circle-hover" : (page == "people-map-hover" || page == "people-dense-hover") ? "hover" : page == "people-replay" ? "replay" : nil
      )
      .environment(\.snapshotProfileReview, page == "person-review")
      .environment(\.snapshotIntegrationAccess, page == "settings-integrations-access")
      .environment(\.snapshotOlderPeople, page == "people-current")
      .environment(\.snapshotPeopleList, page == "people-counts" || arguments.contains("--people-list"))
      .environment(
        \.snapshotPeopleTab,
        (page == "people-people" || page == "people-current") ? "People" : "Circles"
      )
      .environment(\.snapshotFocusedControl, argument("--focus-control"))
      .environment(\.snapshotIncreasedContrast, arguments.contains("--increase-contrast"))
      .environment(\.snapshotReducedMotion, arguments.contains("--reduce-motion"))
      .environment(\.controlActiveState, .key).tint(.blue))
    let size = NSSize(width: dimensions[0], height: dimensions[1])
    // Standalone People captures need the same surface as navigation destinations.
    if page.hasPrefix("y45-") { content = AnyView(content.pageSurface()) }
    let host = NSHostingView(rootView: content)
    // Snapshot dimensions are a contract, independent of a pane's ideal content size.
    host.sizingOptions = []
    host.appearance = NSAppearance(named: arguments.contains("--increase-contrast") ? (dark ? .accessibilityHighContrastDarkAqua : .accessibilityHighContrastAqua) : (dark ? .darkAqua : .aqua))
    host.wantsLayer = true
    host.frame = NSRect(origin: .zero, size: size)
    // S2 also captures native Settings chrome, offscreen, to verify the top-left controls.
    let settingsChrome = page == "s2-settings"
    let titledChrome = settingsChrome || arguments.contains("--titled-window")
    let window = NSWindow(
      contentRect: host.frame,
      styleMask: titledChrome ? [.titled, .closable, .miniaturizable, .resizable] : [.borderless],
      backing: .buffered, defer: false)
    if settingsChrome { window.title = "Settings" }
    if titledChrome && !settingsChrome {
      window.styleMask.insert(.fullSizeContentView)
      window.title = "Limmr"
      window.titleVisibility = .hidden
      window.toolbarStyle = .unified
      window.toolbar = NSToolbar(identifier: "Y1MainToolbar")
      MainWindowSurface.apply(to: window)
    }
    window.appearance = host.appearance
    if titledChrome {
      // A plain content container keeps NSHostingView from taking over native window sizing.
      host.sizingOptions = []
      let container = NSView(frame: NSRect(origin: .zero, size: size))
      host.autoresizingMask = [.width, .height]
      container.addSubview(host)
      window.contentView = container
    } else {
      window.contentView = host
    }
    window.contentMinSize = size
    window.contentMaxSize = size
    window.setContentSize(size)
    if titledChrome && !settingsChrome {
      window.setFrame(NSRect(origin: window.frame.origin, size: size), display: false)
    }
    // An offscreen window supplies AppKit environment and native control rendering without activating the app.
    window.setFrameOrigin(NSPoint(x: -10000, y: -10000))
    window.orderBack(nil)
    host.layoutSubtreeIfNeeded()
    try? await Task.sleep(for: .milliseconds(1400))
    func settleNavigation() async {
      for _ in 0..<2 {
        host.layoutSubtreeIfNeeded()
        try? await Task.sleep(for: .milliseconds(150))
      }
      host.layoutSubtreeIfNeeded()
    }
    if page == "y2-heatmap-day" {
      let day = String(DemoData.now.prefix(10))
      store.activityAgent = "old-agent"; store.activityFilter = "sends"
      store.openActivityDay(day) // The exact action bound to each heatmap Button.
      await settleNavigation()
      guard store.page == .activity, store.activityDay == day,
        store.activityAgent == "all", store.activityFilter == "all",
        store.activityPath.isEmpty, store.pendingActivityRoute == nil,
        ActivityDayFilter.matches(day + "T12:00:00Z", day: day, timeZone: TimeZone(secondsFromGMT: 0)!),
        !ActivityDayFilter.matches("2001-01-01T12:00:00Z", day: day, timeZone: TimeZone(secondsFromGMT: 0)!)
      else { fputs("Y2 day navigation/filter failed.\n", stderr); exit(1) }
      print("Y2: heatmap action opened Activity for the selected day, reset agent/type filters and retained day filtering.")
    }
    if page == "y1-task-open" || page == "y1-navigation-stress" {
      for _ in 0..<(page == "y1-navigation-stress" ? 20 : 1) {
        store.selectPage(.overview)
        await settleNavigation()
        store.openTask("tsk_study")
        await settleNavigation()
        guard store.activityPath == [.task("tsk_study")], store.pendingActivityRoute == nil else {
          fputs("Y1 mounted task destination did not resolve: \(store.activityPath), pending \(String(describing: store.pendingActivityRoute)), ready \(store.activityNavigationReady).\n", stderr); exit(1)
        }
        if page == "y1-navigation-stress" {
          store.route(.back)
          host.layoutSubtreeIfNeeded()
          store.selectPage(.overview)
          try? await Task.sleep(for: .milliseconds(50))
          guard store.activityPath.isEmpty, store.pendingActivityRoute == nil else { exit(1) }
        }
      }
      if page == "y1-navigation-stress" {
        store.openEffort("effort-slides")
        await settleNavigation()
        guard store.activityPath == [.effort("effort-slides")] else { exit(1) }
        store.route(.back)
        store.selectPage(.overview)
        let hits = try? await HomeSearch.find("Pitch deck", backend: store.backend)
        guard let hit = hits?.first(where: { $0.kind == .effort }) else { exit(1) }
        store.openHomeSearchHit(hit)
        await settleNavigation()
        guard store.activityPath == [.effort(hit.target)] else { exit(1) }
        store.route(.back)
        store.selectPage(.overview)
        store.route(.section(3)) // Same command as the Navigate menu's Activity item.
        await settleNavigation()
        guard store.activityPath.isEmpty, store.pendingActivityRoute == nil else { exit(1) }
        print("Y1: 20 Home/task/Back/Home cycles, Home/effort, search/effort and menu/Activity passed; path empty.")
      }
    }
    window.setContentSize(size)
    if titledChrome && !settingsChrome {
      window.setFrame(NSRect(origin: window.frame.origin, size: size), display: false)
    }
    window.contentView?.frame = NSRect(origin: .zero, size: size)
    host.frame = NSRect(origin: .zero, size: size)
    host.layoutSubtreeIfNeeded()
    // A lazy profile task can start on the first print layout. Capture its settled
    // synthetic readback, rather than intermittently exporting the loading branch.
    if page.hasPrefix("person"), !store.selectedPerson.isEmpty {
      await store.loadPerson()
      try? await Task.sleep(for: .milliseconds(100))
      host.layoutSubtreeIfNeeded()
    }
    if titledChrome && !settingsChrome {
      guard window.titlebarSeparatorStyle == .none, window.titlebarAppearsTransparent,
        window.isMovable, window.toolbar != nil,
        window.standardWindowButton(.closeButton) != nil,
        window.standardWindowButton(.miniaturizeButton) != nil,
        window.standardWindowButton(.zoomButton) != nil
      else {
        fputs("Y1 main window chrome or native window controls regressed.\n", stderr)
        exit(1)
      }
    }
    let capture = titledChrome ? (window.contentView?.superview ?? host) : host
    // Export one pixel per requested point on Retina and non-Retina hosts alike.
    guard let bitmap = NSBitmapImageRep(
      bitmapDataPlanes: nil, pixelsWide: Int(size.width), pixelsHigh: Int(size.height),
      bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
      colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0) else { exit(1) }
    bitmap.size = capture.bounds.size
    capture.cacheDisplay(in: capture.bounds, to: bitmap)
    guard let png = bitmap.representation(using: .png, properties: [:]) else { exit(1) }
    do {
      let url = URL(fileURLWithPath: path)
      try FileManager.default.createDirectory(
        at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
      try png.write(to: url, options: .atomic)
    } catch {
      fputs("Could not write snapshot.\n", stderr)
      exit(1)
    }
    if let raw = argument("--perf-seconds"), let seconds = Double(raw), seconds > 0, seconds <= 120,
      let output = argument("--perf-json")
    {
      await P1Performance.measure(seconds: seconds, output: output)
    }
    window.orderOut(nil)
    exit(0)
  }
  static func snapshotStage<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
    ZStack {
      Color(nsColor: .underPageBackgroundColor)
      content().clipShape(RoundedRectangle(cornerRadius: 24)).padding(16)
    }
  }
  private static func o1State(_ page: String) -> EngineSurfaceState? {
    for prefix in ["o1-menu-", "o1-state-"] where page.hasPrefix(prefix) {
      return EngineSurfaceState(rawValue: String(page.dropFirst(prefix.count)))
    }
    return nil
  }
}
