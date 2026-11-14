import LimmrKit
import SwiftUI

struct RootView: View {
  @Bindable var store: AppStore
  @Environment(\.openSettings) private var openSettings

  @ViewBuilder private func routeDestination(_ route: AppRoute) -> some View {
    switch route {
    case .activity(.task(let id)):
      TaskPage(store: store, id: id).navigationTitle((store.allTasks + store.home.tasks).first { $0.id == id }?.title ?? "Task")
    case .activity(.effort(let id)): EffortPage(store: store, id: id)
    case .people(.circle(let id)): CirclePage(store: store, id: id).navigationTitle(store.circleName(id))
    case .people(.person(let id)): CirclePersonPage(store: store, id: id)
    case .people(.group(let jid)): GroupPage(store: store, jid: jid)
    case .people(.chat(let id, let name)):
      if id.hasSuffix("@g.us") { GroupPage(store: store, jid: id) }
      else { HomeChatPage(store: store, id: id, name: name) }
    case .people(.brief(let id, let version)): BriefVersionPage(store: store, circleID: id, version: version)
    }
  }

  private var sidebarSelection: Binding<String?> {
    Binding(get: {
      store.sidebarDestination
    }, set: { value in
      // The sidebar echoes highlight changes made by navigation (for example opening a
      // circle from People); treating that echo as a click would reset the route.
      guard let value, value != store.sidebarDestination else { return }
      store.selectSidebar(value)
    })
  }

  var body: some View {
    GeometryReader { window in
      Group {
        if store.showSetup {
          SetupView(store: store, initialStep: store.setupStep)
        } else {
          HStack(spacing: 0) {
            PaperSidebar(store: store).frame(width: PaperLayout.sidebarWidth(window.size.width))

            NavigationStack(path: store.navigationPath) {
            VStack(alignment: .leading, spacing: 0) {
              ConnectionStatusCard(store: store)
                .padding(.horizontal, 24).padding(.top, store.connectionAttention == nil ? 0 : 16)
              ArchivedCircleUndo(store: store).padding(.horizontal, 24)
              FileSendReviewBanner(store: store).padding(.horizontal, 24)
              Group {
                switch store.page {
                case .overview: HomeView(store: store)
                case .tasks: AllTasksView(store: store)
                case .circles: CirclesView(store: store)
                case .activity: ActivityView(store: store)
                }
              }
              .id(store.page)
            }
            .background { LimmrTheme.panel.ignoresSafeArea(edges: .top) }
            .navigationDestination(for: AppRoute.self) { route in
              routeDestination(route).n1PageSurface()
            }
            }
            .background(LimmrTheme.panel).clipShape(RoundedRectangle(cornerRadius: 24))
            .padding([.top, .trailing, .bottom], 8)
            .task(id: store.pendingSidebarCircle) { await store.sidebarCircleDidAppear() }
            .task(id: store.pendingActivityRoute) { await store.activityDidAppear() }
          }
          .background(LimmrTheme.canvas)
        }
      }
      .environment(\.limmrWindowWidth, window.size.width)
      .environment(\.limmrWindowHeight, window.size.height)
      .overlay(alignment: .topLeading) { MainWindowContentBoundary().frame(width: 0, height: 0) }
    }
    .font(LimmrTheme.TypeScale.body).foregroundStyle(LimmrTheme.ink).tint(LimmrTheme.ink)
    .background(LimmrTheme.canvas)
    .background { MainWindowSurfaceReader() }
    .mainWindowChrome()
    .task {
      guard !store.snapshot && !store.demo else { return }
      while !Task.isCancelled {
        do { try await Task.sleep(for: .seconds(5)) } catch { return }
        // A small independent probe notices a failed engine even when the
        // activity stream has stopped, without refreshing every page.
        let readiness = await store.system.healthReadiness()
        switch readiness {
        case .ready:
          if store.connectionAttention != nil {
            await store.system.startup.recover(using: store.system)
            await store.refresh()
          }
        case .busy: store.loadFailures["/v1/status"] = "Connection timed out"
        case .unavailable: store.loadFailures["/v1/status"] = "Connection unavailable"
        case .starting: break
        }
      }
    }
    .sheet(item: store.setupConnectKind) { kind in ConnectView(store: store, kind: kind).dismissOnEscape() }
    .onExitCommand { store.route(.escape) }
    .onOpenURL { store.navigate($0) }
    .onChange(of: store.showSettings, initial: true) { _, value in
      if value {
        openSettings()
        store.showSettings = false
      }
    }
  }
}
