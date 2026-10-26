import Foundation

public struct HomeSearchHit: Identifiable, Equatable, Sendable {
  public enum Kind: String, Sendable { case person = "Person", chat = "Chat", effort = "Work" }
  public let kind: Kind
  public let target: String
  public let title: String
  public var id: String { kind.rawValue + ":" + target }
  public var symbol: String {
    switch kind { case .person: "person"; case .chat: "bubble.left"; case .effort: "rectangle.stack" }
  }
}

public enum HomeSearch {
  public static func path(_ route: String, query: String, key: String = "query") -> String {
    var parts = URLComponents()
    parts.queryItems = [.init(name: key, value: query), .init(name: "limit", value: "12")]
    return route + "?" + (parts.percentEncodedQuery ?? "").replacingOccurrences(of: "+", with: "%2B")
  }
  public static func find(_ query: String, backend: any Backend) async throws -> [HomeSearchHit] {
    let query = query.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !query.isEmpty else { return [] }
    async let people: PeopleResponse = backend.get(path("/v1/people", query: query))
    async let chats: ChatsResponse = backend.get(path("/v1/chats", query: query))
    async let work: EffortsResponse = backend.get(path("/v1/efforts", query: query, key: "q"))
    let (p, c, w) = try await (people, chats, work)
    try Task.checkCancellation()
    let hits = p.people.map { HomeSearchHit(kind: .person, target: $0.id, title: $0.label?.isEmpty == false ? $0.label! : $0.name) }
      + c.chats.map { HomeSearchHit(kind: .chat, target: $0.jid, title: $0.name) }
      + w.efforts.filter { !$0.hidden }.map { HomeSearchHit(kind: .effort, target: $0.id, title: $0.title) }
    func rank(_ hit: HomeSearchHit) -> Int {
      if hit.title.caseInsensitiveCompare(query) == .orderedSame { return 0 }
      return hit.title.lowercased().hasPrefix(query.lowercased()) ? 1 : 2
    }
    return Array(hits.sorted {
      if rank($0) != rank($1) { return rank($0) < rank($1) }
      return $0.title == $1.title ? $0.id < $1.id : $0.title.localizedStandardCompare($1.title) == .orderedAscending
    }.prefix(12))
  }
}
