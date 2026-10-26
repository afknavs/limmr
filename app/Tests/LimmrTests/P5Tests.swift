import Foundation
import Testing
@testable import Limmr
@testable import LimmrKit

@Suite("P5 retained focus and budget") struct P5Tests {
  @Test func selectedPersonStaysWithinBudgetAndFocusKeepsAllFifty() {
    let map = UnderstandingPresentation.calmMap(P4DenseMapFixture.map, selected: "p4-peer-299")
    #expect(map.nodes.filter { $0.kind == "person" }.count == 120)
    #expect(map.nodes.contains { $0.id == "p4-peer-299" })
    let focus = UnderstandingPresentation.calmMap(P4DenseMapFixture.map, focus: "p4-circle-0")
    var layout = UnderstandingLayout()
    layout.update(map: focus, size: CGSize(width: 1100, height: 600), focused: true)
    #expect(focus.nodes.filter { $0.kind == "person" }.count == 50)
    #expect(layout.positions.count == 51 && layout.labels.count == 51)
  }
}
