import AppKit
import Foundation
import LimmrKit
import SwiftUI
import QuickLookUI
import UniformTypeIdentifiers
import Observation

@MainActor @Observable final class TaskAssetTileCache {
  var urls: [String: URL] = [:]
}

struct TaskAssetTile: View {
  let backend: any Backend
  let taskID: String
  let item: WorkspaceItem
  var cachedPreviews: TaskAssetTileCache? = nil
  var allowModel = true
  var named: (TrackedTask) -> Void = { _ in }
  @State private var busy = false
  @State private var error: String?
  @State private var preview: URL?
  @State private var site: LinkPreview?
  @State private var quickLook: URL?
  @State private var hovering = false
  @FocusState private var focused: Bool
  private var source: TaskAssetSource { .init(backend: backend, taskID: taskID, item: item) }
  private var displayPreview: URL? { preview ?? cachedPreviews?.urls[item.id] }
  private var link: URL? { TaskPresentation.safeLink(item.url) }
  private var name: String { item.kind == "link" ? (site?.title ?? item.aiName ?? item.title) : (item.aiName ?? item.title) }
  private var tooltip: String {
    [name, item.description, item.title == name ? nil : item.title, item.sender, item.chat,
     Timestamp.date(item.addedAt)?.formatted(date: .abbreviated, time: .shortened)].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: "\n")
  }
  var body: some View {
    Button { open() } label: {
      GeometryReader { geometry in
        ZStack(alignment: .bottomLeading) {
          if let preview = displayPreview {
            ThumbnailImage(url: preview).scaledToFill().frame(width: geometry.size.width, height: geometry.size.height).clipped()
          } else {
            Rectangle().fill(.primary.opacity(0.045))
            VStack(alignment: .leading, spacing: 10) {
              if let icon = site?.icon { ThumbnailImage(url: icon, pixels: 48).scaledToFit().frame(width: 22, height: 22) }
              else { Image(systemName: item.kind == "link" ? "link" : item.mediaType == "video" ? "play.rectangle" : item.mediaType == "image" ? "photo" : "doc.text").font(.title2).foregroundStyle(.secondary) }
              Spacer(minLength: 8)
            }.padding(14)
          }
          if hovering || focused || error != nil { VStack(alignment: .leading, spacing: 3) {
            Text(name).font(.callout.weight(.semibold)).lineLimit(2).frame(maxWidth: .infinity, alignment: .leading)
            HStack(spacing: 4) {
              if item.kind == "link", let icon = site?.icon {
                ThumbnailImage(url: icon, pixels: 48).scaledToFit().frame(width: 12, height: 12)
              }
              Text(item.kind == "link" ? (link?.host ?? "Link") : item.title)
                .font(.caption).lineLimit(1)
            }.opacity(0.7)
            if hovering, let description = item.description, !description.isEmpty { Text(description).font(.caption).lineLimit(2).opacity(0.85) }
            if let error { Text(error).font(.caption).lineLimit(2) }
          }.padding(10).foregroundStyle(.primary).coverLabel().padding(8) }
          if busy { ProgressView().controlSize(.small).padding(12).frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topTrailing) }
          if item.mediaType == "video" { Image(systemName: "play.fill").font(.callout).padding(10).foregroundStyle(.white).background(.black.opacity(0.4), in: Circle()).padding(12).frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topTrailing) }
        }.clipShape(RoundedRectangle(cornerRadius: LimmrTheme.tileRadius))
      }.frame(height: 166).contentShape(RoundedRectangle(cornerRadius: LimmrTheme.tileRadius))
    }.quietCard().focused($focused).disabled(busy).help(tooltip)
      .accessibilityLabel(name).accessibilityHint("Open preview. Drag to another app.")
      .onHover { hovering = $0 }
      .onKeyPress(.space) {
        if item.kind == "link" { open() } else { operate(.open) }
        return .handled
      }
      .onKeyPress(.return) { open(); return .handled }
      .sheet(isPresented: Binding(get: { quickLook != nil }, set: { if !$0 { quickLook = nil } })) {
        if let quickLook { TaskFileQuickLook(url: quickLook).frame(minWidth: 600, minHeight: 500).dismissOnEscape() }
      }
      .task(id: taskID + item.id) { await loadPreview() }
      .onDrag { provider() }
      .contextMenu {
        Button("Open") { open() }
        Button("Save to Downloads") { operate(.save) }
        Button("Copy") { operate(.copy) }
        if item.kind == "file" { Button("Show in Finder") { operate(.reveal) } }
        Button("Copy link") { if link != nil { copyLink() } else { operate(.copyLink) } }
      }
  }
  private func loadPreview() async {
    if let link {
      site = await MediaPreviewCache.shared.link(link); preview = site?.image
    } else {
      await source.progressivePreview { url in
        guard !Task.isCancelled else { return }
        preview = url
      }
    }
    guard !Task.isCancelled else { return }
    if let preview { cachedPreviews?.urls[item.id] = preview }
    else { cachedPreviews?.urls[item.id] = nil }
    if let preview, item.kind == "file", item.namedOnDevice != true,
       let updated = await OnDeviceAssetNames.name(preview: preview, source: source, allowModel: allowModel) { named(updated) }
  }
  private func open() {
    if let link { NSWorkspace.shared.open(link) }
    else { operate(.open) }
  }
  private enum Operation { case open, save, copy, copyLink, reveal }
  private func operate(_ operation: Operation) {
    busy = true; error = nil
    Task { @MainActor in
      defer { busy = false }
      do {
        if let link {
          if operation == .save {
            let target = try downloads().appendingPathComponent("Link-\(MediaPreviewCache.key(link.absoluteString).prefix(8)).webloc")
            let data = try PropertyListSerialization.data(fromPropertyList: ["URL": link.absoluteString], format: .xml, options: 0)
            try data.write(to: target, options: .withoutOverwriting)
          } else { copyLink() }; return
        }
        guard let url = try await source.file(download: true) else { return }
        switch operation {
        case .open: quickLook = url
        case .save:
          let directory = try downloads(); var target = directory.appendingPathComponent(url.lastPathComponent)
          if FileManager.default.fileExists(atPath: target.path) { target = directory.appendingPathComponent(UUID().uuidString.prefix(8) + "-" + url.lastPathComponent) }
          try FileManager.default.copyItem(at: url, to: target)
        case .copy: NSPasteboard.general.clearContents(); NSPasteboard.general.writeObjects([url as NSURL])
        case .copyLink: NSPasteboard.general.clearContents(); NSPasteboard.general.setString(url.absoluteString, forType: .string)
        case .reveal: NSWorkspace.shared.activateFileViewerSelecting([url])
        }
        await loadPreview()
      } catch { self.error = "File unavailable. Try again." }
    }
  }
  private func downloads() throws -> URL { try FileManager.default.url(for: .downloadsDirectory, in: .userDomainMask, appropriateFor: nil, create: true) }
  private func copyLink() {
    let value = link?.absoluteString
    if let value { NSPasteboard.general.clearContents(); NSPasteboard.general.setString(value, forType: .string) }
  }
  private func provider() -> NSItemProvider {
    if let link { return NSItemProvider(object: link as NSURL) }
    let provider = NSItemProvider(); provider.suggestedName = item.title
    provider.registerFileRepresentation(forTypeIdentifier: UTType.data.identifier, fileOptions: [], visibility: .all) { completion in
      let work = Task {
        do { guard let url = try await source.file(download: true) else { completion(nil, false, nil); return }; completion(url, false, nil) }
        catch { completion(nil, false, error) }
      }
      let progress = Progress(totalUnitCount: 1); progress.cancellationHandler = { work.cancel() }; return progress
    }
    return provider
  }
}

// QuickLook's SwiftUI modifier is unavailable on macOS. Embed its native view.
private struct TaskFileQuickLook: NSViewRepresentable {
  let url: URL
  func makeNSView(context: Context) -> QLPreviewView { QLPreviewView(frame: .zero, style: .normal)! }
  func updateNSView(_ view: QLPreviewView, context: Context) { view.previewItem = url as NSURL }
}
