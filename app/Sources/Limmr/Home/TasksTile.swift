import AppKit
import LimmrKit
import SwiftUI

struct TaskSummaryRow: View {
  let task: TaskSummary
  let open: () -> Void
  var body: some View {
    Button(action: open) {
      VStack(alignment: .leading, spacing: 8) {
        HStack(alignment: .firstTextBaseline) {
          Text(task.title).font(.headline).fixedSize(horizontal: false, vertical: true)
          Spacer(minLength: 12)
          if task.needsYou {
            Text("Needs you").font(.caption).foregroundStyle(Color.accentColor)
              .padding(.horizontal, 7).padding(.vertical, 3)
              .background(Color.accentColor.opacity(0.1), in: Capsule()).fixedSize()
          }
          else { Text(task.status.capitalized).font(.caption).foregroundStyle(.secondary) }
        }
        if task.progress.total > 0 {
          ProgressView(value: task.progress.fraction).progressViewStyle(TaskCardProgressStyle()).accessibilityLabel(task.progress.text)
          Text(task.progress.text).font(.caption).foregroundStyle(.secondary)
        }
        if ["done", "dropped", "dismissed"].contains(task.status) {
          Text(TaskListPresentation.closure(task)).font(.caption).foregroundStyle(.secondary)
        }
        if let waiting = task.waiting { Text(TaskPresentation.waiting(waiting)).font(.callout).foregroundStyle(.secondary) }
        if let at = task.nextCheckInAt {
          Text("Next check-in · \(TaskPresentation.checkIn(at))").font(.callout).foregroundStyle(.secondary)
        }
      }.padding(16).frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .modifier(HomeTileSurface()).contentShape(RoundedRectangle(cornerRadius: 10))
    }.quietCard()
      .accessibilityElement(children: .ignore)
      .accessibilityLabel(TaskPresentation.accessibilityLabel(task))
      .accessibilityHint("Open task workspace")
  }
}

struct TasksTile: View {
  @Bindable var store: AppStore
  var body: some View {
    VStack(alignment: .leading, spacing: 16) {
      HStack {
        Text("Tasks").font(.title3.weight(.semibold)).foregroundStyle(.secondary)
        Spacer()
        Button("Show all") { store.selectPage(.tasks) }.buttonStyle(.bordered)
      }
      ScrollView(.horizontal) {
        N1TaskGallery(store: store, tasks: Array(TaskListPresentation.active(store.home.tasks).prefix(6)), horizontal: true)
      }.frame(height: 300)
      if let notice = store.taskQuietNotice {
        HStack {
          Text(notice).font(.caption).foregroundStyle(.secondary)
          Button("Undo") { Task { await store.undoQuietTask() } }.buttonStyle(.bordered).controlSize(.small)
        }
      }
    }
  }

}

/// AppKit's default linear style ignores tint inside a row button on macOS.
/// Keep ProgressView's semantics and draw the same accent in both appearances.
struct TaskCardProgressStyle: ProgressViewStyle {
  func makeBody(configuration: Configuration) -> some View {
    GeometryReader { geometry in
      Capsule().fill(.primary.opacity(0.08))
        .overlay(alignment: .leading) {
          Capsule().fill(Color.accentColor)
            .frame(width: geometry.size.width * min(1, max(0, configuration.fractionCompleted ?? 0)))
        }
    }.frame(height: 6)
  }
}


struct SurfacedTaskRow: View {
  @Bindable var store: AppStore
  let task: TaskSummary
  var galleryFocus: FocusState<String?>.Binding? = nil
  @Environment(\.colorScheme) private var colorScheme
  @State private var coverPreview: URL?
  @State private var showCoverPicker = false
  var compact = false
  var changed: (TaskSummary) -> Void = { _ in }
  private var suggested: Bool { task.status == "suggested" }
  var body: some View {
    VStack(alignment: .leading, spacing: 6) {
      Group {
        if compact { PurposeTaskCard(store: store, task: task) }
        else { N1TaskCover(store: store, task: task, preview: coverPreview, galleryFocus: galleryFocus) }
      }
        .contextMenu {
          Button("Open") { store.openTask(task.id) }
          Button("Change cover…") { showCoverPicker = true }
          if task.cover != nil { Button("Remove cover") { removeCover() } }
          if suggested { Button("Keep") { act(.keep) } }
          if TaskPresentation.canUndo(task, now: store.presentationNow) {
            Button("Undo tracking") { act(.undo) }
          }
          Button(task.status == "quiet" ? "Wake task" : "Make quiet") { Task { await store.quietTask(task.id, status: task.status == "quiet" ? "active" : "quiet") } }
          Button("Done") { Task { await store.quietTask(task.id, status: "done") } }
          Button("Not now") { Task { await store.quietTask(task.id, status: "dropped") } }
        }
      if let error = store.taskActionErrors[task.id] {
        Text(error).font(.caption).foregroundStyle(.secondary).padding(.horizontal, 12)
      }
    }.disabled(store.taskActions.contains(task.id))
      .task(id: task.coverCandidate?.thumbnailUrl ?? task.cover?.id) {
        coverPreview = await store.taskCovers.load(task, backend: store.backend, generation: store.backendGeneration)?.url
      }
      .sheet(isPresented: $showCoverPicker) { TaskCoverPicker(store: store, taskID: task.id) }
  }
  private func removeCover() {
    Task {
      var patch = TaskPatch(); patch.coverRemoved = true
      do { _ = try await TaskClient(backend: store.backend).patch(task.id, patch); await store.refreshTaskHome() }
      catch { store.taskActionErrors[task.id] = "Couldn’t remove the cover." }
    }
  }
  private func act(_ action: TaskSurfacingAction) {
    Task { if let updated = await store.surfaceTask(task.id, action: action) { changed(updated) } }
  }
}
