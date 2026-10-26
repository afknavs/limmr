import AppKit
import SwiftUI
import Testing

@testable import Limmr
@testable import LimmrKit

@Suite("A6 understanding and presence") struct A6Tests {
  private func map() async throws -> CircleMap { try await DemoBackend().get("/v1/map") }

  @Test func packingIsStableAndDoesNotOverlap() async throws {
    let map = try await map()
    for width in [524.0, 564, 760, 1100, 1440] {
      var layout = UnderstandingLayout()
      let size = CGSize(width: width, height: 240)
      layout.update(map: map, size: size)
      let first = layout.positions
      #expect(first.count == map.nodes.count, "Every node fits at \(width)")
      var independent = UnderstandingLayout()
      independent.update(map: map, size: size)
      #expect(first == independent.positions)
      layout.update(map: map, size: size)
      #expect(first == layout.positions)
      let dots = map.nodes.filter { $0.kind != "circle" }
      for (i, node) in dots.enumerated() {
        let point = try #require(first[node.id])
        let rect = UnderstandingLayout.footprint(node, at: point)
        #expect(CGRect(origin: .zero, size: layout.size).contains(rect))
        for other in dots.dropFirst(i + 1) {
          let p = try #require(first[other.id])
          #expect(!rect.intersects(UnderstandingLayout.footprint(other, at: p)))
        }
      }
      var added = map
      added.nodes.append(
        CircleMapNode(id: "a-new-node", kind: "person", title: "Ellis Reed", weight: 1))
      layout.update(map: added, size: size)
      #expect(layout.positions.count == added.nodes.count)
      for (id, p) in first where map.nodes.contains(where: { $0.id == id && $0.kind == "circle" }) {
        let next = try #require(layout.positions[id])
        #expect(hypot(p.x - next.x, p.y - next.y) <= 2)
      }
    }
  }
  @Test func eventDiffConsumesEachEventOnce() async throws {
    let events = try await map().events
    var diff = MapEventDiff()
    #expect(diff.ingest(Array(events.dropFirst())).isEmpty)
    #expect(diff.ingest(events).map(\.id) == [events[0].id])
    #expect(diff.ingest(events).isEmpty)
    #expect(diff.ingest([]).isEmpty)
    #expect(diff.ingest(events).isEmpty)
  }
  @MainActor @Test func refreshQueuesEveryNewEventWithoutRepeating() async throws {
    let backend = MapRefreshBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    #expect(store.mapArrivals.isEmpty)
    await backend.reveal()
    await store.refresh()
    #expect(store.mapArrivals.count == 2)
    let first = try #require(store.mapArrivals.first)
    let last = try #require(store.mapArrivals.last)
    #expect(last.at.timeIntervalSince(first.at) == 3)
    #expect(first.event.at < last.event.at)
    await store.refresh()
    #expect(store.mapArrivals.count == 2)
  }
  @Test func originStylingAndHoverText() async throws {
    let map = try await map()
    let structure = try #require(map.edges.first { $0.origin?.byKind == "structure" })
    let agent = try #require(map.edges.first { $0.source == "atlas" && $0.target == "person-04" })
    let you = try #require(map.edges.first { $0.origin?.byKind == "you" })
    let suggested = try #require(map.edges.first { $0.state == "suggested" })
    #expect(structure.style.tone == "neutral" && structure.style.width < agent.style.width)
    #expect(agent.style.tone == "neutral" && !agent.style.dashed)
    #expect(you.style.tone == "neutral" && !you.style.dashed)
    #expect(suggested.style.tone == "neutral" && suggested.style.dashed)
    #expect(agent.hoverText.contains("Materials collaborator · linked by Codex"))
    #expect(agent.hoverText.contains("from 3 messages"))
    let maya = try #require(map.nodes.first { $0.id == "person-04" })
    let text = UnderstandingPresentation.nodeText(
      maya, circles: DemoCircles.circles, now: Timestamp.date(DemoData.now)!)
    #expect(text.contains("Maya Rao") && text.contains("Classmate · University"))
    #expect(
      text.contains("Materials collaborator · Atlas renovation") && text.contains("last active"))
    #expect(
      map.events.first?.capsule(nodes: map.nodes) == "Codex · linked Maya Rao → Atlas renovation")
  }
  @Test func clickSelectsThenOpensAndEmptySpaceClears() async throws {
    let map = try await map()
    let person = try #require(map.nodes.first { $0.id == "person-04" })
    let circle = try #require(map.nodes.first { $0.id == "atlas" })
    var selection = MapSelection()
    #expect(selection.click(person) == nil)
    #expect(selection.tab == "People" && selection.person == person.id)
    #expect(selection.click(person) == person.id)
    #expect(selection.click(circle) == nil)
    #expect(selection.tab == "Circles" && selection.circle == "atlas" && selection.person == nil)
    #expect(selection.click(nil) == nil && selection.tab == nil)
    var layout = UnderstandingLayout()
    layout.update(map: map, size: CGSize(width: 760, height: 240))
    #expect(layout.hit(try #require(layout.positions[person.id]), map: map) == .node(person.id))
    #expect(layout.hit(CGPoint(x: 1, y: 1), map: map) == nil)
  }
  @MainActor @Test func presenceLineIsHiddenWhenIdleAndLinksToAgent() async throws {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    #expect(store.presence.first?.line == "Codex is reading University")
    var idle = try #require(store.presence.first)
    idle.state = "idle"
    #expect(idle.line == nil)
    store.presence = []
    #expect(!store.presence.contains { $0.line != nil })
    store.activityAgent = "agent-2"
    await store.filterActivity(agent: store.activityAgent, kind: "all", chat: "all")
    #expect(store.activity.allSatisfy { $0.agentId == "agent-2" })
    #expect(SnapshotRunner.pages.contains("people-map-hover"))
    #expect(SnapshotRunner.pages.contains("people-replay"))
  }
  @Test func demoStoryAndSinceMatchContract() async throws {
    let backend = DemoBackend()
    let map: CircleMap = try await backend.get("/v1/map")
    #expect(map.events.count == 12)
    #expect(map.edges.filter { $0.state == "suggested" }.count == 2)
    #expect(Set(map.events.map(\.by)).isSuperset(of: ["Codex", "Claude Code", "ChatGPT"]))
    #expect(map.summary?.connections == map.edges.count)
    #expect(map.nodes.contains { $0.weight != nil && $0.lastActiveAt != nil })
    let since = DemoData.date(1440)
    let recent: CircleMap = try await backend.get("/v1/map?since=\(since)")
    #expect(recent.events.count == 1)
    let customPhoto = try await backend.request("/v1/people/person-04/avatar", method: "GET", body: nil)
    #expect(NSImage(data: customPhoto) != nil)
    do {
      _ = try await backend.request("/v1/people/person-06/avatar", method: "GET", body: nil)
      Issue.record("A demo person without a fixture photo must return 404")
    } catch BackendError.http(let code) { #expect(code == 404) }
    let old = try Wire.decode(
      CircleMap.self,
      from: Data(#"{"nodes":[],"edges":[{"source":"a","target":"b"}],"pulses":[]}"#.utf8))
    #expect(old.events.isEmpty && old.edges.first?.style.tone == "neutral")
  }
  @MainActor @Test func avatarsUseImagesAndCacheMissesAndKeepFallbackStable() async throws {
    _ = NSApplication.shared
    #expect(AvatarTint.rgb(id: "person-04") == AvatarTint.rgb(id: "person-04"))
    #expect(AvatarTint.rgb(id: "person-04") == AvatarTint.palette[4])
    let backend = AvatarBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.loadAvatar("sample")
    await store.loadAvatar("sample")
    #expect(store.avatarData["sample"].flatMap(NSImage.init(data:)) != nil)
    #expect(await backend.requests == 1)
    await store.loadAvatar("missing")
    await store.loadAvatar("missing")
    #expect(store.avatarData["missing"] == nil && store.avatarAttempts.contains("missing"))
    #expect(await backend.requests == 2)
  }
}
private actor MapRefreshBackend: Backend {
  let samples = DemoBackend()
  var showingNewEvents = false
  func reveal() { showingNewEvents = true }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    let data = try await samples.request(path, method: method, body: body)
    guard path == "/v1/map", !showingNewEvents else { return data }
    var map = try Wire.decode(CircleMap.self, from: data)
    map.events = Array(map.events.dropFirst(2))
    return try Wire.data(map)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}
private actor AvatarBackend: Backend {
  var requests = 0
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    requests += 1
    guard path == "/v1/people/sample/avatar" else { throw BackendError.http(404) }
    return Data(
      base64Encoded:
        "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aF1sAAAAASUVORK5CYII="
    )!
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}
