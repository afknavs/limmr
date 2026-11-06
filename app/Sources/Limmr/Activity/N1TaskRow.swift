import AppKit
import LimmrKit
import SwiftUI

/// Presentation-only consumer of n1-speed's cache API. A downloaded file wins;
/// no cover view asks the engine to download an original.
enum N1TaskPreview {
  static func load(backend: any Backend, task: TaskSummary) async -> URL? {
    if let candidate = task.coverCandidate {
      guard candidate.thumbnailUrl.hasPrefix("/v1/tasks/"), !candidate.thumbnailUrl.contains("..") else { return nil }
      if let cached: TaskAssetSource.Cached = try? await backend.get(candidate.thumbnailUrl) {
        if !cached.path.isEmpty, let url = await MediaPreviewCache.shared.file(URL(fileURLWithPath: cached.path)) { return url }
        if let data = cached.thumbnail, let url = await MediaPreviewCache.shared.embedded(data, key: candidate.thumbnailUrl) { return url }
      }
      if let link = candidate.linkUrl.flatMap(TaskPresentation.safeLink) { return await MediaPreviewCache.shared.link(link).image }
      return nil
    }
    guard let cover = task.cover else { return nil }
    return await TaskAssetSource(backend: backend, taskID: task.id, item: cover).preview()
  }
}
enum N1TaskStatus {
  static func text(_ task: TaskSummary) -> String {
    if ["done", "dropped", "dismissed"].contains(task.status) { return TaskListPresentation.closure(task) }
    return task.needsYouReason ?? (task.waiting.map(TaskPresentation.waiting) ?? task.status.capitalized)
  }
}
struct N1TaskActions: View {
  @Bindable var store: AppStore
  let task: TaskSummary
  var body: some View {
    Button("Open task") { store.openTask(task.id) }
    if ["active", "waiting", "quiet"].contains(task.status) {
      Button(task.status == "quiet" ? "Wake task" : "Make quiet") {
        Task { await store.quietTask(task.id, status: task.status == "quiet" ? "active" : "quiet") }
      }
    }
  }
}
struct N1TaskThumbnail: View {
  @Bindable var store: AppStore
  let task: TaskSummary
  @State private var preview: URL?
  var body: some View {
    Group {
      if let cached = store.taskCovers.cached(task, generation: store.backendGeneration) { Image(decorative: cached.raster.image, scale: 2).resizable().scaledToFill() }
      else if let preview { ThumbnailImage(url: preview, pixels: 96).scaledToFill() }
      else if let circle = store.taskCircles(task).first { CirclePicture(circle: circle, size: 32) }
      else { Image(systemName: "checklist").symbolRenderingMode(.hierarchical) }
    }.frame(width: 32, height: 38).clipped().clipShape(RoundedRectangle(cornerRadius: 4))
      .accessibilityHidden(true)
      .task(id: task.id + (task.coverCandidate?.thumbnailUrl ?? task.cover?.id ?? "")) { preview = await store.taskCovers.load(task, backend: store.backend, generation: store.backendGeneration)?.url }
  }
}
struct N1TaskRow: View {
  @Bindable var store: AppStore
  let task: TaskSummary
  var body: some View {
    Button { store.openTask(task.id) } label: {
      HStack(spacing: 8) {
        N1TaskThumbnail(store: store, task: task)
        VStack(alignment: .leading, spacing: 2) {
          Text(task.title).font(.headline).lineLimit(1)
          Text(N1TaskStatus.text(task)).font(.caption).foregroundStyle(task.needsYou ? Color.orange : .secondary).lineLimit(1)
        }.frame(maxWidth: .infinity, alignment: .leading)
        Text(store.taskCircleName(task)).font(.caption).foregroundStyle(.secondary).lineLimit(1)
        Text("\(task.progress.done)/\(task.progress.total)").font(.caption).monospacedDigit()
      }.contentShape(Rectangle())
    }.quietCard().modifier(QuietRow())
      .contextMenu { N1TaskActions(store: store, task: task) }
      .accessibilityLabel(TaskPresentation.accessibilityLabel(task) + ". " + store.taskCircleName(task))
  }
}

private struct SnapshotTaskRevealKey: EnvironmentKey { static let defaultValue: String? = nil }
extension EnvironmentValues {
  var snapshotTaskReveal: String? { get { self[SnapshotTaskRevealKey.self] } set { self[SnapshotTaskRevealKey.self] = newValue } }
}
struct N1TaskCover: View {
  @Bindable var store: AppStore
  let task: TaskSummary
  let preview: URL?
  var galleryFocus: FocusState<String?>.Binding? = nil
  @Environment(\.snapshotTaskReveal) private var snapshotReveal
  @State private var hovering = false
  @FocusState private var focused: Bool
  private var revealed: Bool { hovering || focused || galleryFocus?.wrappedValue == task.id || snapshotReveal == task.id }
  private var circle: LimmrKit.Circle? { store.taskCircles(task).first }
  var body: some View {
    if let galleryFocus { cover.focused(galleryFocus, equals: task.id) }
    else { cover }
  }
  private var cover: some View {
    Button { store.openTask(task.id) } label: {
      VStack(alignment: .leading, spacing: 8) {
        GeometryReader { geometry in
          ZStack(alignment: .topTrailing) {
            if let cached = store.taskCovers.cached(task, generation: store.backendGeneration) {
              Image(decorative: cached.raster.image, scale: 2).resizable().scaledToFill().frame(width: geometry.size.width, height: geometry.size.height).clipped()
            } else if let preview {
              ThumbnailImage(url: preview).scaledToFill().frame(width: geometry.size.width, height: geometry.size.height).clipped()
            } else {
              VStack(alignment: .leading, spacing: 8) {
                if let circle { CirclePicture(circle: circle, size: 36) }
                else { Image(systemName: "checklist").font(.title2).foregroundStyle(.secondary) }
                Spacer(minLength: 8)
                Text(task.title).font(.title2.bold()).lineLimit(4)
                Text(store.taskCircleName(task)).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                if let at = task.nextCheckInAt { Text(TaskPresentation.checkIn(at)).font(.caption).foregroundStyle(.secondary).lineLimit(1) }
                Spacer(minLength: 8)
              }.padding(18).frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
                .background(.quaternary.opacity(0.3))
            }
            if task.needsYou { Circle().fill(.orange).frame(width: 8, height: 8).padding(12) }
          }.clipShape(RoundedRectangle(cornerRadius: LimmrTheme.tileRadius))
            .overlay(RoundedRectangle(cornerRadius: LimmrTheme.tileRadius).strokeBorder(revealed ? Color.secondary.opacity(0.5) : Color(nsColor: .separatorColor), lineWidth: revealed ? 1 : 0.5))
        }.aspectRatio(4.0 / 5, contentMode: .fit)
        VStack(alignment: .leading, spacing: 3) {
          Text(task.title).font(.headline).lineLimit(1)
          Text(store.taskCircleName(task)).font(.caption).foregroundStyle(.secondary).lineLimit(1)
          Text(N1TaskStatus.text(task)).font(.caption).foregroundStyle(task.needsYou ? Color.orange : .secondary).lineLimit(1)
        }.frame(maxWidth: .infinity, alignment: .leading)
      }.foregroundStyle(.primary).contentShape(Rectangle())
    }.quietCard().focused($focused)
      .onKeyPress(.return) { store.openTask(task.id); return .handled }
      .onHover { hovering = $0 }
      .accessibilityLabel(TaskPresentation.accessibilityLabel(task) + ". " + store.taskCircleName(task))
      .accessibilityHint("Open task workspace")
  }
}

/// Arrow navigation follows the actual grid width and remains bounded at either end.
struct N1TaskGallery: View {
  @Bindable var store: AppStore
  let tasks: [TaskSummary]
  var horizontal = false
  @State private var columns = 1
  @FocusState private var selection: String?
  var body: some View {
    Group {
      Group {
        if horizontal {
          HStack(alignment: .top, spacing: 20) { tiles }.frame(height: 300)
        } else {
          LazyVGrid(columns: [GridItem(.adaptive(minimum: 200), spacing: 20)], spacing: 20) { tiles }
            .onGeometryChange(for: Int.self) { max(1, Int(($0.size.width + 20) / 220)) } action: { columns = $0 }
        }
      }
    }
    .onKeyPress(.leftArrow) { move(-1); return .handled }
    .onKeyPress(.rightArrow) { move(1); return .handled }
    .onKeyPress(.upArrow) { move(horizontal ? -1 : -columns); return .handled }
    .onKeyPress(.downArrow) { move(horizontal ? 1 : columns); return .handled }
    .onChange(of: store.rowRequest) { _, _ in move(store.rowStep) }
    .onChange(of: store.rowOpenRequest) { _, _ in if let selection { store.openTask(selection) } }
  }
  private var tiles: some View {
    ForEach(tasks) { task in
      SurfacedTaskRow(store: store, task: task, galleryFocus: $selection)
        .frame(width: horizontal ? 180 : nil)
    }
  }
  private func move(_ step: Int) {
    var cursor = KeyboardSelection(id: selection)
    cursor.move(step, in: tasks.map(\.id)); selection = cursor.id
  }
}

/// By-purpose is a reading list, with bounded rows even for a single coverless task.
struct PurposeTaskCard: View {
  @Bindable var store: AppStore
  let task: TaskSummary
  var body: some View {
    Button { store.openTask(task.id) } label: {
      HStack(alignment: .top, spacing: 12) {
        N1TaskThumbnail(store: store, task: task)
        VStack(alignment: .leading, spacing: 6) {
          Text(task.title).font(.headline).lineLimit(2)
          Text(store.taskCircleName(task)).font(.caption).foregroundStyle(.secondary).lineLimit(1)
          Text(store.purposeLastAction(task)).font(.callout).foregroundStyle(.secondary).lineLimit(1)
        }.frame(maxWidth: .infinity, alignment: .leading)
        if task.needsYou { Circle().fill(.orange).frame(width: 6, height: 6) }
      }.padding(14).frame(maxWidth: .infinity, alignment: .leading).frame(height: 112)
        .background(.quaternary.opacity(0.3), in: RoundedRectangle(cornerRadius: 10))
        .contentShape(RoundedRectangle(cornerRadius: 10))
    }.quietCard()
      .accessibilityLabel(task.title + ". " + store.taskCircleName(task) + ". " + store.purposeLastAction(task))
  }
}
extension AppStore {
  func purposeLastAction(_ task: TaskSummary) -> String {
    let action = home.recentLines.first { $0.targetId == task.id }?.text ?? N1TaskStatus.text(task)
    return action + (task.lastActiveAt.map { " · " + relativeTime($0) } ?? "")
  }
}
