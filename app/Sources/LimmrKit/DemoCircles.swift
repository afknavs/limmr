import Foundation

/// Same seven circles, people, roles, chat scope and proposals as engine --demo.
public enum DemoCircles {
  static func make(
    _ id: String, _ name: String, _ kind: String, _ symbol: String, _ rule: SendRule,
    confirmed: Bool = true
  ) -> Circle {
    Circle(
      id: id, name: name, kind: kind, symbol: symbol, description: "Alex’s \(name) circle",
      sendRule: rule, createdBy: confirmed ? "you" : "agent-2", confirmed: confirmed,
      updatedAt: DemoData.now)
  }
  static let baseCircles = [
    make("you", "You", "self", "person.crop.circle", .free),
    make("family", "Family", "family", "house", .free),
    make("friends", "Close friends", "friends", "heart", .free),
    make("university", "University", "university", "graduationcap", .confirm),
    make("internship", "Internship", "work", "briefcase", .confirm),
    make("atlas", "Atlas renovation", "project", "square.stack.3d.up", .free),
    make("flatmates", "Flatmates", "other", "key", .confirm),
  ]
  static let summaries = [
    "Alex is 20, studying psychology and freelancing on a renovation project. Keep Thursday afternoon free for coursework.",
    "Sunday lunch is at home. Mum is organising the meal and Dad can collect Alex from the station.",
    "Maya and Noor are planning a Saturday walk after coursework. The route starts at the library.",
    "The class has a psychology assignment due Tuesday. Dr. Morgan clarified the marking rubric; Ms. Taylor is collecting workshop ideas. Noor is booking the club room.",
    "Jordan needs Alex’s draft by Monday. The team agreed to a shorter onboarding note and Riley is testing the shared checklist with another team.",
    "Client Robin approved the living room layout. Contractor Casey needs the final tile choice; Maya, also Alex’s classmate, is preparing material samples.",
    "The flat is splitting the utilities bill. Noor, also a Design Club member, has the meter photo and Sam will arrange the repair visit.",
  ]
  static let changed = [
    "The weekly plan now includes a client review.", "Lunch moved to one o’clock.",
    "Maya found a shorter route.", "The unofficial class group shared a revised reading list.",
    "Cross-team feedback removed two redundant steps.", "Robin prefers darker bathroom tiles.",
    "The plumber can come Wednesday morning.",
  ]
  static let asks = [
    "Block two hours for revision.", "Tell Mum when the train arrives.",
    "Choose the morning or afternoon slot.", "Check the assignment references before submission.",
    "Send Jordan the revised draft after checking the examples.",
    "Choose a tile sample and confirm the site visit.",
    "Confirm someone can be home for the plumber.",
  ]
  static let dates = [
    "2026-10-20", "2026-10-18", "2026-10-24", "2026-10-20", "2026-10-19", "2026-10-21",
    "2026-10-21",
  ]
  static let upcoming = [
    "Weekly planning", "Sunday lunch at 13:00", "Library walk", "Psychology assignment due",
    "Manager review", "Site visit at 10:00", "Plumber visit",
  ]
  static let people = [
    [0], [0, 1, 2], [0, 3, 4], [0, 3, 4, 5, 6, 7], [0, 8, 9, 10], [0, 3, 10, 11], [0, 4, 5],
  ]
  static let roles = [
    ["You"], ["You", "Mum", "Dad"], ["You", "Friend", "Friend"],
    ["You", "Classmate", "Club member", "Classmate", "HOD", "Class teacher"],
    ["You", "Manager", "Teammate", "Cross-team colleague"],
    ["Designer", "Materials collaborator", "Contractor", "Client"],
    ["You", "Flatmate", "Flatmate"],
  ]
  // g indexes groups; p indexes direct chats (zero based).
  static let groupIndices = [[], [0], [1], [2, 3, 4], [5, 6], [7], [8]]
  static let dmIndices = [[0], [1, 2], [3, 4], [6, 7], [8], [11], [5]]
  public static let details: [CircleDetail] = baseCircles.enumerated().map { i, base in
    var circle = base
    circle.pinned = ["university", "atlas"].contains(circle.id)
    circle.chats =
      groupIndices[i].map {
        .init(
          chatJid: DemoData.groupJid($0), roleInCircle: "Group chat", includedSince: DemoData.now)
      }
      + dmIndices[i].map {
        .init(
          chatJid: DemoData.personJid($0), roleInCircle: "Direct chat", includedSince: DemoData.now)
      }
    circle.people = zip(people[i], roles[i]).map {
      .init(personId: DemoData.personId($0.0), role: $0.1, note: "", source: "you")
    }
    if circle.id == "atlas" {
      circle.topics = [
        .init(
          id: "atlas-tiles", title: "Bathroom tiles", status: "active",
          chats: circle.chats.map(\.chatJid), people: ["person-04", "person-11", "person-12"],
          keywords: ["tiles", "bathroom", "samples"], lastActivityAt: DemoData.now),
        .init(
          id: "atlas-site", title: "Site visit", status: "watching",
          chats: circle.chats.map(\.chatJid), people: ["person-11", "person-12"],
          keywords: ["visit", "Wednesday"], lastActivityAt: DemoData.now),
      ]
    }
    if circle.topics.isEmpty {
      let titles = [
        ["Coursework block", "Weekly plan"], ["Sunday lunch", "Station pickup"],
        ["Library walk", "Reading weekend"], ["Psychology assignment", "Workshop room"],
        ["Onboarding draft", "Shared checklist"], ["Bathroom tiles", "Site visit"],
        ["Utilities bill", "Plumber visit"],
      ][i]
      circle.topics = titles.enumerated().map { topicIndex, title in
        .init(
          id: "\(circle.id)-topic-\(topicIndex)", title: title,
          status: topicIndex == 0 ? "active" : "watching", chats: circle.chats.map(\.chatJid),
          people: circle.people.map(\.personId),
          keywords: title.lowercased().components(separatedBy: " "),
          lastActivityAt: DemoData.now)
      }
    }
    let brief = CircleBrief(
      circleId: circle.id, version: 3, summary: summaries[i], whatChanged: changed[i],
      openAsksForYou: [asks[i]], upcoming: [.init(date: dates[i], text: upcoming[i])],
      peopleNotes: circle.people.filter { $0.personId != "me" }.map {
        .init(personId: $0.personId, contribution: contribution(i, person: $0))
      }, evidenceMessageIds: ["\(circle.id)-00-00"], chatJids: circle.chats.map(\.chatJid),
      writtenBy: "agent-2", writtenAt: DemoData.date(15 + i * 8), coversUntil: DemoData.date(120))
    return CircleDetail(
      circle: circle, chats: circle.chats, people: circle.people, topics: circle.topics,
      brief: brief)
  }
  static func contribution(_ index: Int, person: CirclePerson) -> String {
    if index == 5 {
      return person.role == "Client"
        ? "Approved the layout; asked for darker tiles."
        : person.role == "Contractor"
          ? "Checking availability of the tile samples."
          : "Preparing material samples; also your classmate."
    }
    if index == 3 {
      switch person.role {
      case "Club member": return "Reserved the club room for Friday’s workshop; also your flatmate."
      case "HOD": return "Clarified that references count toward the assignment’s ten-mark rubric."
      case "Class teacher":
        return "Collecting two workshop ideas from each tutorial group by Monday."
      default:
        return person.personId == "person-04"
          ? "Compared the assignment reading list against the library catalogue."
          : "Uploaded the revised chapter list and offered to check citations."
      }
    }
    switch index {
    case 1:
      return person.role == "Mum"
        ? "Moved Sunday lunch to one so Alex can catch the morning train."
        : "Offered to collect Alex from platform two after the 12:10 train."
    case 2:
      return person.personId == "person-04"
        ? "Suggested the riverside route from the library for Saturday’s walk."
        : "Can join after ten and is bringing the borrowed field guide."
    case 4:
      switch person.role {
      case "Manager":
        return "Asked for a shorter onboarding draft with two concrete examples by Monday."
      case "Teammate": return "Tested the checklist and found the duplicate account-setup step."
      default:
        return "Compared the studio’s onboarding notes and suggested removing two repeated steps."
      }
    case 6:
      return person.role == "Flatmate" && person.personId == "person-05"
        ? "Photographed the meter reading and calculated each person’s share of the utilities bill."
        : "Booked the plumber for Wednesday morning and will meet them at the gate."
    default: return "Kept Thursday afternoon clear for the psychology revision block."
    }
  }

  public static let circles = details.map(\.circle)
  static func proposal(
    _ id: String, _ name: String, _ kind: String, _ symbol: String, _ rule: SendRule,
    chats: [String]
  ) -> Circle {
    var circle = make(id, name, kind, symbol, rule, confirmed: false)
    circle.chats = chats.map {
      .init(chatJid: $0, roleInCircle: "Shared activity", includedSince: DemoData.now)
    }
    return circle
  }
  public static let proposals: [CircleProposal] = [
    .init(
      id: "proposal-1",
      circle: proposal(
        "proposal-1", "Reading weekend", "friends", "book", .free,
        chats: [DemoData.personJid(3), DemoData.personJid(4)]),
      why: "Maya and Noor share reading plans across two chats.", source: "agent-2"),
    .init(
      id: "proposal-2",
      circle: proposal(
        "proposal-2", "Studio exchange", "community", "leaf", .confirm,
        chats: [DemoData.groupJid(4), DemoData.groupJid(6)]),
      why: "Shared members connect Design Club and the cross-team studio.", source: "agent-2"),
  ]
  public static let suggestions: [CircleSuggestion] = [
    .init(
      id: "suggestion-1", circleId: "family", kind: "reply", chatJid: DemoData.personJid(1),
      draftText: "I’ll check the train and let you know this evening.",
      why: "Mum needs an arrival time.", createdBy: "agent-2", expiresAt: "2026-10-18T12:00:00Z",
      createdAt: DemoData.date(60)),
    .init(
      id: "suggestion-2", circleId: "university", kind: "reply", chatJid: DemoData.personJid(7),
      draftText: "Could we use the workshop room after class?",
      why: "The club needs a room. Parked from a background run for confirmation.",
      createdBy: "agent-2", expiresAt: "2026-10-18T12:00:00Z", createdAt: DemoData.date(60)),
    .init(
      id: "suggestion-3", circleId: "atlas", kind: "reply", chatJid: DemoData.personJid(11),
      draftText:
        "I’ll bring the darker tile samples to Wednesday’s visit.",
      why: "Robin asked to compare the darker samples.", createdBy: "agent-2",
      expiresAt: "2026-10-18T12:00:00Z", createdAt: DemoData.date(60)),
  ]
  public static func home(
    details: [CircleDetail], suggestions: [CircleSuggestion], proposals: [CircleProposal]
  ) -> HomeResponse {
    var home = HomeResponse(
      today: ["university", "atlas", "internship", "family", "friends", "flatmates", "you"]
        .compactMap { id in
          guard let detail = details.first(where: { $0.circle.id == id }) else { return nil }
          return CircleToday(
            circle: detail.circle, brief: detail.brief, newMessages: detail.chats.count * 2,
            openAsks: detail.brief?.openAsksForYou.count ?? 0,
            upcoming: detail.brief?.upcoming.count ?? 0)
        }, suggestions: suggestions, proposals: proposals,
      integrations: DemoData.agents.filter(\.enabled).map {
        .init(id: $0.id, name: $0.name, kind: $0.kind, lastSeenAt: $0.lastSeenAt)
      })
    home.queued = suggestions.filter { $0.kind == "reply" }.map { value in
      var item = value
      item.body = item.draftText ?? ""
      item.variations = [
        item.body ?? "",
        item.circleId == "family"
          ? "Train at 12:40. I’ll confirm by nine."
          : "Thanks, I’ll check and get back to you this evening.",
        item.circleId == "family"
          ? "I’ll send you the arrival time tonight." : "I’ll have an update for you tomorrow.",
      ]
      return item
    }
    home.circles = HomePresentation.circles(
      home.today.map { value in
        var card = value
        card.pinned = value.circle.pinned
        card.whyShown = card.pinned ? "pinned" : "open_ask"
        if !card.pinned && card.id != "family" {
          card.newMessages = 0
          card.openAsks = 0
        }
        return card
      })
    home.needsReply = [
      .init(
        id: "ask-room", circleId: "university", circle: "University",
        text: "Could we use the workshop room?", chat: "Ms. Taylor", chatJid: DemoData.personJid(7),
        from: "Ms. Taylor", at: DemoData.now),
      .init(
        id: "ask-tiles", circleId: "atlas", circle: "Atlas renovation",
        text: "Which sample should I order?", chat: "Casey", chatJid: DemoData.personJid(10),
        from: "Casey", at: DemoData.now),
      .init(
        id: "ask-review", circleId: "internship", circle: "Internship",
        text: "Can you review the examples by Monday?", chat: "Jordan",
        chatJid: DemoData.personJid(8), from: "Jordan", at: DemoData.now),
    ]
    return home
  }
}
