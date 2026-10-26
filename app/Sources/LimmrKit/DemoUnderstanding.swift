import Foundation

extension DemoCircles {
  /// Fourteen days of invented understanding, not traffic or decorative connections.
  static func understanding(
    _ details: [CircleDetail], proposals: [CircleProposal] = DemoCircles.proposals
  ) -> CircleMap {
    var nodes =
      details.map { CircleMapNode(id: $0.circle.id, kind: "circle", title: $0.circle.name) }
      + DemoData.chats.map { CircleMapNode(id: $0.jid, kind: "chat", title: $0.name) }
      + DemoData.people.enumerated().map { i, person in
        CircleMapNode(
          id: person.id, kind: "person", title: person.name,
          initials: person.name.split(separator: " ").prefix(2).compactMap(\.first).map(String.init)
            .joined(),
          weight: Double(3 + i % 7) / 10, lastActiveAt: DemoData.date(20 + i * 12),
          newMessages: i == 3 ? 2 : 0,
          circles: details.filter { $0.people.contains { $0.personId == person.id } }.map {
            $0.circle.id
          })
      }
    var edges = details.flatMap { detail in
      detail.chats.map {
        CircleMapEdge(
          source: detail.circle.id, target: $0.chatJid,
          id: "chat-\(detail.circle.id)-\($0.chatJid)", label: "Shared chat",
          origin: MapOrigin(
            by: "WhatsApp", byKind: "structure", at: DemoData.date(13 * 1440), evidence: 0),
          strength: 0.3, state: "confirmed")
      }
        + detail.people.map {
          CircleMapEdge(
            source: detail.circle.id, target: $0.personId, kind: "role",
            id: "role-\(detail.circle.id)-\($0.personId)", label: $0.role,
            origin: MapOrigin(
              by: "WhatsApp", byKind: "structure", at: DemoData.date(13 * 1440), evidence: 3),
            strength: 0.7, state: "confirmed")
        }
    }
    let story = [
      ("family", "person-02", "Claude Code", "role_set", "Set Mum’s role in Family"),
      ("friends", "person-04", "ChatGPT", "edge_added", "Linked Maya Rao to Close friends"),
      ("internship", "person-09", "Codex", "role_set", "Set Jordan as manager in Internship"),
      ("university", "person-07", "Claude Code", "role_set", "Set Dr. Morgan as HOD in University"),
      (
        "university", "person-05", "ChatGPT", "brief_updated",
        "Updated Noor’s contribution to University"
      ),
      ("proposal-1", "person-04", "Codex", "circle_proposed", "Proposed circle Reading weekend"),
      (
        "friends", "person-05", "Claude Code", "circle_confirmed", "Confirmed Noor in Close friends"
      ),
      ("internship", "person-10", "ChatGPT", "edge_added", "Linked Riley to Internship"),
      ("atlas", "person-12", "Codex", "topic_added", "Connected Robin to the Atlas site visit"),
      ("flatmates", "person-05", "Claude Code", "edge_added", "Noticed Noor is also in Flatmates"),
      ("proposal-2", "person-11", "ChatGPT", "circle_proposed", "Proposed circle Studio exchange"),
      (
        "atlas", "person-04", "Codex", "edge_added",
        "Linked Maya Rao to Atlas renovation as materials collaborator"
      ),
    ]
    for proposal in proposals {
      nodes.append(CircleMapNode(id: proposal.id, kind: "circle", title: proposal.circle.name))
    }
    let circleIDs = Set(details.map { $0.circle.id } + proposals.map(\.id))
    var events: [MapEvent] = []
    for (i, item) in story.enumerated() where circleIDs.contains(item.0) {
      let at = DemoData.date(i == 11 ? 35 : (13 - i) * 1440)
      let edgeID = "role-\(item.0)-\(item.1)"
      let origin = MapOrigin(
        by: item.3 == "circle_confirmed" ? "You" : item.2,
        byKind: item.3 == "circle_confirmed" ? "you" : "agent",
        at: at, evidence: 3, briefVersion: 3, changeId: "chg-\(i)")
      if let j = edges.firstIndex(where: { $0.id == edgeID }) {
        edges[j].origin = origin
      } else {
        edges.append(
          CircleMapEdge(
            source: item.0, target: item.1, kind: "role", id: edgeID,
            label: "Shared interest", origin: origin, strength: 0.65, state: "suggested"))
      }
      events.append(
        MapEvent(
          id: "ev-\(i)", at: at, kind: item.3, by: origin.byKind == "you" ? "You" : item.2,
          edgeId: edgeID, nodeIds: [item.1, item.0], text: item.4))
    }
    let ids = Set(edges.flatMap { [$0.source, $0.target] })
    nodes = nodes.filter { ids.contains($0.id) }
    let pulses =
      details.map { CirclePulse(id: $0.circle.id, newMessages: $0.chats.count * 2) }
      + Set(details.flatMap { $0.chats.map(\.chatJid) }).sorted().map {
        CirclePulse(id: $0, newMessages: 3)
      }
    return CircleMap(
      nodes: nodes, edges: edges, pulses: pulses, events: events.reversed(),
      summary: MapSummary(
        connections: edges.count, byAi: edges.filter { $0.origin?.byKind == "agent" }.count,
        thisWeek: events.filter { ($0.at > DemoData.date(7 * 1440)) }.count,
        latest: events.isEmpty ? "" : "Codex linked Maya Rao to Atlas renovation · 35 min ago"))
  }
}
