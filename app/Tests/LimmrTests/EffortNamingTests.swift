import Foundation
import Testing

@testable import Limmr
@testable import LimmrKit

@Suite("E3 local effort naming") struct EffortNamingTests {
  @Test @MainActor func boundedContextAndRevisionConditionalWrite() async throws {
    let backend = NamingBackend()
    let model = NamingModel()
    var candidate = fixture()
    candidate.snippets = Array(repeating: String(repeating: "Budget 🌿 ", count: 500), count: 12)
    try await OnDeviceEffortWriter(backend: backend, model: model, session: EffortNamingSession()).name(candidate)
    #expect(model.context.utf8.count <= OnDeviceBriefWriter.contextLimit)
    #expect(model.context.contains("First name: Maya"))
    #expect(!model.context.contains("Rao"))
    #expect(model.context.components(separatedBy: "Snippet:").count <= 7)
    let body = try #require(await backend.written)
    let wire = try #require(JSONSerialization.jsonObject(with: body) as? [String: Any])
    #expect(wire["title"] as? String == "Festival budget")
    #expect(wire["summary"] as? String == "Maya asked for a budget review.")
    #expect(wire["only_unnamed"] as? Bool == true)
    #expect(wire["expected_revision"] as? String == candidate.revision)
    #expect(wire["due"] == nil)
    #expect(await backend.reads == 0)
  }

  @Test @MainActor func cacheRateLimitAndChangedRevision() async throws {
    let backend = NamingBackend()
    let model = NamingModel()
    let session = EffortNamingSession()
    let now = Date(timeIntervalSince1970: 1000)
    let writer = OnDeviceEffortWriter(backend: backend, model: model, session: session, now: now)
    var candidate = fixture()
    try await writer.name(candidate)
    try await OnDeviceEffortWriter(backend: backend, model: model, session: session, now: now.addingTimeInterval(60)).name(candidate)
    #expect(model.calls == 1)
    candidate.revision = "changed-evidence"
    try await writer.name(candidate)
    #expect(model.calls == 1)
    try await OnDeviceEffortWriter(backend: backend, model: model, session: session, now: now.addingTimeInterval(20)).name(candidate)
    #expect(model.calls == 2)
  }

  @Test @MainActor func invalidUncertainAndUnavailableResultsStayInQueue() async throws {
    for title in ["Image with Maya", String(repeating: "x", count: 41), " "] {
      let backend = NamingBackend()
      let model = NamingModel()
      model.title = title
      try await OnDeviceEffortWriter(backend: backend, model: model, session: EffortNamingSession()).name(fixture())
      #expect(await backend.written == nil)
    }
    let backend = NamingBackend()
    let model = NamingModel()
    let session = EffortNamingSession()
    model.fail = true
    do {
      try await OnDeviceEffortWriter(backend: backend, model: model, session: session).name(fixture())
      Issue.record("Model failure should propagate without writing")
    } catch {}
    #expect(await backend.written == nil)
    #expect(session.hasAttempted(fixture()))
  }

  @Test @MainActor func cancelledGenerationCanBeRetried() async throws {
    let backend = NamingBackend()
    let model = NamingModel()
    model.cancel = true
    let session = EffortNamingSession()
    do {
      try await OnDeviceEffortWriter(backend: backend, model: model, session: session).name(fixture())
      Issue.record("Cancellation must propagate")
    } catch is CancellationError {} catch { Issue.record("Unexpected error") }
    #expect(!session.hasAttempted(fixture()))
    #expect(await backend.written == nil)
  }

  @Test @MainActor func sentenceBoundaryPreservesDecimalsAndRejectsOversizedState() async throws {
    let backend = NamingBackend()
    let model = NamingModel()
    model.summary = "Maya approved the v2.0 plan. Noor will review it next."
    try await OnDeviceEffortWriter(backend: backend, model: model, session: EffortNamingSession()).name(fixture())
    let body = try #require(await backend.written)
    let wire = try #require(JSONSerialization.jsonObject(with: body) as? [String: Any])
    #expect(wire["summary"] as? String == "Maya approved the v2.0 plan.")
    let oversized = NamingBackend()
    model.summary = String(repeating: "Festival budget ", count: 100) + "."
    try await OnDeviceEffortWriter(backend: oversized, model: model, session: EffortNamingSession()).name(fixture())
    #expect(await oversized.written == nil)
  }

  private func fixture() -> EffortNamingCandidate {
    .init(id: "invented-effort", revision: "evidence-v1", topTerms: ["festival", "budget"], artefactNames: ["Budget review.pdf"], participantFirstNames: ["Maya"], snippets: ["Can you review the budget?"], messageCount: 3, chatCount: 1, participantCount: 2)
  }
}
@MainActor private final class NamingModel: EffortNamingModel {
  var context = ""
  var title = "Festival budget"
  var summary = "Maya asked for a budget review. A second sentence is discarded."
  var calls = 0
  var fail = false
  var cancel = false
  func name(context: String) async throws -> LocalEffortName {
    self.context = context
    calls += 1
    if cancel { throw CancellationError() }
    if fail { throw BackendError.unavailable("Fixture model unavailable") }
    return .init(title: title, summary: summary)
  }
}
private actor NamingBackend: Backend {
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, any Error> {
    AsyncThrowingStream { $0.finish() }
  }
  var written: Data?
  var reads = 0
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if method == "PATCH" { written = body } else { reads += 1 }
    return Data("{}".utf8)
  }
}
