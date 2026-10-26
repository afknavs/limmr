import Foundation

public struct FunnelRoute: Codable, Hashable, Sendable {
  public var port: Int
  public var path: String
  public var target: String
  public init(port: Int, path: String, target: String) {
    self.port = port; self.path = path; self.target = target
  }
}

public struct FunnelPort: Equatable, Sendable, Identifiable {
  public let port: Int
  public let handlers: [FunnelRoute]
  public let owned: [FunnelRoute]
  public let httpShareable: Bool
  public var id: Int { port }
  public var targets: [String] { Array(Set(handlers.map(\.target))).sorted() }
  public var targetDescription: String { handlers.isEmpty ? "Unknown handler" : handlers.map { $0.path + " → " + $0.target }.joined(separator: ", ") }
  /// Ownership comes from our local journal, never from a guessed backend port.
  public var protected: Bool { handlers.isEmpty || handlers.contains { !owned.contains($0) } }
}

public enum FunnelPorts {
  public static let candidates = [443, 8443, 10000]
  public static func parse(_ data: Data) -> Set<Int> { Set(routes(data).map(\.port)) }
  public static func routes(_ data: Data, owned: [FunnelRoute] = []) -> [FunnelPort] {
    guard let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return [] }
    var ports: [Int: Set<FunnelRoute>] = [:]
    var httpPorts = Set<Int>(), nonHTTPPorts = Set<Int>()
    func number(_ key: String) -> Int? { Int(key) ?? Int(key.split(separator: ":").last ?? "") }
    // Web owns path routes. TCP / AllowFunnel still reserve a port even with no known handlers.
    for section in ["TCP", "AllowFunnel", "Web"] {
      for (key, value) in json[section] as? [String: Any] ?? [:] {
        guard let port = number(key), candidates.contains(port) else { continue }
        if ports[port] == nil { ports[port] = [] }
        if section == "TCP" {
          if (value as? [String: Any])?["HTTPS"] as? Bool == true { httpPorts.insert(port) }
          else { nonHTTPPorts.insert(port) }
        }
        guard section == "Web" else { continue }
        httpPorts.insert(port)
        for (path, handler) in (value as? [String: Any])?["Handlers"] as? [String: Any] ?? [:] {
          let target = (handler as? [String: Any])?["Proxy"] as? String ?? "Unrecognised handler"
          ports[port, default: []].insert(FunnelRoute(port: port, path: path, target: target))
        }
      }
    }
    return ports.map { port, handlers in
      FunnelPort(port: port, handlers: handlers.sorted { $0.path < $1.path }, owned: owned.filter { $0.port == port },
        httpShareable: httpPorts.contains(port) && !nonHTTPPorts.contains(port))
    }.sorted { $0.port < $1.port }
  }
  public static func shares443(_ occupied: FunnelPort?) -> Bool {
    guard let occupied else { return false }
    return !occupied.handlers.contains { $0.path == "/" && occupied.owned.contains($0) }
  }
  public static func plan(port: Int, sharing: Bool) -> [FunnelRoute] {
    let backend = "http://127.0.0.1:18801"
    guard sharing else { return [FunnelRoute(port: port, path: "/", target: backend)] }
    return [FunnelRoute(port: port, path: "/limmr", target: backend)] + [
      "oauth-authorization-server", "oauth-protected-resource", "openid-configuration",
    ].map {
      FunnelRoute(port: port, path: "/.well-known/" + $0 + "/limmr",
        target: backend + "/.well-known/" + ($0 == "openid-configuration" ? "oauth-authorization-server" : $0))
    }
  }
  public static func command(_ route: FunnelRoute, remove: Bool = false) -> [String] {
    ["funnel"] + (remove ? [] : ["--bg"]) + ["--https=\(route.port)", "--set-path=\(route.path)", remove ? "off" : route.target]
  }
}

/// Every process, HTTP call and ownership write is injectable. Tests never invoke Tailscale.
@MainActor public struct RemoteSetup {
  public typealias Runner = @MainActor ([String]) async throws -> String
  public typealias HTTP = @MainActor (URLRequest) async throws -> (Data, HTTPURLResponse)
  public var run: Runner
  public var http: HTTP
  public var loadOwned: @MainActor () throws -> [FunnelRoute]
  public var saveOwned: @MainActor ([FunnelRoute]) throws -> Void
  public init(run: @escaping Runner, http: @escaping HTTP,
    loadOwned: @escaping @MainActor () throws -> [FunnelRoute] = { [] },
    saveOwned: @escaping @MainActor ([FunnelRoute]) throws -> Void = { _ in }) {
    self.run = run; self.http = http; self.loadOwned = loadOwned; self.saveOwned = saveOwned
  }
  public func inspect() async throws -> [FunnelPort] {
    let text = try await run(["funnel", "status", "--json"])
    let data = Data(text.utf8)
    guard (try? JSONSerialization.jsonObject(with: data)) is [String: Any] else {
      throw BackendError.unavailable("Tailscale did not return its port configuration. Check again.")
    }
    return FunnelPorts.routes(data, owned: try loadOwned())
  }
  public func enable(port: Int, inspected: [FunnelPort]) async throws -> String {
    guard FunnelPorts.candidates.contains(port) else { throw BackendError.unavailable("Choose a Funnel port.") }
    let old = inspected.first { $0.port == port }
    guard port == 443 || old == nil || old?.protected == false else {
      throw BackendError.unavailable("This port belongs to another app. Choose a free port.")
    }
    let raw = try await run(["status", "--json"])
    struct Status: Decodable { struct Device: Decodable { var DNSName: String }; var `Self`: Device }
    guard let status = try? JSONDecoder().decode(Status.self, from: Data(raw.utf8)) else {
      throw BackendError.unavailable("Couldn’t find your Tailscale address. Check Tailscale and try again.")
    }
    let host = status.Self.DNSName.trimmingCharacters(in: CharacterSet(charactersIn: "."))
    guard !host.isEmpty, host.allSatisfy({ $0.isASCII && ($0.isLetter || $0.isNumber || $0 == "-" || $0 == ".") }) else {
      throw BackendError.unavailable("Tailscale returned an invalid address.")
    }
    let current = try await inspect()
    guard current.first(where: { $0.port == port }) == old else {
      throw BackendError.unavailable("That port changed. Check the ports and review them again.")
    }
    guard old == nil || old?.httpShareable == true else {
      throw BackendError.unavailable("This port has a non-HTTP or unknown handler. Use your own HTTPS URL.")
    }
    let sharing = port == 443 && FunnelPorts.shares443(old)
    let plan = FunnelPorts.plan(port: port, sharing: sharing)
    // Refuse collisions even if an unowned route happens to point to our backend.
    for route in plan {
      if let existing = old?.handlers.first(where: { $0.path == route.path }),
        existing != route || old?.owned.contains(existing) != true {
        throw BackendError.unavailable("A route at \(route.path) belongs to another app. Use your own HTTPS URL.")
      }
    }
    var owned = try loadOwned()
    try saveOwned(owned) // Fail before changing Funnel if the journal isn't writable.
    for route in plan where old?.handlers.contains(route) != true {
      try Task.checkCancellation()
      _ = try await run(FunnelPorts.command(route))
      owned.removeAll { $0.port == route.port && $0.path == route.path }
      owned.append(route)
      try saveOwned(owned) // Journal each successful route, including a partial setup.
    }
    return "https://" + host + (port == 443 ? "" : ":\(port)") + (sharing ? "/limmr" : "")
  }
  public func remove() async throws {
    let current = try await inspect()
    var owned = try loadOwned()
    for route in owned {
      // If another app changed a target, ownership is lost; never remove it.
      if current.first(where: { $0.port == route.port })?.handlers.contains(route) == true {
        _ = try await run(FunnelPorts.command(route, remove: true))
      }
      owned.removeAll { $0 == route }
      try saveOwned(owned)
    }
  }
  public static func validAddress(_ base: String) -> Bool {
    guard let url = URL(string: base), let host = url.host else { return false }
    return url.scheme == "https" && !host.isEmpty && url.user == nil && url.password == nil
      && url.query == nil && url.fragment == nil && !url.path.contains("..")
      && !base.hasSuffix("/")
  }
  public func verify(_ base: String) async throws -> Bool {
    guard Self.validAddress(base), let origin = URLComponents(string: base) else {
      throw BackendError.unavailable("Enter a public HTTPS address.")
    }
    func fetch(_ absolute: String) async throws -> (Data, HTTPURLResponse) {
      guard let url = URL(string: absolute) else { throw BackendError.invalidResponse }
      var request = URLRequest(url: url)
      request.timeoutInterval = 8
      request.setValue("application/json", forHTTPHeaderField: "Accept")
      let result = try await http(request)
      guard result.1.url == url else { throw BackendError.unavailable("The public address redirected. Check your tunnel.") }
      return result
    }
    var root = origin; root.path = ""
    guard let authority = root.string else { throw BackendError.invalidResponse }
    // RFC 8414 / 9728 insert the well-known name BEFORE the issuer's path.
    let (authData, authResponse) = try await fetch(authority + "/.well-known/oauth-authorization-server" + origin.percentEncodedPath)
    guard authResponse.statusCode == 200,
      let auth = try? JSONSerialization.jsonObject(with: authData) as? [String: Any],
      auth["issuer"] as? String == base,
      auth["authorization_endpoint"] as? String == base + "/authorize",
      auth["token_endpoint"] as? String == base + "/token",
      auth["registration_endpoint"] as? String == base + "/register" else { return false }
    let (resourceData, resourceResponse) = try await fetch(authority + "/.well-known/oauth-protected-resource" + origin.percentEncodedPath)
    guard resourceResponse.statusCode == 200,
      let resource = try? JSONSerialization.jsonObject(with: resourceData) as? [String: Any],
      resource["resource"] as? String == base + "/mcp",
      resource["authorization_servers"] as? [String] == [base] else { return false }
    let (_, mcpResponse) = try await fetch(base + "/mcp")
    let challenge = mcpResponse.value(forHTTPHeaderField: "WWW-Authenticate") ?? ""
    return mcpResponse.statusCode == 401 && challenge.lowercased().hasPrefix("bearer ")
      && challenge.contains("resource_metadata=\"\(base)/.well-known/oauth-protected-resource/mcp\"")
  }
}
