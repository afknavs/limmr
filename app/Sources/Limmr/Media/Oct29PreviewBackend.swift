import AppKit
import Foundation
import LimmrKit

/// Synthetic, local-only preview fixtures. Never loads a real chat or website.
actor Oct29PreviewBackend: Backend {
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { AsyncThrowingStream { $0.finish() } }
  let base = DemoBackend()
  var task: TrackedTask
  let paths: [String: URL]
  init(task: TrackedTask, paths: [String: URL]) { self.task = task; self.paths = paths }
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    let route = URLComponents(string: path)?.path ?? path
    if route == "/v1/home" {
      var home: HomeResponse = try await base.get(path); home.tasks = [task.compact(now: Timestamp.date(DemoData.now)!)]; home.queued = []; home.proposals = []
      return try Wire.data(home)
    }
    if route == "/v1/tasks" { return try Wire.data(TasksResponse(tasks: [task.compact(now: Timestamp.date(DemoData.now)!)])) }
    if route.hasPrefix("/v1/tasks/tsk_workshop/items/") {
      let id = String(route.dropFirst("/v1/tasks/tsk_workshop/items/".count).split(separator: "/").first ?? "")
      if route.hasSuffix("/preview") { return try JSONSerialization.data(withJSONObject: ["path": paths[id]?.path ?? "", "thumbnail": ""]) }
      if route.hasSuffix("/download"), let url = paths[id] { return try Wire.data(["path": url.path]) }
      return try Wire.data(task)
    }
    if route == "/v1/tasks/tsk_workshop" { return try Wire.data(task) }
    return try await base.request(path, method: method, body: body)
  }
}

@MainActor enum Oct29PreviewFixtures {
  static func make() throws -> Oct29PreviewBackend {
    let home = URL(fileURLWithPath: ProcessInfo.processInfo.environment["LIMMR_HOME"]!)
    let root = home.appendingPathComponent("synthetic-assets", isDirectory: true)
    try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    func poster(_ name: String, title: String, subtitle: String, color: NSColor) throws -> URL {
      let image = NSImage(size: NSSize(width: 900, height: 1200), flipped: false) { bounds in
        color.setFill(); bounds.fill()
        NSColor.black.withAlphaComponent(0.16).setFill(); NSBezierPath(ovalIn: NSRect(x: 320, y: 180, width: 800, height: 800)).fill()
        NSColor.white.withAlphaComponent(0.18).setStroke()
        for x in stride(from: 70, to: 900, by: 80) { let path = NSBezierPath(); path.move(to: NSPoint(x: x, y: 280)); path.line(to: NSPoint(x: x, y: 900)); path.lineWidth = 1; path.stroke() }
        let text: (String, CGFloat, CGFloat, CGFloat, NSFont.Weight) -> Void = { string, x, y, size, weight in
          (string as NSString).draw(in: NSRect(x: x, y: y, width: 760, height: 250), withAttributes: [.font: NSFont.systemFont(ofSize: size, weight: weight), .foregroundColor: NSColor.white])
        }
        text("DESIGN CLUB / 2026", 64, 930, 25, .medium)
        text(title, 64, 600, 104, .bold)
        text(subtitle, 64, 190, 32, .medium)
        text("NORTH CAMPUS   ·   5 NOV   ·   4 PM", 64, 20, 23, .semibold)
        return true
      }
      let url = root.appendingPathComponent(name + ".png")
      let rep = NSBitmapImageRep(data: image.tiffRepresentation!)!
      try rep.representation(using: .png, properties: [:])!.write(to: url)
      return url
    }
    let posterURL = try poster("workshop-poster", title: "Make ideas\nwith AI.", subtitle: "A practical workshop\nBring your curiosity.", color: NSColor(calibratedRed: 0.18, green: 0.37, blue: 0.30, alpha: 1))
    let secondURL = try poster("session-plan", title: "From idea\nto demo.", subtitle: "Try it. Build it. Share it.", color: NSColor(calibratedRed: 0.48, green: 0.26, blue: 0.17, alpha: 1))
    let pdf = root.appendingPathComponent("Workshop guide.pdf")
    var box = CGRect(x: 0, y: 0, width: 600, height: 800)
    let context = CGContext(pdf as CFURL, mediaBox: &box, nil)!
    context.beginPDFPage(nil); context.setFillColor(NSColor.white.cgColor); context.fill(box)
    NSGraphicsContext.saveGraphicsState(); NSGraphicsContext.current = NSGraphicsContext(cgContext: context, flipped: false)
    ("WORKSHOP GUIDE\n\nMake ideas with AI\n\n1. Start with a question\n2. Try a small prototype\n3. Share what you learned" as NSString).draw(in: NSRect(x: 50, y: 200, width: 500, height: 530), withAttributes: [.font: NSFont.systemFont(ofSize: 28), .foregroundColor: NSColor.black])
    NSGraphicsContext.restoreGraphicsState(); context.endPDFPage(); context.closePDF()
    func asset(_ id: String, title: String, name: String, type: String, date: String) -> WorkspaceItem {
      var item = WorkspaceItem(id: id, kind: "file", title: title, source: "chat", addedAt: date)
      item.aiName = name; item.mediaType = type; item.sender = "Maya Rao"; item.chat = "Design Club"; item.namedOnDevice = true
      item.width = 900; item.height = 1200; item.description = "Workshop poster with a green background and the session date"; item.ocrText = "North Campus 5 Nov 4 pm"
      return item
    }
    var main = asset("poster", title: "IMG_1028.png", name: "AI workshop poster — 5 Nov", type: "image", date: "2026-10-17T11:40:00Z")
    main.caption = "Workshop poster"
    var link = WorkspaceItem(id: "link", kind: "link", title: "example.com/workshop", url: "https://example.com/workshop", addedAt: "2026-10-17T11:30:00Z")
    link.aiName = "Workshop details & registration"; link.sender = "Maya Rao"; link.chat = "Design Club"
    let cache = home.appendingPathComponent("previews", isDirectory: true); try FileManager.default.createDirectory(at: cache, withIntermediateDirectories: true)
    let preview = LinkPreview(title: "Workshop details & registration", image: secondURL, icon: nil)
    try JSONEncoder().encode(preview).write(to: cache.appendingPathComponent(MediaPreviewCache.key(link.url!) + ".json"))
    let items = [main,
      asset("alternate", title: "session-v2.png", name: "Workshop session poster", type: "image", date: "2026-10-17T11:35:00Z"),
      asset("guide", title: "Workshop guide.pdf", name: "Workshop guide — exercises & plan", type: "document", date: "2026-10-17T11:20:00Z"), link,
      asset("unavailable", title: "IMG_1029.jpg", name: "Venue photo — not downloaded", type: "image", date: "2026-10-17T11:10:00Z")]
    var task = DemoTasks.tasks[0]; task.id = "tsk_workshop"; task.title = "AI workshop"; task.goal = "Get the workshop ready for 5 November."
    task.summary.text = "The poster and session guide are ready. Confirm the room and share the registration link."
    task.checklist = [.init(id: "poster-step", text: "Finish the poster", done: true, order: 0), .init(id: "room-step", text: "Confirm North Hall", order: 1)]
    task.progress = .init(done: 1, total: 2)
    task.items = items; task.cover = main
    task.ownerNotes = [.init(id: "owner-note", text: "The venue changed to North Hall. Involve Maya in the final poster review.", createdAt: "2026-10-17T11:45:00Z", updatedAt: "2026-10-17T11:45:00Z")]
    task.history = [.init(at: "2026-10-17T10:00:00Z", kind: "due", text: "Check-in fired — ready for review"), .init(at: "2026-10-17T10:01:00Z", kind: "check_in_received", text: "Sent to your dot")]
    return Oct29PreviewBackend(task: task, paths: ["poster": posterURL, "alternate": secondURL, "guide": pdf])
  }
}
