import Foundation
import LimmrKit
import Testing
@testable import Limmr

@Suite("Task member removal", .serialized) @MainActor struct TaskMembershipViewTests {
  @Test func removalUpdatesWorkspaceWithoutReloading() async throws {
    let backend = MembershipRecordingBackend()
    let workspace = TaskWorkspace(id: "tsk_study", backend: backend)
    await workspace.load()
    let before = try #require(workspace.task)
    let chat = try #require(before.chats.first)
    let person = try #require(before.people.first { $0.id != "me" })
    let reads = await backend.reads
    await workspace.removeMembers(chats: [chat.id], people: [person.id])
    let after = try #require(workspace.task)
    #expect(after.chats == before.chats.filter { $0.id != chat.id })
    #expect(after.people == before.people.filter { $0.id != person.id })
    #expect(await backend.reads == reads)
    #expect(workspace.error == nil && !workspace.saving)
    await workspace.removeMembers(people: ["me"])
    #expect(workspace.task == after && workspace.error != nil && !workspace.saving)
  }
}

private actor MembershipRecordingBackend: Backend {
  var reads = 0
  private let demo = DemoBackend()
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if method == "GET" { reads += 1 }
    return try await demo.request(path, method: method, body: body)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
    AsyncThrowingStream { $0.finish() }
  }
}
