import Testing
@testable import LimmrKit

@Suite("Demo directory") struct DemoDirectoryTests {
  @Test func demoDirectoryContainsCircleGroupsAndOverlaps() {
    #expect(DemoData.people.count == 13)
    #expect(DemoData.groups.count == 9)
    #expect(DemoPeople.groups(for: "person-04").contains { $0.name == "Atlas renovation" })
    #expect(DemoPeople.groups(for: "person-05").contains { $0.name == "Flatmates" })
    #expect(DemoPeople.groups(for: "person-05").contains { $0.name == "Design Club" })
    for group in DemoData.groups {
      #expect(
        DemoData.graph().edges.filter { $0.source == group.jid && $0.kind == "member" }.count
          == group.memberCount)
    }
  }

}
