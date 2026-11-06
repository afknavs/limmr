import LimmrKit
import SwiftUI

struct RecentWidget: View {
  @Bindable var store: AppStore
  var includesCaptions = false
  @Environment(\.openSettings) private var openSettings
  private var tasks: [ActivityTask] {
    Array(
      ActivityPresentation.tasks(
        store.allRecentActivity + store.home.recentActivity + (store.dashboard?.recentSends ?? [])
      ).prefix(4))
  }
  var body: some View {
    VStack(alignment: .leading, spacing: 16) {
      HStack {
        BlockHeading(title: "Recent")
        Button("See all") {
          store.activityFilter = "all"
          store.selectPage(.activity)
        }
        .buttonStyle(.bordered).font(AppType.caption).foregroundStyle(AccessibleSecondary())
      }
      if includesCaptions && HomePresentation.hasChanges(store.home) {
        Button {
          store.activityAgent = "all"; store.activityFilter = "all"
          store.selectPage(.activity)
        } label: {
          VStack(alignment: .leading, spacing: 8) {
            Text(HomePresentation.changesLine(store.home, now: store.presentationNow))
              .font(.callout).fixedSize(horizontal: false, vertical: true)
            Text("Review in Activity").font(AppType.caption).foregroundStyle(AccessibleSecondary())
          }.padding(8).frame(maxWidth: .infinity, alignment: .leading)
            .background(.primary.opacity(0.035), in: RoundedRectangle(cornerRadius: 8)).contentShape(Rectangle())
        }.quietCard()
        Divider()
      }
      ForEach(tasks) { task in
        Button {
          for row in task.steps where !store.activity.contains(where: { $0.id == row.id }) {
            store.activity.append(row)
          }
          store.selectedTask = task.id
          store.activityFilter = "all"
          store.selectPage(.activity)
        } label: {
          VStack(alignment: .leading, spacing: 8) {
            Text(task.title(agent: store.agentName(task.agentId), chatName: store.chatName)).font(
              .callout
            ).lineLimit(3)
            Text("\(task.status) · \(store.relativeTime(task.end)) ago").font(AppType.caption)
              .foregroundStyle(AccessibleSecondary())
          }.padding(8).frame(maxWidth: .infinity, alignment: .leading)
            .background(.primary.opacity(0.035), in: RoundedRectangle(cornerRadius: 8)).contentShape(Rectangle())
        }.quietCard()
        if task.id != tasks.last?.id { Divider() }
      }
      if tasks.isEmpty && store.isLoading("/v1/activity") {
        QuietPlaceholder(height: 76)
      } else if tasks.isEmpty && store.hasLoaded("/v1/activity") {
        Text("No recent work to show.").font(.callout).foregroundStyle(AccessibleSecondary())
      }
      if includesCaptions && store.hasLoaded("/v1/home") {
        Divider()
        VStack(alignment: .leading, spacing: 8) {
          Text(store.home.integrations.isEmpty ? "No integrations connected" : store.home.integrations.map(\.name).formatted(.list(type: .and)) + " connected")
            .font(AppType.caption).foregroundStyle(AccessibleSecondary())
          Button("Manage") { store.settingsTab = .integrations; openSettings() }
            .buttonStyle(.bordered).font(AppType.caption).foregroundStyle(AccessibleSecondary())
        }
      }
    }.padding(16).background(.primary.opacity(0.025), in: RoundedRectangle(cornerRadius: 8))
  }
}
