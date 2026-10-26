import Foundation
import Testing
@testable import LimmrKit

@Suite("P2 route ownership and public discovery") @MainActor struct P2RemoteTests {
  @Test func sharing443PreservesAccessoryRootAndAllOtherRoutes() async throws {
    let fake = FakeTailscale(routes: [FunnelRoute(port: 443, path: "/", target: "http://127.0.0.1:18771"),
      FunnelRoute(port: 443, path: "/other", target: "http://127.0.0.1:19998")])
    let setup = fake.setup()
    let before = fake.routes
    let url = try await setup.enable(port: 443, inspected: setup.inspect())
    #expect(url == "https://sample.ts.net/limmr")
    #expect(Set(before).isSubset(of: Set(fake.routes)))
    #expect(fake.owned == FunnelPorts.plan(port: 443, sharing: true))
    #expect(fake.mutations.count == 4)
    #expect(!fake.mutations.contains { $0.contains("--set-path=/") || $0.last == "off" })
    #expect(fake.mutations[3].last == "http://127.0.0.1:18801/.well-known/oauth-authorization-server")
    try await setup.remove()
    #expect(fake.routes == before)
    #expect(fake.owned.isEmpty)
    #expect(fake.mutations.suffix(4).allSatisfy { $0.contains("--set-path=/limmr") || $0.contains { $0.hasSuffix("/limmr") } })
  }
  @Test func free443UsesRootAndRepeatedSetupIsIdempotent() async throws {
    let fake = FakeTailscale()
    let setup = fake.setup()
    #expect(try await setup.enable(port: 443, inspected: setup.inspect()) == "https://sample.ts.net")
    #expect(fake.routes == FunnelPorts.plan(port: 443, sharing: false))
    _ = try await setup.enable(port: 443, inspected: setup.inspect())
    #expect(fake.mutations.count == 1)
    try await setup.remove()
    #expect(fake.routes.isEmpty)
    #expect(fake.mutations.last == ["funnel", "--https=443", "--set-path=/", "off"])
  }
  @Test func unknownTargetsAndHandlersAreProtectedIncludingOldMCP() async throws {
    let fake = FakeTailscale(routes: [FunnelRoute(port: 10000, path: "/", target: "http://127.0.0.1:18796")])
    let setup = fake.setup()
    let inspected = try await setup.inspect()
    #expect(inspected.first?.protected == true)
    await #expect(throws: (any Error).self) { try await setup.enable(port: 10000, inspected: inspected) }
    #expect(fake.mutations.isEmpty)
    let unknown = FunnelPorts.routes(Data(#"{"Web":{"sample.ts.net:443":{"Handlers":{"/":{"Text":"hello"}}}}}"#.utf8))
    #expect(unknown.first?.protected == true)
    #expect(unknown.first?.targets == ["Unrecognised handler"])
  }
  @Test func unownedPathCollisionIsRefusedEvenIfTargetLooksLikeLimmr() async throws {
    let fake = FakeTailscale(routes: [FunnelRoute(port: 443, path: "/limmr", target: "http://127.0.0.1:18801")])
    let setup = fake.setup()
    await #expect(throws: (any Error).self) { try await setup.enable(port: 443, inspected: setup.inspect()) }
    #expect(fake.mutations.isEmpty)
  }
  @Test func stalePlanCannotReplaceChangedRoutes() async throws {
    let fake = FakeTailscale()
    let setup = fake.setup()
    let old = try await setup.inspect()
    fake.routes = [FunnelRoute(port: 443, path: "/limmr", target: "http://127.0.0.1:19999")]
    await #expect(throws: (any Error).self) { try await setup.enable(port: 443, inspected: old) }
    #expect(fake.mutations.isEmpty)
  }
  @Test func removalLeavesManuallyReplacedRoutesAlone() async throws {
    let fake = FakeTailscale()
    let setup = fake.setup()
    _ = try await setup.enable(port: 443, inspected: setup.inspect())
    fake.routes[0].target = "http://127.0.0.1:19999"
    let before = fake.routes
    try await setup.remove()
    #expect(fake.routes == before)
    #expect(fake.mutations.count == 1)
    #expect(fake.owned.isEmpty)
  }
  @Test func partialSetupJournalsOnlySuccessfulRoutesAndCanRemoveThem() async throws {
    let root = FunnelRoute(port: 443, path: "/", target: "http://127.0.0.1:18771")
    let fake = FakeTailscale(routes: [root])
    fake.failMutation = 3
    let setup = fake.setup()
    await #expect(throws: (any Error).self) { try await setup.enable(port: 443, inspected: setup.inspect()) }
    #expect(fake.owned.count == 2)
    fake.failMutation = nil
    try await setup.remove()
    #expect(fake.routes == [root])
    #expect(fake.owned.isEmpty)
  }
  @Test func corruptStatusAndUnwritableJournalFailBeforeMutation() async throws {
    let fake = FakeTailscale()
    var setup = fake.setup()
    setup.saveOwned = { _ in throw BackendError.invalidResponse }
    await #expect(throws: (any Error).self) { try await setup.enable(port: 443, inspected: []) }
    #expect(fake.mutations.isEmpty)
    setup.run = { _ in "not-json" }
    await #expect(throws: (any Error).self) { try await setup.inspect() }
  }
  @Test func alternateFreePortIsStillAvailable() async throws {
    let fake = FakeTailscale()
    let setup = fake.setup()
    #expect(try await setup.enable(port: 8443, inspected: []) == "https://sample.ts.net:8443")
    #expect(fake.routes.first?.port == 8443)
  }
  @Test func rawTCP443CannotBeOverwrittenByAddingHTTPPaths() async throws {
    var commands: [[String]] = []
    let setup = RemoteSetup(run: { args in
      commands.append(args)
      if args == ["status", "--json"] { return #"{"Self":{"DNSName":"sample.ts.net."}}"# }
      return #"{"TCP":{"443":{"TCPForward":"127.0.0.1:19999"}},"AllowFunnel":{"sample.ts.net:443":true}}"#
    }, http: { _ in throw BackendError.invalidResponse })
    await #expect(throws: (any Error).self) { try await setup.enable(port: 443, inspected: setup.inspect()) }
    #expect(commands.allSatisfy { $0 == ["status", "--json"] || $0 == ["funnel", "status", "--json"] })
  }
  @Test(arguments: ["https://sample.example", "https://sample.example/limmr"])
  func verifiesCanonicalMetadataAndUnauthenticatedChallenge(base: String) async throws {
    var paths: [String] = []
    let setup = RemoteSetup(run: { _ in "" }, http: { request in
      paths.append(request.url!.path)
      return try metadata(request, base: base)
    })
    #expect(try await setup.verify(base))
    let suffix = URL(string: base)!.path
    #expect(paths == ["/.well-known/oauth-authorization-server" + suffix,
      "/.well-known/oauth-protected-resource" + suffix, suffix + "/mcp"])
  }
  @Test func wrongIssuerRedirectAndExposedMCPAllFailVerification() async throws {
    let base = "https://sample.example/limmr"
    for failure in ["issuer", "redirect", "mcp"] {
      let setup = RemoteSetup(run: { _ in "" }, http: { request in
        if failure == "redirect" {
          return (Data(), HTTPURLResponse(url: URL(string: "https://elsewhere.example")!, statusCode: 200, httpVersion: nil, headerFields: nil)!)
        }
        if failure == "mcp", request.url!.path.hasSuffix("/mcp") {
          return (Data(), HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: nil)!)
        }
        return try metadata(request, base: failure == "issuer" ? "https://wrong.example" : base)
      })
      do { #expect(try await !setup.verify(base)) } catch { #expect(failure == "redirect") }
    }
  }
  @Test func verifierPinsPublicAddressAndNeverUsesTailnetOrPrivateAnswers() async throws {
    let request = URLRequest(url: URL(string: "https://sample.ts.net/limmr/mcp")!)
    var pins: [String] = []
    let answers = DNSAnswers()
    let client = PublicDiscovery(resolve: { host in #expect(host == "sample.ts.net"); return answers.values }, fetch: { req, ip in
      pins.append(ip)
      #expect(req.url?.host == "sample.ts.net")
      return (Data(), HTTPURLResponse(url: req.url!, statusCode: 401, httpVersion: nil, headerFields: nil)!)
    })
    _ = try await client.request(request)
    #expect(pins == ["8.8.8.8"])
    for address in ["100.100.1.1", "127.0.0.1", "10.0.0.1", "192.168.1.2", "172.16.0.1", "169.254.1.1", "::1", "224.0.0.1"] {
      answers.values = [address]
      await #expect(throws: (any Error).self) { try await client.request(request) }
    }
    #expect(pins.count == 1)
  }
  @Test func mixedPublicPrivateDNSAnswersCannotFallBackToLocalConnection() async throws {
    let client = PublicDiscovery(resolve: { _ in ["8.8.8.8", "100.64.0.2"] }, fetch: { _, _ in
      Issue.record("must reject before dialing"); throw BackendError.invalidResponse
    })
    await #expect(throws: (any Error).self) {
      try await client.request(URLRequest(url: URL(string: "https://sample.ts.net")!))
    }
  }
  @Test func customHTTPSURLAllowsPathButNotCredentialsOrQueries() {
    #expect(RemoteSetup.validAddress("https://sample.example/custom/path"))
    for url in ["http://sample.example", "https://user@sample.example", "https://sample.example?x=1", "https://sample.example/#x"] {
      #expect(!RemoteSetup.validAddress(url))
    }
  }
}

@MainActor private final class FakeTailscale {
  var routes: [FunnelRoute]
  var owned: [FunnelRoute] = []
  var mutations: [[String]] = []
  var failMutation: Int?
  init(routes: [FunnelRoute] = []) { self.routes = routes }
  func setup() -> RemoteSetup {
    RemoteSetup(run: run, http: { _ in throw BackendError.invalidResponse },
      loadOwned: { self.owned }, saveOwned: { self.owned = $0 })
  }
  func run(_ arguments: [String]) async throws -> String {
    if arguments == ["status", "--json"] { return #"{"Self":{"DNSName":"sample.ts.net."}}"# }
    if arguments == ["funnel", "status", "--json"] {
      var web: [String: [String: [String: [String: String]]]] = [:]
      for route in routes { web["sample.ts.net:\(route.port)", default: [:]]["Handlers", default: [:]][route.path] = ["Proxy": route.target] }
      return String(decoding: try JSONSerialization.data(withJSONObject: ["Web": web]), as: UTF8.self)
    }
    mutations.append(arguments)
    if mutations.count == failMutation { throw BackendError.invalidResponse }
    guard let port = arguments.first(where: { $0.hasPrefix("--https=") }).flatMap({ Int($0.dropFirst(8)) }),
      let path = arguments.first(where: { $0.hasPrefix("--set-path=") }).map({ String($0.dropFirst(11)) }) else {
      throw BackendError.invalidResponse
    }
    routes.removeAll { $0.port == port && $0.path == path }
    if arguments.last != "off" { routes.append(FunnelRoute(port: port, path: path, target: arguments.last!)) }
    return ""
  }
}

private func metadata(_ request: URLRequest, base: String) throws -> (Data, HTTPURLResponse) {
  let path = request.url!.path
  if path.hasSuffix("/mcp") {
    return (Data(), HTTPURLResponse(url: request.url!, statusCode: 401, httpVersion: nil,
      headerFields: ["WWW-Authenticate": "Bearer resource_metadata=\"\(base)/.well-known/oauth-protected-resource/mcp\""])!)
  }
  let object: [String: Any] = path.contains("oauth-authorization-server") ? [
    "issuer": base, "authorization_endpoint": base + "/authorize", "token_endpoint": base + "/token", "registration_endpoint": base + "/register",
  ] : ["resource": base + "/mcp", "authorization_servers": [base]]
  return (try JSONSerialization.data(withJSONObject: object), HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: nil)!)
}

@MainActor private final class DNSAnswers { var values = ["8.8.8.8"] }
