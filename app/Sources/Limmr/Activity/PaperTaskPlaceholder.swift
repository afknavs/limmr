import LimmrKit
import SwiftUI

/// Cover for a task that has no picture: who the task is with, on paper, with the circle's symbol behind.
/// Nothing is invented: faces come from the task's people (or its group chats), the symbol from its circle.
struct PaperTaskPlaceholder: View {
  @Bindable var store: AppStore
  let task: TaskSummary

  static func isDefault(_ task: TaskSummary) -> Bool {
    if let candidate = task.coverCandidate { return candidate.itemId?.hasPrefix("default:") == true }
    return task.cover == nil || task.cover?.id.hasPrefix("default:") == true
  }

  private var date: Date? { task.progress.total == 0 ? task.nextCheckInAt.flatMap(Timestamp.date) : nil }
  private var circle: LimmrKit.Circle? { store.circles.first { task.circleIds.contains($0.id) && $0.kind != "self" } }
  private var glyph: String { circle?.displaySymbol ?? (date != nil ? "calendar" : "checklist") }

  var body: some View {
    GeometryReader { geometry in
      let width = geometry.size.width, height = geometry.size.height
      if width < 96 || height < 72 {
        // Row thumbnails: one calm glyph, no type shrunk to noise.
        ZStack {
          LimmrTheme.surface2
          Image(systemName: glyph).font(.system(size: max(9, min(15, height * 0.44)))).foregroundStyle(LimmrTheme.ink3)
        }
      } else {
        let face = min(60, max(34, height * 0.4))
        let empty = task.faces.isEmpty && task.progress.total == 0
        ZStack(alignment: .bottomLeading) {
          LimmrTheme.surface2
          // With nobody to show, the symbol carries the cover a little more.
          Image(systemName: glyph).font(.system(size: height * 0.7, weight: .light))
            .foregroundStyle(LimmrTheme.ink.opacity(empty ? 0.075 : 0.05))
            .frame(width: height, height: height).position(x: width - height * 0.4, y: height * 0.57)
          Group {
            if !task.faces.isEmpty { faces(face) }
            else if task.progress.total > 0 { PaperNumber(value: "\(task.progress.done)/\(task.progress.total)", label: "steps done") }
          }.padding(.horizontal, 14).padding(.bottom, 12)
        }
      }
    }.accessibilityHidden(true)
  }

  private func faces(_ size: CGFloat) -> some View {
    HStack(spacing: -size * 0.22) {
        ForEach(Array(task.faces.prefix(3).enumerated()), id: \.element.id) { index, person in
          PersonAvatar(store: store, id: person.id, name: person.name.isEmpty ? store.personName(person.id) : person.name, size: size, fill: LimmrTheme.surface)
            .padding(2.5).background(LimmrTheme.surface2, in: SwiftUI.Circle())
            .zIndex(Double(4 - index))
        }
        if task.faceCount > 3 {
          Text("+\(task.faceCount - 3)").font(LimmrTheme.font(size * 0.3, .semibold)).foregroundStyle(LimmrTheme.ink2)
            .frame(width: size, height: size).background(LimmrTheme.surface, in: SwiftUI.Circle())
            .padding(2.5).background(LimmrTheme.surface2, in: SwiftUI.Circle())
        }
    }.padding(-2.5)
  }
}
