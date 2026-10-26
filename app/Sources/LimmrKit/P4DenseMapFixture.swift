import Foundation

/// Wholly invented 300-person / eight-circle account. Four hundred confirmed memberships.
public enum P4DenseMapFixture {
  static let titles = ["Workshop collective", "Neighbourhood garden", "Reading companions", "Weekend walkers", "Studio collaborators", "Community fundraiser", "Evening classes", "Board game friends"]
  public static var circles: [Circle] {
    (0..<8).map { i in
      var c = DemoCircles.circles[0]
      c.id = "p4-circle-\(i)"; c.name = titles[i]; c.kind = "other"; c.confirmed = true
      c.chats = []; c.topics = []
      c.people = (0..<50).map { j in CirclePerson(personId: "p4-peer-\((i*37+j)%300)", role: "Member", note: "Shared project", source: "you") }
      return c
    }
  }
  public static var map: CircleMap {
    let cs = circles
    var nodes = cs.map { CircleMapNode(id: $0.id, kind: "circle", title: $0.name) }
    var edges: [CircleMapEdge] = []
    let first = ["Avery", "Casey", "Quinn", "Reese", "Sage", "Jamie", "Drew", "Emery", "Blair", "Rowan"]
    let last = ["Stone", "Brooks", "Lane", "Reed", "Hart", "Bell", "West", "Vale", "Fox", "Hill"]
    for i in 0..<300 {
      let id = "p4-peer-\(i)"
      let memberships = cs.filter { $0.people.contains { $0.personId == id } }.map(\.id)
      var node = CircleMapNode(id: id, kind: "person", title: first[i%10]+" "+last[(i/10)%10]+(i>=100 ? " \(i/100+1)" : ""), weight: Double((i*13)%100)/100, lastActiveAt: DemoData.date(i*80), circles: memberships)
      node.primaryCircleId = memberships.first
      node.howKnown = "You plan community projects together."
      node.circleAffinity = memberships.map { CircleAffinity(circleId: $0, score: 1, reasons: ["Confirmed circle member", "Recent direct contact"]) }
      node.groupsInCommon = memberships.map { GroupInCommon(id: $0+"-planning", name: "Planning group", circleIds: [$0]) }
      node.groupsInCommonCount = memberships.count
      nodes.append(node)
      for circle in memberships {
        edges.append(CircleMapEdge(source: circle, target: id, origin: MapOrigin(by: i%9 == 0 ? "Example AI" : "You", byKind: i%9 == 0 ? "agent" : "you", at: DemoData.date(i*80), evidence: 2), state: "confirmed"))
      }
    }
    return CircleMap(nodes: nodes, edges: edges, pulses: [], summary: MapSummary(connections: edges.count, byAi: edges.filter { $0.origin?.byKind == "agent" }.count, thisWeek: 0, latest: "You connected Avery Stone to Workshop collective after reviewing their shared planning groups and the community project brief."))
  }
}
