import LimmrKit
import SwiftUI

@MainActor @Observable final class AppStore {
  private(set) var backend: any Backend
  @ObservationIgnored private let retryPause: @Sendable (Duration) async throws -> Void
  private(set) var demo: Bool
  let snapshot: Bool
  @ObservationIgnored private(set) var demoClockStartedAt = Date()
  private(set) var system: SystemActions
  private(set) var notifications: NotificationBridge
  var ownerProfile = OwnerProfile()
  var setupStatus: SetupStatus?
  var briefSettings = BriefSettings() { didSet { syncEffortNaming() } }
  var showProposalReview = false
  var writingMacBriefs = false
  var macBriefNotice: String?
  var home = HomeResponse.empty { didSet { syncEffortNaming() } }
  var circles: [LimmrKit.Circle] = []
  var circleMap = CircleMap.empty
  var presence: [AgentPresence] = []
  var contactsImportTask: Task<Void, Never>?
  var avatarData: [String: Data] = [:]
  var avatarAttempts: Set<String> = []
  var mapSelection = MapSelection()
  var mapArrivals: [MapArrival] = []
  @ObservationIgnored private var mapEventDiff = MapEventDiff()
  var activityAgent = "all"
  var activityDay: String?
  var taskActions: Set<String> = []
  var taskActionErrors: [String: String] = [:]
  var activityFilter = "sends"
  var selectedTask: Int64?
  var history: StatsHistory?
  var recentIcons: [EntityIcon] = []
  var requestedReplyDrafts: Set<String> = []
  var settingsQuery = ""
  var settingsSearchTarget = ""
  var setupComplete: Bool { status?.linked == true && !showSetup }
  var dashboard: DashboardStats?
  var circlePreviews: [String: Message] = [:]
  var digests = ActivityDigests()
  var digestError: String?
  @ObservationIgnored private var lastUsageRefresh = Date.distantPast
  @ObservationIgnored private var lastDigestRefresh = Date.distantPast
  var understandingLayout = UnderstandingLayout()
  @ObservationIgnored private var layoutTask: Task<Void, Never>?
  func updateUnderstandingLayout(map: CircleMap, size: CGSize, focused: Bool = false, extra: String? = nil) {
    layoutTask?.cancel()
    let previous = understandingLayout
    let generation = backendGeneration
    layoutTask = Task {
      do { try await Task.sleep(for: .milliseconds(50)) } catch { return }
      let next = await Task.detached(priority: .userInitiated) {
        var layout = previous
        layout.update(map: map, size: size, focused: focused)
        if let extra { layout.placeLabels(map: map, focused: focused, extra: extra) }
        return layout
      }.value
      guard !Task.isCancelled, generation == backendGeneration else { return }
      understandingLayout = next
    }
  }
  var seenMessageCounts: [String: Int] = [:]
  var circlePath: [CircleDestination] = []
  var activityPath: [ActivityRoute] = []
  var pendingActivityRoute: ActivityRoute?
  /// Where a task or effort was opened from, so Back returns there instead of to Activity.
  var activityReturnPage: Page?
  var activityReturnPath: [CircleDestination] = []
  var activityNavigationReady = false
  var effortRevision = 0
  var effortUndo: [String: String] = [:]
  @ObservationIgnored let effortNamingEnvironment: EffortNamingWorker.Environment?
  @ObservationIgnored let effortNamingModel: (any EffortNamingModel)?
  @ObservationIgnored var resettingEffortNaming = false
  var effortNamer: EffortNamingWorker?
  @ObservationIgnored var effortNamingSession = EffortNamingSession()
  var homeEfforts: [Effort] = []
  var homeEffortsReady = false
  var circleDetails: [String: CircleDetail] = [:]
  var circleHistory: [String: [CircleBrief]] = [:]
  var circleError: String?
  var rowStep = 1
  var rowRequest = 0
  var rowOpenRequest = 0
  var pendingSearchPage: Page?
  var searchRequest = 0
  var escapeRequest = 0
  var page: Page = .overview
  var settingsTab: SettingsTab = .general
  var showSettings = false
  var showConnect = false
  var refreshError: String?
  private(set) var backendGeneration = 0
  @ObservationIgnored private var permissionSave: Task<Void, Never>?
  @ObservationIgnored private var settingsSave: Task<Void, Never>?
  var permissionsSaved = false
  var settingsSaved = false
  var action = "general"
  @ObservationIgnored private var retryWork: (() async throws -> Void)?
  var agents: [Agent] = []
  var approvals: [Approval] = []
  var activity: [Activity] = []
  var allRecentActivity: [Activity] = []
  var activityQuery = ""
  var checks: [HealthCheck] = []
  var chats: [Chat] = []
  var people: [Person] = []
  var changes: [PeopleChange] = []
  var graph = Graph(nodes: [], edges: [])
  var status: Status?
  var settings = AppSettings(owner: "")
  private var settingsLoaded = false
  var setupStep = 0
  var remote: RemoteState?
  var selectedAgent: String?
  var selectedPerson = "person-04"
  var selectedApproval: String?
  var selectedActivity: Int64?
  var showSetup = false
  var connectKind: ConnectKind?
  var showRemote = false
  var error: String?
  var notice: String?
  var busy = false
  var attemptedSuggestionSends: Set<String> = []
  var canRetry: Bool { retryWork != nil }
  func failWithoutRetry(_ message: String) {
    error = message
    retryWork = nil
  }
  var link = LinkEvent(state: "idle")
  var permissions: Permissions = .defaults(remote: false)
  private(set) var permissionsAgentID: String?
  var profile: PersonDetail?
  var groupProfile: GroupDetail?
  var activityMessages: [Message] = []
  var migrationAvailable = false
  var activityBefore: Int64?
  var loadingMore = false
  var pending: [Approval] { approvals.filter(\.isPending) }
  var resolved: [Approval] { approvals.filter { !$0.isPending } }
  var currentAgent: Agent? { agents.first { $0.id == selectedAgent } }
  init(demo: Bool, snapshot: Bool = false, backend: (any Backend)? = nil,
       launchAtLoginService: (any LaunchAtLoginManaging)? = nil,
       retryPause: (@Sendable (Duration) async throws -> Void)? = nil,
       effortNamingEnvironment: EffortNamingWorker.Environment? = nil,
       effortNamingModel: (any EffortNamingModel)? = nil) {
    self.effortNamingEnvironment = effortNamingEnvironment
    self.effortNamingModel = effortNamingModel
    self.demo = demo
    self.snapshot = snapshot
    // Offscreen fixtures exercise retry attempts without waiting on a real clock.
    // Live requests keep the production backoff; tests may supply a controlled clock.
    self.retryPause = retryPause ?? { duration in
      if snapshot { await Task.yield() } else { try await Task.sleep(for: duration) }
    }
    system = SystemActions(demo: demo, loginService: launchAtLoginService ?? MainAppLoginService(demo: demo || snapshot))
    notifications = NotificationBridge(active: !demo && !snapshot)
    self.backend =
      backend
      ?? (demo
        ? DemoBackend() as any Backend
        : APIClient(credentialFile: RuntimePaths().home.appending(path: "control-token")))
    notifications.navigate = { [weak self] url in self?.navigate(url) }
    notifications.resolve = { [weak self] id, approve in
      Task { await self?.resolve(id, approve: approve) }
    }
  }
  private(set) var refreshing = false
  @ObservationIgnored private var lastRefresh = Date.distantPast
  @ObservationIgnored private var refreshScheduled: Task<Void, Never>?
  @ObservationIgnored private var payloads: [String: Data] = [:]
  var loadFailures: [String: String] = [:]
  var peopleCursor: String?
  var chatCursor: String?
  var peopleSearchResults: [Person]?
  private(set) var loadedRoutes: Set<String> = []
  func hasLoaded(_ path: String) -> Bool {
    loadedRoutes.contains(path.components(separatedBy: "?")[0])
  }
  func isLoading(_ path: String) -> Bool {
    let route = path.components(separatedBy: "?")[0]
    return !loadedRoutes.contains(route)
      && !loadFailures.keys.contains { $0.components(separatedBy: "?")[0] == route }
  }
  func read<T: Decodable & Sendable>(_ path: String) async throws -> T {
    let data = try await CalmLoadRetry.request(backend, path: path, pause: retryPause)
    return try await Task.detached(priority: .userInitiated) { try Wire.decode(T.self, from: data, route: path) }.value
  }
  func load<T: Decodable & Sendable>(_ path: String, title: String, apply: (T) -> Void)
    async
  {
    let generation = backendGeneration
    let source = backend
    let route = path.components(separatedBy: "?")[0]
    for key in Array(loadFailures.keys) where key.components(separatedBy: "?")[0] == route {
      loadFailures[key] = nil
    }
    updateRefreshError()
    do {
      let data = try await CalmLoadRetry.request(source, path: path, pause: retryPause)
      guard generation == backendGeneration, !Task.isCancelled else { return }
      if payloads[path] != data {
        let value = try await Task.detached(priority: .userInitiated) {
          try Wire.decode(T.self, from: data, route: path)
        }.value
        guard generation == backendGeneration, !Task.isCancelled else { return }
        apply(value)
        if payloads.count > 64 { payloads.removeAll() }
        payloads[path] = data
      }
      loadedRoutes.insert(route)
      for key in Array(loadFailures.keys) where key.components(separatedBy: "?")[0] == route {
        loadFailures[key] = nil
      }
    } catch is CancellationError {
      return
    } catch {
      guard generation == backendGeneration, !Task.isCancelled else { return }
      loadFailures[path] = "\(title): \(CallFailure(path: path, cause: error).localizedDescription)"
    }
    updateRefreshError()
  }
  private func updateRefreshError() {
    refreshError = loadFailures.isEmpty ? nil : "Couldn’t refresh. Showing the last available information."
  }
  var peopleInferenceStarted = false
  func refreshHomeEfforts() async {
    await load("/v1/efforts?limit=500", title: "Circle work") { (v: EffortsResponse) in
      homeEfforts = v.efforts
      homeEffortsReady = true
    }
  }
  var peopleInferenceTask: Task<Void, Never>?
  func refresh() async {
    guard !refreshing else {
      scheduleRefresh()
      return
    }
    let generation = backendGeneration
    refreshing = true
    defer {
      if generation == backendGeneration {
        refreshing = false
        lastRefresh = Date()
      }
    }
    guard generation == backendGeneration else { return }
    await load("/v1/status", title: "Connection") { (v: Status) in status = v }
    guard generation == backendGeneration, !Task.isCancelled else { return }
    // A down helper cannot answer the remaining reads. Keep their placeholders pending,
    // rather than spending a separate retry window on every endpoint.
    guard status != nil else { return }
    await load("/v1/agents", title: "Integrations") { (v: AgentsResponse) in agents = v.agents }
    guard generation == backendGeneration else { return }
    await load("/v1/approvals", title: "Approvals") { (v: ApprovalsResponse) in
      approvals = v.approvals
    }
    guard generation == backendGeneration else { return }
    await load("/v1/activity?limit=50", title: "Activity") { (v: ActivityPage) in
      allRecentActivity = v.items.sorted(by: Activity.newestFirst)
      if activityQuery.isEmpty {
        activity = allRecentActivity
        activityBefore = v.nextBefore
      }
    }
    guard generation == backendGeneration else { return }
    await load("/v1/health", title: "Health") { (v: HealthResponse) in checks = v.checks }
    guard generation == backendGeneration else { return }
    await load("/v1/chats?limit=100", title: "Chats") { (v: ChatsResponse) in
      chats = v.chats
      chatCursor = v.nextCursor
    }
    guard generation == backendGeneration else { return }
    await load("/v1/people?limit=50", title: "People") { (v: PeopleResponse) in
      people = v.people
      peopleCursor = v.nextCursor
    }
    guard generation == backendGeneration else { return }
    await load("/v1/people/changes?limit=50", title: "People changes") { (v: ChangesResponse) in
      changes = v.changes
    }
    guard generation == backendGeneration else { return }
    await load("/v1/home", title: "Home") { (v: HomeResponse) in home = v }
    if home.effortsReady == false { retryHomeUntilEffortsReady() }
    guard generation == backendGeneration else { return }
    await load("/v1/circles", title: "Circles") { (v: CirclesResponse) in circles = v.circles }
    guard generation == backendGeneration else { return }
    await load("/v1/map" + (circleMap.cursor.map { "?since=" + $0 } ?? ""), title: "People map") {
      (v: CircleMap) in
      let arrivals = mapEventDiff.ingest(v.events)
      circleMap = circleMap.applying(v)
      if !arrivals.isEmpty {
        let now = Date()
        mapArrivals.removeAll { now.timeIntervalSince($0.at) >= 3 }
        var next = max(now, mapArrivals.last?.at.addingTimeInterval(3) ?? now)
        for event in arrivals {
          mapArrivals.append(MapArrival(event: event, at: next))
          next = next.addingTimeInterval(3)
        }
      }
    }
    guard generation == backendGeneration else { return }
    await load("/v1/presence", title: "AI activity") { (v: PresenceResponse) in presence = v.agents
    }
    guard generation == backendGeneration else { return }
    await load("/v1/settings", title: "Settings") { (v: AppSettings) in
      settings = v
      settingsLoaded = true
    }
    guard generation == backendGeneration else { return }
    // Login registration belongs to macOS, not the engine's settings payload.
    system.login.refresh()
    settings.launchAtLogin = system.login.isOn
    await load("/v1/remote", title: "Remote access") { (v: RemoteState) in remote = v }
    RemotePairingReview.shared.update(store: self)
    await load("/v1/setup/status", title: "Setup") { (v: SetupStatus) in
      setupStatus = v
      if let owner = v.ownerProfile { ownerProfile = owner }
    }
    await load("/v1/settings/briefs", title: "Brief settings") { (v: BriefSettings) in
      briefSettings = v
    }
    let refreshUsage = Date().timeIntervalSince(lastUsageRefresh) >= 60
    if refreshUsage {
      do {
        let value: DashboardStats = try await backend.get("/v1/stats")
        if generation == backendGeneration {
          dashboard = value
          lastUsageRefresh = Date()
        }
        for item in HomePresentation.circles(home.circles).prefix(12) {
          let jids = Set(item.circle.chats.map(\.chatJid))
          guard
            let chat = chats.filter({ jids.contains($0.jid) }).max(by: {
              ($0.lastMessageTime ?? "") < ($1.lastMessageTime ?? "")
            })
          else { continue }
          var query = URLComponents()
          query.queryItems = [
            URLQueryItem(name: "chat_jid", value: chat.jid),
            URLQueryItem(name: "limit", value: "1"),
          ]
          let response: MessagesResponse = try await backend.get(
            "/v1/messages?" + (query.percentEncodedQuery ?? ""))
          guard generation == backendGeneration else { return }
          circlePreviews[item.id] = response.messages.first
        }
      } catch { /* Old engines keep the dashboard unavailable instead of inventing totals. */  }
    }
    // History has its own loading/error state, even when the totals endpoint is unavailable.
    if status?.linked == false {
      history = nil
      payloads = payloads.filter { $0.key.components(separatedBy: "?")[0] != "/v1/stats/history" }
      loadedRoutes.remove("/v1/stats/history")
      for key in Array(loadFailures.keys) where key.components(separatedBy: "?")[0] == "/v1/stats/history" {
        loadFailures[key] = nil
      }
      updateRefreshError()
    } else if status?.linked == true {
      if history == nil || refreshUsage { await refreshHistory() }
      if Date().timeIntervalSince(lastDigestRefresh) >= 4 * 3600 { await refreshDigests() }
    }
    notifications.update(approvals)
    schedulePeopleInference()
    scheduleContactsImport()
    syncEffortNaming()
  }
  func loadMorePeople() async {
    guard let cursor = peopleCursor, !cursor.isEmpty, !loadingMore else { return }
    loadingMore = true
    defer { loadingMore = false }
    await perform {
      var query = URLComponents()
      query.queryItems = [
        URLQueryItem(name: "limit", value: "50"), URLQueryItem(name: "cursor", value: cursor),
      ]
      let page: PeopleResponse = try await self.backend.get(
        "/v1/people?" + (query.percentEncodedQuery ?? ""))
      self.people += page.people.filter { p in !self.people.contains { $0.id == p.id } }
      self.peopleCursor = page.nextCursor
    }
  }
  func searchPeople(_ query: String) async {
    peopleSearchResults = nil
    guard !query.isEmpty else { return }
    let generation = backendGeneration
    do {
      try await Task.sleep(for: .milliseconds(300))
      var parts = URLComponents()
      parts.queryItems = [
        URLQueryItem(name: "query", value: query), URLQueryItem(name: "all", value: "true"),
        URLQueryItem(name: "limit", value: "100"),
      ]
      let page: PeopleResponse = try await backend.get(
        "/v1/people?" + (parts.percentEncodedQuery ?? ""))
      guard generation == backendGeneration, !Task.isCancelled else { return }
      peopleSearchResults = page.people
    } catch { if !Task.isCancelled { self.error = "People search couldn't load. Try again." } }
  }
  func loadMoreChats() async {
    guard let cursor = chatCursor, !cursor.isEmpty, !loadingMore else { return }
    loadingMore = true
    defer { loadingMore = false }
    await perform {
      let page: ChatsResponse = try await self.backend.get("/v1/chats?limit=100&cursor=" + cursor)
      self.chats += page.chats.filter { c in !self.chats.contains { $0.jid == c.jid } }
      self.chatCursor = page.nextCursor
    }
  }
  func searchChats(_ query: String) async {
    guard !query.isEmpty else { return }
    let generation = backendGeneration
    do {
      try await Task.sleep(for: .milliseconds(300))
      var parts = URLComponents()
      parts.queryItems = [
        URLQueryItem(name: "query", value: query), URLQueryItem(name: "limit", value: "100"),
      ]
      let page: ChatsResponse = try await backend.get(
        "/v1/chats?" + (parts.percentEncodedQuery ?? ""))
      guard generation == backendGeneration, !Task.isCancelled else { return }
      chats += page.chats.filter { c in !chats.contains { $0.jid == c.jid } }
    } catch { if !Task.isCancelled { self.error = "Chat search couldn't load. Try again." } }
  }
  var homeEffortsRetry: Task<Void, Never>?
  /// The engine serves Home before efforts are warm; re-read only Home until circle updates are ready.
  func retryHomeUntilEffortsReady(pause: Duration = .seconds(3), attempts: Int = 20) {
    guard homeEffortsRetry == nil else { return }
    let generation = backendGeneration
    homeEffortsRetry = Task {
      defer { homeEffortsRetry = nil }
      for _ in 0..<attempts {
        try? await Task.sleep(for: pause)
        guard !Task.isCancelled, generation == backendGeneration else { return }
        await load("/v1/home", title: "Home") { (v: HomeResponse) in home = v }
        if home.effortsReady != false { return }
      }
    }
  }
  private func scheduleRefresh() {
    guard refreshScheduled == nil else { return }
    let delay = refreshing ? 2 : max(0, 2 - Date().timeIntervalSince(lastRefresh))
    refreshScheduled = Task {
      try? await Task.sleep(for: .seconds(delay))
      guard !Task.isCancelled else { return }
      refreshScheduled = nil
      await refresh()
    }
  }
  /// Try again: refresh, and if the helper is down, restart it (an app update can leave launchd
  /// refusing the new helper until it is registered again).
  func reconnect(automatic: Bool = false) async {
    await refresh()
    guard !demo, status == nil, !automatic || system.engineRegistered else { return }
    do {
      try await system.startEngine()
      for _ in 0..<16 where status == nil {
        try await Task.sleep(for: .milliseconds(500))
        await refresh()
      }
    } catch {
      refreshError =
        "Limmr’s background helper didn’t start. Settings → Health has the details."
    }
  }
  func monitor() async {
    guard !snapshot else { return }
    async let events: Void = watchActivity()
    async let digests: Void = monitorDigests()
    _ = await (events, digests)
  }
  @ObservationIgnored private var pairingMonitorRunning = false
  /// OAuth pairing doesn't emit activity events. Poll only this small endpoint, independently
  /// of refresh and window tasks, with a short production timeout (750 ms + 1 s cadence).
  func monitorRemotePairings(
    review: RemotePairingReview = .shared,
    fetch: (@MainActor () async throws -> RemoteState)? = nil,
    pause: @escaping @MainActor (Duration) async throws -> Void = { try await Task.sleep(for: $0) }
  ) async {
    guard !snapshot, !pairingMonitorRunning else { return }
    pairingMonitorRunning = true
    defer { pairingMonitorRunning = false }
    while !Task.isCancelled {
      if !demo {
        let generation = backendGeneration
        do {
          let next: RemoteState
          if let fetch {
            next = try await fetch()
          } else {
            next = try Wire.decode(RemoteState.self, from: await system.localData("/v1/remote"))
          }
          guard !Task.isCancelled else { return }
          if generation == backendGeneration {
            remote = next
            review.update(store: self)
          }
        } catch { /* Keep the last state; never block the full refresh or show polling errors. */  }
      }
      do { try await pause(.seconds(1)) } catch { return }
    }
  }
  func refreshDigests() async {
    let generation = backendGeneration
    do {
      let value: ActivityDigests = try await backend.get("/v1/activity/digests")
      guard generation == backendGeneration else { return }
      digests = value
      digestError = nil
      lastDigestRefresh = Date()
    } catch {
      guard generation == backendGeneration else { return }
      digestError = "Read digests couldn't load. Try again."
    }
  }
  private func monitorDigests() async {
    while !Task.isCancelled {
      do { try await Task.sleep(for: .seconds(60)) } catch { return }
      let generation = backendGeneration
      do {
        let stats: DashboardStats = try await backend.get("/v1/stats")
        guard generation == backendGeneration, !Task.isCancelled else { continue }
        dashboard = stats
        lastUsageRefresh = Date()
        await refreshHistory()
      } catch { /* Preserve the last successful totals while the helper reconnects. */  }
      do {
        let waiting: WaitingResponse = try await backend.get("/v1/waiting")
        guard generation == backendGeneration, !Task.isCancelled else { continue }
        home.waiting = waiting.waiting
      } catch { /* Older engines still refresh Home on activity events. */  }
      if Date().timeIntervalSince(lastDigestRefresh) >= 4 * 3600 { await refreshDigests() }
    }
  }
  private func watchActivity() async {
    while !Task.isCancelled {
      do { for try await _ in backend.events("/v1/activity/stream") { scheduleRefresh() } } catch
      { /* Keep last good data; reconnect without reloading heavy endpoints. */  }
      try? await Task.sleep(for: .seconds(5))
    }
  }
  func perform(action: String = "general", _ work: @escaping () async throws -> Void) async {
    busy = true
    self.action = action
    error = nil
    retryWork = work
    defer { busy = false }
    do {
      try await work()
      retryWork = nil
    } catch {
      // System errors can include local paths; keep failures useful without exposing them.
      self.error =
        (error as? BackendError)?.localizedDescription ?? "This action couldn’t finish. Try again."
    }
  }
  func retry() async {
    guard let work = retryWork else { return }
    await perform(action: action, work)
  }
  func prepareHomeLoadingSnapshot() {
    guard snapshot && demo else { return }
    backendGeneration += 1
    clearEntities()
  }
  private func clearEntities() {
    resettingEffortNaming = true
    defer { resettingEffortNaming = false }
    effortNamer?.stop(); effortNamer = nil
    homeEffortsRetry?.cancel(); homeEffortsRetry = nil
    refreshing = false
    refreshScheduled?.cancel()
    refreshScheduled = nil
    payloads = [:]
    loadFailures = [:]
    loadedRoutes = []
    refreshError = nil
    peopleCursor = nil
    chatCursor = nil
    peopleSearchResults = nil
    setupStatus = nil
    ownerProfile = OwnerProfile()
    briefSettings = BriefSettings()
    macBriefNotice = nil
    showProposalReview = false
    home = .empty
    homeEfforts = []
    homeEffortsReady = false
    circles = []
    circleMap = .empty
    presence = []
    contactsImportTask?.cancel(); contactsImportTask = nil
    avatarData = [:]
    avatarAttempts = []
    mapSelection = MapSelection()
    mapEventDiff = MapEventDiff()
    mapArrivals = []
    layoutTask?.cancel()
    understandingLayout = UnderstandingLayout()
    activityAgent = "all"
    activityDay = nil
    taskActions = []; taskActionErrors = [:]
    activityFilter = "sends"
    selectedTask = nil
    activityPath = []
    pendingActivityRoute = nil
    activityNavigationReady = false
    history = nil
    recentIcons = []
    requestedReplyDrafts = []
    dashboard = nil
    circlePreviews = [:]
    digests = ActivityDigests()
    digestError = nil
    lastUsageRefresh = .distantPast
    lastDigestRefresh = .distantPast
    seenMessageCounts = [:]
    circlePath = []
    circleDetails = [:]
    circleHistory = [:]
    circleError = nil
    attemptedSuggestionSends = []
    status = nil
    selectedPerson = ""
    selectedApproval = nil
    permissions = .defaults(remote: true)
    permissionsAgentID = nil
    link = LinkEvent(state: "idle")
    migrationAvailable = false
    activityBefore = nil
    agents = []
    approvals = []
    activity = []
    allRecentActivity = []
    people = []
    chats = []
    changes = []
    peopleInferenceTask?.cancel(); peopleInferenceTask = nil; peopleInferenceStarted = false
    graph = .init(nodes: [], edges: [])
    checks = []
    remote = nil
    profile = nil
    groupProfile = nil
    selectedAgent = nil
    selectedActivity = nil
    activityMessages = []
    settings = AppSettings(owner: "")
    settingsLoaded = false
  }
  func useSampleData(_ enabled: Bool, realBackend: (any Backend)? = nil) async {
    demoClockStartedAt = Date()
    backendGeneration += 1
    effortNamingSession = EffortNamingSession()
    clearEntities()
    activityQuery = ""
    demo = enabled
    system = SystemActions(demo: enabled)
    notifications = NotificationBridge(active: !enabled && !snapshot)
    notifications.navigate = { [weak self] url in self?.navigate(url) }
    notifications.resolve = { [weak self] id, approve in
      Task { await self?.resolve(id, approve: approve) }
    }
    backend =
      enabled
      ? DemoBackend()
      : realBackend
        ?? APIClient(credentialFile: RuntimePaths().home.appending(path: "control-token"))
    error = nil
    retryWork = nil
    notice = nil
    showSetup = false
    await refresh()
  }
  func resolve(_ id: String, approve: Bool, text: String? = nil) async {
    guard let request = approvals.first(where: { $0.id == id }), request.isPending else { return }
    await perform(action: "approval-\(id)") { [self] in
      var body: [String: String] = [:]
      if let text { body["text"] = text }
      try await backend.post("/v1/approvals/\(id)/\(approve ? "approve" : "deny")", body)
      await refresh()
    }
  }
  func loadPermissions() async {
    permissionsAgentID = nil
    guard let id = selectedAgent else { return }
    let generation = backendGeneration
    let source = backend
    await perform(action: "permissions") { [self] in
      let agent: Agent = try await source.get("/v1/agents/\(id)")
      guard generation == backendGeneration, selectedAgent == id else { return }
      permissions = agent.permissions ?? .defaults(remote: agent.transport == "remote")
      permissionsAgentID = id
    }
  }
  func permissionBinding<Value>(_ key: WritableKeyPath<Permissions, Value>) -> Binding<Value> {
    Binding(
      get: { self.permissions[keyPath: key] },
      set: { value in
        self.editPermissions { $0[keyPath: key] = value }
      })
  }
  func editPermissions(_ edit: (inout Permissions) -> Void) {
    guard let id = selectedAgent, permissionsAgentID == id else { return }
    edit(&permissions)
    permissionsSaved = false
    let draft = permissions
    let generation = backendGeneration
    let previous = permissionSave
    permissionSave = Task {
      await previous?.value
      guard generation == backendGeneration else { return }
      await persistPermissions(draft, for: id)
    }
  }
  func updateIntegrationAccess(_ access: AgentAccess, id: String) {
    guard permissionsAgentID == id else { return }
    permissions.access = access
    let source = backend
    let generation = backendGeneration
    let previous = permissionSave
    permissionSave = Task {
      await previous?.value
      guard generation == backendGeneration else { return }
      await perform(action: "access") {
        try await source.updateAccess(access, agentID: id)
        let updated: Agent = try await source.get("/v1/agents/\(id)")
        guard generation == self.backendGeneration else { return }
        if let index = self.agents.firstIndex(where: { $0.id == id }) {
          self.agents[index] = updated
        }
        if self.permissionsAgentID == id && self.permissions.access == access {
          self.permissions.access = updated.access ?? updated.permissions?.access ?? access
        }
        self.notice = "Access saved"
      }
    }
  }
  func waitForPermissionSave() async { await permissionSave?.value }
  func savePermissions() async {
    guard let id = selectedAgent, permissionsAgentID == id else { return }
    await persistPermissions(permissions, for: id)
  }
  private func persistPermissions(_ draft: Permissions, for id: String) async {
    let source = backend
    let generation = backendGeneration
    await perform(action: "permissions") { [self] in
      _ = try await source.request(
        "/v1/agents/\(id)/permissions", method: "PUT", body: Wire.data(draft))
      guard generation == backendGeneration else { return }
      if let index = agents.firstIndex(where: { $0.id == id }) {
        agents[index].permissions = draft
      }
      if selectedAgent == id && permissions == draft {
        permissionsSaved = true
        Task {
          try? await Task.sleep(for: .seconds(2))
          if permissions == draft { permissionsSaved = false }
        }
      }
    }
  }
  func loadPerson() async {
    profile = nil
    groupProfile = nil
    let generation = backendGeneration
    let selection = selectedPerson
    let source = backend
    do {
      if graph.nodes.first(where: { $0.id == selection })?.kind == "group" {
        let value: GroupDetail = try await source.get("/v1/groups/\(selection)")
        guard generation == backendGeneration, selectedPerson == selection else { return }
        groupProfile = value
      } else if !selection.isEmpty {
        let value: PersonDetail = try await source.get("/v1/people/\(selection)")
        guard generation == backendGeneration, selectedPerson == selection else { return }
        profile = value
      }
    } catch {
      guard generation == backendGeneration else { return }
      self.error = "This profile could not be loaded. Try again."
    }
  }
  func loadActivity() async {
    activityMessages = []
    guard let row = activity.first(where: { $0.id == selectedActivity }) else { return }
    let generation = backendGeneration
    let source = backend
    await perform(action: "activity-detail") { [self] in
      var messages: [Message] = []
      for chat in row.chatJids {
        for id in row.messageIds {
          var components = URLComponents()
          components.queryItems = [
            URLQueryItem(name: "chat_jid", value: chat),
            URLQueryItem(name: "message_id", value: id),
            URLQueryItem(name: "limit", value: "100"),
          ]
          let response: MessagesResponse = try await source.get(
            "/v1/messages?" + (components.percentEncodedQuery ?? ""))
          messages += response.messages.filter { candidate in
            !messages.contains { $0.id == candidate.id && $0.chatJid == candidate.chatJid }
          }
        }
      }
      if generation == backendGeneration, selectedActivity == row.id { activityMessages = messages }
    }
  }

  func filterActivity(agent: String, kind: String, chat: String) async {
    var components = URLComponents()
    components.queryItems = [("agent", agent), ("kind", kind), ("chat", chat)].compactMap {
      name, value in value == "all" ? nil : URLQueryItem(name: name, value: value)
    }
    activityQuery = components.percentEncodedQuery ?? ""
    await perform { [self] in
      let query = activityQuery
      let generation = backendGeneration
      let page: ActivityPage = try await read("/v1/activity?limit=50&" + query)
      guard query == activityQuery, generation == backendGeneration else { return }
      let selected = activity.first { $0.id == selectedActivity }
      activity = page.items.sorted(by: Activity.newestFirst)
      activityBefore = page.nextBefore
      if let selected, !activity.contains(where: { $0.id == selected.id }),
        agent == "all" || selected.agentId == agent, kind == "all" || selected.kind == kind
      {
        activity.append(selected)
      } else if !activity.contains(where: { $0.id == selectedActivity }) {
        selectedActivity = nil
      }
    }
  }
  func loadMoreActivity() async {
    guard let before = activityBefore, !loadingMore else { return }
    loadingMore = true
    defer { loadingMore = false }
    await perform { [self] in
      let nextPage: ActivityPage = try await backend.get(
        "/v1/activity?limit=50&before=\(before)&" + activityQuery)
      let next = nextPage.items
      activityBefore = nextPage.nextBefore
      activity += next.filter { row in !activity.contains { $0.id == row.id } }
      activity.sort(by: Activity.newestFirst)
      if next.isEmpty { notice = "You’ve reached the beginning of the log." }
    }
  }
  func fix(_ id: String) async {
    if id == "migrate_whatsapp_mcp" {
      setupStep = 1
      showSetup = true
      return
    }
    if id == "update_client" {
      notice = "Update the engine helper to refresh the WhatsApp client, then reconnect."
      return
    }
    await perform { [self] in
      try await backend.post("/v1/fix/\(id)")
      await refresh()
    }
  }
  func revert(_ id: String) async {
    await perform { [self] in
      try await backend.post("/v1/people/changes/\(id)/revert")
      await refresh()
      await loadPerson()
    }
  }
  func settingsBinding<Value>(_ key: WritableKeyPath<AppSettings, Value>) -> Binding<Value> {
    Binding(
      get: { self.settings[keyPath: key] },
      set: { value in
        self.editSettings { $0[keyPath: key] = value }
      })
  }
  var launchAtLoginBinding: Binding<Bool> {
    Binding(get: { self.system.login.isOn }, set: { self.system.login.setEnabled($0) })
  }
  func editSettings(_ edit: (inout AppSettings) -> Void) {
    edit(&settings)
    settingsSaved = false
    let draft = settings
    let generation = backendGeneration
    let previous = settingsSave
    settingsSave = Task {
      await previous?.value
      guard generation == backendGeneration else { return }
      await persistSettings(draft)
    }
  }
  func waitForSettingsSave() async { await settingsSave?.value }
  func saveSettings() async { await persistSettings(settings) }
  private func persistSettings(_ draft: AppSettings) async {
    let source = backend
    let generation = backendGeneration
    await perform(action: "settings") { [self] in
      _ = try await source.request("/v1/settings", method: "PUT", body: Wire.data(draft))
      guard generation == backendGeneration else { return }
      settingsSaved = true
      Task {
        try? await Task.sleep(for: .seconds(2))
        if settings == draft { settingsSaved = false }
      }
    }
  }
  func startLink(phone: String? = nil) async {
    await perform { [self] in
      var body = ["method": phone == nil ? "qr" : "phone"]
      if let phone { body["phone"] = phone }
      try await backend.post("/v1/link/start", body)
    }
  }
  func linkEvents() async {
    let generation = backendGeneration
    let source = backend
    for attempt in 0..<3 {
      do {
        for try await event in source.events("/v1/link/events") {
          guard !Task.isCancelled, generation == backendGeneration else { return }
          link = try Wire.decoder().decode(LinkEvent.self, from: Data(event.data.utf8))
          if link.state == "ready" { await refresh(); return }
          if link.state == "error" { return }
        }
      } catch {
        guard !Task.isCancelled, generation == backendGeneration else { return }
      }
      guard !Task.isCancelled, generation == backendGeneration else { return }
      if attempt < 2 {
        do { try await retryPause(.seconds(attempt + 1)) } catch { return }
      }
    }
    link = LinkEvent(state: "error")
    link.reason = "Pairing was interrupted. Try linking again."
  }
  func detectMigration() async {
    do {
      let data = try await backend.request(
        "/v1/migrate/whatsapp-mcp", method: "POST", body: Wire.data(["dry_run": true]))
      migrationAvailable =
        (try? Wire.decoder().decode(MigrationPlan.self, from: data))?.available ?? false
    } catch { migrationAvailable = false }
  }
  func navigate(_ url: URL) {
    guard let link = DeepLink(url: url) else { return }
    switch link {
    case .approvals(let id):
      selectPage(.overview)
      selectedApproval = id
      showSetup = false
    case .agent(let id):
      showSetup = false
      settingsTab = .integrations
      showSettings = true
      selectedAgent = id
    case .person(let id):
      selectPage(.circles)
      selectedPerson = id
      circlePath = [.person(id)]
    case .health:
      settingsTab = .health
      showSettings = true
    }
  }
  func chatName(_ jid: String) -> String { chats.first { $0.jid == jid }?.name ?? "Chat" }
  func agentName(_ id: String) -> String {
    (id == "structure" || id == "Limmr") ? "Limmr" : id == "you" ? "You" : agents.first { $0.id == id }?.name ?? "Integration"
  }
  func senderName(_ id: String) -> String {
    if id == "me" { return "You" }
    if let person = people.first(where: { $0.jids.contains(id) }) {
      return person.label?.isEmpty == false ? person.label! : person.name
    }
    if id.hasSuffix("@s.whatsapp.net") { return "+" + String(id.split(separator: "@")[0]) }
    return "Unknown"
  }
  var todayActivity: [Activity] {
    let today = demo ? Timestamp.date(DemoData.now)! : Date()
    return allRecentActivity.filter { row in
      Timestamp.date(row.at).map { Calendar.current.isDate($0, inSameDayAs: today) } ?? false
    }
  }
  func expires(_ date: String) -> String {
    guard let end = Timestamp.date(date) else { return "Expires in 30 minutes" }
    let now = demo ? Timestamp.date(DemoData.now)! : Date()
    let minutes = max(0, Int(ceil(end.timeIntervalSince(now) / 60)))
    return minutes == 0 ? "Expiring now" : "Expires in \(minutes) min"
  }
  func time(_ date: String) -> String {
    guard let value = Timestamp.date(date) else { return "Recently" }
    return value.formatted(date: .omitted, time: .shortened)
  }
}

enum Page: String, CaseIterable, Identifiable {
  case overview = "Home"
  case circles = "People"
  case activity = "Activity"
  var id: String { rawValue }
  var symbol: String {
    switch self {
    case .overview: "house"
    case .circles: "person.2"
    case .activity: "clock"
    }
  }
}

enum SettingsTab: String, CaseIterable, Identifiable {
  case general = "General"
  case sources = "Sources"
  case integrations = "Integrations"
  case people = "People"
  case briefs = "Briefs"
  case hiddenChats = "Hidden chats"
  case remote = "Remote access"
  case health = "Health"
  case advanced = "Advanced"
  var id: String { rawValue }
  var symbol: String {
    switch self {
    case .general: "gearshape"
    case .sources: "tray.2"
    case .people: "person.2"
    case .integrations: "puzzlepiece.extension"
    case .briefs: "text.alignleft"
    case .hiddenChats: "eye.slash"
    case .remote: "network"
    case .health: "checkmark.circle"
    case .advanced: "slider.horizontal.3"
    }
  }
}

enum ConnectKind: String, Identifiable, CaseIterable {
  case chatgpt = "ChatGPT"
  case claude = "Claude"
  case claudeCode = "Claude Code"
  case codex = "Codex"
  case cursor = "Cursor"
  case other = "Other"
  var id: String { rawValue }
  var kind: String {
    switch self {
    case .chatgpt: "chatgpt"
    case .claude: "claude_ai"
    case .claudeCode: "claude_code"
    case .codex: "codex"
    case .cursor: "cursor"
    case .other: "custom"
    }
  }
  var remote: Bool { self == .chatgpt || self == .claude }
  var symbol: String {
    switch self {
    case .chatgpt: "bubble.left.and.bubble.right"
    case .claude: "asterisk"
    case .claudeCode: "chevron.left.forwardslash.chevron.right"
    case .codex: "terminal"
    case .cursor: "cursorarrow.rays"
    case .other: "sparkle"
    }
  }
}

/// Read-only loads wait quietly through a helper restart. Four waits total 15 seconds;
/// invalid payloads and non-transient HTTP failures surface immediately. Writes never retry.
enum CalmLoadRetry {
  static let delays: [Duration] = [.seconds(1), .seconds(2), .seconds(4), .seconds(8)]
  static func request(_ backend: any Backend, path: String,
    pause: @Sendable (Duration) async throws -> Void
  ) async throws -> Data {
    var startupProgress: String?
    var startupWait: TimeInterval = 0
    var startupDeadline: TimeInterval = 30
    let began = ProcessInfo.processInfo.systemUptime
    for attempt in 0...delays.count {
      while true {
        try Task.checkCancellation()
        do {
          let data = try await backend.request(path, method: "GET", body: nil)
          try Task.checkCancellation()
          return data
        } catch BackendError.starting(let progress) {
          startupWait = max(startupWait, ProcessInfo.processInfo.systemUptime - began)
          if progress != startupProgress {
            startupProgress = progress
            startupDeadline = startupWait + 30
          }
          guard startupWait < startupDeadline else { throw BackendError.starting(progress: progress) }
          try await pause(.seconds(1))
          startupWait += 1
        } catch {
          if error is CancellationError { throw error }
          try Task.checkCancellation()
          if let failure = error as? BackendError {
            switch failure {
            case .http(let status) where status == 408 || status == 429 || status >= 500: break
            default: throw error
            }
          }
          guard attempt < delays.count else { throw error }
          try await pause(delays[attempt])
          break
        }
      }
    }
    throw BackendError.invalidResponse
  }
}
