import AppKit
import SwiftUI
import Testing
@testable import Limmr
@testable import LimmrKit

@Suite("N1 avatar network", .serialized) struct N1MapTests {
  @MainActor @Test func fourHundredPeopleRemainPlacedStableAndCullWithoutLosingSelection() async throws {
    let base: CircleMap = try await DemoBackend().get("/v1/map")
    #expect(NetworkMapMetrics.aiThisWeek(base, now: Timestamp.date(DemoData.now)!) == 5)
    let map = DemoPeopleUnderstanding.network400(base)
    #expect(map.nodes.filter { $0.kind == "person" }.count == 400)
    var layout = UnderstandingLayout()
    layout.updateNetwork(map: map, size: CGSize(width: 1600, height: 1000))
    #expect(layout.positions.count == map.nodes.count)
    let first = layout.positions
    var reordered = map; reordered.nodes.reverse(); reordered.edges.reverse()
    layout.updateNetwork(map: reordered, size: CGSize(width: 1600, height: 1000))
    #expect(layout.positions == first)
    let scene = NetworkMapProjection(map: map, positions: first)
    let person = try #require(first["person-04"])
    let fit = NetworkMapCamera(viewport: CGSize(width: 1200, height: 750), world: scene.world)
    #expect(scene.visible(camera: fit).count == map.nodes.count)
    let camera = NetworkMapCamera(viewport: fit.viewport, world: scene.world, zoom: 3, focus: person)
    #expect(scene.visible(camera: camera).count < map.nodes.count)
    #expect(camera.screen(person) == CGPoint(x: 600, y: 375))
    #expect(scene.hit(camera.screen(person), camera: camera) == "person-04")
    let away = NetworkMapCamera(viewport: fit.viewport, world: scene.world, pan: CGSize(width: 10000, height: 10000))
    #expect(scene.visible(camera: away).isEmpty)
    #expect(camera.showsAvatars)
    #expect(!NetworkMapCamera(viewport: fit.viewport, world: scene.world, zoom: 0.45).showsAvatars)
    let related = scene.neighbours(of: "person-04")
    #expect(related.contains("person-05") && related.contains("n1-studio@g.us") && related.contains("family"))
    #expect(!related.contains("n1-peer-399"))
    let neighbourhood = scene.focusedCamera(viewport: fit.viewport, selected: "person-04")
    for id in related {
      if let point = scene.positions[id] { #expect(neighbourhood.isVisible(point, radius: 0)) }
    }
  }
  @MainActor @Test func densePreparationYieldsTheUIActorAndBothEndpointsReuseIt() async throws {
    let base: CircleMap = try await DemoBackend().get("/v1/map")
    let map = DemoPeopleUnderstanding.network400(base)
    let start = Date()
    let work = Task { await NetworkMapLayoutCache.projection(map) }
    try await Task.sleep(for: .milliseconds(30))
    // This main-actor continuation must run while the expensive layout is still off actor.
    #expect(Date().timeIntervalSince(start) < 2)
    let first = await work.value
    let cached = try #require(NetworkMapLayoutCache.cached(map))
    #expect(cached.positions == first.positions)
    #expect(cached.preparationMilliseconds == first.preparationMilliseconds)
    #expect(first.positions.count == map.nodes.count)
  }
  @Test func revertedEdgesDoNotHighlightAndMissingProfileHasNoInventedLinks() throws {
    let one = CircleMapNode(id: "one", kind: "person", title: "Avery Lane")
    let two = CircleMapNode(id: "two", kind: "person", title: "Casey Reed")
    let edge = CircleMapEdge(source: "one", target: "two", state: "reverted")
    let map = CircleMap(nodes: [one, two], edges: [edge], pulses: [])
    let scene = NetworkMapProjection(map: map, positions: [:])
    #expect(scene.neighbours(of: "one") == ["one"])
    let included = PersonNetworkPresentation.map(map, id: "missing", name: "Sage Hart")
    #expect(included.nodes.count == 3 && included.edges.count == 1)
    #expect(PersonNetworkPresentation.map(included, id: "missing", name: "Sage Hart").nodes.count == 3)
    var circle = try #require(DemoCircles.circles.first)
    circle.confirmed = true
    circle.people = [CirclePerson(personId: "missing", role: "Member", note: "", source: "you")]
    let connected = PersonNetworkPresentation.map(map, id: "missing", name: "Sage Hart", circles: [circle])
    #expect(connected.nodes.contains { $0.id == circle.id && $0.kind == "circle" })
    #expect(connected.edges.contains { $0.source == circle.id && $0.target == "missing" && $0.state == "confirmed" })
    circle.confirmed = false
    #expect(PersonNetworkPresentation.map(map, id: "missing", name: "Sage Hart", circles: [circle]).nodes.count == 3)
  }
  @MainActor @Test func previewImagesAreBoundedAndBackendChangesCannotLeakOldPhoto() async throws {
    _ = NSApplication.shared
    let data = try #require(N1MapPortraits.make().first)
    let image = try #require(NetworkAvatarThumbnail.decode(data))
    #expect(image.width <= 96 && image.height <= 96)
    #expect(NetworkAvatarThumbnail.decode(Data("invalid photo".utf8)) == nil)
    let backend = N1AvatarProbe(data: data)
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.loadAvatar("invented id")
    await store.loadAvatar("invented id")
    #expect(await backend.paths == ["/v1/people/invented%20id/avatar"])
    #expect(store.avatarData["invented id"] == data)
    await store.useSampleData(false, realBackend: EmptyBackend())
    #expect(store.avatarData.isEmpty)
  }
}

private actor N1AvatarProbe: Backend {
  let data: Data
  var paths: [String] = []
  init(data: Data) { self.data = data }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if path.hasSuffix("/avatar") { paths.append(path); #expect(method == "GET" && body == nil); return data }
    return try await EmptyBackend().request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { AsyncThrowingStream { $0.finish() } }
}
