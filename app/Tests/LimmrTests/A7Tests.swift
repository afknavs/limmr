import Foundation
import Testing
@testable import Limmr
@testable import LimmrKit

@Suite("A7 profile and sources") struct A7Tests {
  @Test func oldProfilesDecodeWithoutDetails() throws {
    let data = Data(#"{"id":"example","jids":[],"name":"Maya Rao","circle":"","role":"","how_known":"","notes":"","updated_at":"2026-10-17T12:00:00Z"}"#.utf8)
    let person = try Wire.decode(Person.self, from: data)
    #expect(person.details == nil)
    #expect(person.detailSuggestions == nil)
  }
  @Test func demoReviewKeepsConfirmedValuesAndExport() async throws {
    let backend = DemoBackend()
    let before: PersonDetail = try await backend.get("/v1/people/person-04")
    #expect(before.person.detailSuggestions?.count == 2)
    try await backend.post("/v1/people/person-04/suggestions/detail-email/confirm")
    try await backend.post("/v1/people/person-04/suggestions/detail-pronouns/dismiss")
    let after: PersonDetail = try await backend.get("/v1/people/person-04")
    #expect(after.person.details?.emails.count == 2)
    #expect(after.person.details?.emails.last?.source == "you")
    #expect(after.person.details?.emails.first == before.person.details?.emails.first)
    #expect(after.person.details?.pronouns == "")
    #expect(after.person.detailSuggestions?.isEmpty == true)
    try await backend.post("/v1/facts/f2/confirm")
    let confirmed: PersonDetail = try await backend.get("/v1/people/person-04")
    #expect(confirmed.facts.first { $0.id == "f2" }?.sourceAgent == "you")
    let export = try await backend.request("/v1/people/export", method: "GET", body: nil)
    let object = try #require(JSONSerialization.jsonObject(with: export) as? [String: Any])
    #expect(object["people"] != nil && object["circles"] != nil)
    _ = try await backend.request("/v1/people/person-04", method: "DELETE", body: nil)
    let people: PeopleResponse = try await backend.get("/v1/people")
    #expect(!people.people.contains { $0.id == "person-04" })
    let map: CircleMap = try await backend.get("/v1/map")
    #expect(!map.nodes.contains { $0.id == "person-04" })
    let circles: CirclesResponse = try await backend.get("/v1/circles")
    #expect(!circles.circles.contains { $0.people.contains { $0.personId == "person-04" } })
  }
  @Test func recentColumnUsesContentWidth() {
    #expect(!HomePresentation.usesRecentColumn(contentWidth: 979))
    #expect(HomePresentation.usesRecentColumn(contentWidth: 980))
    #expect(HomePresentation.usesRecentColumn(contentWidth: 1100))
  }
  @Test @MainActor func sourcesAndSnapshotRoutesExist() {
    #expect(SettingsTab.allCases.prefix(3) == [.general, .sources, .integrations])
    for page in ["person", "home", "settings-sources", "settings-integrations-access", "settings-general", "people-fill"] {
      #expect(SnapshotRunner.pages.contains(page))
    }
    #expect(FillFromAI.prompt.contains("suggested:true"))
    #expect(FillFromAI.prompt.contains("update_person"))
  }
}
