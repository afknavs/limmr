import AppKit
import SwiftUI
import Testing
@testable import Limmr
@testable import LimmrKit

@Suite("P10 people polish", .serialized) struct P10Tests {
  @Test func circlePreviewCapsNamesCountsFullMembershipAndSpeaksIt() throws {
    let map = P4DenseMapFixture.map
    let circle = try #require(P4DenseMapFixture.circles.first)
    let preview = CircleHoverPreview(id: circle.id, circles: P4DenseMapFixture.circles, people: [], map: map)
    #expect(preview.total == 50)
    #expect(preview.names.count == 8)
    #expect(preview.remaining == 42)
    #expect(preview.spoken.contains("50 people:"))
    #expect(preview.spoken.hasSuffix("and 42 more"))
    #expect(preview.names.allSatisfy { preview.spoken.contains($0) })
    var reordered = map; reordered.nodes.reverse()
    #expect(CircleHoverPreview(id: circle.id, circles: P4DenseMapFixture.circles, people: [], map: reordered).names == preview.names)
  }
  @Test func smallEmptyAndBoundedMapPreviewsHaveHonestCounts() throws {
    let family = try #require(DemoCircles.circles.first { $0.id == "family" })
    let preview = CircleHoverPreview(id: family.id, circles: DemoCircles.circles, people: DemoData.people, map: DemoCircles.understanding(DemoCircles.details))
    #expect(preview.total == 3 && preview.names.count == 3 && preview.remaining == 0)
    #expect(preview.spoken.hasPrefix("Family, 3 people:"))
    let empty = CircleHoverPreview(id: "missing", circles: [], people: [], map: .empty)
    #expect(empty.total == 0 && empty.names.isEmpty && empty.remaining == 0)
    let dense = try #require(P4DenseMapFixture.circles.first)
    var bounded = P4DenseMapFixture.map
    bounded.nodes = bounded.nodes.filter { $0.kind == "circle" }
    let partial = CircleHoverPreview(id: dense.id, circles: P4DenseMapFixture.circles, people: [], map: bounded)
    #expect(partial.total == 50 && partial.names.isEmpty && partial.remaining == 50)
  }
  @Test func cardStaysInsideMapAtEveryEdge() {
    for size in [CGSize(width: 524, height: 280), CGSize(width: 760, height: 280)] {
      for point in [CGPoint(x: 8, y: 8), CGPoint(x: size.width-90, y: 8), CGPoint(x: 8, y: 244), CGPoint(x: size.width-90, y: 244)] {
        let label = CGRect(origin: point, size: CGSize(width: 80, height: 22))
        let origin = CircleHoverPreview.cardOrigin(label: label, canvas: size, height: 216)
        #expect(CGRect(origin: .zero, size: size).insetBy(dx: 8, dy: 8).contains(CGRect(origin: origin, size: CGSize(width: 208, height: 216))))
      }
    }
  }
  @Test func hitTestingFindsLabelsPeopleAndCircleRegionsWithoutEdges() throws {
    let map = UnderstandingPresentation.calmMap(P4DenseMapFixture.map)
    var layout = UnderstandingLayout()
    layout.update(map: map, size: CGSize(width: 760, height: 280))
    for circle in map.nodes.filter({ $0.kind == "circle" }) {
      let label = try #require(layout.labels[circle.id])
      #expect(layout.hit(CGPoint(x: label.midX, y: label.midY), map: map) == .node(circle.id))
    }
    let person = try #require(map.nodes.first { $0.kind == "person" })
    #expect(layout.hit(try #require(layout.positions[person.id]), map: map) == .node(person.id))
    let hits = stride(from: 12.0, to: 260, by: 41).flatMap { y in
      stride(from: 12.0, to: 748, by: 41).compactMap { x in layout.hit(CGPoint(x: x, y: y), map: map) }
    }
    #expect(layout.hit(CGPoint(x: 1, y: 1), map: map) == nil)
    #expect(hits.contains { hit in if case .node(let id) = hit { return map.nodes.contains { $0.id == id && $0.kind == "circle" } }; return false })
  }
  @MainActor @Test func settingsActionsAreReachableAndListColumnsDoNotChangeWithDigitCount() async throws {
    _ = NSApplication.shared
    #expect(SettingsSearchEntry.matches("export people").contains { $0.tab == .people })
    #expect(SettingsSearchEntry.matches("fill AI").contains { $0.tab == .people })
    let sizes = [1, 123, 1234].map { count in
      NSHostingView(rootView: CircleCountColumns(pinned: count == 1, members: count, new: count)).fittingSize
    }
    #expect(sizes.allSatisfy { $0.width == sizes[0].width })
    let backend = P10SnapshotBackend()
    let directory: CirclesResponse = try await backend.get("/v1/circles")
    #expect(Set(directory.circles.map { $0.people.count }).isSuperset(of: [1, 123, 1234, 0]))
  }
  @Test func obsoleteStoredSwitchIsRemovedWithoutReadingOtherData() throws {
    let home = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: home) }
    let obsolete = home.appendingPathComponent("map-drift.json")
    let retained = home.appendingPathComponent("retained-setting.json")
    try Data("true".utf8).write(to: obsolete)
    try Data("keep".utf8).write(to: retained)
    PeopleMapStorage.removeRetiredSetting(in: home)
    PeopleMapStorage.removeRetiredSetting(in: home)
    #expect(!FileManager.default.fileExists(atPath: obsolete.path))
    #expect(try Data(contentsOf: retained) == Data("keep".utf8))
  }
  @Test func removedControlsAndDrawingCannotRegressAndActionsLiveInSettings() throws {
    let root = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
    func source(_ path: String) throws -> String { try String(contentsOf: root.appendingPathComponent("Sources/Limmr/" + path), encoding: .utf8) }
    let map = try source("People/PeopleWebHeader.swift")
    let layout = try source("People/UnderstandingLayout.swift")
    for removed in ["PeopleMapLinesToggle", "DriftPreferences", "--drift-lines", "layout.threads", "addLine(to:", "addQuadCurve(to:"] {
      #expect(!map.contains(removed))
    }
    #expect(!layout.contains("threads("))
    #expect(!layout.contains("case edge("))
    #expect(!FileManager.default.fileExists(atPath: root.appendingPathComponent("Sources/Limmr/People/DriftPreferences.swift").path))
    #expect(try !source("Circles/CirclesView.swift").contains("PeopleActions("))
    #expect(try source("Settings/PeopleSettings.swift").contains("PeopleActions(store: store)"))
    #expect(try source("People/PeopleActions.swift").contains(".sheet(isPresented: $fill) { FillFromAI() }"))
    #expect(try source("People/PeopleActions.swift").contains("NSSavePanel()"))
  }
}
