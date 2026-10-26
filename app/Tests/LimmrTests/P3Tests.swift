import AppKit
import Testing
@testable import Limmr
@testable import LimmrKit

@Suite("P3 people understanding") struct P3Tests {
  @Test func driftRepresentsEveryCircleAndUnaffiliatedPeople() async throws {
    let base: CircleMap = try await DemoBackend().get("/v1/map")
    let dense = DemoPeopleUnderstanding.map(base)
    for width in [524.0, 760, 1100] {
      var layout = UnderstandingLayout()
      layout.update(map: dense, size: CGSize(width: width, height: 400))
      #expect(layout.positions.count == dense.nodes.count)
      for circle in dense.nodes.filter({ $0.kind == "circle" }) {
        _ = try #require(layout.labels[circle.id])
        let members = dense.nodes.filter { UnderstandingLayout.primary($0, map: dense) == circle.id }
        #expect(!members.isEmpty)
        for member in members {
          let point = try #require(layout.positions[member.id])
          #expect(CGRect(origin: .zero, size: layout.size).contains(point))
        }
      }
      for person in dense.nodes.filter({ $0.kind == "person" && UnderstandingLayout.primary($0, map: dense) == nil }) {
        let point = try #require(layout.positions[person.id])
        #expect(layout.unaffiliatedBounds.contains(point))
      }
    }
    let calm = UnderstandingPresentation.calmMap(dense)
    #expect(calm.nodes.filter { $0.kind == "person" }.count == 96)
    #expect(UnderstandingPresentation.calmMap(dense, expanded: true).nodes.count == dense.nodes.count)
    #expect(calm.edges.allSatisfy { edge in calm.nodes.contains { $0.id == edge.source } && calm.nodes.contains { $0.id == edge.target } })
  }
  @Test func commonGroupsDecodeAndCardsUseNamesAndCircleTags() throws {
    let data = Data(#"{"id":"quiet-person","name":"Avery Lane","kind":"person","circle":"","weight":1,"groups_in_common":[{"id":"quiet@g.us","name":"Quiet workshop","circle_ids":["university"],"last_active_at":"2026-10-18T12:00:00Z"}]}"#.utf8)
    let person = try Wire.decode(Person.self, from: data)
    let graph = try Wire.decode(GraphNode.self, from: data)
    #expect(person.groupsInCommon?.first?.circleIds == ["university"])
    #expect(graph.groupsInCommon?.first?.name == "Quiet workshop")
    var node = CircleMapNode(id: person.id, kind: "person", title: person.name)
    node.groupsInCommon = person.groupsInCommon; node.groupsInCommonCount = 4
    let card = UnderstandingPresentation.nodeText(node, circles: DemoCircles.circles, now: Date())
    #expect(card.contains("Groups in common (4)"))
    #expect(card.contains("Quiet workshop · University"))
    #expect(!card.contains("quiet@g.us"))
  }
  @Test func recentFrequentContactIsNearestItsCircle() throws {
    var recent = CircleMapNode(id: "recent", kind: "person", title: "Casey", weight: 0.9, lastActiveAt: "2026-10-18T12:00:00Z")
    var older = CircleMapNode(id: "older", kind: "person", title: "Avery", weight: 0.1, lastActiveAt: "2026-09-18T12:00:00Z")
    recent.primaryCircleId = "studio"; older.primaryCircleId = "studio"
    let map = CircleMap(nodes: [CircleMapNode(id: "studio", kind: "circle", title: "Studio"), older, recent], edges: [], pulses: [])
    var layout = UnderstandingLayout(); layout.update(map: map, size: CGSize(width: 760, height: 400))
    let anchor = try #require(layout.positions["studio"])
    let r = try #require(layout.positions["recent"]), o = try #require(layout.positions["older"])
    #expect(hypot(r.x-anchor.x, r.y-anchor.y) <= hypot(o.x-anchor.x, o.y-anchor.y))
  }
  @Test func parsingRejectsUncertainMalformedAndOversizedOutput() throws {
    let good = RelationshipInference(label: "your teammate", howKnown: "You plan the studio project together.", confidence: 0.9)
    #expect(RelationshipInference.parse(try Wire.data(good)) == good)
    var bad = good; bad.confidence = 0.3
    #expect(RelationshipInference.parse(try Wire.data(bad)) == nil)
    bad = good; bad.howKnown = String(repeating: "x", count: 501)
    #expect(RelationshipInference.parse(try Wire.data(bad)) == nil)
    #expect(RelationshipInference.parse(Data("not JSON".utf8)) == nil)
    #expect(RelationshipInference.parse(Data(#"{"label":"friend"}"#.utf8)) == nil)
  }
  @MainActor @Test func unavailableOrUncertainModelNeverWritesAndInputIsBounded() async throws {
    let backend = P3Backend()
    let writer = OnDevicePeopleWriter(backend: backend, model: UncertainRelationshipModel(), now: Timestamp.date(DemoData.now)!)
    #expect(try await writer.run() == 0)
    #expect(await backend.writes == 0)
    let messages = DemoData.messages
    #expect(OnDevicePeopleWriter.context(messages, now: Timestamp.date(DemoData.now)!).utf8.count <= 2000)
    let unavailable = OnDevicePeopleWriter(backend: backend, model: UnavailableRelationshipModel(), now: Timestamp.date(DemoData.now)!)
    do { _ = try await unavailable.run(); Issue.record("Unavailable model should fail locally") } catch { }
    #expect(await backend.writes == 0)
  }
}
@MainActor private struct UncertainRelationshipModel: RelationshipModel {
  func infer(context: String) async throws -> RelationshipInference? { nil }
}
@MainActor private struct UnavailableRelationshipModel: RelationshipModel {
  func infer(context: String) async throws -> RelationshipInference? { throw BackendError.unavailable("Unavailable on this Mac") }
}
private actor P3Backend: Backend {
  var writes = 0
  let samples = DemoBackend()
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if path == "/v1/people/to-understand" {
      return Data(#"{"people":[{"id":"person-04","name":"Maya Rao","jids":["15550100004@s.whatsapp.net"]}]}"#.utf8)
    }
    if method == "PUT" { writes += 1; return Data("{}".utf8) }
    return try await samples.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { AsyncThrowingStream { $0.finish() } }
}
