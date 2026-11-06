import Foundation
import LimmrKit
import Observation

/// Lives with AppStore rather than a lazy tile. View cancellation cannot discard
/// an in-flight warm, and a repeat visit reads decoded pixels synchronously.
@MainActor @Observable final class TaskCoverCache {
  struct Display {
    let url: URL
    let raster: PreviewRaster
    var cost: Int { raster.image.bytesPerRow * raster.image.height }
  }
  private var displays: [String: Display] = [:]
  @ObservationIgnored private var pending: [String: Task<Display?, Never>] = [:]
  @ObservationIgnored private var order: [String] = []
  private var generation = -1
  static func identity(_ task: TaskSummary) -> String {
    task.id + ":" + (task.coverCandidate?.thumbnailUrl ?? task.cover.map { $0.id + $0.addedAt } ?? "none")
  }
  func cached(_ task: TaskSummary, generation: Int) -> Display? {
    guard generation == self.generation else { return nil }
    return displays[Self.identity(task)]
  }
  func reset(generation: Int) {
    guard generation > self.generation else { return }
    for work in pending.values { work.cancel() }
    pending = [:]; displays = [:]; order = []; self.generation = generation
  }
  @discardableResult func load(_ task: TaskSummary, backend: any Backend, generation: Int) async -> Display? {
    guard generation >= self.generation else { return nil }
    reset(generation: generation)
    let key = Self.identity(task)
    if let value = displays[key] { order.removeAll { $0 == key }; order.append(key); return value }
    if let work = pending[key] { return await work.value }
    guard task.coverCandidate != nil || task.cover != nil else { return nil }
    let work = Task<Display?, Never> {
      guard let url = await N1TaskPreview.load(backend: backend, task: task),
        let raster = await MediaPreviewCache.shared.raster(url, pixels: MediaPreviewCache.coverPixels), !Task.isCancelled else { return nil }
      return Display(url: url, raster: raster)
    }
    pending[key] = work
    let result = await work.value
    guard generation == self.generation else { return nil }
    pending[key] = nil
    if let result {
      // Keep the ten startup covers within a bounded 64 MiB display cache.
      while displays.values.reduce(0, { $0 + $1.cost }) + result.cost > MediaPreviewCache.decodedByteLimit, !order.isEmpty {
        displays[order.removeFirst()] = nil
      }
      displays[key] = result; order.append(key)
    }
    return result
  }
  func warm(_ tasks: [TaskSummary], backend: any Backend, generation: Int) async {
    let recent = tasks.sorted { ($0.lastActiveAt ?? $0.createdAt ?? "") > ($1.lastActiveAt ?? $1.createdAt ?? "") }.prefix(10)
    // Bounded concurrency; independent of which tiles SwiftUI instantiates.
    for batch in stride(from: 0, to: recent.count, by: 3) {
      let slice = Array(recent.dropFirst(batch).prefix(3))
      await withTaskGroup(of: Void.self) { group in
        for task in slice { group.addTask { await self.load(task, backend: backend, generation: generation) } }
      }
    }
  }
}
