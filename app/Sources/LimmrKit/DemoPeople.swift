import Foundation

/// Same invented directory and overlaps as the engine's Alex demo.
public enum DemoPeople {
  public static let groupNames = [
    "Family", "Close friends", "University class", "Unofficial class", "Design Club",
    "Internship team", "Cross-team studio", "Atlas renovation", "Flatmates",
  ]
  public static let members = [
    [0, 1, 2], [0, 3, 4], [0, 3, 4, 5, 6, 7], [0, 3, 4, 5], [0, 4, 5], [0, 8, 9], [0, 8, 9, 10],
    [0, 3, 10, 11], [0, 4, 5],
  ]
  public static let people: [Person] = [
    "Alex", "Mum", "Dad", "Maya Rao", "Noor Chen", "Sam Patel", "Dr. Morgan", "Ms. Taylor",
    "Jordan Lee", "Riley Park", "Casey Stone", "Robin Bell", "Avery Lane",
  ].enumerated().map { i, name in
    var person = Person(
      id: DemoData.personId(i), jids: [DemoData.personJid(i)], name: name, pushName: name,
      circle: "", role: "", howKnown: "", notes: "", updatedAt: DemoData.date(i + 1))
    if i == 3 { person.otherNames = ["Maya", "Mayu"] }
    if i == 3 || i == 4 { person.photo = .init(source: "custom") }
    person.howKnown = i == 3 ? "Classmate and collaborator on Atlas" : ""
    person.details = PersonDetails(
      phones: [
        .init(label: "mobile", value: String(format: "+1 555-01%02d", i), source: "whatsapp")
      ],
      emails: i == 3 ? [.init(label: "personal", value: "maya.rao@example.com")] : [],
      birthday: i == 3 ? "04-12" : i == 4 ? "1999-08-23" : "",
      handles: i == 3 ? [.init(label: "LinkedIn", value: "linkedin.com/in/maya-rao-example")] : [],
      location: i == 3 ? "Bengaluru" : "")
    person.detailSuggestions =
      i == 3
      ? [
        .init(
          id: "detail-email", field: "emails", label: "work", value: "maya.studio@example.com",
          source: "agent-2"),
        .init(id: "detail-pronouns", field: "pronouns", value: "she/her", source: "agent-2"),
      ] : []
    if i == 4 { person.details?.emails = [.init(label: "personal", value: "noor@example.com", source: "contacts")]; person.details?.sources = ["birthday": "contacts"] }
    return person
  }
  public static let groups: [PeopleGroup] = groupNames.enumerated().map { i, name in
    var group = PeopleGroup(
      jid: DemoData.groupJid(i), name: name, description: "Plans and updates for " + name,
      memberCount: members[i].count, iAmAdmin: false)
    group.lastActivity = DemoData.date(i == 8 ? 400 * 1440 : i * 4)
    group.isCommunity = i == 2
    group.parentJid = i == 3 ? DemoData.groupJid(2) : nil
    group.messages30 = 12 + i * 7
    group.circleIds = i == 4 ? ["university"] : []
    group.taskIds = []
    group.photo = i == 4
    group.icon = i == 2 ? EntityIcon(kind: "symbol", value: "books.vertical") : nil
    group.agentAccess = [.init(agentId: "agent-1", name: "Claude", read: true, send: "approval"), .init(agentId: "agent-2", name: "Codex", read: true, send: "never")]
    return group
  }
  public static func groups(for person: String) -> [PeopleGroup] {
    guard let i = people.firstIndex(where: { $0.id == person }) else { return [] }
    return groups.enumerated().compactMap { members[$0.offset].contains(i) ? $0.element : nil }
  }
  /// Compatibility fixture for the retired force-layout tests, generated on demand.
  public static func graph(count: Int = 22) -> Graph {
    var nodes = [GraphNode(id: "me", name: "You", kind: "me", circle: "", weight: 30)]
    var edges: [GraphEdge] = []
    for (i, group) in groups.enumerated() {
      nodes.append(
        .init(
          id: group.jid, name: group.name, kind: "group", circle: "",
          weight: Double(group.memberCount)))
      for member in members[i] {
        edges.append(
          .init(
            source: group.jid, target: DemoData.personId(member), weight: 1, recent: false,
            kind: "member"))
      }
    }
    for i in 0..<max(people.count, count - groups.count) {
      let p = people[i % people.count]
      let id = i < people.count ? p.id : "extra-\(i)"
      if id == "me" { continue }
      nodes.append(
        .init(
          id: id, name: i < people.count ? p.name : "Member \(i + 1)", kind: "person", circle: "",
          weight: Double(2 + i % 14)))
      edges.append(.init(source: "me", target: id, weight: Double(2 + i % 14), recent: false))
      if i >= people.count {
        edges.append(
          .init(
            source: groups[i % groups.count].jid, target: id, weight: 1, recent: false,
            kind: "member"))
      }
    }
    return Graph(nodes: nodes, edges: edges)
  }
}
