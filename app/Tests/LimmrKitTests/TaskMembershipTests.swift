import Foundation
import Testing
@testable import LimmrKit

@Suite struct TaskMembershipTests {
  @Test func demoRemoveReturnsUpdatedTaskAndPersists() async throws {
    let client = TaskClient(backend: DemoBackend())
    let before = try await client.get("tsk_study")
    let chat = try #require(before.chats.first)
    let person = try #require(before.people.first { $0.id != "me" })
    let removed = try await client.removeMembers(before.id, chats: [chat.id], people: [person.id])
    #expect(!removed.chats.contains { $0.id == chat.id })
    #expect(!removed.people.contains { $0.id == person.id })
    #expect(removed.chats == before.chats.filter { $0.id != chat.id })
    #expect(removed.people == before.people.filter { $0.id != person.id })
    #expect(removed.history.first?.text == "Removed \(chat.name), \(person.name) from this task")
    #expect(try await client.get(before.id) == removed)
    await #expect(throws: BackendError.self) { try await client.removeMembers(before.id, people: ["me"]) }
    #expect(try await client.get(before.id) == removed)
  }
}
