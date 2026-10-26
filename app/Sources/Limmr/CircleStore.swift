import LimmrKit
import SwiftUI

enum CircleDestination: Hashable {
  case circle(String)
  case person(String)
  case group(String)
  case chat(String, String)
  case brief(String, Int)
}
extension AppStore {
  var setupConnectKind: Binding<ConnectKind?> {
    Binding(get: { self.showSetup ? self.connectKind : nil }, set: { self.connectKind = $0 })
  }
  var settingsConnectKind: Binding<ConnectKind?> {
    Binding(get: { self.showSetup ? nil : self.connectKind }, set: { self.connectKind = $0 })
  }
  func reviewIntegration(_ id: String?) {
    selectedAgent = id
    settingsTab = .integrations
    connectKind = nil
    showConnect = false
    showSetup = false
    showSettings = true
  }
  func selectPage(_ page: Page) {
    pendingSearchPage = nil
    clearActivityRoute()
    if page != self.page { activityNavigationReady = false }
    // Sidebar navigation returns to the People lists, and leaving People closes any open circle.
    if page == .circles || page != self.page {
      circlePath = []
      selectedPerson = ""
    }
    self.page = page
  }
  func unseenMessages(_ chat: String) -> Int {
    max(
      0,
      (circleMap.pulses.first { $0.id == chat }?.newMessages ?? 0) - (seenMessageCounts[chat] ?? 0))
  }
  func markCircleSeen(_ id: String) {
    for chat in circleDetails[id]?.chats ?? circles.first(where: { $0.id == id })?.chats ?? [] {
      seenMessageCounts[chat.chatJid] =
        circleMap.pulses.first { $0.id == chat.chatJid }?.newMessages ?? 0
    }
  }
  func openCircle(_ id: String) {
    selectPage(.circles)
    circlePath = [.circle(id)]
    Task {
      await perform(action: "circle-seen") { [self] in
        try await backend.post("/v1/circles/\(id)/seen")
      }
    }
  }
  func loadCircle(_ id: String) async {
    let source = backend
    let generation = backendGeneration
    do {
      async let circle: LimmrKit.Circle = source.get("/v1/circles/\(id)")
      async let history: BriefsResponse = source.get("/v1/circles/\(id)/briefs")
      let (value, versions) = try await (circle, history)
      guard generation == backendGeneration else { return }
      guard circles.contains(where: { $0.id == id }) else { return }
      circleDetails[id] = CircleDetail(
        circle: value, chats: value.chats, people: value.people, topics: value.topics,
        brief: versions.briefs.first)
      circleHistory[id] = versions.briefs
      circleError = nil
    } catch {
      guard generation == backendGeneration else { return }
      circleError = "Couldn’t refresh this circle."
    }
  }
  func circleAction(
    _ id: String, suffix: String = "", method: String = "POST", values: [String: String] = [:]
  ) async {
    await perform(action: "circle-\(id)") { [self] in
      _ = try await backend.request(
        "/v1/circles/\(id)\(suffix)", method: method,
        body: method == "DELETE" ? nil : Wire.data(values))
      await refresh()
      if circles.contains(where: { $0.id == id }) { await loadCircle(id) }
    }
  }
  func keepProposal(_ id: String, name: String? = nil) async {
    if let name {
      await circleAction(id, method: "PATCH", values: ["name": name])
      guard error == nil else { return }
    }
    await circleAction(id, suffix: "/confirm")
    if error == nil && briefSettings.thisMacEnabled { await writeMacBriefs() }
  }
  func suggestionText(_ item: CircleSuggestion) -> String { item.body ?? item.draftText ?? "" }
  func decideSuggestion(
    _ item: CircleSuggestion, send: Bool, text: String? = nil,
    variation: Int = 0
  ) async {
    guard !send || !attemptedSuggestionSends.contains(item.id) else { return }
    if send { attemptedSuggestionSends.insert(item.id) }
    var rejectedBeforeDelivery = false
    var receivedOutcome = false
    await perform(action: "suggestion-\(item.id)") { [self] in
      do {
        let data = try await backend.request(
          "/v1/suggestions/\(item.id)/\(send ? "send" : "dismiss")", method: "POST",
          body: send
            ? Wire.data(
              SuggestionSendRequest(
                text: text,
                variation: variation)) : Wire.data([String: String]()))
        if send {
          let result = try Wire.decode(CircleDraft.self, from: data)
          receivedOutcome = true
          await refresh()
          if result.state != "sent" {
            failWithoutRetry(
              result.state == "failed"
                ? "This reply was not sent. Check Activity before creating a new draft."
                : "This reply’s delivery could not be confirmed. Check Activity before creating a new draft."
            )
          }
        } else {
          await refresh()
        }
      } catch {
        if let failure = error as? BackendError, case .http(let code) = failure,
          [400, 403, 404, 422].contains(code)
        {
          rejectedBeforeDelivery = true
        }
        throw error
      }
    }
    if send, error != nil {
      if rejectedBeforeDelivery {
        attemptedSuggestionSends.remove(item.id)
        failWithoutRetry(
          "This reply was rejected before delivery. Check its text, permissions and circle rules.")
      } else if !receivedOutcome {
        failWithoutRetry(
          "This reply’s delivery could not be confirmed. Check Activity before creating a new draft."
        )
      }
    }
  }

  func togglePin(_ circle: LimmrKit.Circle) async {
    await perform(action: "circle-\(circle.id)") { [self] in
      _ = try await backend.request(
        "/v1/circles/\(circle.id)", method: "PATCH", body: Wire.data(["pinned": !circle.pinned]))
      await refresh()
      await loadCircle(circle.id)
    }
  }
  func completeAsk(_ ask: ReplyAsk) async {
    await perform(action: "ask-\(ask.id)") { [self] in
      try await backend.post("/v1/circles/\(ask.circleId)/asks/\(ask.id)/done")
      await refresh()
    }
  }
  func openActivityBrief(_ row: Activity) async {
    await perform(action: "activity-brief") { [self] in
      let circleID =
        row.circleId ?? circles.first { Set($0.chats.map(\.chatJid)) == Set(row.chatJids) }?.id
      guard let id = circleID else {
        throw BackendError.unavailable("The circle for this brief is no longer available.")
      }
      await loadCircle(id)
      let history = circleHistory[id] ?? []
      let version =
        row.briefVersion
        ?? history.first { brief in
          guard let eventAt = Timestamp.date(row.at),
            let writtenAt = Timestamp.date(brief.writtenAt), writtenAt <= eventAt
          else { return false }
          return
            (row.note?.hasPrefix(brief.summary) == true
            || Set(brief.evidenceMessageIds) == Set(row.messageIds) && !row.messageIds.isEmpty)
        }?.version
      guard let version, history.contains(where: { $0.version == version }) else {
        throw BackendError.unavailable("This brief version is no longer available.")
      }
      selectPage(.circles)
      circlePath = [.circle(id), .brief(id, version)]
    }
  }
  func personName(_ id: String) -> String {
    if id == "me" && !ownerProfile.name.isEmpty { return ownerProfile.name }
    return people.first { $0.id == id }?.name ?? "Person"
  }
  func circleName(_ id: String) -> String { circles.first { $0.id == id }?.name ?? "Circle" }
  func canSee(_ agent: Agent, circle: LimmrKit.Circle) -> Bool {
    guard agent.enabled, let permissions = agent.permissions, permissions.readPeople,
      permissions.readMessages
    else { return false }
    return circle.chats.allSatisfy { chat in
      if settings.hiddenChats.contains(chat.chatJid) { return false }
      switch permissions.chats.mode {
      case "only": return permissions.chats.jids.contains(chat.chatJid)
      case "all_except": return !permissions.chats.jids.contains(chat.chatJid)
      default: return true
      }
    }
  }
}
