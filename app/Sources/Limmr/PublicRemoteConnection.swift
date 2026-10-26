import Foundation
import LimmrKit

/// Used only after the owner clicks Verify / Enable. No system DNS fallback or proxy:
/// curl --resolve keeps TLS SNI and certificate checks on the original public hostname.
@MainActor final class PublicRemoteConnection {
  let system: SystemActions
  private var cached: (host: String, at: Date, addresses: [String])?
  init(system: SystemActions) { self.system = system }
  func request(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
    try await PublicDiscovery(resolve: resolve, fetch: fetch).request(request)
  }
  private func resolve(_ host: String) async throws -> [String] {
    if let cached, cached.host == host, Date().timeIntervalSince(cached.at) < 30 { return cached.addresses }
    var url = URLComponents(string: "https://1.1.1.1/dns-query")!
    url.queryItems = [URLQueryItem(name: "name", value: host), URLQueryItem(name: "type", value: "A")]
    var request = URLRequest(url: url.url!)
    request.timeoutInterval = 5
    request.setValue("application/dns-json", forHTTPHeaderField: "Accept")
    let (data, response) = try await URLSession.shared.data(for: request, delegate: PublicNoRedirects())
    struct Answer: Decodable { var type: Int; var data: String }
    struct DNS: Decodable { var Status: Int; var Answer: [Answer]? }
    guard (response as? HTTPURLResponse)?.statusCode == 200,
      let dns = try? JSONDecoder().decode(DNS.self, from: data), dns.Status == 0 else {
      throw BackendError.unavailable("Public DNS isn’t available yet. Try again shortly.")
    }
    let addresses = dns.Answer?.filter { $0.type == 1 }.map(\.data) ?? []
    cached = (host, Date(), addresses)
    return addresses
  }
  private func fetch(_ request: URLRequest, ip: String) async throws -> (Data, HTTPURLResponse) {
    guard let url = request.url, let host = url.host else { throw BackendError.invalidResponse }
    let directory = FileManager.default.temporaryDirectory.appending(path: "limmr-discovery-" + UUID().uuidString)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true,
      attributes: [.posixPermissions: 0o700])
    defer { try? FileManager.default.removeItem(at: directory) }
    let headers = directory.appending(path: "headers"), body = directory.appending(path: "body")
    _ = try await system.run("/usr/bin/curl", [
      "--disable", "--silent", "--show-error", "--noproxy", "*", "--proto", "=https",
      "--connect-timeout", "4", "--max-time", "8", "--max-filesize", "1048576",
      "--resolve", "\(host):\(url.port ?? 443):\(ip)", "--header", "Accept: application/json",
      "--dump-header", headers.path, "--output", body.path, url.absoluteString,
    ])
    let raw = try String(contentsOf: headers, encoding: .utf8)
    let lines = raw.components(separatedBy: "\r\n")
    guard let status = lines.first?.split(separator: " ").dropFirst().first.flatMap({ Int($0) }) else {
      throw BackendError.invalidResponse
    }
    var fields: [String: String] = [:]
    for line in lines.dropFirst() {
      guard let colon = line.firstIndex(of: ":") else { continue }
      fields[String(line[..<colon])] = line[line.index(after: colon)...].trimmingCharacters(in: .whitespaces)
    }
    guard let response = HTTPURLResponse(url: url, statusCode: status, httpVersion: nil, headerFields: fields) else {
      throw BackendError.invalidResponse
    }
    return (try Data(contentsOf: body), response)
  }
}
private final class PublicNoRedirects: NSObject, URLSessionTaskDelegate, Sendable {
  func urlSession(_ session: URLSession, task: URLSessionTask,
    willPerformHTTPRedirection response: HTTPURLResponse, newRequest request: URLRequest,
    completionHandler: @escaping @Sendable (URLRequest?) -> Void) { completionHandler(nil) }
}

extension SystemActions {
  /// No tokens or personal information; records only routes this app successfully installed.
  private var funnelJournal: URL { paths.home.appending(path: "funnel-routes.json") }
  func ownedFunnelRoutes() throws -> [FunnelRoute] {
    guard FileManager.default.fileExists(atPath: funnelJournal.path) else { return [] }
    return try JSONDecoder().decode([FunnelRoute].self, from: Data(contentsOf: funnelJournal))
  }
  func saveFunnelRoutes(_ routes: [FunnelRoute]) throws {
    guard !demo else { return }
    try FileManager.default.createDirectory(at: paths.home, withIntermediateDirectories: true)
    try JSONEncoder().encode(routes).write(to: funnelJournal, options: .atomic)
  }
  func remoteConnection() throws -> RemoteSetup {
    let publicHTTP = PublicRemoteConnection(system: self)
    return RemoteSetup(run: { [self] arguments in
      guard let executable = executable("tailscale") else {
        throw BackendError.unavailable("Install Tailscale and sign in, or use your own HTTPS address.")
      }
      return try await run(executable, arguments)
    }, http: publicHTTP.request, loadOwned: ownedFunnelRoutes, saveOwned: saveFunnelRoutes)
  }
}
