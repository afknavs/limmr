import Foundation
import Testing
@testable import Limmr
@testable import LimmrKit

@Suite("A4 owner journeys", .serialized) @MainActor struct A4FlowTests {
  @Test func searchDestinationsAndReturnPaths() async throws {
    let backend = DemoBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    for (query, kind) in [("Maya", HomeSearchHit.Kind.person), ("Design Club", .chat), ("Pitch deck", .effort)] {
      let hits = try await HomeSearch.find(query, backend: backend)
      let hit = try #require(hits.first { $0.kind == kind })
      store.openHomeSearchHit(hit)
      switch kind {
      case .person:
        #expect(store.page == .circles && store.selectedPerson == hit.target)
        #expect(store.circlePath == [.person(hit.target)])
        await store.loadPerson()
        #expect(store.profile?.person.id == hit.target)
        store.circlePath.removeLast() // Person page's Back action.
        #expect(store.circlePath.isEmpty)
      case .chat:
        #expect(store.page == .circles && store.circlePath == [.chat(hit.target, hit.title)])
      case .effort:
        #expect(store.page == .activity && store.pendingActivityRoute == .effort(hit.target))
        let detail: EffortDetailResponse = try await backend.get("/v1/efforts/\(hit.target)")
        #expect(detail.effort.id == hit.target && !detail.timeline.isEmpty)
        store.clearActivityRoute() // Effort page's Activity action.
        #expect(store.page == .activity)
      }
    }
    // Activity search uses the same effort API; People search returns to its map path.
    let work: EffortsResponse = try await backend.get(HomeSearch.path("/v1/efforts", query: "Lab report", key: "q"))
    let effort = try #require(work.efforts.first)
    store.openEffort(effort.id)
    store.clearActivityRoute()
    store.selectPage(.circles)
    await store.searchPeople("Maya")
    let person = try #require(store.peopleSearchResults?.first)
    store.circlePath.append(.person(person.id))
    store.selectedPerson = person.id
    await store.loadPerson()
    #expect(store.profile?.person.id == person.id)
    store.circlePath.removeLast()
    #expect(store.circlePath.isEmpty && store.page == .circles)
  }

  @Test func circleTilePinPageUpdateAndReviewReadback() async throws {
    let backend = DemoBackend(structurePlacements: true)
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    let circle = try #require(store.circles.first { $0.id == "university" })
    await store.togglePin(circle) // Tile context menu.
    #expect(store.circleDetails[circle.id]?.circle.pinned == !circle.pinned)
    let pinned = try #require(store.circles.first { $0.id == circle.id })
    await store.togglePin(pinned) // Circle page button uses the same action.
    let read: LimmrKit.Circle = try await backend.get("/v1/circles/\(circle.id)")
    #expect(read.pinned == circle.pinned)
    await store.refreshHomeEfforts()
    let tiles = HomeTiles.tiles(circles: store.circles, home: store.home, efforts: store.homeEfforts,
      now: Timestamp.date(DemoData.now)!)
    let tile = try #require(tiles.first { $0.id == circle.id })
    #expect(tile.pinned || tile.reason == "on you")
    store.openCircle(tile.id)
    #expect(store.circlePath == [.circle(circle.id)])
    store.openEffort(try #require(tile.updates.first).effortId)
    #expect(store.page == .activity && (store.pendingActivityRoute != nil || !store.activityPath.isEmpty))
    let change = try #require(store.home.circleChanges.first)
    await store.undoCircleChanges([try #require(change.changeIds.first)], single: true)
    let remaining: CircleChangesResponse = try await backend.get("/v1/circles/changes")
    #expect(remaining.changes.first?.count == change.count - 1)
    await store.undoCircleChanges(try #require(remaining.changes.first).changeIds)
    let after: CircleChangesResponse = try await backend.get("/v1/circles/changes")
    #expect(after.changes.isEmpty && store.home.circleChanges.isEmpty)
    let fresh: LimmrKit.Circle = try await backend.get("/v1/circles/\(circle.id)")
    #expect(fresh.people.isEmpty)
    let reviewStore = AppStore(demo: true, snapshot: true)
    await reviewStore.refresh()
    let proposals = reviewStore.home.proposals
    #expect(proposals.count == 2)
    for (index, proposal) in proposals.enumerated() {
      await reviewStore.reviewProposals([proposal.id], keep: index == 0)
    }
    await reviewStore.refresh()
    #expect(reviewStore.home.proposals.isEmpty)
    #expect(reviewStore.circles.contains { $0.id == proposals[0].id })
    #expect(!reviewStore.circles.contains { $0.id == proposals[1].id })
  }

  @Test func pausedWatchStaysReachableAndCountdownAdvances() async throws {
    let backend = DemoBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    let watch = try #require(store.home.watches.first)
    let now = Timestamp.date(DemoData.now)!
    for (action, state) in [("pause", "paused"), ("resume", "active"), ("stop", "stopped")] {
      try await backend.post("/v1/watches/\(watch.id)/\(action)")
      await store.refresh()
      let read: WatchDetail = try await backend.get("/v1/watches/\(watch.id)")
      #expect(read.summary.status == state)
      let visible = HomeTriggerPresentation.visibleWatches(store.home.watches, now: now)
      #expect(visible.contains { $0.id == watch.id } == (state != "stopped"))
    }
    let timed = DemoHomeTriggers.watches[2]
    let before = HomeTriggerPresentation(timed, now: now)
    let after = HomeTriggerPresentation(timed, now: now.addingTimeInterval(3600))
    #expect(before.sourceGlyph == .time && after.waitProgress > before.waitProgress)
    let ticking = AppStore(demo: true)
    let start = ticking.demoClockStartedAt
    #expect(ticking.triggerDate(at: start.addingTimeInterval(3600)) == DemoData.epoch.addingTimeInterval(3600))
    #expect(store.triggerDate(at: start.addingTimeInterval(3600)) == DemoData.epoch)
    #expect(HomeTriggerPresentation(DemoHomeTriggers.watches[0], now: now, sourceIsPerson: true).sourceGlyph == .person)
    #expect(HomeTriggerPresentation(DemoHomeTriggers.watches[1], now: now).firedRecently)
    var expired = timed
    expired.expiresAt = DemoData.now
    #expect(HomeTriggerPresentation.visibleWatches([expired], now: now).isEmpty)
  }

  @Test func effortPinHidePersistsAndRemovesHomeAndSearch() async throws {
    let backend = DemoBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    let effort = DemoEfforts.efforts[0]
    #expect(await store.updateEffort(effort.id, values: ["pinned": true])?.pinned == true)
    let pinned: EffortDetailResponse = try await backend.get("/v1/efforts/\(effort.id)")
    #expect(pinned.effort.pinned)
    #expect(await store.updateEffort(effort.id, values: ["pinned": false])?.pinned == false)
    #expect(await store.updateEffort(effort.id, values: ["hidden": true])?.hidden == true)
    #expect(!store.home.circleUpdates.contains { $0.effortId == effort.id })
    #expect(!store.homeEfforts.contains { $0.id == effort.id })
    #expect(try await HomeSearch.find(effort.title, backend: backend).isEmpty)
  }

  @Test func replyRequestEditSendDenyAndVariations() async throws {
    let backend = DemoBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    let ask = try #require(store.home.needsReply.first)
    let agent = try #require(store.agents.first { $0.enabled })
    await store.requestReplyDraft(ask, agent: agent)
    let draft = try #require(store.home.queued.first { $0.id == "demo-reply-" + ask.id })
    #expect(draft.createdBy == agent.id && !(draft.draftText ?? "").isEmpty)
    await store.decideSuggestion(draft, send: true, text: "I’ll send the review tomorrow.")
    #expect(store.error == nil)
    let sent: ActivityPage = try await backend.get("/v1/activity")
    #expect(sent.items.contains { $0.kind == "sent" && $0.note == "I’ll send the review tomorrow." })
    let queued = store.home.queued
    let deny = try #require(queued.first)
    await store.decideSuggestion(deny, send: false)
    #expect(!store.home.queued.contains { $0.id == deny.id })
    let varied = try #require(store.home.queued.first)
    let expected = varied.variations[1]
    await store.decideSuggestion(varied, send: true, variation: 1)
    let history: ActivityPage = try await backend.get("/v1/activity")
    #expect(history.items.contains { $0.kind == "sent" && $0.note == expected })
    await store.completeAsk(ask)
    #expect(!store.home.needsReply.contains { $0.id == ask.id })
  }

  @Test func settingsAndYouBriefReadback() async throws {
    let backend = DemoBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    for value in [false, true] {
      store.settingsBinding(\.autoCircles).wrappedValue = value
      store.launchAtLoginBinding.wrappedValue = value
      await store.system.login.waitForChange()
      store.settingsBinding(\.retentionDays).wrappedValue = value ? 180 : 30
      store.settingsBinding(\.currentDays).wrappedValue = value ? 180 : 90
      await store.waitForSettingsSave()
      let read: AppSettings = try await backend.get("/v1/settings")
      #expect(read.autoCircles == value && read.retentionDays == (value ? 180 : 30))
      #expect(read.currentDays == (value ? 180 : 90))
      await store.refresh()
      #expect(store.settings.launchAtLogin == value && store.system.isLaunchAtLoginEnabled == value)
      await store.enableMacBriefs(value)
      let briefs: BriefSettings = try await backend.get("/v1/settings/briefs")
      #expect(briefs.thisMacEnabled == value)
      _ = try await backend.request("/v1/settings/people", method: "PUT", body: Wire.data(["enabled": value]))
      let people: PeopleInferenceSettings = try await backend.get("/v1/settings/people")
      #expect(people.enabled == value)
      try await backend.post("/v1/remote", ["enabled": value])
      let remote: RemoteState = try await backend.get("/v1/remote")
      #expect(remote.enabled == value)
    }
    let selfCircle = try #require(store.aboutYouCircle)
    _ = try await backend.request("/v1/circles/\(selfCircle.id)/brief", method: "PUT",
      body: Wire.data(["summary": "You are preparing the studio exhibition.", "written_by": "agent-2"]))
    await store.refresh()
    await store.loadCircle(selfCircle.id)
    #expect(store.aboutYouBrief?.summary == "You are preparing the studio exhibition.")
    #expect(store.aboutYouBrief?.writtenBy == "agent-2")
  }

  @Test func integrationLifecycleAndMenuDecisions() async throws {
    let backend = DemoBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    let created: CreatedAgent = try Wire.decode(CreatedAgent.self, from: await backend.request(
      "/v1/agents", method: "POST", body: Wire.data(["name": "Studio assistant", "kind": "custom"])))
    await store.refresh()
    #expect(store.agents.contains { $0.id == created.agent.id })
    try await store.renameAgent(created.agent.id, name: "Studio helper")
    let renamed: Agent = try await backend.get("/v1/agents/\(created.agent.id)")
    #expect(renamed.name == "Studio helper")
    _ = try await backend.request("/v1/agents/\(created.agent.id)", method: "DELETE", body: nil)
    await store.refresh()
    #expect(!store.agents.contains { $0.id == created.agent.id })
    #expect(!store.home.waiting.isEmpty)
    #expect(store.menuWaitingCount == store.pending.count + store.home.waiting.count)
    let pending = store.pending
    let approve = try #require(pending.first)
    await store.resolve(approve.id, approve: true, text: "The edited studio reply.")
    let deny = try #require(store.pending.first)
    await store.resolve(deny.id, approve: false)
    let read: ApprovalsResponse = try await backend.get("/v1/approvals")
    #expect(read.approvals.first { $0.id == approve.id }?.state == "sent")
    #expect(read.approvals.first { $0.id == approve.id }?.text == "The edited studio reply.")
    #expect(read.approvals.first { $0.id == deny.id }?.state == "denied")
    #expect(store.menuWaitingCount == pending.count - 2 + store.home.waiting.count)
  }
}
