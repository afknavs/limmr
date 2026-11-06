import AppKit
import SwiftUI
import Testing
@testable import Limmr
@testable import LimmrKit

@Suite("N2 calm People", .serialized) struct N2PeopleTests {
  @MainActor @Test func personMapSelectionStaysOnPeopleAndClosesWithoutNavigation() {
    let store = AppStore(demo: true, snapshot: true)
    store.selectPage(.circles)
    let person = CircleMapNode(id: "person-04", kind: "person", title: "Maya Rao")
    store.circleMap = CircleMap(nodes: [person], edges: [], pulses: [])
    #expect(store.peoplePreview(person.id)?.name == "Maya Rao")
    store.selectPeopleMapNode(person)
    #expect(store.mapSelection.person == person.id && store.circlePath.isEmpty)
    store.closePeopleInspector()
    #expect(store.mapSelection.person == nil && store.circlePath.isEmpty)
    store.selectPeopleMapNode(person)
    store.openPeopleRoute(.person(person.id))
    #expect(store.circlePath == [.person(person.id)])
  }
  @MainActor @Test func circleLabelOpensCircleAndBackReturnsToPeople() {
    let store = AppStore(demo: true, snapshot: true)
    store.selectPage(.circles)
    store.selectPeopleMapNode(CircleMapNode(id: "university", kind: "circle", title: "University"))
    #expect(store.circlePath == [.circle("university")])
    #expect(store.peopleBackTitle == "People")
    store.leavePeopleRoute()
    #expect(store.page == .circles && store.circlePath.isEmpty)
  }
  @MainActor @Test func profileCacheIsImmediateAndIsolatedByBackend() async throws {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    let person = try #require(store.people.first { $0.id == "person-04" })
    #expect(person.name.isEmpty == false) // first render can use list identity
    await store.loadPersonProgressively(person.id)
    let detail = try #require(PersonPageCache.cached(person.id, store: store))
    store.profile = nil
    #expect(PersonPageCache.cached(person.id, store: store)?.person.name == detail.person.name)
    #expect(PersonPageCache.cached(person.id, store: AppStore(demo: true, snapshot: true)) == nil)
    await store.useSampleData(false, realBackend: EmptyBackend())
    #expect(PersonPageCache.cached(person.id, store: store) == nil)
  }
}

extension N2PeopleTests {
  @Test func miniMapIncludesOnlyDirectNeighboursAndNeverOwner() throws {
    var person = Person(id: "maya", jids: [], name: "Maya Rao")
    let nodes = [CircleMapNode(id: "maya", kind: "person", title: "Maya Rao"),
      .init(id: "peer", kind: "person", title: "Noor Chen"),
      .init(id: "distant", kind: "person", title: "Sage Reed")]
    let map = CircleMap(nodes: nodes, edges: [.init(source: "maya", target: "peer", kind: "knows", state: "confirmed"),
      .init(source: "peer", target: "distant", kind: "knows", state: "confirmed")], pulses: [])
    #expect(PersonNetworkPresentation.shouldShow(map, person: person, id: person.id))
    let mini = PersonNetworkPresentation.neighbourhood(map, id: person.id, name: person.name)
    #expect(Set(mini.nodes.map(\.id)) == ["maya", "peer"])
    #expect(mini.edges.count == 1)
    let scene = PersonNetworkPresentation.miniScene(NetworkMapProjection(map: mini, positions: [:]), selected: person.id)
    let camera = scene.focusedCamera(viewport: .init(width: 560, height: 196), selected: person.id, padding: .init(width: 28, height: 32))
    let a = camera.screen(scene.positions["maya"]!), b = camera.screen(scene.positions["peer"]!)
    #expect(hypot(a.x-b.x, a.y-b.y) > 32)
    person.isOwner = true
    #expect(!PersonNetworkPresentation.shouldShow(map, person: person, id: person.id))
    #expect(!PersonNetworkPresentation.shouldShow(map, person: nil, id: "me"))
    #expect(!PersonNetworkPresentation.shouldShow(.empty, person: nil, id: "isolated"))
    let decoded: Person = try Wire.decode(Person.self, from: Data(#"{"id":"owner-id","name":"Alex Example","is_owner":true}"#.utf8))
    #expect(decoded.isOwner == true)
  }
}

extension N2PeopleTests {
  @Test func circleLabelsDoNotOverlapAndClicksUseTheirDrawnFrames() {
    let nodes = (0..<9).map { CircleMapNode(id: "circle-\($0)", kind: "circle", title: "Design circle \($0)") }
    let positions = Dictionary(uniqueKeysWithValues: nodes.enumerated().map { ($0.element.id, CGPoint(x: 700 + Double($0.offset % 3) * 80, y: 400 + Double($0.offset / 3) * 80)) })
    let scene = NetworkMapProjection(map: .init(nodes: nodes, edges: [], pulses: []), positions: positions)
    let camera = NetworkMapCamera(viewport: .init(width: 480, height: 246), world: scene.world)
    let labels = NetworkCircleLabels.frames(scene: scene, camera: camera)
    #expect(labels.count == nodes.count)
    for (id, box) in labels {
      #expect(scene.hit(CGPoint(x: box.midX, y: box.midY), camera: camera) == id)
      #expect(!labels.contains { $0.key != id && $0.value.intersects(box) })
    }
  }
}
