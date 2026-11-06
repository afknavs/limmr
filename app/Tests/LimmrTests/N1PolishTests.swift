import SwiftUI
import Testing
@testable import Limmr
@testable import LimmrKit

@Suite("N1 polish", .serialized) @MainActor struct N1PolishTests {
  @Test func navigationKeepsSourceAndNamesBack() async {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    for page in [Page.overview, .tasks, .activity] {
      store.selectPage(page); store.openTask("tsk_study")
      #expect(store.sidebarDestination == "page:" + page.rawValue)
      #expect(store.activityBackTitle == page.rawValue)
      await store.activityDidAppear(); store.leaveActivityRoute()
      #expect(store.page == page && store.activityPath.isEmpty)
    }
    store.selectPage(.overview); store.openCircle("university")
    #expect(store.peopleBackTitle == "Home")
    #expect(store.sidebarDestination == "page:Home")
    store.circlePath.append(.person("person-04"))
    #expect(store.peopleBackTitle == store.circleName("university"))
    store.openTask("tsk_study")
    // N2: the chain started on Home, so the sidebar stays on Home. Moving the highlight
    // to a circle would make the split view reset the detail stack.
    #expect(store.sidebarDestination == "page:Home")
    #expect(store.activityBackTitle == store.personName("person-04"))
    await store.activityDidAppear(); store.leaveActivityRoute()
    store.leavePeopleRoute(); store.leavePeopleRoute()
    #expect(store.page == .overview && store.circlePath.isEmpty)
    store.openPeopleRoute(.person("person-04"))
    #expect(store.sidebarDestination == "page:Home" && store.peopleBackTitle == "Home")
    store.leavePeopleRoute(); #expect(store.page == .overview)
    // A circle opened from People keeps People highlighted; one opened from the sidebar highlights itself.
    store.selectSidebar("page:" + Page.circles.rawValue); store.openCircle("university")
    #expect(store.sidebarDestination == "page:" + Page.circles.rawValue && store.circlePath == [.circle("university")])
    store.selectSidebar("circle:university"); await store.sidebarCircleDidAppear()
    #expect(store.sidebarDestination == "circle:university" && store.circlePath == [.circle("university")])
    store.circlePath.append(.person("person-04"))
    #expect(store.sidebarDestination == "circle:university")
  }
  @Test func networkMembershipDeterminesClustersAndSharedMembers() {
    let a = CircleMapNode(id: "a", kind: "circle", title: "Studio")
    let b = CircleMapNode(id: "b", kind: "circle", title: "Campus")
    var one = CircleMapNode(id: "one", kind: "person", title: "Avery")
    one.circles = ["a"]
    var two = CircleMapNode(id: "two", kind: "person", title: "Casey")
    two.circles = ["b"]
    var shared = CircleMapNode(id: "shared", kind: "person", title: "Robin")
    shared.circles = ["a", "b"]
    let isolated = CircleMapNode(id: "isolated", kind: "person", title: "Sage")
    let map = CircleMap(nodes: [a, b, one, two, shared, isolated], edges: [], pulses: [])
    var layout = UnderstandingLayout()
    layout.updateNetwork(map: map, size: CGSize(width: 1600, height: 1000))
    let p = layout.positions
    func distance(_ a: String, _ b: String) -> Double { hypot(p[a]!.x - p[b]!.x, p[a]!.y - p[b]!.y) }
    #expect(distance("one", "a") < distance("one", "b"))
    #expect(distance("two", "b") < distance("two", "a"))
    #expect(abs(distance("shared", "a") - distance("shared", "b")) < 80)
    #expect(hypot(p["isolated"]!.x - 800, p["isolated"]!.y - 500) > 430)
  }
  @Test func mapUsesPersistedCircleHue() throws {
    var circle = try #require(DemoCircles.circles.first)
    circle.icon = .init(kind: "symbol", value: "book", hue: "pink")
    #expect(NetworkMapStyle.hue(circle.id, circle: circle) == .pink)
    #expect(NetworkMapStyle.hue(circle.id, circle: circle) == circle.hue)
  }

  @Test func miniMapFitsDirectNeighbourhoodWithScreenPadding() {
    let person = CircleMapNode(id: "person", kind: "person", title: "Avery")
    let hub = CircleMapNode(id: "hub", kind: "circle", title: "University")
    let peer = CircleMapNode(id: "peer", kind: "person", title: "Casey")
    let map = CircleMap(nodes: [person, hub, peer], edges: [
      .init(source: "person", target: "hub", kind: "member", state: "confirmed"),
      .init(source: "peer", target: "person", kind: "knows", state: "confirmed")], pulses: [])
    let scene = NetworkMapProjection(map: map, positions: ["person": .init(x: 800, y: 500), "hub": .init(x: 800, y: 940), "peer": .init(x: 100, y: 100)])
    for size in [CGSize(width: 760, height: 180), CGSize(width: 560, height: 180)] {
      let camera = scene.focusedCamera(viewport: size, selected: "person")
      for point in scene.positions.values {
        let screen = camera.screen(point)
        #expect(CGRect(origin: .zero, size: size).insetBy(dx: 16, dy: 16).contains(screen))
      }
    }
  }

  @Test func layoutCacheUsesCompleteRevisionAndReusesReorderedReopen() async throws {
    let a = CircleMapNode(id: "polish-cache-a", kind: "circle", title: "Studio")
    var person = CircleMapNode(id: "polish-cache-p", kind: "person", title: "Avery")
    person.circles = [a.id]
    var map = CircleMap(nodes: [a, person], edges: [], pulses: [])
    let before = NetworkMapLayoutCache.preparations
    let first = await NetworkMapLayoutCache.projection(map)
    map.nodes.reverse()
    for _ in 0..<3 { _ = await NetworkMapLayoutCache.projection(map) }
    #expect(NetworkMapLayoutCache.preparations == before + 1)
    #expect(NetworkMapLayoutCache.cached(map)?.positions == first.positions)
    map.nodes[0].circles = []
    #expect(NetworkMapLayoutCache.cached(map) == nil)
    _ = await NetworkMapLayoutCache.projection(map)
    #expect(NetworkMapLayoutCache.preparations == before + 2)
  }

}
