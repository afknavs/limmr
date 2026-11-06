import LimmrKit
import SwiftUI

struct OverviewRow: Identifiable {
  let id: String
  let text: String
  let target: String
  let kind: String
  var needsYou = false
  var context = ""
  var by: String? = nil
}
extension AppStore {
  var overviewRows: [OverviewRow] {
    var rows = pending.map { OverviewRow(id: "approval-" + $0.id, text: "Review a draft from " + agentName($0.agentId), target: $0.id, kind: "approval", needsYou: true, context: chatName($0.chatJid)) }
    rows += home.queued.map { OverviewRow(id: "queued-" + $0.id, text: "A draft is waiting for your review", target: $0.id, kind: "queued", needsYou: true, context: circleName($0.circleId)) }
    rows += TaskPresentation.open(home.tasks).filter(\.needsYou).map { OverviewRow(id: "task-" + $0.id, text: $0.title + " · " + ($0.needsYouReason ?? "Ready for your review"), target: $0.id, kind: "task", needsYou: true, context: taskCircleName($0), by: $0.createdBy.name) }
    let needs = Set(rows.filter { $0.kind == "task" }.map(\.target))
    rows += home.recentLines.filter { !needs.contains($0.targetId) }.map { OverviewRow(id: $0.id, text: $0.text, target: $0.targetId, kind: $0.kind, context: $0.at.map(relativeTime) ?? "", by: $0.by) }
    // Older engines keep the existing string list. Resolve only unambiguous task titles;
    // count-only lines have no destination and are deliberately not offered as buttons.
    if home.recentLines.isEmpty {
      for text in home.recently {
        let matches = home.tasks.filter { text.contains("· " + $0.title + ":") && !needs.contains($0.id) }
        if matches.count == 1, let task = matches.first {
          rows.append(.init(id: "recent-" + task.id, text: text, target: task.id, kind: "task", context: taskCircleName(task), by: task.createdBy.name))
        }
      }
    }
    var seen = Set<String>()
    return rows.filter { seen.insert($0.id).inserted }
  }
  func taskCircles(_ task: TaskSummary) -> [LimmrKit.Circle] {
    task.circleIds.compactMap { id in circles.first { $0.id == id } ?? circleDetails[id]?.circle }
  }
  func taskCircleName(_ task: TaskSummary) -> String {
    guard !task.circleIds.isEmpty else { return "No circle" }
    return task.circleIds.map { id in circles.first { $0.id == id }?.name ?? circleDetails[id]?.circle.name ?? "Circle" }.joined(separator: " · ")
  }
}
/// Plain, actionable rows. Empty sections disappear instead of reserving space.
struct N1Overview: View {
  @Bindable var store: AppStore
  var open: (OverviewRow) -> Void
  var body: some View {
    let rows = store.overviewRows
    VStack(alignment: .leading, spacing: 20) {
      section("Recently", rows: Array(rows.filter { !$0.needsYou }.prefix(3)))
      section("Needs you", rows: rows.filter(\.needsYou))
    }
  }
  @ViewBuilder private func section(_ title: String, rows: [OverviewRow]) -> some View {
    if !rows.isEmpty {
      VStack(alignment: .leading, spacing: 8) {
        Text(title).font(.title3.weight(.semibold))
        ForEach(rows) { row in
          Button { open(row) } label: {
            HStack(spacing: 8) {
              if row.needsYou { Circle().fill(.orange).frame(width: 6, height: 6) }
              VStack(alignment: .leading, spacing: 2) {
                Text(row.text).font(.body).lineLimit(2)
                if let by = row.by, !by.isEmpty, by != "You" {
                  Text("by " + by + (row.context.isEmpty ? "" : " · " + row.context)).font(.caption).foregroundStyle(.secondary)
                } else if !row.context.isEmpty {
                  Text(row.context).font(.caption).foregroundStyle(.secondary)
                }
              }.frame(maxWidth: .infinity, alignment: .leading)
              Image(systemName: "chevron.right").imageScale(.small).foregroundStyle(.secondary)
            }.contentShape(Rectangle())
          }.quietCard()
            .modifier(QuietRow())
            .contextMenu { Button("Open") { open(row) } }
            .accessibilityLabel(title + ". " + row.text)
        }
      }
    }
  }
}
