import Testing
@testable import Limmr

@MainActor struct Z2NavigationTests {
  @Test func taskAndEffortBackRestoreSource() {
    for route in [ActivityRoute.task("task-demo"), .effort("effort-demo")] {
      for path in [[CircleDestination.person("maya")], [.group("design")], [.circle("club"), .person("maya")]] {
        let store = AppStore(demo: true, snapshot: true)
        store.selectPage(.circles); store.circlePath = path
        store.openActivityRoute(route)
        store.leaveActivityRoute()
        #expect(store.page == .circles && store.circlePath == path)
      }
      let store = AppStore(demo: true, snapshot: true)
      store.openActivityRoute(route); store.leaveActivityRoute()
      #expect(store.page == .overview && store.activityPath.isEmpty && store.pendingActivityRoute == nil)
      store.selectPage(.activity); store.openActivityRoute(route); store.leaveActivityRoute()
      #expect(store.page == .activity && store.activityPath.isEmpty && store.pendingActivityRoute == nil)
    }
  }
  @Test func personGroupAndCircleBackAlwaysHaveDestination() {
    for destination in [CircleDestination.person("maya"), .group("design"), .circle("club")] {
      let store = AppStore(demo: true, snapshot: true)
      store.selectPage(.circles); store.circlePath = [.circle("root"), destination]
      store.leavePeopleRoute(); #expect(store.circlePath == [.circle("root")])
      store.leavePeopleRoute(); #expect(store.circlePath.isEmpty && store.page == .circles)
      store.leavePeopleRoute(); #expect(store.circlePath.isEmpty && store.page == .circles)
    }
  }
}
