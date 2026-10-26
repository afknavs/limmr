import Foundation
import Testing
@testable import LimmrKit

/// The fixtures are real responses from `limmr-engine --demo` (invented people, 555-01xx numbers).
/// Regenerate them whenever the engine's shapes change; these tests then show where the app disagrees.
@Suite struct EngineContractTests {
  private func fixture<T: Decodable>(_ name: String, as type: T.Type) throws -> T {
    let url = URL(filePath: #filePath).deletingLastPathComponent()
      .appending(path: "Fixtures/engine/\(name).json")
    return try Wire.decode(T.self, from: Data(contentsOf: url))
  }

  @Test func homeV2DecodesWithRealValues() throws {
    let home = try fixture("home", as: HomeResponse.self)
    #expect(home.queued.count == 3)
    #expect(home.queued.contains { $0.variations.count > 1 })
    #expect(home.queued.allSatisfy { ($0.body ?? "").isEmpty == false })
    #expect(home.queued.allSatisfy { $0.body == $0.draftText })
    #expect(home.queued.allSatisfy { !($0.body ?? "").contains("on behalf of") })
    #expect(home.needsReply.count == 3)
    #expect(home.needsReply.allSatisfy { !$0.id.isEmpty && !$0.circleId.isEmpty })
    #expect(home.circles.contains { $0.pinned || $0.circle.pinned })
    #expect(!home.recentActivity.isEmpty)
    #expect(!home.watches.isEmpty)
  }

  @Test func circlesMapAndDetailDecode() throws {
    let circles = try fixture("circles", as: CirclesResponse.self)
    #expect(circles.circles.count == 7)
    #expect(circles.circles.allSatisfy { !$0.name.isEmpty && !$0.kind.isEmpty })
    let circle = try fixture("circle", as: Circle.self)
    #expect(circle.id == circles.circles[0].id)
    let map = try fixture("map", as: CircleMap.self)
    #expect(!map.nodes.isEmpty && !map.edges.isEmpty)
  }

  @Test func watchesDecode() throws {
    let list = try fixture("watches", as: WatchesResponse.self)
    #expect(list.watches.count >= 3)
    #expect(list.watches.contains { $0.kind == "handoff" })
    let detail = try fixture("watch", as: WatchDetail.self)
    #expect(detail.summary.id == list.watches[0].id)
    #expect(!detail.digest.isEmpty)
  }
}
