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
        .limmrButton(id: "see-all").font(AppType.caption).foregroundStyle(AccessibleSecondary())
      }
      if includesCaptions && HomePresentation.hasChanges(store.home) {
        Button {
          store.activityAgent = "all"; store.activityFilter = "all"
          store.selectPage(.activity)
        } label: {
          VStack(alignment: .leading, spacing: 8) {
            Text(HomePresentation.changesLine(store.home, now: store.demo ? Timestamp.date(DemoData.now)! : Date()))
              .font(.callout).fixedSize(horizontal: false, vertical: true)
            Text("Review in Activity").font(AppType.caption).foregroundStyle(AccessibleSecondary())
          }.frame(maxWidth: .infinity, alignment: .leading).contentShape(Rectangle())
        }.limmrButton(.row(radius: 8, horizontal: 8, vertical: 6), id: "recent-changes")
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
          }.frame(maxWidth: .infinity, alignment: .leading).contentShape(Rectangle())
        }.limmrButton(.row(radius: 8, horizontal: 8, vertical: 6), id: "recent")
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
            .limmrButton().font(AppType.caption).foregroundStyle(AccessibleSecondary())
        }
      }
    }.padding(16).background(.primary.opacity(0.025), in: RoundedRectangle(cornerRadius: 8))
  }
}
