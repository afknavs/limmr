import Foundation
import LimmrKit
import Observation

@MainActor @Observable final class AgentNameEditor {
  var draft = ""
  private(set) var editing = false
  private(set) var saving = false
  private(set) var error: String?

  func begin(_ name: String) {
    guard !saving else { return }
    draft = name
    error = nil
    editing = true
  }

  func cancel() {
    guard !saving else { return }
    editing = false
    error = nil
    draft = ""
  }

  func save(using save: (String) async throws -> Void) async {
    guard editing, !saving else { return }
    error = nil
    do {
      let name = try AgentName.normalized(draft)
      saving = true
      defer { saving = false }
      try await save(name)
      editing = false
    } catch BackendError.http(404) {
      error = "This integration could not be renamed. Update the engine or reopen Integrations and try again."
    } catch BackendError.http(400) {
      error = "The name wasn’t accepted. Use 1–40 characters and try again."
    } catch {
      self.error = (error as? BackendError)?.localizedDescription ?? "The name couldn’t be saved. Try again."
    }
  }
}

extension AppStore {
  func renameAgent(_ id: String, name: String) async throws {
    let name = try AgentName.normalized(name)
    let generation = backendGeneration
    _ = try await backend.request("/v1/agents/\(id)", method: "PATCH", body: Wire.data(["name": name]))
    guard generation == backendGeneration, !Task.isCancelled else { return }
    // Change only the name: permission edits may have completed while PATCH was in flight.
    if let index = agents.firstIndex(where: { $0.id == id }) { agents[index].name = name }
  }
}
