import Foundation
import Testing
@testable import LimmrKit

@Suite("R3 startup HTTP") struct R3NetworkTests {
  @Test(arguments: [200, 503]) func startingResponsesAreDistinctFromHTTPFailure(code: Int) async throws {
    let config = URLSessionConfiguration.ephemeral
    config.protocolClasses = [R3URLProtocol.self]
    let session = URLSession(configuration: config)
    defer { session.invalidateAndCancel() }
    let client = APIClient(baseURL: URL(string: "http://127.0.0.1:\(code)")!, token: "synthetic", session: session)
    do {
      _ = try await client.request("/v1/home", method: "GET", body: nil)
      Issue.record("Starting response decoded as ready data")
    } catch BackendError.starting(let progress) {
      #expect(progress == "warmup.efforts:8")
    }
    do {
      _ = try await client.request("/v1/unrelated", method: "GET", body: nil)
      Issue.record("Ordinary 503 accepted as startup")
    } catch BackendError.http(let status) { #expect(status == 503) }
  }
}
private final class R3URLProtocol: URLProtocol, @unchecked Sendable {
  override class func canInit(with request: URLRequest) -> Bool { true }
  override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
  override func startLoading() {
    let starting = request.url!.path == "/v1/home"
    let response = HTTPURLResponse(url: request.url!, statusCode: starting ? request.url!.port! : 503, httpVersion: nil, headerFields: ["Content-Type": "application/json"])!
    client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
    client?.urlProtocol(self, didLoad: Data((starting ? #"{"status":"starting","stage":"warmup.efforts","progress":8}"# : #"{"error":{"code":"unavailable"}}"#).utf8))
    client?.urlProtocolDidFinishLoading(self)
  }
  override func stopLoading() {}
}
