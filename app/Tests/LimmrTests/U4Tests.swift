import Foundation
import Testing

@testable import Limmr
@testable import LimmrKit

@Suite("U4 app flows") struct U4Tests {
  @MainActor @Test func historyAndIconsRefreshThenClearAcrossBackends() async throws {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    #expect(store.history?.days.count == 364)
    let circle = store.circles.first!
    #expect(
      await store.setIcon(bucket: "circles", id: circle.id, icon: .init(kind: "emoji", value: "🎓")))
    #expect(store.circles.first { $0.id == circle.id }?.icon?.value == "🎓")
    #expect(store.recentIcons.first?.value == "🎓")
    #expect(await store.setIcon(bucket: "circles", id: circle.id, icon: nil))
    #expect(store.circles.first { $0.id == circle.id }?.icon == nil)
    await store.useSampleData(false, realBackend: EmptyBackend())
    #expect(store.history == nil && store.recentIcons.isEmpty)
  }
  @MainActor @Test func replyDraftCreatesActionSuggestionAndDoneSurvivesRefresh() async throws {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    let ask = store.home.needsReply.first!
    let agent = store.agents.first { $0.enabled }!
    await store.requestReplyDraft(ask, agent: agent)
    #expect(store.requestedReplyDrafts.contains(ask.id))
    #expect(store.home.queued.filter { $0.taskId == nil }.allSatisfy { $0.kind == "reply" && !($0.body ?? "").isEmpty })
    struct Response: Decodable, Sendable { var suggestions: [CircleSuggestion] }
    let suggestions: Response = try await store.backend.get("/v1/suggestions")
    #expect(
      suggestions.suggestions.contains {
        $0.kind == "action" && $0.why.contains(ask.text) && $0.createdBy == agent.id
      })
    await store.completeAsk(ask)
    await store.refresh()
    #expect(!store.home.needsReply.contains { $0.id == ask.id })
  }
  @MainActor @Test func settingsSearchMatchesKeywordsAndSetupMenusFollowLinkState() async {
    #expect(SettingsSearchEntry.matches("retention").first?.label == "Keep activity for")
    #expect(SettingsSearchEntry.matches("apple intelligence").first?.tab == .briefs)
    #expect(SettingsSearchEntry.matches("nonexistent setting").isEmpty)
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    #expect(store.setupComplete)
    await store.useSampleData(false, realBackend: EmptyBackend())
    #expect(!store.setupComplete)
  }
}
