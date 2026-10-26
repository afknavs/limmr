import LimmrKit
import SwiftUI

struct TaskSummaryRow: View {
  let task: TaskSummary
  let open: () -> Void
  var body: some View {
    Button(action: open) {
      VStack(alignment: .leading, spacing: 8) {
        HStack(alignment: .firstTextBaseline) {
          Text(task.title).font(AppType.rowTitle).fixedSize(horizontal: false, vertical: true)
          Spacer(minLength: 12)
          if task.needsYou {
            Text("Needs you").font(AppType.caption).foregroundStyle(Color.accentColor)
              .padding(.horizontal, 7).padding(.vertical, 3)
              .background(Color.accentColor.opacity(0.1), in: Capsule()).fixedSize()
          }
          else { Text(task.status.capitalized).font(AppType.caption).foregroundStyle(AccessibleSecondary()) }
        }
        if task.progress.total > 0 {
          ProgressView(value: task.progress.fraction).progressViewStyle(TaskCardProgressStyle()).accessibilityLabel(task.progress.text)
          Text(task.progress.text).font(AppType.caption).foregroundStyle(AccessibleSecondary())
        }
        if let waiting = task.waiting { Text(TaskPresentation.waiting(waiting)).font(.callout).foregroundStyle(AccessibleSecondary()) }
        if let at = task.nextCheckInAt {
          Text("Next check-in · \(TaskPresentation.checkIn(at))").font(.callout).foregroundStyle(AccessibleSecondary())
        }
      }.padding(16).frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .modifier(HomeTileSurface()).contentShape(RoundedRectangle(cornerRadius: 10))
    }.limmrButton(.row(radius: 10, bordered: false), id: "task-\(task.id)")
      .accessibilityElement(children: .ignore)
      .accessibilityLabel(TaskPresentation.accessibilityLabel(task))
      .accessibilityHint("Open task workspace")
  }
}

struct TasksTile: View {
  @Bindable var store: AppStore
  var body: some View {
    VStack(alignment: .leading, spacing: 16) {
      Text("Tasks").font(AppType.blockHeading).foregroundStyle(AccessibleSecondary())
      VStack(alignment: .leading, spacing: 10) {
        ForEach(TaskPresentation.open(store.home.tasks).prefix(6)) { task in
          SurfacedTaskRow(store: store, task: task)
        }
        ForEach(TaskPresentation.suggested(store.home.tasks).prefix(3)) { task in
          SurfacedTaskRow(store: store, task: task)
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
  var changed: (TaskSummary) -> Void = { _ in }
  private var suggested: Bool { task.status == "suggested" }
  var body: some View {
    VStack(alignment: .leading, spacing: 6) {
      Button { store.openTask(task.id) } label: {
        VStack(alignment: .leading, spacing: 6) {
          HStack(alignment: .firstTextBaseline, spacing: 12) {
            Text(task.title).font(.callout.weight(suggested ? .regular : .semibold)).lineLimit(1)
            Spacer(minLength: 8)
            if suggested { Color.clear.frame(width: 150, height: 22) }
            else if task.needsYou {
              Text("Needs you").font(AppType.caption).foregroundStyle(Color.accentColor)
            } else if task.progress.total > 0 {
              Text(task.progress.text).font(AppType.caption).foregroundStyle(AccessibleSecondary())
            }
          }
          if suggested, let signal = task.suggestion?.signals.first {
            Text(signal).font(AppType.caption).foregroundStyle(AccessibleSecondary()).lineLimit(1).help(signal)
          }
        }.padding(12).frame(maxWidth: .infinity, alignment: .leading)
          .background(.primary.opacity(suggested ? 0.012 : 0.035), in: RoundedRectangle(cornerRadius: 8))
          .contentShape(RoundedRectangle(cornerRadius: 8))
      }.limmrButton(.row(radius: 8, bordered: false), id: "surfaced-task-\(task.id)")
        .accessibilityLabel("Open task: \(task.title)")
        .overlay(alignment: .topTrailing) {
          if suggested {
            HStack(spacing: 8) {
              Button("Keep") { act(.keep) }.buttonStyle(.borderedProminent)
                .accessibilityLabel("Keep \(task.title)")
              Button("Not a task") { act(.dismiss) }.buttonStyle(.bordered)
                .accessibilityLabel("Dismiss \(task.title) as a task")
            }.controlSize(.small).padding(12)
          }
        }
      if TaskPresentation.canUndo(task, now: store.demo ? Timestamp.date(DemoData.now)! : Date()) {
        HStack(spacing: 4) {
          Text("Started by \(task.createdBy.name.isEmpty ? "an AI" : task.createdBy.name) ·")
          Button("Undo") { act(.undo) }.limmrButton().accessibilityLabel("Undo starting \(task.title)")
        }.font(AppType.caption).foregroundStyle(AccessibleSecondary()).padding(.horizontal, 12)
      }
      if let error = store.taskActionErrors[task.id] {
        Text(error).font(AppType.caption).foregroundStyle(AccessibleSecondary()).padding(.horizontal, 12)
      }
    }.disabled(store.taskActions.contains(task.id))
  }
  private func act(_ action: TaskSurfacingAction) {
    Task { if let updated = await store.surfaceTask(task.id, action: action) { changed(updated) } }
  }
}
