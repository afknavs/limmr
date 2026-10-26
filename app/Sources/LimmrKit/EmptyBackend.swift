import Foundation

/// An explicitly unlinked workspace for UI validation. Contains no sample entities.
public struct EmptyBackend: Backend {
  public init() {}
  public func request(_ path: String, method: String, body: Data?) async throws -> Data {
    guard method == "GET" else { throw BackendError.unavailable("Link WhatsApp first.") }
    switch path.split(separator: "?").first {
    case "/v1/owner":return try Wire.data(OwnerProfile())
    case "/v1/setup/status": return try Wire.data(SetupStatus(stage: "new"))
    case "/v1/settings/briefs": return try Wire.data(BriefSettings())
    case "/v1/waiting": return try Wire.data(WaitingResponse(waiting: []))
    case "/v1/tasks": return try Wire.data(TasksResponse())
    case "/v1/efforts": return try Wire.data(EffortsResponse(efforts: []))
    case "/v1/home": return try Wire.data(HomeResponse.empty)
    case "/v1/circles": return try Wire.data(CirclesResponse(circles: []))
    case "/v1/presence": return try Wire.data(PresenceResponse(agents: []))
    case "/v1/map": return try Wire.data(CircleMap.empty)
    case "/v1/status":
      return try Wire.data(
        Status(
          linked: false, connected: false, accountName: "", pendingApprovals: 0, agents: [],
          health: "ok"))
    case "/v1/agents": return try Wire.data(AgentsResponse(agents: []))
    case "/v1/approvals": return try Wire.data(ApprovalsResponse(approvals: []))
    case "/v1/activity": return try Wire.data(ActivityPage(items: [], nextBefore: nil))
    case "/v1/health": return try Wire.data(HealthResponse(checks: [], health: "ok"))
    case "/v1/chats": return try Wire.data(ChatsResponse(chats: []))
    case "/v1/people": return try Wire.data(PeopleResponse(people: []))
    case "/v1/people/changes": return try Wire.data(ChangesResponse(changes: []))
    case "/v1/graph": return try Wire.data(Graph(nodes: [], edges: []))
    case "/v1/remote":
      return try Wire.data(
        RemoteState(
          enabled: false, publicUrl: "", mcpUrl: "", port: 18801, pendingCodes: [], clients: []))
    case "/v1/settings": return try Wire.data(AppSettings(owner: ""))
    default: throw BackendError.unavailable("Link WhatsApp first.")
    }
  }
  public func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}
