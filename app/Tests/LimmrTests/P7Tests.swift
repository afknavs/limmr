import Foundation
import Testing
@testable import Limmr
@testable import LimmrKit

@Suite("P7 soft map regions") struct P7Tests {
  @Test func peopleStayWithinCapacityBasedRegions() throws {
    let map = UnderstandingPresentation.calmMap(P4DenseMapFixture.map)
    for width in [524.0, 760, 1100, 1440] {
      var layout = UnderstandingLayout()
      layout.update(map: map, size: CGSize(width: width, height: 280))
      let people = map.nodes.filter { $0.kind == "person" }
      for node in people {
        let circle = try #require(UnderstandingLayout.primary(node, map: map))
        let anchor = try #require(layout.positions[circle])
        let point = try #require(layout.positions[node.id])
        let radius = try #require(layout.regionRadii[circle])
        #expect(hypot(point.x-anchor.x, point.y-anchor.y) <= radius,
                "\(width): \(circle) has a member outside its soft region")
        #expect(radius < width * 0.45)
      }

    }
  }

  @Test func anchorsAreOrganicAndAllLabelEnvelopesRemainClear() throws {
    let map = UnderstandingPresentation.calmMap(P4DenseMapFixture.map)
    for width in [524.0, 760, 1100, 1440] {
      var layout = UnderstandingLayout()
      layout.update(map: map, size: CGSize(width: width, height: 280))
      let circles = map.nodes.filter { $0.kind == "circle" }
      #expect(layout.labels.count == circles.count)
      let anchors = try circles.map { try #require(layout.positions[$0.id]) }
      // The previous two rows (even with their alternating 7pt offsets) fail this.
      #expect(Set(anchors.map { Int($0.y / 8) }).count > 4)
      let envelopes = try circles.map { circle in
        try #require(layout.labels[circle.id]).union(try #require(layout.overviewBadge(circle.id)))
      }
      let canvas = CGRect(origin: .zero, size: layout.size).insetBy(dx: 8, dy: 8)
      for (index, box) in envelopes.enumerated() {
        #expect(canvas.contains(box))
        for other in envelopes.dropFirst(index+1) { #expect(!box.intersects(other)) }
        for node in map.nodes where node.kind == "person" {
          let point = try #require(layout.positions[node.id])
          #expect(!box.intersects(UnderstandingLayout.footprint(node, at: point)))
        }
      }
    }
  }

  @Test func assignmentAvoidsGreedyCapacityTails() {
    // Greedy rank-by-rank gives x=2 to the left region, then x=0 to the right.
    // Solving capacities together preserves the two nearest soft regions.
    let samples = [CGPoint(x: 0, y: 0), CGPoint(x: 2, y: 0), CGPoint(x: 9, y: 0), CGPoint(x: 11, y: 0)]
    let result = DriftRegions.assign(anchors: [CGPoint(x: 1, y: 0), CGPoint(x: 10, y: 0),
                                             CGPoint(x: 1, y: 0), CGPoint(x: 10, y: 0)],
                                    radii: [4, 4, 4, 4], samples: samples)
    #expect(Set([result[0], result[2]]) == Set([0, 1]))
    #expect(Set([result[1], result[3]]) == Set([2, 3]))
  }
}
