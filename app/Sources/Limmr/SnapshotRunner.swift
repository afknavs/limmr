import AppKit
import LimmrKit
import SwiftUI

@MainActor enum SnapshotRunner {
  static let pages = N2PeopleSnapshots.pages + [
    "n2-shell-home", "n2-shell-gallery", "n2-shell-list", "n2-shell-sidebar", "n2-shell-circle", "n2-shell-home-circles",
    "n1-realdata-home", "n1-realdata-sidebar-unlinked", "n1-realdata-sidebar-linked",
    "n1-realdata-integrations-unlinked", "n1-realdata-integrations-linked",
    "n1-realdata-purpose", "n1-realdata-circle-empty", "n1-realdata-map-dense",
    "n1-speed-log", "n1-speed-purpose",
    "preview-home", "preview-tasks", "preview-list", "preview-people", "preview-activity", "preview-task", "preview-circle", "preview-integrations",
    "n1-look-home", "n1-look-gallery", "n1-look-list", "n1-look-task", "n1-look-circle", "n1-look-integrations",
    "n1-map", "n1-map-400", "n1-map-person", "n1-map-start", "n1-map-end",
    "n1-tasks-home", "n1-tasks-all", "n1-tasks-circle", "n1-tasks-cover",
    "n1-file-pdf", "n1-file-image",
    "tasks-previews-workspace", "tasks-previews-narrow", "tasks-previews-home", "tasks-previews-cover", "tasks-previews-navigation",
    "status-healthy", "status-busy", "status-stopped", "status-relink", "status-restarting", "status-phone-offline", "status-remote", "status-sync",
    "settings-connections", "settings-privacy", "settings-ai-apps", "settings-connections-advanced",
    "lane3-home", "lane3-map", "lane3-profile", "lane3-updated", "lane3-picker", "lane3-circle",
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
    "engine-busy", "engine-stalled", "engine-restarting", "engine-updating", "agent-renaming", "agent-rename-error", "agent-renamed",
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
  static func usesFixtureClock(page: String) -> Bool { !page.hasPrefix("preview-") }
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
    if page.hasPrefix("preview-") {
      // Read-only preview of a separate engine (for example a copy of real data on a spare port).
      guard let engine = argument("--preview-engine"), let url = URL(string: engine), url.host == "127.0.0.1" else {
        fputs("preview pages need --preview-engine http://127.0.0.1:<port>\n", stderr); exit(2)
      }
      backend = APIClient(credentialFile: RuntimePaths().home.appending(path: "control-token"), baseURL: url)
    } else if page.hasPrefix("n2-shell-") {
      backend = try! N2ShellSnapshotBackend.make()
    } else if page.hasPrefix("n2-") {
      backend = N2PeopleSnapshotBackend(page: page, portraits: N1MapPortraits.make())
    } else if page.hasPrefix("n1-realdata-") {
      backend = N1RealDataSnapshotBackend(linked: page.hasSuffix("-linked"))
    } else if page.hasPrefix("n1-look-") {
      backend = try! N1LookSnapshotBackend.make()
    } else if page.hasPrefix("n1-map") {
      backend = N1MapSnapshotBackend(dense: page == "n1-map-400", portraits: N1MapPortraits.make())
    } else if page.hasPrefix("n1-tasks-") {
      backend = N1TasksSnapshotBackend(page: page)
    } else if page.hasPrefix("tasks-previews-") {
      backend = try! Oct29PreviewFixtures.make()
    } else if page.hasPrefix("lane3-") {
      backend = Lane3SnapshotBackend()
    } else if page.hasPrefix("y7-dot-") {
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
      demo: !page.hasPrefix("empty-") && !page.hasPrefix("o1-empty-") && page != "setup-helper-error", snapshot: true, fixedClock: usesFixtureClock(page: page), backend: backend)
    if page.hasPrefix("n1-realdata-") { store.fixtureNow = Timestamp.date(N1RealDataFixture.now)! }
    await store.refresh()
    if page.hasPrefix("n1-map") || page == "n1-realdata-map-dense" || page == "preview-people" {
      // Offline fixtures only: wait for real map preparation and avatar responses,
      // rather than capturing a loading frame after the standard fixed delay.
      _ = await NetworkMapLayoutCache.projection(store.circleMap)
      for node in store.circleMap.nodes where node.kind == "person" { await store.loadAvatar(node.id) }
    }
    if page.hasPrefix("h2-") { await store.refreshHomeEfforts() }
    let dark = arguments.contains("--dark")
    NSApp.appearance = NSAppearance(named: dark ? .darkAqua : .aqua)
    var content: AnyView
    switch page {
    case let page where page.hasPrefix("n2-shell-"):
      store.setupStatus = nil
      if page == "n2-shell-gallery" || page == "n2-shell-list" { store.selectSidebar("page:Tasks") }
      if page == "n2-shell-circle" || page == "n2-shell-sidebar" {
        let id = page == "n2-shell-sidebar" ? "family" : "university"
        await store.loadCircle(id)
        store.selectSidebar("circle:" + id)
      }
      content = AnyView(RootView(store: store)
        .environment(\.snapshotTaskLayout, page == "n2-shell-list" ? "list" : "gallery")
        .environment(\.snapshotTaskReveal, page == "n2-shell-gallery" ? "tsk_workshop" : nil))
    case let page where page.hasPrefix("n2-"):
      content = await N2PeopleSnapshots.content(page: page, store: store)
    case let page where page.hasPrefix("n1-realdata-"):
      store.setupStatus = nil
      if page.contains("integrations") {
        store.settingsTab = .integrations
        content = AnyView(SettingsView(store: store))
      } else if page.hasSuffix("map-dense") {
        content = AnyView(VStack(alignment: .leading, spacing: 12) {
          Text("People").font(.largeTitle.bold())
          Text("120 people · 80 at Northbridge University").font(.callout).foregroundStyle(.secondary)
          NetworkMapView(store: store, map: store.circleMap)
        }.padding(20).background { AppBackground() })
      } else {
        if page.hasSuffix("purpose") { store.selectPage(.activity) }
        if page.hasSuffix("circle-empty") {
          await store.loadCircle("you"); await store.refreshAllTasks()
          store.selectPage(.circles); store.circlePath = [.circle("you")]
        }
        content = AnyView(RootView(store: store))
      }
    case "n1-speed-log", "n1-speed-purpose":
      store.activityFilter = "all"
      for i in 0..<12 {
        var empty = DemoData.activity[0]; empty.id = Int64(8000 + i)
        empty.kind = "read"; empty.tool = "list_messages"; empty.count = 0; empty.messageIds = []
        store.activity.append(empty)
      }
      precondition(store.activity.filter(ActivityPresentation.isEmptyRead).count >= 12)
      precondition(!ActivityPresentation.detailed(store.activity).contains(where: ActivityPresentation.isEmptyRead))
      print("N1: source includes 12 empty reads; detailed projection omits them and retains meaningful actions.")
      content = AnyView(ActivityView(store: store, detailedLog: page == "n1-speed-log").background { AppBackground() })
    case "preview-home", "preview-tasks", "preview-list", "preview-people", "preview-activity", "preview-task", "preview-circle", "preview-integrations":
      store.setupStatus = nil
      let selected = argument("--select")
      if page == "preview-integrations" {
        store.settingsTab = .integrations
        content = AnyView(SettingsView(store: store))
      } else {
        if page == "preview-tasks" || page == "preview-list" { store.selectPage(.tasks); await store.refreshAllTasks() }
        if page == "preview-people" { store.selectPage(.circles) }
        if page == "preview-activity" { store.selectPage(.activity) }
        if page == "preview-task", let selected { await store.refreshAllTasks(); store.openTask(selected) }
        if page == "preview-circle", let selected { await store.loadCircle(selected); await store.refreshAllTasks(); store.selectPage(.circles); store.circlePath = [.circle(selected)] }
        content = AnyView(RootView(store: store).environment(\.snapshotTaskLayout, page == "preview-list" ? "list" : "gallery"))
      }
    case "n1-look-home", "n1-look-gallery", "n1-look-list", "n1-look-task", "n1-look-circle", "n1-look-integrations":
      store.setupStatus = nil
      if page == "n1-look-integrations" {
        store.settingsTab = .integrations
        content = AnyView(SettingsView(store: store))
      } else {
        if page == "n1-look-gallery" || page == "n1-look-list" { store.selectPage(.tasks); await store.refreshAllTasks() }
        if page == "n1-look-task" { store.openTask("tsk_workshop") }
        if page == "n1-look-circle" { await store.loadCircle("university"); await store.refreshAllTasks(); store.selectPage(.circles); store.circlePath = [.circle("university")] }
        content = AnyView(RootView(store: store).environment(\.snapshotTaskLayout, page == "n1-look-list" ? "list" : "gallery").environment(\.snapshotTaskReveal, page == "n1-look-gallery" ? "tsk_workshop" : nil))
      }
    case "n1-map", "n1-map-400":
      content = AnyView(VStack(alignment: .leading, spacing: 16) {
        Text("People").font(AppType.pageTitle)
        Text("Your circles and connections").font(.callout).foregroundStyle(AccessibleSecondary())
        NetworkMapView(store: store, map: store.circleMap)
        Text("\(store.circleMap.nodes.filter { $0.kind == "person" }.count) people · Drag to explore · Pinch to zoom").font(.caption).foregroundStyle(AccessibleSecondary())
      }.padding(24).background { AppBackground() })
    case "n1-map-person", "n1-map-start", "n1-map-end":
      store.selectedPerson = "person-04"; await store.loadPerson()
      content = AnyView(CirclePersonPage(store: store, id: "person-04")
        .environment(\.snapshotNetworkExpanded, page == "n1-map-end"))
    case "n1-tasks-home", "n1-tasks-cover":
      store.setupStatus = nil
      content = AnyView(RootView(store: store))
    case "n1-tasks-all":
      store.selectPage(.tasks); await store.refreshAllTasks()
      content = AnyView(RootView(store: store))
    case "n1-tasks-circle":
      await store.loadCircle("university"); await store.refreshAllTasks()
      store.selectPage(.circles); store.circlePath = [.circle("university")]
      content = AnyView(RootView(store: store))
    case "tasks-previews-narrow":
      store.openTask("tsk_workshop")
      content = AnyView(RootView(store: store))
    case "tasks-previews-workspace":
      content = AnyView(TaskPage(store: store, id: "tsk_workshop"))
    case "tasks-previews-home", "tasks-previews-navigation":
      store.setupStatus = nil
      content = AnyView(RootView(store: store))
    case "tasks-previews-cover":
      content = AnyView(snapshotStage { TaskCoverPicker(store: store, taskID: "tsk_workshop") })
    case "lane3-home", "lane3-map", "lane3-profile", "lane3-updated", "lane3-picker", "lane3-circle":
      store.showSetup = false
      for circle in store.circles { await store.loadCircle(circle.id) }
      store.selectedPerson = "person-04"
      await store.loadPerson()
      if page == "lane3-map" {
        store.selectPage(.circles); store.mapSelection.person = "person-04"
        content = AnyView(RootView(store: store))
      } else if page == "lane3-updated" {
        content = AnyView(ScrollView {
          VStack(alignment: .leading, spacing: 20) {
            Text(store.personName("person-04")).font(AppType.pageTitle)
            PersonLately(store: store, id: "person-04", profile: store.profile)
          }.frame(maxWidth: 760).padding(24).frame(maxWidth: .infinity)
        }.background { AppBackground() }.environment(\.snapshotProfileUpdated, true))
      } else if page == "lane3-profile" {
        content = AnyView(CirclePersonPage(store: store, id: "person-04"))
      } else if page == "lane3-picker", let circle = store.circles.first {
        content = AnyView(snapshotStage { CirclePicturePicker(store: store, circle: circle) })
      } else if page == "lane3-circle" {
        content = AnyView(CirclePage(store: store, id: "atlas"))
      } else {
        store.selectPage(.overview); await store.refreshHomeEfforts()
        content = AnyView(ScrollView {
          CircleUpdatesView(store: store).frame(maxWidth: 760).padding(24).frame(maxWidth: .infinity)
        }.background { AppBackground() })
      }
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
        TaskWorkspaceView(store: store, id: DemoTasks.tasks[index].id, initial: DemoTasks.tasks[index]).padding(24)
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
    case "n1-file-pdf", "n1-file-image":
      content = AnyView(snapshotStage {
        FileSendReviewCard(store: store, item: DemoData.fileSuggestion(image: page == "n1-file-image"))
          .frame(width: 620).padding(28).background { AppBackground() }
      })
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
      store.privacyPeople = true
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

    case "status-healthy", "status-busy", "status-stopped", "status-relink", "status-restarting", "status-phone-offline", "status-remote", "status-sync":
      store.setupStatus = nil
      store.checks = []
      store.remote?.enabled = false
      store.connectionSync = .demo("never")
      switch page {
      case "status-busy": store.snapshotConnectionState = .busy
      case "status-stopped": store.snapshotConnectionState = .notRunning
      case "status-relink": store.status?.linked = false; store.status?.connected = false; store.snapshotConnectionState = .loggedOut
      case "status-restarting": store.snapshotConnectionState = .restarting
      case "status-phone-offline": store.snapshotConnectionState = .phoneUnreachable
      case "status-remote":
        store.remote?.enabled = true
        store.checks = [.init(id: "remote", title: "Remote access", state: "fail", detail: "Unavailable", fixId: "restart_remote")]
      case "status-sync": store.connectionSync = .demo("error")
      default: store.snapshotConnectionState = .connected
      }
      content = AnyView(RootView(store: store))
    case "settings-connections", "settings-connections-advanced", "settings-privacy", "settings-ai-apps":
      store.settingsTab = page == "settings-privacy" ? .privacy : page == "settings-ai-apps" ? .integrations : .connections
      store.connectionAdvanced = page == "settings-connections-advanced"
      content = AnyView(SettingsView(store: store))
    case "engine-busy":
      content = AnyView(EngineStartupView(phase: .busy) {})
    case "engine-stalled":
      content = AnyView(EngineStartupView(phase: .stalled,
        failureMessage: EngineReadinessStalled().localizedDescription) {})
    case "engine-restarting":
      content = AnyView(EngineStartupView(phase: .restarting,
        progressMessage: "Waiting for Limmr’s background engine to respond…") {})
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
      case "settings-briefs": store.settingsTab = .briefs; store.privacyBriefs = true
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
      case "y7-dot-never", "y7-dot-healthy", "y7-dot-error", "y7-dot-review": store.settingsTab = .connections
      case "settings-integrations": store.settingsTab = .integrations
      case "integration-detail", "settings-integrations-access":
        store.settingsTab = .integrations
        store.selectedAgent = "agent-2"
        await store.loadPermissions()
      case "settings-hidden": store.settingsTab = .hiddenChats; store.privacyHidden = true
      case "settings-remote": store.settingsTab = .remote; store.connectionRemote = true
      case "settings-health": store.settingsTab = .health; store.connectionDetails = true
      case "settings-advanced": store.settingsTab = .advanced; store.connectionAdvanced = true
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
        page == "n2-shell-home-circles" ? "n2-circles" : page == "y2-home-suggestions" ? "tasks" : page == "y2-home-circles" ? "circles" : page == "u4-replies"
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
      .environment(\.snapshotReduceTransparency, arguments.contains("--reduce-transparency"))
      .environment(\.controlActiveState, .key).tint((page.hasPrefix("n2-") || page.hasPrefix("n1-look-") || page.hasPrefix("n1-realdata-")) ? Color.accentColor : .blue))
    let size = NSSize(width: dimensions[0], height: dimensions[1])
    // Standalone People captures need the same surface as navigation destinations.
    if page.hasPrefix("y45-") { content = AnyView(content.pageSurface()) }
    // Native List and toolbar preferences need a hosting controller, just as WindowGroup uses.
    // Keep historical capture paths untouched; N1 captures include the native window chrome.
    let n1Native = page.hasPrefix("n2-") || page.hasPrefix("n1-look-") || page.hasPrefix("preview-") || (page.hasPrefix("n1-realdata-") && !page.hasSuffix("map-dense"))
    let nativeController = n1Native ? NSHostingController(rootView: content) : nil
    let host = (nativeController?.view as? NSHostingView<AnyView>) ?? NSHostingView(rootView: content)
    // Snapshot dimensions are a contract, independent of a pane's ideal content size.
    host.sizingOptions = []
    host.appearance = NSAppearance(named: arguments.contains("--increase-contrast") ? (dark ? .accessibilityHighContrastDarkAqua : .accessibilityHighContrastAqua) : (dark ? .darkAqua : .aqua))
    host.wantsLayer = true
    host.frame = NSRect(origin: .zero, size: size)
    // S2 also captures native Settings chrome, offscreen, to verify the top-left controls.
    let settingsChrome = page == "s2-settings"
    let titledChrome = n1Native || settingsChrome || arguments.contains("--titled-window")
    let window = NSWindow(
      contentRect: host.frame,
      styleMask: titledChrome ? [.titled, .closable, .miniaturizable, .resizable] : [.borderless],
      backing: .buffered, defer: false)
    if settingsChrome { window.title = "Settings" }
    if titledChrome && !settingsChrome {
      window.styleMask.insert(.fullSizeContentView)
      window.title = page.contains("integrations") ? "Integrations" : "Limmr"
      window.titleVisibility = n1Native ? .visible : .hidden
      window.toolbarStyle = .unified
      if !n1Native || page == "n1-look-integrations" || page == "preview-integrations" || page.hasPrefix("n1-realdata-integrations-") { window.toolbar = NSToolbar(identifier: "Y1MainToolbar") }
      MainWindowSurface.apply(to: window)
    }
    window.appearance = host.appearance
    if let nativeController {
      window.contentViewController = nativeController
    } else if titledChrome {
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
    if page == "n2-shell-circle" || page == "n2-shell-sidebar" {
      let selected = page == "n2-shell-sidebar" ? "family" : "university"
      await settleNavigation()
      guard store.circlePath == [.circle(selected)], store.sidebarDestination == "circle:" + selected else {
        fputs("N2 sidebar circle did not open/highlight.\n", stderr); exit(1)
      }
      // Exercise every main/circle row against the mounted native stack, including
      // initial empty-path writes and Back. AI row targets are covered in tests.
      for destination in Page.allCases {
        store.selectSidebar("page:" + destination.rawValue); await settleNavigation()
        guard store.sidebarDestination == "page:" + destination.rawValue else { exit(1) }
      }
      for circle in store.circles {
        store.selectSidebar("circle:" + circle.id); await settleNavigation()
        guard store.circlePath == [.circle(circle.id)], store.sidebarDestination == "circle:" + circle.id else { exit(1) }
        store.route(.back); await settleNavigation()
        guard store.circlePath.isEmpty else { exit(1) }
      }
      store.selectSidebar("circle:" + selected); await settleNavigation()
      print("N2 mounted sidebar rows and circle Back passed")
    }
    if page == "n2-circle-open" {
      // Same action as the existing circle row, exercised on the mounted stack.
      store.circlePath.append(.circle("university"))
      await settleNavigation()
      guard store.navigationPath.wrappedValue == [.people(.circle("university"))] else { fputs("N2 mounted navigation cleared circle route: path count=\(store.circlePath.count), page=\(store.page.rawValue).\n", stderr); exit(1) }
      store.leavePeopleRoute(); await settleNavigation()
      guard store.circlePath.isEmpty && store.page == .circles else { exit(1) }
      store.selectPeopleMapNode(.init(id: "university", kind: "circle", title: "University"))
      await settleNavigation()
      guard store.navigationPath.wrappedValue == [.people(.circle("university"))] else { fputs("N2 mounted navigation cleared circle route: path count=\(store.circlePath.count), page=\(store.page.rawValue).\n", stderr); exit(1) }
      print("N2: mounted circle-row action, Back to People, map-label action passed.")
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
    if page == "tasks-previews-navigation" {
      for _ in 0..<25 {
        store.selectPage(.circles); store.circlePath = [.person("person-04")]
        await settleNavigation()
        await store.refresh() // Same data replacement as an automatic summary update.
        await settleNavigation()
        guard store.navigationPath.wrappedValue == [.people(.person("person-04"))] else { exit(1) }
        store.openTask("tsk_workshop")
        await settleNavigation()
        guard store.navigationPath.wrappedValue == [.activity(.task("tsk_workshop"))] else { exit(1) }
        store.route(.back)
        await settleNavigation()
        guard store.navigationPath.wrappedValue == [.people(.person("person-04"))] else { exit(1) }
        store.selectPage(.overview); await settleNavigation()
      }
      store.selectPage(.circles); store.circlePath = [.person("person-04")]
      await settleNavigation()
      print("Oct29: 25 mounted profile / automatic refresh / task / Back / sidebar cycles passed.")
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
    if n1Native {
      do { try N1NativeSnapshot.write(window: window, to: path, size: size) }
      catch { fputs("Native window capture failed: \(error.localizedDescription)\n", stderr); exit(1) }
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
