import Foundation

/// Share concurrent GETs only. No response TTL, permission caching or write replay.
/// A viewport cancellation releases its waiter and cancels transport when last.
public actor CoalescingBackend: Backend {
  private let base: any Backend
  private struct Pending {
    let task: Task<Data, Error>
    var waiters: Set<UUID>
  }
  private var pending: [String: Pending] = [:]
  public init(backend: any Backend) { base = backend }
  public func request(_ path: String, method: String, body: Data?) async throws -> Data {
    try Task.checkCancellation()
    guard method == "GET", body == nil else { return try await base.request(path, method: method, body: body) }
    let waiter = UUID()
    if pending[path] != nil { pending[path]?.waiters.insert(waiter) }
    else {
      let base = base
      pending[path] = Pending(task: Task { try await base.request(path, method: "GET", body: nil) }, waiters: [waiter])
    }
    let task = pending[path]!.task
    return try await withTaskCancellationHandler {
      defer { release(path, waiter: waiter) }
      let value = try await task.value
      try Task.checkCancellation()
      return value
    } onCancel: {
      Task { await self.release(path, waiter: waiter) }
    }
  }
  private func release(_ path: String, waiter: UUID) {
    guard pending[path]?.waiters.remove(waiter) != nil else { return }
    if pending[path]?.waiters.isEmpty == true { pending.removeValue(forKey: path)?.task.cancel() }
  }
  public nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { base.events(path) }
}
