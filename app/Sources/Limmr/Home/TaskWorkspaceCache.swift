import Foundation
import LimmrKit

// MARK: lane-2 — reuse task state across navigation; mutations refresh only Home.
extension AppStore {
  func prepareTaskCovers() async {
    let generation = backendGeneration
    if taskCoverStartupGeneration != generation {
      if let response: TasksResponse = try? await backend.get("/v1/tasks?status=all&sort=recent&limit=10"), generation == backendGeneration {
        taskCoverStartupGeneration = generation
        if allTasks.isEmpty { allTasks = response.tasks }
        await taskCovers.warm(response.tasks, backend: backend, generation: generation)
      }
    }
    guard generation == backendGeneration else { return }
    await taskCovers.warm(home.tasks, backend: backend, generation: generation)
  }

  func workspace(_ id: String, initial: TrackedTask? = nil) -> TaskWorkspace {
    if taskWorkspaceGeneration != backendGeneration {
      taskWorkspaceCache.removeAll(); taskWorkspaceOrder.removeAll(); taskWorkspaceGeneration = backendGeneration
    }
    if let cached = taskWorkspaceCache[id] {
      taskWorkspaceOrder.removeAll { $0 == id }; taskWorkspaceOrder.append(id)
      return cached
    }
    var seed = initial
    if seed == nil, let card = (allTasks + home.tasks).first(where: { $0.id == id }) {
      var value = TrackedTask()
      value.id = id; value.title = card.title; value.kind = card.kind; value.status = card.status
      value.progress = card.progress; value.needsYou = card.needsYou; value.createdAt = card.createdAt ?? ""
      value.lastActiveAt = card.lastActiveAt; value.circleIds = card.circleIds
      seed = value
    }
    let value = TaskWorkspace(id: id, backend: backend, initial: seed, summaryModel: TaskSummaryWriterPolicy.model(demo: demo, snapshot: snapshot))
    taskWorkspaceCache[id] = value; taskWorkspaceOrder.append(id)
    if taskWorkspaceOrder.count > 32 { taskWorkspaceCache[taskWorkspaceOrder.removeFirst()] = nil }
    return value
  }
  func refreshTaskCheckIns() async {
    struct Response: Decodable, Sendable { var suggestions: [CircleSuggestion] }
    let generation = backendGeneration
    guard let response: Response = try? await backend.get("/v1/suggestions"), generation == backendGeneration else { return }
    let due = response.suggestions.filter { $0.kind == "check_in" && $0.taskId != nil }
    let previous = Set(home.queued.filter { $0.kind == "check_in" }.map(\.id))
    if previous != Set(due.map(\.id)) {
      home.queued.removeAll { $0.kind == "check_in" }
      home.queued.append(contentsOf: due)
      for item in due { if let id = item.taskId { taskWorkspaceCache[id]?.loadedAt = nil } }
    }
    notifyTaskCheckIns()
  }
  func monitorTaskCheckIns() async {
    guard !demo, !snapshot else { return }
    while !Task.isCancelled {
      await refreshTaskCheckIns()
      do { try await Task.sleep(for: .seconds(30)) } catch { return }
    }
  }
  func notifyTaskCheckIns() {
    guard !demo, !snapshot else { return }
    notifications.openTask = { [weak self] id in self?.openTask(id) }
    notifications.updateTaskCheckIns(home.queued)
  }
  func refreshTaskHome() async {
    let generation = backendGeneration
    if let value: HomeResponse = try? await backend.get("/v1/home"), generation == backendGeneration { home = value }
  }
  func quietTask(_ id: String, status: String) async {
    guard !taskActions.contains(id) else { return }
    taskActions.insert(id); defer { taskActions.remove(id) }
    let generation = backendGeneration
    do {
      let client = TaskClient(backend: backend)
      let before = try await client.get(id)
      if before.status == "suggested" { _ = try await client.surface(id, action: .keep) }
      _ = try await client.patch(id, .init(status: status))
      guard generation == backendGeneration else { return }
      taskQuietUndo = before; taskQuietNotice = status == "done" ? "Task marked done" : status == "quiet" ? "Task made quiet" : status == "active" ? "Task awake" : "Task set aside"
      taskWorkspaceCache[id] = nil; taskWorkspaceOrder.removeAll { $0 == id }
      await refreshTaskHome(); await refreshAllTasks(); taskActionErrors[id] = nil
    } catch { if generation == backendGeneration { taskActionErrors[id] = "Couldn’t save this task. Try again." } }
  }
  func undoQuietTask() async {
    guard let previous = taskQuietUndo else { return }
    let generation = backendGeneration
    do {
      let current = try await TaskClient(backend: backend).get(previous.id)
      if ["done", "dropped"].contains(current.status) {
        _ = try await backend.request("/v1/tasks/" + previous.id + "/reopen", method: "POST", body: nil)
        if previous.status == "quiet" { _ = try await TaskClient(backend: backend).patch(previous.id, .init(status: "quiet")) }
      } else { _ = try await TaskClient(backend: backend).patch(previous.id, .init(status: previous.status)) }
      guard generation == backendGeneration else { return }
      taskQuietUndo = nil; taskQuietNotice = nil
      await refreshTaskHome(); await refreshAllTasks()
    } catch { taskQuietNotice = "Couldn’t undo. Try again." }
  }
}
