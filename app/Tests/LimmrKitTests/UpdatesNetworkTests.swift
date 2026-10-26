import CryptoKit
import Foundation
import Testing
@testable import LimmrKit

@Suite("U1 offline download transport") struct UpdatesNetworkTests {
  private func session() -> URLSession {
    let config = URLSessionConfiguration.ephemeral
    config.protocolClasses = [UpdateFixtureProtocol.self]
    return URLSession(configuration: config)
  }
  private func fixture(_ data: Data, status: Int = 200) -> URL {
    let url = URL(string: "https://update.invalid/\(UUID().uuidString)/Limmr.zip")!
    UpdateFixtureProtocol.fixtures.put(url, data: data, status: status)
    return url
  }
  @Test func fetchVerifiesTheActualTransportBytes() async throws {
    let key = Curve25519.Signing.PrivateKey()
    var feed = UpdateFeed(version: "0.3", build: 3, minMacOS: "26.0", notesMarkdown: "Synthetic notes",
                          url: fixture(Data("zip".utf8)), size: 3, sha256: String(repeating: "a", count: 64))
    feed.ed25519Signature = try key.signature(for: feed.canonicalBytes()).base64EncodedString()
    let url = fixture(try JSONEncoder().encode(feed))
    let result = try await UpdateChecker(session: session()).fetch(feedURL: url, publicKey: key.publicKey.rawRepresentation)
    #expect(result == feed)
  }
  @Test func oversizedFeedAndHTTPFailuresAreRejected() async {
    let checker = UpdateChecker(session: session())
    for url in [fixture(Data(repeating: 0, count: 262_145)), fixture(Data(), status: 503)] {
      await #expect(throws: (any Error).self) { try await checker.fetch(feedURL: url, publicKey: Data(repeating: 0, count: 32)) }
    }
  }
  @Test func downloadStreamsChecksTheHashAndDeletesFailedFiles() async throws {
    let directory = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }
    let checker = UpdateChecker(session: session())
    let data = Data("synthetic archive".utf8)
    let correct = SHA256.hash(data: data).map { String(format: "%02x", $0) }.joined()
    let path = directory.appending(path: "download.zip")
    let feed = UpdateFeed(version: "0.3", build: 3, minMacOS: "26.0", notesMarkdown: "",
                          url: fixture(data), size: Int64(data.count), sha256: correct)
    try await checker.download(feed, to: path)
    #expect(try Data(contentsOf: path) == data)
    try FileManager.default.removeItem(at: path)
    let corrupt = UpdateFeed(version: "0.3", build: 3, minMacOS: "26.0", notesMarkdown: "",
                             url: fixture(data), size: Int64(data.count), sha256: String(repeating: "0", count: 64))
    await #expect(throws: UpdateError.checksum) { try await checker.download(corrupt, to: path) }
    #expect(!FileManager.default.fileExists(atPath: path.path))
  }
}

private final class UpdateFixtureState: @unchecked Sendable {
  private let lock = NSLock()
  private var entries: [URL: (Data, Int)] = [:]
  func put(_ url: URL, data: Data, status: Int) {
    lock.lock(); defer { lock.unlock() }; entries[url] = (data, status)
  }
  func take(_ url: URL) -> (Data, Int)? {
    lock.lock(); defer { lock.unlock() }; return entries.removeValue(forKey: url)
  }
}
private final class UpdateFixtureProtocol: URLProtocol, @unchecked Sendable {
  static let fixtures = UpdateFixtureState()
  override class func canInit(with request: URLRequest) -> Bool { request.url?.host == "update.invalid" }
  override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
  override func startLoading() {
    guard let url = request.url, let (data, status) = Self.fixtures.take(url) else {
      client?.urlProtocol(self, didFailWithError: UpdateError.unavailable); return
    }
    let response = HTTPURLResponse(url: url, statusCode: status, httpVersion: "HTTP/1.1",
                                   headerFields: ["Content-Length": String(data.count)])!
    client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
    client?.urlProtocol(self, didLoad: data)
    client?.urlProtocolDidFinishLoading(self)
  }
  override func stopLoading() {}
}
