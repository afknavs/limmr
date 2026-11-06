import Foundation
import LimmrKit

extension AppStore {
  /// Media downloads and unpairing need no activity event. Keep these small projections live.
  func refreshVisualState(includeCovers: Bool = true) async {
    let generation = backendGeneration
    if let next: DotSyncStatus = try? await backend.get("/v1/sync"), generation == backendGeneration, !Task.isCancelled {
      connectionSync = next
      if let response: AgentsResponse = try? await backend.get("/v1/agents"), generation == backendGeneration, !Task.isCancelled {
        agents = response.agents
      }
    }
    guard includeCovers, generation == backendGeneration, !Task.isCancelled,
      let response: TasksResponse = try? await backend.get("/v1/tasks?status=all&sort=recent&limit=10"),
      generation == backendGeneration, !Task.isCancelled else { return }
    let recent = Dictionary(response.tasks.map { ($0.id, $0) }, uniquingKeysWith: { _, last in last })
    for index in allTasks.indices {
      if let updated = recent[allTasks[index].id] { allTasks[index].coverCandidate = updated.coverCandidate }
    }
    for index in home.tasks.indices {
      if let updated = recent[home.tasks[index].id] { home.tasks[index].coverCandidate = updated.coverCandidate }
    }
    await taskCovers.warm(response.tasks, backend: backend, generation: generation)
  }

  func monitorVisualState() async {
    guard !demo, !snapshot else { return }
    var ticks = 0
    while !Task.isCancelled {
      do { try await Task.sleep(for: .seconds(5)) } catch { return }
      await refreshVisualState(includeCovers: ticks % 6 == 0)
      ticks += 1
    }
  }
}
