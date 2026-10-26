import Foundation

public enum DemoPeopleUnderstanding {
  /// Dense, wholly invented fixture for structure and privacy review.
  public static func map(_ base: CircleMap) -> CircleMap {
    let ids = ["university", "atlas", "family", "friends", "flatmates", "internship"]
    var nodes = base.nodes.filter { $0.kind == "circle" && ids.contains($0.id) }
    var edges: [CircleMapEdge] = []
    let existing = base.nodes.filter { $0.kind == "person" && $0.id != "me" }.prefix(12)
    for (i, value) in existing.enumerated() {
      var person = value
      let circle = value.circles?.first(where: { ids.contains($0) }) ?? base.edges.first(where: { $0.target == value.id && ids.contains($0.source) })?.source ?? ids[i % ids.count]
      person.primaryCircleId = circle; person.circles = [circle]
      person.circleAffinity = [.init(circleId: circle, score: 0.94, reasons: ["Member of 3 shared groups in this circle", "Recent direct contact"])]
      person.howKnown = person.id == "person-04" ? "You collaborate on the studio project." : "You know each other through shared projects."
      person.groupsInCommon = [.init(id: circle + "-planning@g.us", name: "Project planning", circleIds: [circle])]
      person.groupsInCommonCount = 1
      nodes.append(person)
      edges.append(CircleMapEdge(source: circle, target: person.id, label: "Shared circle", state: "confirmed"))
    }
    let names = ["Avery", "Casey", "Taylor", "Quinn", "Jamie", "Reese", "Drew", "Sage", "Blair", "Rowan", "Skyler", "Emery"]
    let surnames = ["Stone", "Brooks", "Vale", "Reed", "Lane", "Hart", "West", "Bell", "Fox"]
    for i in 0..<(108 - existing.count) {
      let circle: String? = i < (96 - existing.count) ? ids[i % ids.count] : nil
      let id = "demo-peer-\(i)"
      var node = CircleMapNode(id: id, kind: "person", title: names[i % 12] + " " + surnames[i / 12],
        weight: Double(100 - i) / 110, lastActiveAt: DemoData.date(40 + i * 23), circles: circle.map { [$0] } ?? [])
      node.primaryCircleId = circle
      if let circle {
        node.circleAffinity = [.init(circleId: circle, score: 0.92, reasons: ["Member of 3 shared groups in this circle"])]
        edges.append(CircleMapEdge(source: circle, target: id, label: "Shared groups", state: "confirmed"))
      }
      nodes.append(node)
    }
    return CircleMap(nodes: nodes, edges: edges, pulses: [])
  }
}
