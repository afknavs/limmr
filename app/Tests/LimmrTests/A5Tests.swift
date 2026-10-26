import AppKit
import SwiftUI
import Testing

@testable import Limmr
@testable import LimmrKit

@Suite("A5 People and Home") struct A5Tests {
  @MainActor @Test func homeBlockOrderAndOmission() async throws {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    #expect(
      HomePresentation.sections(store.home) == [
        "greeting", "waiting", "queued", "circles", "needs_reply", "recent_activity", "proposals",
        "integrations",
      ])
    #expect(HomePresentation.sections(.empty) == ["greeting", "integrations"])
    #expect(store.home.circles.filter(\.pinned).map(\.id) == ["university", "atlas"])
    #expect(store.home.circles.filter { !$0.pinned }.contains { $0.id == "family" })
    #expect(
      store.home.circles.filter { !$0.pinned }.allSatisfy {
        $0.newMessages > 0 || $0.openAsks > 0 || $0.upcoming > 0
      })
    var home = store.home
    home.watches = []
    home.waiting = []
    home.queued = []
    home.needsReply = []
    home.proposals = []
    #expect(
      HomePresentation.sections(home) == ["greeting", "circles", "recent_activity", "integrations"])
  }
  @MainActor @Test func homeFocusSentenceNamesWhatNeedsYou() async throws {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    let sentence = HomePresentation.focus(store.home)
    #expect(sentence.hasPrefix("\(store.home.queued.count) drafts wait for you"))
    #expect(sentence.hasSuffix("."))
    var home = store.home
    home.watches = []
    home.needsReply = Array(home.needsReply.prefix(1))
    home.queued = Array(home.queued.prefix(1))
    #expect(HomePresentation.focus(home) == "1 draft waits for you and 1 message needs a reply.")
    #expect(HomePresentation.focus(.empty) == "Nothing needs you right now.")
  }
  @MainActor @Test func leavingPeopleClosesTheOpenCircle() async {
    let store = AppStore(demo: true, snapshot: true)
    store.selectPage(.circles)
    store.circlePath = [.circle("close-friends")]
    store.selectPage(.circles)
    #expect(store.circlePath.isEmpty)
    store.circlePath = [.circle("close-friends")]
    store.selectPage(.overview)
    #expect(store.page == .overview && store.circlePath.isEmpty)
  }
  @MainActor @Test func variationAndEditedBodyOnWire() async throws {
    let backend = A5RecordingBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    let family = try #require(store.home.queued.first)
    let encoded = try #require(
      JSONSerialization.jsonObject(with: Wire.data(family)) as? [String: Any])
    #expect(encoded["voice"] == nil && encoded["signature"] == nil)
    var draft = QueuedDraftState(
      family, circle: store.circles.first { $0.id == "family" }, fallback: "")
    draft.selectVariation(1)
    #expect(draft.variation == 1 && draft.text == family.variations[1])
    await store.decideSuggestion(family, send: true, variation: draft.variation)
    #expect(await backend.requestValue == SuggestionSendRequest(variation: 1))
    #expect(store.activity.first?.note == family.variations[1])
    let university = try #require(store.home.queued.first { $0.circleId == "university" })
    draft = QueuedDraftState(university, circle: nil, fallback: "")
    draft.selectVariation(2)
    draft.edits[2] = "An exact edited body."
    draft.selectVariation(-1)
    draft.selectVariation(1)
    #expect(draft.text == "An exact edited body.")
    let request = draft.request
    await store.decideSuggestion(
      university, send: true, text: request.text, variation: request.variation
    )
    #expect(await backend.requestValue == request)
    #expect(store.activity.first?.note == "An exact edited body.")
    #expect(store.error == nil)
  }
  @MainActor @Test func draftBodyDoesNotDependOnCircleKind() async throws {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    let item = try #require(store.home.queued.first)
    for kind in ["family", "university"] {
      let draft = QueuedDraftState(
        item, circle: store.circles.first { $0.id == kind }, fallback: "")
      #expect(draft.text == item.variations[0])
    }
  }
  @MainActor @Test func swipeContextAndAccessibilityShareDismissAction() async throws {
    let backend = A5RecordingBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    #expect(!HomePresentation.shouldDismiss(translation: -90, width: 600))
    #expect(HomePresentation.shouldDismiss(translation: -360, width: 600))
    let rows = store.home.queued
    await store.dismissQueued(rows[0], trigger: .swipe(translation: -90, width: 600))
    #expect(await backend.dismissals == 0 && store.home.queued.count == rows.count)
    await store.dismissQueued(rows[0], trigger: .swipe(translation: -360, width: 600))
    await store.dismissQueued(rows[1], trigger: .context)
    await store.dismissQueued(rows[2], trigger: .accessibility)
    #expect(Set(store.home.queued.map(\.id)) == Set(rows.dropFirst(3).map(\.id)))
    #expect(await backend.dismissals == 3)
    #expect(await backend.requestValue == nil)
  }
  @MainActor @Test func pinsAndAskDonePersistAcrossReadback() async throws {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    let university = try #require(store.circles.first { $0.id == "university" })
    await store.togglePin(university)
    #expect(store.circles.first { $0.id == university.id }?.pinned == false)
    await store.togglePin(try #require(store.circles.first { $0.id == university.id }))
    #expect(
      store.home.circles.first?.id == university.id && store.home.circles.first?.pinned == true)
    let ask = try #require(store.home.needsReply.first)
    await store.completeAsk(ask)
    await store.refresh()
    #expect(store.home.needsReply.count == 2 && !store.home.needsReply.contains { $0.id == ask.id })
    #expect(HomePresentation.whatsappURL(jid: ask.chatJid)?.scheme == "whatsapp")
    #expect(HomePresentation.whatsappURL(jid: "123@g.us") == nil)
    #expect(HomePresentation.whatsappURL(jid: nil) == nil)
  }
  @MainActor @Test func searchFiltersBothTabsIncludingRolesAcrossCircles() async {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    #expect(PeoplePresentation.circles(store.circles, query: "univ").map(\.id) == ["university"])
    #expect(
      PeoplePresentation.people(store.people, circles: store.circles, query: "Maya").map(\.id) == [
        "person-04"
      ])
    #expect(
      PeoplePresentation.people(
        store.people, circles: store.circles, query: "Materials collaborator"
      ).map(\.id) == ["person-04"])
    let roles = PeoplePresentation.roles("person-04", circles: store.circles)
    #expect(
      roles.contains("Classmate · University") && roles.contains("Materials collaborator · Atlas"))
    #expect(
      PeoplePresentation.people(store.people, circles: store.circles, query: "no-such-person")
        .isEmpty)
  }
  @MainActor @Test func editorStartsWithCaretAtEnd() async {
    _ = NSApplication.shared
    let host = NSHostingView(
      rootView: DraftBodyEditor(text: .constant("Sample draft")).frame(width: 400, height: 60))
    let window = NSWindow(
      contentRect: NSRect(x: 0, y: 0, width: 400, height: 60), styleMask: [.borderless],
      backing: .buffered, defer: false)
    window.contentView = host
    host.layoutSubtreeIfNeeded()
    // Wait behind makeNSView's queued caret placement, regardless of machine load.
    await withCheckedContinuation { continuation in
      DispatchQueue.main.async { continuation.resume() }
    }
    func editor(_ view: NSView) -> NSTextView? {
      (view as? NSTextView) ?? view.subviews.compactMap { editor($0) }.first
    }
    #expect(editor(host)?.selectedRange() == NSRange(location: 12, length: 0))
    window.orderOut(nil)
  }
  @Test func additiveWireAndStructuredBriefAsks() throws {
    let json =
      #"{"queued":[{"id":"s","circle_id":"family","kind":"reply","created_by":"agent-2","body":"One","variations":["One","Two"],"voice":null,"default_voice":"as_me","allowed_voices":["as_me","as_agent"],"signature":"Signed","future":true}],"needs_reply":[],"circles":[],"recent_activity":[]}"#
    let home = try Wire.decode(HomeResponse.self, from: Data(json.utf8))
    #expect(home.queued.first?.body == "One")
    #expect(home.queued.first?.variations.count == 2)
    let brief =
      #"{"circle_id":"family","version":1,"summary":"Lunch","what_changed":"Time","open_asks_for_you":[{"id":"a","text":"When?","chat_jid":"15550000@s.whatsapp.net"}],"written_by":"agent-2","written_at":"","covers_until":""}"#
    #expect(try Wire.decode(CircleBrief.self, from: Data(brief.utf8)).openAsksForYou == ["When?"])
  }
}
private actor A5RecordingBackend: Backend {
  let samples = DemoBackend()
  var requestValue: SuggestionSendRequest?
  var dismissals = 0
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if path.hasSuffix("/send"), path.hasPrefix("/v1/suggestions/"), let body {
      requestValue = try Wire.decode(SuggestionSendRequest.self, from: body)
    }
    if path.hasSuffix("/dismiss") { dismissals += 1 }
    return try await samples.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}
