import Foundation
import LimmrKit
import Testing
@testable import Limmr

@MainActor @Suite("N4 Paper fixture", .serialized) struct N4Tests {
  @Test func islandsSizeCasesHaveExactInventedPopulations() throws {
    for (population,circleCount,looseCount) in [(220,4,18),(500,9,60),(12,1,0)] {
      let fixture = try N4SnapshotBackend(population:population), map = try fixture.mesh()
      #expect(fixture.people.count == population && fixture.circles.count == circleCount)
      #expect(map.nodes.filter { $0.kind == "person" && ($0.circles ?? []).isEmpty }.count == looseCount)
      #expect(map.nodes.filter { $0.newThisWeek == true }.count == 3)
      let layout = PaperIslandLayout.prepare(map)
      #expect(map.nodes.filter { $0.needsYou == true }.allSatisfy { layout.faces.contains($0.id) && layout.diameters[$0.id, default:0] >= 26 })
    }
  }
  @Test func fixtureSupportsRealPageReaders() async throws {
    let backend = try N4SnapshotBackend()
    let circles: CirclesResponse = try await backend.get("/v1/circles")
    #expect(circles.circles.count == 4)
    let circle: LimmrKit.Circle = try await backend.get("/v1/circles/university")
    let briefs: BriefsResponse = try await backend.get("/v1/circles/university/briefs")
    #expect(!circle.people.isEmpty && !briefs.briefs.isEmpty)
    let person: PersonDetail = try await backend.get("/v1/people/n4-1")
    #expect(person.person.name == "Isha Sen")
    #expect(person.stats.dmMessages30 == 128 && person.groups.count == 2)
    let tasks: TasksResponse = try await backend.get("/v1/tasks?person=n4-1&status=all")
    #expect(tasks.tasks.filter { !["done", "dropped"].contains($0.status) }.count == 2)
    let messages: MessagesResponse = try await backend.get("/v1/messages?chat_jid=fixture")
    #expect(messages.messages.first?.text.isEmpty == false)
    let history: PersonConversationHistory = try await backend.get("/v1/people/n4-1/conversation-history")
    #expect(history.weeks.count == 12 && history.sharedFilesTotal == 14)
    let activity: StatsHistory = try await backend.get("/v1/stats/history")
    #expect(activity.period(7).reduce(0) { $0 + $1.messages } == 1284)
    #expect(activity.period(7).reduce(0) { $0 + $1.agentActivity } == 86)
    let home: HomeResponse = try await backend.get("/v1/home")
    #expect(home.tasks.filter(\.needsYou).count == 2)
  }
}
