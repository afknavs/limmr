import AppKit
import Testing
@testable import Limmr
@testable import LimmrKit

@Suite("Lane 3 circles and people") struct Lane3Tests {
  @MainActor @Test func pictureChoiceSurvivesRefresh() async throws {
    let backend = DemoBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    let circle = try #require(store.circles.first)
    let choice = EntityIcon(kind: "monogram", value: "WS")
    #expect(await store.setIcon(bucket: "circles", id: circle.id, icon: choice))
    await store.refresh()
    #expect(store.circles.first { $0.id == circle.id }?.icon == choice)
    #expect(await store.setIcon(bucket: "circles", id: circle.id, icon: nil))
    let automatic = try #require(store.circles.first { $0.id == circle.id })
    #expect(CirclePictures.chosen(automatic) == EntityIcon(kind: "symbol", value: automatic.symbol))
    #expect(CirclePictures.suggestions(automatic).count == 3)
  }
  @Test func waitingRequiresSpecificContent() {
    let circle = DemoCircles.circles[0]
    let vague = HomeCircleTile(circle: circle, pinned: true, reason: "on you", brief: nil, newMessages: 0, rectangles: [])
    #expect(HomeTiles.waitingDescription(vague) == nil)
    let specific = HomeCircleTile(circle: circle, pinned: true, reason: "on you", brief: nil, newMessages: 0, rectangles: [.ask(id: "ask", text: "Reply to Maya about the venue", from: "Maya")])
    #expect(HomeTiles.waitingDescription(specific) == "Reply to Maya about the venue")
  }
  @Test func markersUseKnownLabelsAndSuppressCrowdedOrRevertedLinks() {
    let map = CircleMap(nodes: [CircleMapNode(id: "maya", kind: "person", title: "Maya"), CircleMapNode(id: "studio", kind: "circle", title: "Studio"), CircleMapNode(id: "old", kind: "circle", title: "Old")], edges: [.init(source: "studio", target: "maya", label: "Coordinator"), .init(source: "old", target: "maya", label: "Previous", state: "reverted")], pulses: [])
    let markers = MapConnectionMarker.make(map: map, positions: ["maya": CGPoint(x: 20, y: 20), "studio": CGPoint(x: 180, y: 80), "old": CGPoint(x: 180, y: 180)], selected: "maya")
    #expect(markers.count == 1)
    #expect(markers.first?.label.contains("Studio · Coordinator") == true)
    #expect(markers.first?.symbol == "person.2")
    #expect(MapConnectionMarker.make(map: map, positions: ["maya": .zero, "studio": CGPoint(x: 10, y: 10)], selected: "maya").isEmpty)
  }
}
