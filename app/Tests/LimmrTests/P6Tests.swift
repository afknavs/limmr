import Foundation
import Testing
@testable import Limmr
@testable import LimmrKit

@Suite("P6 Drift field") struct P6Tests {
  @Test func fieldHasClearLabelsEvenSpacingAndStableRefreshes() throws {
    let map = UnderstandingPresentation.calmMap(P4DenseMapFixture.map)
    for width in [524.0, 760, 1100, 1440] {
      var layout = UnderstandingLayout()
      layout.update(map: map, size: CGSize(width: width, height: 280))
      #expect(width < 760 || layout.overviewContentHeight == 280)
      #expect(layout.positions.count == map.nodes.count)
      #expect(layout.labels.count == 8)
      let dots = map.nodes.filter { $0.kind == "person" }
      let canvas = CGRect(origin: .zero, size: layout.size)
      for (i, node) in dots.enumerated() {
        let p = try #require(layout.positions[node.id])
        #expect(canvas.contains(UnderstandingLayout.footprint(node, at: p)))
        for other in dots.dropFirst(i+1) {
          let q = try #require(layout.positions[other.id])
          #expect(hypot(p.x-q.x, p.y-q.y) >= UnderstandingLayout.minimumDotSpacing)
        }
      }
      let labels = Array(layout.labels.values)
      for (i, box) in labels.enumerated() {
        #expect(canvas.insetBy(dx: 8, dy: 8).contains(box))
        for other in labels.dropFirst(i+1) { #expect(!box.intersects(other)) }
        for dot in dots {
          #expect(!box.intersects(UnderstandingLayout.footprint(dot, at: try #require(layout.positions[dot.id]))))
        }
      }
      for circle in map.nodes.filter({ $0.kind == "circle" }) {
        let badge = try #require(layout.overviewBadge(circle.id))
        #expect(canvas.contains(badge))
        #expect(!labels.contains { $0.intersects(badge) })
        for dot in dots {
          #expect(!badge.intersects(UnderstandingLayout.footprint(dot, at: try #require(layout.positions[dot.id]))))
        }
      }
      let first = layout.positions, names = layout.labels
      #expect(dots.allSatisfy { layout.displayed($0.id, time: 0) == layout.displayed($0.id, time: 1000) })
      layout.update(map: map, size: layout.size)
      #expect(layout.positions == first && layout.labels == names)
      var reordered = map
      reordered.nodes.reverse(); reordered.edges.reverse()
      var independent = UnderstandingLayout()
      independent.update(map: reordered, size: layout.size)
      #expect(independent.positions == first && independent.labels == names)
      for dot in dots {
        layout.placeLabels(map: map, focused: false, extra: dot.id)
        #expect(layout.positions == first && layout.labels == names)
      }
    }
  }

  @Test func noCirclePeopleShareTheFieldAndRecencyRanksRemainOrdered() throws {
    var map = UnderstandingPresentation.calmMap(P4DenseMapFixture.map)
    map.nodes.append(CircleMapNode(id: "unplaced", kind: "person", title: "Ellis Reed"))
    var layout = UnderstandingLayout()
    layout.update(map: map, size: CGSize(width: 760, height: 280))
    let unplaced = try #require(map.nodes.last)
    #expect(layout.positions[unplaced.id] != nil)
    #expect(layout.driftEmphasis(unplaced, map: map) < 0.18)
    let recent = map.nodes.filter { $0.kind == "person" }.sorted(by: UnderstandingLayout.recent)
    let ranks = recent.compactMap { layout.recency[$0.id] }
    #expect(zip(ranks, ranks.dropFirst()).allSatisfy { $0 >= $1 })
  }
}
