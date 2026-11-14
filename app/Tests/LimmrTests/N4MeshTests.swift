import Foundation
import LimmrKit
import Testing
@testable import Limmr

@Suite("N4 Paper mesh", .serialized) @MainActor struct N4MeshTests {
  @Test func selectedLinksKeepOnlyTheEightStrongestRealGroups() throws {
    let map = try N4SnapshotBackend().mesh()
    let scene = PaperMesh.prepare(map)
    let edges = PaperMesh.selectedEdges(scene,person:"n4-1")
    #expect(edges.count == 8)
    let neighbours = edges.map { $0.source == "n4-1" ? $0.target : $0.source }
    #expect(neighbours == ["n4-0","n4-3","n4-2","n4-4","n4-5","n4-6","n4-7","n4-8"])
    #expect(PaperMesh.selectedEdges(scene,person:"n4-1",faded:["n4-1"]).isEmpty)
  }
  @Test func platesAvoidEveryNodeAndEachOtherAtAllRequestedSizes() throws {
    for population in [220,500] {
      let map = try N4SnapshotBackend(population:population).mesh()
      for width in [1280.0,1030,1000] {
        let height = PaperPeopleNetwork.mapHeight(windowHeight:width == 1280 ? 800 : 740)
        // Sidebar 232, panel horizontal padding 64, inspector 256, gap 16.
        let size = CGSize(width:width-568,height:height-(width < 1100 ? 72 : 56))
        let scene = PaperMesh.prepare(map,size:size)
        let boxes = scene.meshLabelFrames.sorted { $0.key < $1.key }
        #expect(boxes.count == (population == 500 ? 9 : 4), "\(population) people, width \(width)")
        for (id,box) in boxes {
          #expect(CGRect(origin:.zero,size:size).insetBy(dx:23,dy:23).contains(box), "\(id) within margins at \(width)")
          for node in scene.nodes where node.kind == "person" {
            guard let point = scene.positions[node.id] else { continue }
            // Any dot may become the selected 30-point face plus its ring.
            let radius = max(19,scene.meshDiameters[node.id,default:0]/2+4)
            #expect(!PaperIslandLayout.intersects(box,point:point,radius:radius), "\(id) covers \(node.id), population \(population), width \(width)")
            if !(scene.meshMemberships[node.id] ?? []).isEmpty {
              #expect(!PaperIslandLayout.intersects(box,point:point,radius:scene.meshDiameters[node.id,default:0]/2+13), "\(id) covers island shape")
            }
          }
        }
        for i in boxes.indices { for j in (i+1)..<boxes.count {
          #expect(!boxes[i].value.intersects(boxes[j].value))
        } }
        var extent = boxes.reduce(CGRect.null) { $0.union($1.value) }
        for node in scene.nodes where node.kind == "person" && (node.id == "me" || !(scene.meshMemberships[node.id] ?? []).isEmpty) {
          let point = scene.positions[node.id]!, r = scene.meshDiameters[node.id]!/2+13
          extent = extent.union(CGRect(x:point.x-r,y:point.y-r,width:r*2,height:r*2))
        }
        let owner = scene.positions["me"]!
        extent = extent.union(CGRect(x:owner.x-18,y:owner.y+23,width:36,height:20))
        #expect(abs(extent.midX-size.width/2) < 0.01 && abs(extent.midY-size.height/2) < 0.01)
        #expect(min(abs(extent.width-(size.width-48)),abs(extent.height-(size.height-48))) < 0.01)
      }
    }
  }
  @Test func smallFixtureNeverGrowsBeyondOnePointSix() throws {
    let map = try N4SnapshotBackend(population:12).mesh()
    let result = PaperIslandLayout.prepare(map,size:CGSize(width:1032,height:640))
    #expect(result.diameters.values.max()! <= 34*1.6+0.01)
    #expect(result.diameters["me"] == 40)
  }
  @Test func seededFiveHundredPeopleStayVisibleAndDoNotOverlap() throws {
    let fixture = try N4SnapshotBackend(population: 500)
    let map = try fixture.mesh()
    let start = Date()
    let scene = PaperMesh.prepare(map)
    print("N4 Islands layout: \(Date().timeIntervalSince(start)*1000) ms")
    #expect(scene.nodes.filter { $0.kind == "person" && $0.id != "me" }.count == 500)
    #expect(scene.nodes.filter { $0.kind == "person" && !(scene.meshMemberships[$0.id] ?? []).isEmpty }.allSatisfy { scene.positions[$0.id] != nil })
    var reversed = map; reversed.nodes.reverse(); reversed.edges.reverse()
    let second = PaperMesh.prepare(reversed)
    #expect(scene.positions == second.positions)
    let people = scene.nodes.filter { $0.kind == "person" && scene.positions[$0.id] != nil }
    for i in people.indices { for j in (i+1)..<people.count {
      let a = try #require(scene.positions[people[i].id]), b = try #require(scene.positions[people[j].id])
      let minimum = (scene.meshDiameters[people[i].id, default: 0] + scene.meshDiameters[people[j].id, default: 0]) / (2 * PaperMesh.referenceScale)
      #expect(hypot(a.x-b.x, a.y-b.y) >= minimum)
    } }
    for node in people {
      let p = try #require(scene.positions[node.id]), r = scene.meshDiameters[node.id, default: 0]/2
      #expect(p.x-r >= 0 && p.y-r >= 0 && p.x+r <= scene.world.width && p.y+r <= scene.world.height)
    }
    let byID = Dictionary(uniqueKeysWithValues: map.nodes.map { ($0.id, $0) })
    for edge in scene.edges {
      let a = try #require(byID[edge.source]), b = try #require(byID[edge.target])
      let groups = Set(a.groupsInCommon?.map(\.id) ?? [])
      let sharesGroup = !groups.isDisjoint(with: b.groupsInCommon?.map(\.id) ?? [])
      #expect(sharesGroup)
      #expect(!(a.circles ?? []).isEmpty && !(b.circles ?? []).isEmpty)
    }
  }
  @Test func directionalNavigationChoosesSpatialNeighbour() {
    let points: [String:CGPoint] = ["origin":.zero,"right":CGPoint(x:40,y:1),"down":CGPoint(x:1,y:25),"left":CGPoint(x:-20,y:0)]
    #expect(PaperMapNavigation.next(from:"origin",dx:1,dy:0,positions:points,people:Set(points.keys)) == "right")
    #expect(PaperMapNavigation.next(from:"origin",dx:0,dy:1,positions:points,people:Set(points.keys)) == "down")
  }
  @Test func sharedMembersSitBetweenTheirCirclesAndLoosePeopleHaveNoLines() {
    var a = CircleMapNode(id: "a", kind: "person", title: "Maya Rao"); a.circles = ["circle-a"]
    var b = CircleMapNode(id: "b", kind: "person", title: "Isha Sen"); b.circles = ["circle-b"]
    var shared = CircleMapNode(id: "shared", kind: "person", title: "Nina Das"); shared.circles = ["circle-a", "circle-b"]
    let loose = CircleMapNode(id: "loose", kind: "person", title: "Theo Park")
    let map = CircleMap(nodes: [.init(id: "circle-a", kind: "circle", title: "University"), .init(id: "circle-b", kind: "circle", title: "Family"), a,b,shared,loose], edges: [], pulses: [])
    let scene = PaperMesh.prepare(map)
    #expect(scene.edges.isEmpty)
    let center = scene.positions[shared.id]!
    let first = scene.meshHubs["circle-a"]!, last = scene.meshHubs["circle-b"]!
    #expect(abs(hypot(center.x-first.x, center.y-first.y) - hypot(center.x-last.x, center.y-last.y)) < 100)
  }
}
