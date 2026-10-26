import Foundation

/// Public discovery must not accidentally succeed through a tailnet DNS answer.
@MainActor public struct PublicDiscovery {
  public var resolve: @MainActor (String) async throws -> [String]
  public var fetch: @MainActor (URLRequest, String) async throws -> (Data, HTTPURLResponse)
  public init(resolve: @escaping @MainActor (String) async throws -> [String],
    fetch: @escaping @MainActor (URLRequest, String) async throws -> (Data, HTTPURLResponse)) {
    self.resolve = resolve; self.fetch = fetch
  }
  public func request(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
    guard let url = request.url, url.scheme == "https", let host = url.host,
      url.user == nil, url.password == nil else { throw BackendError.invalidResponse }
    let addresses = try await resolve(host)
    guard !addresses.isEmpty, addresses.allSatisfy(Self.publicIPv4) else {
      throw BackendError.unavailable("Public DNS hasn’t returned a public address yet. Try again shortly.")
    }
    for address in addresses {
      try Task.checkCancellation()
      if let result = try? await fetch(request, address) { return result }
    }
    throw BackendError.unavailable("The public address isn’t reachable yet. Try again shortly.")
  }
  public static func publicIPv4(_ address: String) -> Bool {
    let pieces = address.split(separator: ".", omittingEmptySubsequences: false)
    let bytes = pieces.compactMap { UInt8($0) }
    guard pieces.count == 4, bytes.count == 4,
      zip(pieces, bytes).allSatisfy({ String($0.1) == $0.0 }) else { return false }
    let a = bytes[0], b = bytes[1]
    return a != 0 && a != 10 && a != 127 && a < 224
      && !(a == 100 && (64...127).contains(b)) && !(a == 169 && b == 254)
      && !(a == 172 && (16...31).contains(b)) && !(a == 192 && (b == 0 || b == 168))
      && !(a == 198 && (b == 18 || b == 19 || b == 51)) && !(a == 203 && b == 0)
  }
}
