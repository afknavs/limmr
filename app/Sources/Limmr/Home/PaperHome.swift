import LimmrKit
import SwiftUI

struct PaperHomeContents: View {
  @Bindable var store: AppStore
  @Environment(\.limmrWindowWidth) private var windowWidth
  @State private var query = ""
  @State private var hits: [HomeSearchHit] = []
  @State private var searchError: String?
  private var tasks: [TaskSummary] { TaskPresentation.open(store.home.tasks) }
  private var asks: [TaskSummary] { tasks.filter { $0.needsYou || store.taskLaterNotices[$0.id] != nil } }
  private var working: [TaskSummary] { tasks.filter { !$0.needsYou } }
  private var scheduled: [TaskSummary] { tasks.filter { $0.nextCheckInAt != nil } }
  private var circles: [LimmrKit.Circle] { store.circles.filter { $0.kind != "self" } }
  var body: some View {
    let width = PaperLayout.contentWidth(windowWidth), rail = PaperLayout.homeRail(windowWidth)
    let main = rail > 0 ? width - rail - 24 : width
    VStack(alignment: .leading, spacing: 20) {
      header
      if !query.isEmpty {
        VStack(alignment: .leading, spacing: 12) {
          if let searchError { Text(searchError) }
          ForEach(hits) { hit in Button { store.openHomeSearchHit(hit) } label: { Label(hit.title, systemImage: hit.symbol) }.buttonStyle(.plain) }
          if hits.isEmpty && searchError == nil { Text("No matches").foregroundStyle(LimmrTheme.ink2) }
        }.padding(16).frame(maxWidth: .infinity, alignment: .leading).n1Card()
      }
      if rail > 0 {
        HStack(alignment: .top, spacing: 24) {
          VStack(alignment: .leading, spacing: 24) { mainSections(main) }.frame(width: main, alignment: .leading)
          VStack(alignment: .leading, spacing: 20) { today; week(rail); recent }.frame(width: rail, alignment: .leading)
        }
      } else {
        VStack(alignment: .leading, spacing: 24) {
          mainSections(main)
          // Narrow windows: the two small widgets share a row, each filling its half.
          HStack(alignment: .top, spacing: 12) {
            if !scheduled.isEmpty { today.frame(width: (main - 12) / 2, alignment: .leading) }
            week(scheduled.isEmpty ? main : (main - 12) / 2)
          }
          recent
        }
      }
    }.font(LimmrTheme.TypeScale.body).foregroundStyle(LimmrTheme.ink)
      .task(id: "\(store.backendGeneration):\(query)") {
        hits = []; searchError = nil
        guard !query.isEmpty else { return }
        do { try await Task.sleep(for: .milliseconds(250)); let found = try await HomeSearch.find(query, backend: store.backend); if !Task.isCancelled { hits = found } }
        catch { if !Task.isCancelled { searchError = "Search could not be loaded. Try again." } }
      }
  }
  private var header: some View {
    PaperPageHeader(title: "Home", subtitle: store.presentationNow.formatted(.dateTime.weekday(.wide).day().month(.wide))) {
      PaperSearch(store: store, query: $query, prompt: "Search people, circles and tasks").frame(width: 300)
    }
  }
  private func title(_ text: String, link: String? = nil, action: @escaping () -> Void = {}) -> some View {
    HStack(alignment: .firstTextBaseline) {
      Text(text).font(LimmrTheme.TypeScale.section)
      Spacer(minLength: 8)
      if let link { Button(link, action: action).buttonStyle(.plain).font(LimmrTheme.font(12, .semibold)).foregroundStyle(LimmrTheme.ink2) }
    }
  }
  @ViewBuilder private func mainSections(_ width: Double) -> some View {
    if !asks.isEmpty {
      VStack(alignment: .leading, spacing: 10) {
        HStack(spacing: 8) { PaperMarker(); Text("Needs you").font(LimmrTheme.TypeScale.section); Text("\(asks.filter { $0.needsYou && store.taskLaterNotices[$0.id] == nil }.count)").foregroundStyle(LimmrTheme.ink2) }
        PaperGrid(items: asks, width: width, minimum: 300, maximum: 620) { task in
          if let until = store.taskLaterNotices[task.id] { TaskLaterNotice(store: store, id: task.id, until: until) }
          else { PaperAskCard(store: store, task: task) }
        }
      }
    }
    if !working.isEmpty {
      VStack(alignment: .leading, spacing: 10) {
        title("Happening now", link: "All tasks") { store.selectPage(.tasks) }
        // Whole rows only (the rest is behind "All tasks"): one row, or two when fewer than four cards fit.
        let columns = PaperLayout.fit(width: width, minimum: 190, maximum: 300, items: working.count).count
        let shown = working.count <= columns ? working.count : columns * min(columns < 4 ? 2 : 1, working.count / columns)
        PaperGrid(items: Array(working.prefix(shown)), width: width, minimum: 190, maximum: 300) { task in
          PaperTaskCard(store: store, task: task, compact: true)
        }
      }
    }
    if !circles.isEmpty {
      VStack(alignment: .leading, spacing: 10) {
        title("Circles", link: "People map") { store.selectPage(.circles) }
        PaperGrid(items: Array(circles.prefix(8)), width: width, minimum: 190, maximum: 320) { circle in circleCard(circle) }
      }
    }
  }
  private func circleCard(_ circle: LimmrKit.Circle) -> some View {
    Button { store.openCircle(circle.id) } label: {
      VStack(alignment: .leading, spacing: 6) {
        Label(circle.name, systemImage: circle.displaySymbol).font(LimmrTheme.font(14, .semibold)).lineLimit(1)
        Text(store.circleDetails[circle.id]?.brief?.openAsksForYou.first ?? circle.description).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2).lineLimit(2, reservesSpace: true).frame(maxWidth: .infinity, alignment: .leading)
        Spacer(minLength: 4)
        HStack(spacing: 8) { PaperAvatarStack(store: store, ids: circle.people.prefix(3).map(\.personId)); Text("\(circle.people.count) people").font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3).lineLimit(1) }
      }.padding(14).frame(maxWidth: .infinity, alignment: .leading).frame(height: 112).n1Card().contentShape(Rectangle())
    }.buttonStyle(.plain).contextMenu { CircleIdentityMenu(store: store, circle: circle) }
  }
  @ViewBuilder private var today: some View {
    if !scheduled.isEmpty {
      VStack(alignment: .leading, spacing: 10) {
        title("Coming up")
        VStack(alignment: .leading, spacing: 0) {
          ForEach(scheduled.prefix(3)) { task in
            Button { store.openTask(task.id) } label: {
              HStack(alignment: .firstTextBaseline, spacing: 12) {
                if let at = task.nextCheckInAt.flatMap(Timestamp.date) {
                  VStack(alignment: .leading, spacing: 2) {
                    Text(at.formatted(.dateTime.hour(.twoDigits(amPM: .omitted)).minute(.twoDigits).locale(Locale(identifier: "en_GB")))).font(LimmrTheme.font(17, .semibold)).tracking(-0.3)
                    Text(at.formatted(.dateTime.weekday(.abbreviated))).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3)
                  }.frame(width: 52, alignment: .leading)
                }
                VStack(alignment: .leading, spacing: 2) {
                  Text(task.title).font(LimmrTheme.font(14, .semibold)).lineLimit(1)
                  Text(N1TaskStatus.text(task)).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2).lineLimit(1)
                }
                Spacer(minLength: 0)
              }.padding(.vertical, 12).contentShape(Rectangle())
            }.buttonStyle(.plain)
            if task.id != scheduled.prefix(3).last?.id { Rectangle().fill(LimmrTheme.hairline).frame(height: 1) }
          }
        }.padding(.horizontal, 14).frame(maxWidth: .infinity, alignment: .leading).n1Card().accessibilityLabel("Today and upcoming reminders")
      }
    }
  }
  @ViewBuilder private func week(_ width: Double) -> some View {
    if let history = store.history {
      VStack(alignment: .leading, spacing: 10) {
        title("This week", link: "Activity") { store.selectPage(.activity) }
        PaperWeekActivity(history: history, width: width, openDay: store.openActivityDay)
      }.frame(width: width, alignment: .leading)
    }
  }
  @ViewBuilder private var recent: some View {
    if !store.home.recentLines.isEmpty {
      VStack(alignment: .leading, spacing: 10) {
        title("Recently")
        VStack(alignment: .leading, spacing: 0) {
          ForEach(store.home.recentLines.prefix(4)) { line in
            Button {
              if line.kind == "circle" { store.openCircle(line.targetId) }
              else if line.kind == "task" { store.openTask(line.targetId) }
              else { store.selectPage(.activity) }
            } label: {
              HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(line.text).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink).lineLimit(1)
                Spacer(minLength: 0)
                if let at = line.at { Text(store.relativeTime(at)).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3).lineLimit(1).fixedSize() }
              }.padding(.vertical, 9).contentShape(Rectangle())
            }.buttonStyle(.plain)
            if line.id != store.home.recentLines.prefix(4).last?.id { Rectangle().fill(LimmrTheme.hairline).frame(height: 1) }
          }
        }.padding(.horizontal, 14).padding(.vertical, 3).frame(maxWidth: .infinity, alignment: .leading).n1Card()
      }
    }
  }
}

struct PaperAskCard: View {
  @Bindable var store: AppStore
  let task: TaskSummary
  @State private var person: EffortEntity?
  var body: some View {
    VStack(alignment: .leading, spacing: 8) {
      HStack(spacing: 8) {
        if let person { PersonAvatar(store: store, id: person.id, name: person.name, size: 22) }
        Text(person?.name ?? task.title).font(LimmrTheme.font(12, .semibold)).lineLimit(2)
        Text("· " + store.taskCircleName(task)).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2).lineLimit(1)
        Spacer(minLength: 0)
        if let at = task.lastUpdateAt ?? task.lastActiveAt { Text(store.relativeTime(at)).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3) }
      }
      Text(task.needsYouReason ?? task.title).font(LimmrTheme.font(17, .semibold)).lineLimit(2).fixedSize(horizontal: false, vertical: true).frame(maxWidth: .infinity, alignment: .leading)
      Spacer(minLength: 0)
      PaperFlow(spacing: 8) {
        Button { store.openTask(task.id) } label: { PaperChip(text: task.title) }.buttonStyle(.plain)
        TaskLaterButton(store: store, id: task.id)
        Button("Review") { store.openTask(task.id) }.buttonStyle(PaperButtonStyle(primary: true))
      }
      if let error = store.taskActionErrors[task.id] { Text(error).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink2) }
    }.padding(.horizontal, 16).padding(.top, 16).padding(.bottom, 12).frame(minHeight: 154).n1Card()
      .task(id: task.id) { let detail: TrackedTask? = try? await store.backend.get("/v1/tasks/" + task.id); person = detail?.people.first }
  }
}
struct PaperWeekActivity: View {
  let history: StatsHistory
  var width: Double
  var openDay: (String) -> Void
  /// Whole weeks that fit the card at a comfortable cell size, never more than there is history for;
  /// the caption states the number drawn and the cells stretch to fill the card.
  static func weeks(inner: Double, days: Int = .max) -> Int {
    max(1, min(max(8, min(26, Int((inner + 3) / 17))), days == .max ? .max : (days + 6) / 7))
  }
  var body: some View {
    let inner = max(0, width - 28), weeks = Self.weeks(inner: inner, days: history.days.count)
    let days = history.period(7), cells = history.period(weeks * 7)
    let maximum = max(1, cells.map(\.messages).max() ?? 1)
    let side = (inner - 3 * Double(weeks - 1)) / Double(weeks)
    VStack(alignment: .leading, spacing: 14) {
      HStack(alignment: .top, spacing: 24) {
        PaperNumber(value: days.reduce(0) { $0 + $1.messages }.formatted(), label: "messages")
        PaperNumber(value: days.reduce(0) { $0 + $1.agentActivity }.formatted(), label: "AI actions")
        Spacer(minLength: 0)
      }
      VStack(alignment: .leading, spacing: 6) {
        HStack(spacing: 3) {
          ForEach(0..<weeks, id: \.self) { column in
            VStack(spacing: 3) {
              ForEach(0..<7, id: \.self) { row in
                let index = column * 7 + row
                if cells.indices.contains(index) {
                  let day = cells[index]
                  Button { openDay(day.date) } label: {
                    RoundedRectangle(cornerRadius: 3).fill(LimmrTheme.ink.opacity(day.messages == 0 ? 0.07 : max(0.14, 0.82 * Double(day.messages) / Double(maximum)))).frame(width: side, height: min(side, 14))
                  }.buttonStyle(.plain).accessibilityLabel("\(day.date), \(day.messages) messages, \(day.agentActivity) AI actions")
                } else { Color.clear.frame(width: side, height: min(side, 14)).accessibilityHidden(true) }
              }
            }
          }
        }
        Text("Messages a day · last \(weeks) weeks").font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3)
      }
    }.padding(14).frame(width: width, alignment: .leading).n1Card()
  }
}
