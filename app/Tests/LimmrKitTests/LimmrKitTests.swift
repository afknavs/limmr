import Foundation
import Testing
@testable import LimmrKit

@Suite("Wire contracts") struct WireTests {
    @Test func snakeCaseStatus() throws {
        let data = Data(#"{"linked":true,"connected":true,"account_name":"Alex","pending_approvals":2,"agents":[{"name":"Codex","last_seen_at":"2026-10-16T09:30:00Z"}],"health":"ok"}"#.utf8)
        let status = try Wire.decoder().decode(Status.self, from: data)
        #expect(status.accountName == "Alex")
        #expect(status.pendingApprovals == 2)
        #expect(status.agents[0].lastSeenAt != nil)
    }
    @Test func permissionsRoundTrip() throws {
        var permissions = Permissions.defaults(remote: false)
        permissions.chats = .init(mode: "only", jids: ["design@g.us", "study@g.us"])
        let data = try Wire.data(permissions)
        let object = try #require(JSONSerialization.jsonObject(with: data) as? [String: Any])
        #expect(object["read_messages"] as? Bool == true)
        #expect(object["request_send"] as? Bool == true)
        #expect(try Wire.decoder().decode(Permissions.self, from: data) == permissions)
    }
    @Test func linkEvents() throws {
        let qr = try Wire.decoder().decode(LinkEvent.self, from: Data(#"{"state":"qr","code":"DEMO","expires_in":60}"#.utf8))
        #expect(qr.expiresIn == 60)
        let sync = try Wire.decoder().decode(LinkEvent.self, from: Data(#"{"state":"syncing","progress":0.5,"chats":128,"messages":1000}"#.utf8))
        #expect(sync.chats == 128)
        let ready = try Wire.decoder().decode(LinkEvent.self, from: Data(#"{"state":"ready","account":{"name":"Alex","phone_masked":"+1 555 …"}}"#.utf8))
        #expect(ready.account?.phoneMasked == "+1 555 …")
    }
    @Test func demoModelsRoundTrip() throws {
        #expect(try Wire.decoder().decode([Activity].self, from: Wire.data(DemoData.activity)) == DemoData.activity)
        #expect(try Wire.decoder().decode(Graph.self, from: Wire.data(DemoData.graph())) == DemoData.graph())
        #expect(try Wire.decoder().decode([Approval].self, from: Wire.data(DemoData.approvals)) == DemoData.approvals)
    }
}

@Suite("Permissions in plain words") struct PermissionTests {
    @Test func scopes() {
        #expect(ChatScope().summary == "Can read all visible chats")
        #expect(ChatScope(mode: "only", jids: []).summary == "Cannot read any chats")
        #expect(ChatScope(mode: "only", jids: ["a"]).summary == "Can read 1 chat")
        #expect(ChatScope(mode: "only", jids: ["a", "b"]).summary == "Can read 2 chats")
        #expect(ChatScope(mode: "all_except", jids: ["a"]).summary == "Can read all chats except 1")
    }
    @Test func remoteDefaults() {
        let permissions = Permissions.defaults(remote: true)
        #expect(permissions.readMessages && permissions.search && permissions.readPeople)
        #expect(!permissions.requestSend && !permissions.downloadMedia && !permissions.writePeople && !permissions.saveSummaries)
    }
    @Test func localDefaults() {
        let permissions = Permissions.defaults(remote: false)
        #expect(permissions.requestSend && permissions.downloadMedia && permissions.writePeople && permissions.saveSummaries)
    }
}

@Suite("Approval decisions") struct ApprovalTests {
    @Test func editAndApprove() throws {
        var approval = DemoData.approvals[0]
        try approval.transition(to: "edited", text: "Thursday at four?\n\n— Codex, on behalf of Alex")
        try approval.transition(to: "sent")
        #expect(approval.state == "sent")
        #expect(approval.text == "Thursday at four?\n\n— Codex, on behalf of Alex")
        #expect(throws: BackendError.self) { try approval.transition(to: "sent") }
    }
    @Test(arguments: ["denied", "expired", "failed"]) func terminalStates(state: String) throws {
        var approval = DemoData.approvals[0]
        try approval.transition(to: state)
        #expect(!approval.isPending)
        #expect(throws: BackendError.self) { try approval.transition(to: "sent") }
    }
    @Test func demoResolvesOnlyOnce() async throws {
        let backend = DemoBackend()
        try await backend.post("/v1/approvals/a1/approve", ["text": "Edited exact text"])
        let response: ApprovalsResponse = try await backend.get("/v1/approvals")
        #expect(response.approvals[0].text == "Edited exact text")
        #expect(response.approvals[0].state == "sent")
        await #expect(throws: BackendError.self) { try await backend.post("/v1/approvals/a1/deny", Empty()) }
    }
    @Test func demoPersistsPermissions() async throws {
        let backend = DemoBackend()
        var permissions = Permissions.defaults(remote: false)
        permissions.chats = .init(mode: "only", jids: ["design@g.us"])
        _ = try await backend.request("/v1/agents/agent-2/permissions", method: "PUT", body: Wire.data(permissions))
        let result: Agent = try await backend.get("/v1/agents/agent-2")
        #expect(result.permissions == permissions)
    }
    @Test func revertIsAttributed() async throws {
        let backend = DemoBackend()
        try await backend.post("/v1/facts/f1/revert", Empty())
        let response: ChangesResponse = try await backend.get("/v1/people/changes")
        #expect(response.changes[0].reverted)
        #expect(response.changes[0].source == "you")
    }
}

@Suite("Streaming and navigation") struct StreamingTests {
    @Test func multilineSSE() {
        var parser = SSEParser()
        #expect(parser.consume(": keepalive") == nil)
        #expect(parser.consume("event: update\r") == nil)
        #expect(parser.consume("id: 42") == nil)
        #expect(parser.consume("data: one") == nil)
        #expect(parser.consume("data: two") == nil)
        #expect(parser.consume("") == .init(event: "update", data: "one\ntwo", id: "42"))
        #expect(parser.consume("") == nil)
        _ = parser.consume("data: next")
        #expect(parser.consume("") == .init(event: "message", data: "next", id: "42"))
    }
    @Test func demoLinkStream() async throws {
        var count = 0
        for try await event in DemoBackend().events("/v1/link/events") {
            let link = try Wire.decoder().decode(LinkEvent.self, from: Data(event.data.utf8))
            #expect(link.state == "qr"); count += 1
        }
        #expect(count == 1)
    }
    @Test func deepLinks() {
        #expect(DeepLink(url: URL(string: "limmr://approvals/a1")!) == .approvals("a1"))
        #expect(DeepLink(url: URL(string: "limmr://agents/codex")!) == .agent("codex"))
        #expect(DeepLink(url: URL(string: "limmr://people/maya")!) == .person("maya"))
        #expect(DeepLink(url: URL(string: "limmr://health")!) == .health)
        #expect(DeepLink(url: URL(string: "https://health")!) == nil)
        #expect(DeepLink(url: URL(string: "limmr://agents")!) == nil)
    }
    @Test func funnelPortDetection() {
        let data = Data(#"{"TCP":{"10000":{"HTTPS":true}},"Web":{"demo.ts.net:8443":{"Handlers":{}}},"AllowFunnel":{"demo.ts.net:10000":true}}"#.utf8)
        #expect(FunnelPorts.parse(data) == [8443, 10000])
        #expect(FunnelPorts.parse(Data("invalid".utf8)).isEmpty)
    }
}

final class FixtureURLProtocol: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        guard request.url?.path == "/v1/status", request.value(forHTTPHeaderField: "Authorization") == "Bearer test-only-placeholder" else {
            client?.urlProtocol(self, didReceive: HTTPURLResponse(url: request.url!, statusCode: 401, httpVersion: nil, headerFields: nil)!, cacheStoragePolicy: .notAllowed)
            client?.urlProtocolDidFinishLoading(self); return
        }
        let data = Data(#"{"linked":true,"connected":true,"account_name":"Alex","pending_approvals":0,"agents":[],"health":"ok"}"#.utf8)
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: ["Content-Type": "application/json"])!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: data)
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}
@Suite("URLSession client") struct ClientTests {
    func session() -> URLSession { let configuration = URLSessionConfiguration.ephemeral; configuration.protocolClasses = [FixtureURLProtocol.self]; return URLSession(configuration: configuration) }
    @Test func authenticatesAndDecodesFixture() async throws {
        let client = APIClient(token: "test-only-placeholder", session: session())
        let status: Status = try await client.get("/v1/status")
        #expect(status.connected && status.accountName == "Alex")
    }
    @Test func rejectsHTTPFailure() async {
        let client = APIClient(token: "invalid-placeholder", session: session())
        await #expect(throws: BackendError.self) { let _: Status = try await client.get("/v1/status") }
    }
    @Test func refusesNonControlPaths() async {
        let client = APIClient(token: "test-only-placeholder", session: session())
        await #expect(throws: BackendError.self) { _ = try await client.request("https://example.com", method: "GET", body: nil) }
    }
}

@Suite("Published engine contract") struct PublishedContractTests {
    @Test func numericActivityEnvelope() throws {
        let data = Data(#"{"items":[{"id":42,"at":"2026-10-16T12:00:00.123456789Z","agent_id":"agent-2","tool":"list_messages","kind":"read","chat_jids":["120363000001@g.us"],"message_ids":["group-01-01"],"count":1}],"next_before":42}"#.utf8)
        let page = try Wire.decode(ActivityPage.self, from: data)
        let rows = page.items
        #expect(page.nextBefore == 42)
        #expect(rows[0].id == 42)
        #expect(rows[0].verb == "Read 1 message")
        #expect(Timestamp.date(rows[0].at) != nil)
    }
    @Test func graphEdgeAndAbsentCircle() throws {
        let data = Data(#"{"nodes":[{"id":"me","kind":"me","name":"You","weight":3},{"id":"person-01","kind":"person","name":"Maya Rao","weight":5,"circle":"Friends"}],"edges":[{"a":"me","b":"person-01","kind":"dm","weight":5,"recent":true}]}"#.utf8)
        let graph = try Wire.decode(Graph.self, from: data)
        #expect(graph.nodes[0].circle.isEmpty)
        #expect(graph.edges[0].source == "me")
        #expect(graph.edges[0].target == "person-01")
    }
    @Test func settingsExcludeAppOnlyPreference() throws {
        let data = Data(#"{"hidden_chats":[],"retention_days":180,"owner":"Alex"}"#.utf8)
        var settings = try Wire.decode(AppSettings.self, from: data)
        settings.launchAtLogin = true
        let object = try #require(JSONSerialization.jsonObject(with: Wire.data(settings)) as? [String: Any])
        #expect(object["launch_at_login"] == nil)
        #expect(object["owner"] as? String == "Alex")
    }
    @Test func remoteNamesAndClientIDs() throws {
        let data = Data(#"{"enabled":true,"public_url":"https://limmr.example","mcp_url":"https://limmr.example/mcp","port":18801,"pending":[{"code":"123456","name":"ChatGPT","kind":"chatgpt","returns_to":"chatgpt.com","requested_at":"2026-10-16T12:00:00Z","expires_at":"2026-10-16T12:10:00Z","state":"waiting"}],"clients":[{"client_id":"client-demo","name":"ChatGPT","redirect_uris":["https://chatgpt.com/callback"],"token_endpoint_auth_method":"none","agent_id":"agent-3","approved_at":"2026-10-16T12:00:00Z","last_use":null,"expires_at":"2026-10-16T13:00:00Z"}]}"#.utf8)
        let remote = try Wire.decode(RemoteState.self, from: data)
        #expect(remote.pendingCodes[0].clientName == "ChatGPT")
        #expect(remote.clients[0].id == "client-demo")
    }
    @Test func messageContentAndAttachment() throws {
        let response = try Wire.decode(MessagesResponse.self, from: Data(#"{"messages":[{"id":"m1","chat_jid":"demo@g.us","sender":"demo","content":"Bring a sketch.","timestamp":"2026-10-16T12:00:00Z","is_from_me":false}]}"#.utf8))
        #expect(response.messages[0].text == "Bring a sketch.")
        let approval = try Wire.decode(Approval.self, from: Data(#"{"id":"a1","agent_id":"agent-2","chat_jid":"demo@g.us","kind":"file","text":"Sketch","voice":"as_agent","created_at":"2026-10-16T12:00:00Z","expires_at":"2026-10-16T12:30:00Z","state":"pending","edited":false,"attachment":{"id":"file1","name":"sketch.txt","size":20,"sha256":"demo-only"}}"#.utf8))
        #expect(approval.attachment?.name == "sketch.txt")
    }
    @Test func personStatsAndFactSources() throws {
        let stats = try Wire.decode(PersonStats.self, from: Data(#"{"shared_groups":["demo@g.us"],"dm_messages_30":4,"dm_messages_365":8,"last_dm":null,"messages_in_shared_groups":2}"#.utf8))
        #expect(stats.sharedGroups.count == 1 && stats.dmMessages30 == 4)
        let fact = try Wire.decode(Fact.self, from: Data(#"{"id":"f1","subject":"person","subject_id":"person-01","text":"Organises sessions.","source":"agent-1","evidence_message_ids":["m1"],"evidence_chat_jids":["demo@g.us"],"created_at":"2026-10-16T12:00:00Z","status":"active"}"#.utf8))
        #expect(fact.sourceAgent == "agent-1" && fact.subjectId == "person-01")
    }
}
