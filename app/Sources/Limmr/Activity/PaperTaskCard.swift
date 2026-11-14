import LimmrKit
import SwiftUI

struct PaperTaskArtwork: View {
  @Bindable var store: AppStore
  let task: TaskSummary
  var body: some View {
    GeometryReader { geometry in
      ZStack {
        LimmrTheme.surface2
        if !PaperTaskPlaceholder.isDefault(task), let cached = store.taskCovers.cached(task, generation: store.backendGeneration) {
          let document = ["pdf", "document"].contains(task.cover?.mediaType ?? "") || task.cover?.title.lowercased().hasSuffix(".pdf") == true
          Image(decorative: cached.raster.image, scale: 2).resizable()
            .aspectRatio(contentMode: document ? .fit : .fill)
            .padding(document ? min(14, geometry.size.height * 0.08) : 0).frame(width: geometry.size.width, height: geometry.size.height).clipped()
        } else {
          PaperTaskPlaceholder(store: store, task: task)
        }
      }
    }.task(id: TaskCoverCache.identity(task)) {
      guard !PaperTaskPlaceholder.isDefault(task) else { return }
      await store.taskCovers.load(task, backend: store.backend, generation: store.backendGeneration)
    }
      .accessibilityHidden(true)
  }
}
struct PaperTaskCard: View {
  @Bindable var store: AppStore
  let task: TaskSummary
  var compact = false
  @State private var detail: TrackedTask?
  var body: some View {
    Button { store.openTask(task.id) } label: {
      VStack(alignment: .leading, spacing: 0) {
        PaperTaskArtwork(store: store, task: task).aspectRatio(1.65, contentMode: .fit)
          // The tint is translucent: a paper backing keeps the chip readable over a document or photo cover.
          .overlay(alignment: .topLeading) { status.background(LimmrTheme.surface, in: Capsule()).padding(8) }
        VStack(alignment: .leading, spacing: 5) {
          Text(task.title).font(LimmrTheme.font(14, .semibold)).lineLimit(2, reservesSpace: true)
          Text(store.taskCircleName(task)).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2).lineLimit(1)
          Text(detail?.summary.text.isEmpty == false ? detail!.summary.text : N1TaskStatus.text(task)).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2).lineLimit(2)
          Spacer(minLength: 8)
          if task.progress.total > 0 {
            HStack(spacing: 8) {
              PaperSteps(progress: task.progress, working: working != nil)
              if !compact { Text("\(task.progress.done)/\(task.progress.total)").font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3) }
            }
          } else if let at = task.lastActiveAt ?? task.lastUpdateAt {
            // Most real tasks have no checklist: say when it last moved instead of an empty "0/0".
            Text("Active " + store.relativeTime(at)).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3).lineLimit(1)
          }
        }.padding(.horizontal, 12).padding(.top, 10).padding(.bottom, 12).frame(height: compact ? 134 : 144)
      }.foregroundStyle(LimmrTheme.ink).n1Card().clipShape(RoundedRectangle(cornerRadius: 16)).contentShape(Rectangle())
    }.quietCard().modifier(PaperTaskContext(store: store, task: task))
      .accessibilityLabel(TaskPresentation.accessibilityLabel(task) + ". " + store.taskCircleName(task))
      .accessibilityHint("Open task workspace")
      .task(id: task.id) { let workspace = store.workspace(task.id); await workspace.load(); detail = workspace.task }
  }
  private var working: String? { store.paperWorkingAgent(task) }
  @ViewBuilder private var status: some View {
    if task.needsYou { PaperChip(text: "Needs you", kind: "need") }
    else if let working { PaperChip(text: working + " working", kind: "working") }
    else if let date = task.nextCheckInAt.flatMap(Timestamp.date) { PaperChip(text: date.formatted(.dateTime.weekday(.abbreviated).day().month(.abbreviated))) }
  }
}
extension AppStore {
  /// Presence alone is not evidence that an AI is working on this particular task.
  func paperWorkingAgent(_ task: TaskSummary) -> String? {
    guard let presence = presence.first(where: { row in
      row.line != nil && row.targetIds.contains(task.id) && Timestamp.date(row.since).map { presentationNow.timeIntervalSince($0) >= 0 && presentationNow.timeIntervalSince($0) < 900 } == true
    }) else { return nil }
    return presence.name
  }
}
