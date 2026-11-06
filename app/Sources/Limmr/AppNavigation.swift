import SwiftUI
import LimmrKit

// One navigation authority and one element type for the lifetime of the detail
// column. Swapping bound stacks reused SwiftUI's column state across incompatible
// path element types, including when a profile's summary refreshed.
enum AppRoute: Hashable {
  case people(CircleDestination)
  case activity(ActivityRoute)
}
extension AppStore {
  var navigationPath: Binding<[AppRoute]> {
    let section = page
    return Binding(get: {
      switch self.page {
      case .overview: []
      case .circles: self.circlePath.map(AppRoute.people)
      case .activity, .tasks: self.activityPath.map(AppRoute.activity)
      }
    }, set: { routes in
      guard self.page == section else { return }
      // A deferred back transaction from a previous section must never overwrite
      // the current section's path.
      switch self.page {
      case .overview: break
      case .circles:
        guard routes.allSatisfy({ if case .people = $0 { true } else { false } }) else { return }
        self.circlePath = routes.compactMap { if case .people(let r) = $0 { r } else { nil } }
      case .activity, .tasks:
        guard routes.allSatisfy({ if case .activity = $0 { true } else { false } }) else { return }
        self.activityPath = routes.compactMap { if case .activity(let r) = $0 { r } else { nil } }
      }
    })
  }
}

// Sidebar selection is explicit intent, independent of the stack's initial
// empty-path transaction when its root section changes.
extension AppStore {
  func selectSidebar(_ row: String) {
    if row.hasPrefix("circle:") {
      let id = String(row.dropFirst(7))
      guard circles.contains(where: { $0.id == id }) else { return }
      selectPage(.circles)
      pendingSidebarCircle = id
    } else if row.hasPrefix("page:"), let destination = Page(rawValue: String(row.dropFirst(5))) {
      pendingSidebarCircle = nil
      sidebarOpenedCircle = nil
      selectPage(destination)
    } else if row.hasPrefix("ai:") {
      let id = String(row.dropFirst(3))
      guard AIIdentity.rows(agents, sync: connectionSync).contains(where: { $0.id == id }) else { return }
      reviewIntegration(id)
    }
  }
  func sidebarCircleDidAppear() async {
    await Task.yield()
    guard !Task.isCancelled, page == .circles, let id = pendingSidebarCircle else { return }
    pendingSidebarCircle = nil
    openCircle(id)
    sidebarOpenedCircle = id
  }
}
