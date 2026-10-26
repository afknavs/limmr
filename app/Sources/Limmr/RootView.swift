import LimmrKit
import SwiftUI

struct RootView: View {
  @Bindable var store: AppStore
  @Environment(\.colorScheme) private var colorScheme
  private var sidebarInk: Color { colorScheme == .dark ? Color(white: 0.92) : Color(white: 0.10) }
  @Environment(\.openSettings) private var openSettings

  var body: some View {
    GeometryReader { window in
      Group {
        if (!store.showSetup && !store.snapshot && !store.demo && [.starting, .restarting, .updating, .failed].contains(store.system.startup.phase))
          || (store.snapshot && ProcessInfo.processInfo.arguments.contains("--helper-restarting")) {
          EngineStartupView(phase: store.snapshot ? .restarting : store.system.startup.phase,
                            failureMessage: store.system.startup.failureMessage) {
            Task {
              await store.system.startup.recover(using: store.system, allowInstall: true)
              await store.refresh()
              if store.status?.linked == false { store.showSetup = true }
            }
          }
        } else if store.showSetup {
          SetupView(store: store, initialStep: store.setupStep)
        } else {
          NavigationSplitView {
            VStack(alignment: .leading, spacing: 16) {
              Text("Limmr").foregroundStyle(sidebarInk).font(.headline).padding(.horizontal, 12).padding(.top, 16)
              ForEach(Page.allCases) { page in
                Button {
                  store.selectPage(page)
                } label: {
                  HStack(spacing: 12) {
                    Image(systemName: page.symbol).frame(width: 20)
                    Text(page.rawValue)
                    Spacer(minLength: 0)
                    if page == .overview, !store.home.queued.isEmpty {
                      Text("\(store.home.queued.count)").font(.caption.weight(.semibold))
                        .foregroundStyle(Color.accentColor)
                    }
                  }
                  .foregroundStyle(sidebarInk)
                  .padding(.horizontal, 12).padding(.vertical, 10)
                  .background(
                    store.page == page ? Color.accentColor.opacity(0.09) : .clear,
                    in: RoundedRectangle(cornerRadius: 10)
                  )
                  .contentShape(Rectangle())
                }
                .limmrButton(.row(radius: 10), id: "sidebar")
                .accessibilityLabel(page.rawValue)
                .accessibilityValue(Spoken.sidebarValue(page, selected: store.page, queued: store.home.queued.count))
                .accessibilityHint("Open \(page.rawValue)")
                .accessibilityAddTraits(store.page == page ? .isSelected : [])
              }
              Spacer()
            }
            .padding(.horizontal, 12)
            .toolbar(removing: .title)
            .navigationSplitViewColumnWidth(min: 165, ideal: 180, max: 220)
            .safeAreaInset(edge: .bottom) {
              VStack(alignment: .leading, spacing: 0) {
                Button { openSettings() } label: {
                  Label("Settings", systemImage: "gearshape").foregroundStyle(sidebarInk)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 12).padding(.vertical, 10)
                    .contentShape(Rectangle())
                }
                .limmrButton(.row(radius: 10), id: "sidebar")
                .padding(.horizontal, 12)
                SidebarPresence(store: store)
              }
            }
          } detail: {
            VStack(alignment: .leading, spacing: 0) {
              if store.engineSurfaceState != .connected {
                EngineStatusBanner(state: store.engineSurfaceState) {
                  Task { await store.connectionAction(store.engineSurfaceState) }
                }
                .padding(.horizontal, 24).padding(.top, 24)
              } else if store.page != .overview, let message = store.refreshError {
                HStack {
                  Text(message).font(.callout).foregroundStyle(AccessibleSecondary())
                  Button("Try again") { Task { await store.reconnect() } }
                }.padding(.horizontal, 24).padding(.top, 24)
              }
              Group {
                switch store.page {
                case .overview: HomeView(store: store)
                case .circles: CirclesView(store: store)
                case .activity: ActivityView(store: store)
                }
              }
              .id(store.page)
            }
            .background { AppBackground().ignoresSafeArea(edges: .top) }
          }
          .navigationSplitViewStyle(.balanced)
        }
      }
      .environment(\.limmrWindowWidth, window.size.width)
    }
    .pageSurface()
    .background { MainWindowSurfaceReader() }
    .task(id: store.page) { if store.status != nil { await store.refresh() } }
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
