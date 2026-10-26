import Foundation
import LimmrKit

/// Read-only contract check. The output contains routes and coding paths only.
public enum DecodeAudit {
  @MainActor public static func run(backend: any Backend, emit: (String) -> Void) async -> Bool {
    var ok = true
    func check<T: Decodable>(_ route: String, _ type: T.Type) async -> T? {
      do {
        let data = try await backend.request(route, method: "GET", body: nil)
        let value = try Wire.decode(type, from: data, route: route, strictLists: true)
        emit("\(route)  ok")
        return value
      } catch {
        ok = false
        emit("\(route)  FAIL  \(path(error))")
        return nil
      }
    }
    _ = await check("/v1/home", HomeResponse.self)
    let circles = await check("/v1/circles", CirclesResponse.self)
    for circle in circles?.circles ?? [] {
      _ = await check("/v1/circles/\(component(circle.id))", Circle.self)
      _ = await check("/v1/circles/\(component(circle.id))/briefs", BriefsResponse.self)
    }
    let efforts = await check("/v1/efforts", EffortsResponse.self)
    for effort in efforts?.efforts ?? [] {
      _ = await check("/v1/efforts/\(component(effort.id))", EffortDetailResponse.self)
    }
    _ = await check("/v1/efforts/unnamed", EffortNamingResponse.self)
    _ = await check("/v1/activity", ActivityPage.self)
    _ = await check("/v1/activity/digests", ActivityDigests.self)
    let people = await check("/v1/people", PeopleResponse.self)
    for person in people?.people ?? [] {
      let route = "/v1/people/\(component(person.id))"
      _ = await check(route, PersonDetail.self)
      _ = await check(route + "/summary", PersonSummaryResponse.self)
    }
    let groups = await check("/v1/groups", GroupsResponse.self)
    for group in groups?.groups ?? [] {
      _ = await check("/v1/groups/\(component(group.jid))", GroupDetail.self)
    }
    let tasks = await check("/v1/tasks?status=all&limit=500", TasksResponse.self)
    for task in tasks?.tasks ?? [] {
      _ = await check("/v1/tasks/\(component(task.id))", TrackedTask.self)
    }
    _ = await check("/v1/sync", DotSyncStatus.self)
    let agents = await check("/v1/agents", AgentsResponse.self)
    for agent in agents?.agents ?? [] {
      _ = await check("/v1/agents/\(component(agent.id))", Agent.self)
    }
    _ = await check("/v1/presence", PresenceResponse.self)
    _ = await check("/v1/settings", AppSettings.self)
    _ = await check("/v1/settings/briefs", BriefSettings.self)
    _ = await check("/v1/settings/people", PeopleInferenceSettings.self)
    _ = await check("/v1/settings/people/contacts", ContactsSettingsState.self)
    _ = await check("/v1/people/to-understand", UnderstandingContacts.self)
    _ = await check("/v1/health", HealthResponse.self)
    _ = await check("/v1/status", Status.self)
    _ = await check("/v1/setup/status", SetupStatus.self)
    _ = await check("/v1/owner", OwnerProfile.self)
    _ = await check("/v1/remote", RemoteState.self)
    _ = await check("/v1/map", CircleMap.self)
    _ = await check("/v1/approvals", ApprovalsResponse.self)
    _ = await check("/v1/chats", ChatsResponse.self)
    _ = await check("/v1/people/changes", ChangesResponse.self)
    _ = await check("/v1/stats", DashboardStats.self)
    _ = await check("/v1/stats/history", StatsHistory.self)
    _ = await check("/v1/waiting", WaitingResponse.self)
    return ok
  }
  private static func component(_ value: String) -> String {
    value.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed.subtracting(CharacterSet(charactersIn: "/?#%"))) ?? ""
  }
  public static func path(_ error: any Error) -> String {
    let context: DecodingError.Context
    var missing: String?
    switch error {
    case DecodingError.keyNotFound(let key, let c): context = c; missing = key.stringValue
    case DecodingError.typeMismatch(_, let c), DecodingError.valueNotFound(_, let c), DecodingError.dataCorrupted(let c): context = c
    default: return "request"
    }
    return (["$"] + context.codingPath.map(\.stringValue) + (missing.map { [$0] } ?? [])).joined(separator: ".")
  }
}
