import Foundation
import Testing
@testable import LimmrKit

@Suite("E1 efforts wire and demo") struct EffortsTests {
  @Test func flattenedDetailPreservesReferencesAndInterleavedHistory() throws {
    let fixture = DemoEfforts.detail(DemoEfforts.efforts[0])
    let data = try Wire.data(fixture)
    let object = try #require(JSONSerialization.jsonObject(with: data) as? [String: Any])
    #expect(object["title"] as? String == fixture.effort.title)
    #expect(object["effort"] == nil)
    let detail = try Wire.decode(EffortDetailResponse.self, from: data)
    #expect(detail.effort.messageRefs.count == 8)
    #expect(detail.timeline.first?.action != nil)
    #expect(detail.timeline.dropFirst().allSatisfy { $0.message != nil })
    #expect(detail.effort.statusLabel.contains("due"))
  }
  @Test func demoSearchAndHomeUsePurposeStateAndEmptyModeHasNoEfforts() async throws {
    let backend = DemoBackend()
    let result: EffortsResponse = try await backend.get("/v1/efforts?q=Maya")
    #expect(result.efforts.count == 2)
    let detail: EffortDetailResponse = try await backend.get("/v1/efforts/effort-slides")
    #expect(detail.effort.title == "Pitch deck with Maya")
    let home: HomeResponse = try await backend.get("/v1/home")
    #expect(home.circleUpdates.first?.status == "on_you")
    let acknowledged = CircleEffortSeen(home.circleUpdates)
    #expect(acknowledged.seen["university"] == home.circleUpdates.first?.lastAt)
    let seenWire = try #require(JSONSerialization.jsonObject(with: Wire.data(acknowledged)) as? [String: Any])
    #expect(seenWire["seen"] is [String: String])
    #expect(home.circleUpdates.allSatisfy { !$0.line.isEmpty && !$0.title.isEmpty })
    let empty: EffortsResponse = try await EmptyBackend().get("/v1/efforts")
    #expect(empty.efforts.isEmpty)
    let oldHome = try Wire.decode(HomeResponse.self, from: Data("{}".utf8))
    #expect(oldHome.circleUpdates.isEmpty)
  }
}
