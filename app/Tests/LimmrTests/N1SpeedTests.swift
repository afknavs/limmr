import AppKit
import ImageIO
import SwiftUI
import Testing
import LimmrKit
@testable import Limmr

@Suite("N1 speed", .serialized) @MainActor struct N1SpeedTests {
  @Test func baseline() async throws {
    SpeedTrace.enabled = true; SpeedTrace.bodies = [:]
    defer { SpeedTrace.enabled = false }
    let backend = N1CountingBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    store.people = (0..<400).map { i in var p = DemoData.people[i % DemoData.people.count]; p.id = "synthetic-\(i)"; return p }
    store.chats = (0..<300).map { i in var c = DemoData.chats[i % DemoData.chats.count]; c.jid = "1555\(String(format: "%07d", i))@s.whatsapp.net"; return c }
    store.activity = (0..<(ProcessInfo.processInfo.environment["LIMMR_SPEED_STRESS"] == "1" ? 2000 : 50)).map { i in var a = DemoData.activity[i % DemoData.activity.count]; a.id = Int64(i + 1); return a }
    store.allTasks = (0..<40).map { i in var t = DemoTasks.tasks[0].compact(now: Date()); t.id = "synthetic-\(i)"; return t }
    if let section = ProcessInfo.processInfo.environment["LIMMR_HOME_PROFILE"] {
      let view: AnyView
      switch section {
      case "overview": view = AnyView(N1Overview(store: store) { _ in })
      case "tasks": view = AnyView(TasksTile(store: store))
      case "circles": view = AnyView(N1CirclesGrid(store: store))
      case "rail": view = AnyView(N1HomeRail(store: store))
      default: view = AnyView(HomeView(store: store))
      }
      let host = NSHostingView(rootView: view)
      host.sizingOptions = []; host.frame = NSRect(x: 0, y: 0, width: 1280, height: 800)
      let start = ContinuousClock.now; host.layoutSubtreeIfNeeded()
      print("N1 Home section \(section) first layout \(start.duration(to: .now))")
      return
    }
    let detector = Task.detached {
      var delays: [Double] = []
      while !Task.isCancelled {
        let start = ContinuousClock.now
        await MainActor.run {}
        let duration = start.duration(to: .now).components
        delays.append(Double(duration.seconds) * 1000 + Double(duration.attoseconds) / 1e15)
        try? await Task.sleep(for: .milliseconds(10))
      }
      return delays
    }
    try await Task.sleep(for: .milliseconds(20))
    print("N1 initial refresh calls \(await backend.calls)")
    for (name, view) in [("Home", AnyView(HomeView(store: store))), ("Tasks", AnyView(AllTasksView(store: store))), ("People", AnyView(CirclesView(store: store))), ("Activity", AnyView(ActivityView(store: store, detailedLog: true)))] {
      let host = NSHostingView(rootView: view); host.sizingOptions = []; host.frame = NSRect(x: 0, y: 0, width: 1280, height: 800)
      let start = ContinuousClock.now; host.layoutSubtreeIfNeeded(); let duration = start.duration(to: .now)
      try await Task.sleep(for: .milliseconds(200))
      print("N1 \(name) API calls \(await backend.calls)")
      print("N1 \(name) first layout \(duration), bodies \(SpeedTrace.bodies)")
    }
    // Reopen the first cached page after initial framework/view construction.
    store.activity = Array(store.activity.prefix(50))
    let cached = NSHostingView(rootView: ActivityView(store: store, detailedLog: true))
    cached.sizingOptions = []; cached.frame = NSRect(x: 0, y: 0, width: 1280, height: 800)
    let reopen = ContinuousClock.now; cached.layoutSubtreeIfNeeded()
    print("N1 cached first page reopen \(reopen.duration(to: .now))")
    detector.cancel(); let delays = await detector.value
    print("N1 main heartbeat worst ms \(delays.max() ?? 0), stalls over 100ms \(delays.filter { $0 > 100 }.count)")
    let image = NSImage(size: NSSize(width: 4000, height: 3000), flipped: false) { r in NSColor.systemBlue.setFill(); r.fill(); return true }
    let data = try #require(image.tiffRepresentation)
    let start = ContinuousClock.now
    for _ in 0..<40 { let img = try #require(NSImage(data: data)); _ = img.cgImage(forProposedRect: nil, context: nil, hints: nil) }
    print("N1 full decode 40 images \(start.duration(to: .now))")
  }
}

private actor N1CountingBackend: Backend {
  let base = DemoBackend()
  var calls: [String: Int] = [:]
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    let start = ContinuousClock.now
    calls[path, default: 0] += 1
    let value = try await base.request(path, method: method, body: body)
    print("N1 API \(path) \(start.duration(to: .now))")
    return value
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { AsyncThrowingStream { $0.finish() } }
}
