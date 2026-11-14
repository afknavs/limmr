import Foundation
import Testing
@testable import LimmrKit

@Suite struct TaskHistoryUndoTests {
  @Test func historyUndoWindowAndLegacyDecode() throws {
    let at = "2026-11-13T12:00:00Z"
    let until = "2026-12-13T12:00:00Z"
    var event = try Wire.decode(TaskHistoryEvent.self, from: Data(#"{"at":"2026-11-13T12:00:00Z","kind":"rule_protocol_demote","text":"Moved back to suggestions: Limmr made this, you never asked for it","reason":"Limmr made this, you never asked for it","change_id":"chg_demo","undo_until":"2026-12-13T12:00:00Z"}"#.utf8))
    #expect(event.changeId == "chg_demo" && event.reason == "Limmr made this, you never asked for it")
    #expect(!event.canUndo(now: Timestamp.date(at)!.addingTimeInterval(-1)))
    #expect(event.canUndo(now: Timestamp.date(at)!))
    #expect(event.canUndo(now: Timestamp.date(until)!.addingTimeInterval(-0.001)))
    #expect(!event.canUndo(now: Timestamp.date(until)!))
    #expect(!event.canUndo(now: Timestamp.date(until)!.addingTimeInterval(1)))
    event.undone = true
    #expect(!event.canUndo(now: Timestamp.date(at)!))
    let legacy = try Wire.decode(TaskHistoryEvent.self, from: Data(#"{"at":"2026-11-13T12:00:00Z","kind":"created","text":"Task created · you"}"#.utf8))
    #expect(!legacy.undone && !legacy.canUndo(now: Timestamp.date(at)!))
    event.undone = false; event.undoUntil = "bad"
    #expect(!event.canUndo(now: Timestamp.date(at)!))
    event.undoUntil = until; event.changeId = nil
    #expect(!event.canUndo(now: Timestamp.date(at)!))
  }

  @Test func undoTransportPreservesAPIsMessage() async throws {
    let config = URLSessionConfiguration.ephemeral
    config.protocolClasses = [HistoryUndoErrorProtocol.self]
    let session = URLSession(configuration: config)
    defer { session.invalidateAndCancel() }
    let client = TaskClient(backend: APIClient(baseURL: URL(string: "http://127.0.0.1:18999")!, token: "invented", session: session))
    do {
      _ = try await client.undoChange("demo", changeId: "chg_demo")
      Issue.record("Expected API refusal")
    } catch {
      #expect(error.localizedDescription == "This change has already been undone.")
    }
  }
}

private final class HistoryUndoErrorProtocol: URLProtocol, @unchecked Sendable {
  override class func canInit(with request: URLRequest) -> Bool { true }
  override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
  override func startLoading() {
    let response = HTTPURLResponse(url: request.url!, statusCode: 409, httpVersion: "HTTP/1.1", headerFields: ["Content-Type": "application/json"])!
    client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
    client?.urlProtocol(self, didLoad: Data(#"{"error":{"code":"conflict","message":"This change has already been undone."}}"#.utf8))
    client?.urlProtocolDidFinishLoading(self)
  }
  override func stopLoading() {}
}
