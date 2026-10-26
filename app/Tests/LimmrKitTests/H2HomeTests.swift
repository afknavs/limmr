import Foundation
import Testing
@testable import LimmrKit

@Suite("H2 Home tiles and trigger chains") struct H2HomeTests {
  let now = Timestamp.date(DemoData.now)!
  func circle(_ id: String, pinned: Bool = false) -> Circle {
    var value = DemoCircles.details[1].circle
    value.id = id; value.name = id; value.pinned = pinned
    return value
  }
  func effort(_ id: String, circle: String, status: String = "moving", due: Date? = nil) -> Effort {
    var work = DemoEfforts.efforts[0]
    work.id = id; work.circleIds = [circle]; work.status = status
    work.due = due.map { ISO8601DateFormatter().string(from: $0) }
    return work
  }
  @Test func surfacingUsesWorkAndUnreadEvidenceAndAllPinnedCircles() {
    let circles = [circle("pinned", pinned: true), circle("owner"), circle("due"), circle("changed"), circle("chatty"), circle("late"), circle("too-far"), circle("finished")]
    var home = HomeResponse.empty
    // The server-selected busy circle now surfaces even without an effort.
    home.circles = [.init(circle: circles[4], brief: nil, newMessages: 999, openAsks: 10, upcoming: 0)]
    home.circleUpdates = [.init(circle: .init(id: "changed", name: "Changed"), effortId: "news", title: "Update", status: "news", line: "A new date", lastAt: DemoData.now)]
    let work = [effort("a", circle: "owner", status: "on_you"),
      effort("b", circle: "due", due: now.addingTimeInterval(7 * 86400)),
      effort("c", circle: "late", due: now.addingTimeInterval(-1)),
      effort("d", circle: "too-far", due: now.addingTimeInterval(7 * 86400 + 1)),
      effort("e", circle: "finished", status: "done", due: now)]
    let tiles = HomeTiles.tiles(circles: circles, home: home, efforts: work, now: now)
    #expect(tiles.map(\.id) == ["pinned", "owner", "due", "changed", "chatty"])
    #expect(tiles.map(\.reason) == [nil, "on you", "due within 7 days", "changed since last look", "Busy"])
    #expect(tiles[0].updates.isEmpty)
    #expect(HomeTiles.dueSoon(ISO8601DateFormatter().string(from: now), now: now))
    #expect(!HomeTiles.dueSoon("invalid", now: now))
  }
  @Test func hiddenUnconfirmedAndUnpinnedSelfStayOutAndOrderIsStable() {
    var selfCircle = circle("self"); selfCircle.kind = "self"
    var proposed = circle("proposed", pinned: true); proposed.confirmed = false
    var hidden = effort("hidden", circle: "hidden", status: "on_you"); hidden.hidden = true
    let circles = [circle("z", pinned: true), circle("a", pinned: true), circle("hidden"), selfCircle, proposed]
    let work = [hidden, effort("self", circle: "self", status: "on_you")]
    let first = HomeTiles.tiles(circles: circles, home: .empty, efforts: work, now: now)
    let reversed = HomeTiles.tiles(circles: circles.reversed(), home: .empty, efforts: work.reversed(), now: now)
    #expect(first.map(\.id) == ["a", "z"])
    #expect(first.map(\.id) == reversed.map(\.id))
    selfCircle.pinned = true
    #expect(HomeTiles.tiles(circles: [selfCircle], home: .empty, efforts: [], now: now).count == 1)
  }
  @Test func rectanglesPrioritizeActionThenDueThenRecencyAndCapAtThree() throws {
    let work = [effort("old", circle: "one"), effort("due", circle: "one", due: now),
      effort("mine", circle: "one", status: "on_you"), effort("extra", circle: "one")]
    var home = HomeResponse.empty
    home.circleUpdates = work.map { .init(circle: .init(id: "one", name: "One"), effortId: $0.id, title: $0.title, status: $0.status, line: $0.summary, due: $0.due, lastAt: $0.lastAt) }
    let tiles = HomeTiles.tiles(circles: [circle("one")], home: home, efforts: work, now: now)
    let tile = try #require(tiles.first)
    #expect(tile.updates.map(\.effortId) == ["mine", "due", "extra"])
    #expect(HomeTiles.acknowledgedUpdates(tiles: tiles, unread: home.circleUpdates).isEmpty)
    #expect(HomeTiles.acknowledgedUpdates(tiles: tiles, unread: Array(home.circleUpdates.prefix(2))).isEmpty)
    #expect(HomeTiles.acknowledgedUpdates(tiles: tiles, unread: tile.updates).count == 3)
    #expect(["on_you", "moving", "waiting_on_them", "news", "done"].map(HomeTiles.stateShape) == ["filled", "ring", "ring", "square", "check"])
  }
  @Test func gridHasNoOrphanGapOrReordering() {
    for count in 0...21 {
      let rows = HomeTiles.rows(count: count)
      #expect(rows.flatMap { $0 } == Array(0..<count))
      #expect(rows.dropLast().allSatisfy { $0.count == 2 })
      if count > 0 { #expect(rows.last?.count == (count.isMultiple(of: 2) ? 2 : 1)) }
    }
  }
  @Test func triggerGlyphsUseIdentityOrScheduleAndOnlyRecentFiresSolidify() {
    let watches = DemoHomeTriggers.watches
    let person = HomeTriggerPresentation(watches[0], now: now, sourceIsPerson: true)
    let group = HomeTriggerPresentation(watches[1], now: now)
    let time = HomeTriggerPresentation(watches[2], now: now)
    #expect(person.sourceGlyph == .person && person.actor == "Codex")
    #expect(group.sourceGlyph == .group && group.actor == "You" && group.firedRecently)
    #expect(person.listeningLabel == "Listening, checked 4 min ago")
    #expect(time.sourceGlyph == .time && abs(time.waitProgress - 1.0 / 3) < 0.001)
    #expect(time.fireTime != nil && !time.firedRecently)
    #expect(HomeTriggerPresentation(watches[2], now: now.addingTimeInterval(200000)).waitProgress == 1)
    #expect(HomeTriggerPresentation(watches[2], now: now.addingTimeInterval(-200000)).waitProgress == 0)
    #expect(!HomeTriggerPresentation(watches[1], now: now.addingTimeInterval(901)).firedRecently)
    var expiryOnly = watches[0]; expiryOnly.expiresAt = watches[2].nextFireAt; expiryOnly.lastCheckedAt = nil
    let unknown = HomeTriggerPresentation(expiryOnly, now: now)
    #expect(unknown.sourceGlyph != .time && unknown.fireTime == nil)
    #expect(unknown.listeningLabel == "Listening, last check unavailable")
  }
  @Test func searchCombinesThreeAPIsAndSafelyEncodesQueries() async throws {
    let backend = SearchProbe()
    let query = "Maya + Noor & work? #1"
    let hits = try await HomeSearch.find(query, backend: backend)
    #expect(Set(hits.map(\.kind)) == [.person, .chat, .effort])
    let paths = await backend.paths
    #expect(paths.count == 3)
    for path in paths {
      let components = try #require(URLComponents(string: path))
      #expect(components.queryItems?.first?.value == query)
      #expect(!path.contains("+"))
      #expect(components.queryItems?.first?.name == (path.hasPrefix("/v1/efforts") ? "q" : "query"))
    }
    let empty = try await HomeSearch.find("  \n", backend: backend)
    #expect(empty.isEmpty)
    #expect(await backend.paths.count == 3)
  }
}

private actor SearchProbe: Backend {
  var paths: [String] = []
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    #expect(method == "GET" && body == nil)
    paths.append(path)
    if path.hasPrefix("/v1/people") { return try Wire.data(["people": Array(DemoData.people.prefix(1))]) }
    if path.hasPrefix("/v1/chats") { return try Wire.data(["chats": Array(DemoData.chats.prefix(1))]) }
    return try Wire.data(["efforts": Array(DemoEfforts.efforts.prefix(1))])
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { AsyncThrowingStream { $0.finish() } }
}
