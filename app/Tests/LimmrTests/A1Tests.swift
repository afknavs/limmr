import AppKit
import SwiftUI
import Testing

@testable import Limmr
@testable import LimmrKit

@Suite("A1 native UI audit", .serialized)
struct A1Tests {
  @MainActor @Test func standaloneQuietCircleStaysShort() async throws {
    _ = NSApplication.shared
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    let circle = try #require(store.circles.first { $0.id == "atlas" })
    let quiet = HomeCircleTile(circle: circle, pinned: true, reason: nil, brief: nil, newMessages: 0, rectangles: [])
    for width in [300.0, 680.0] {
      let host = NSHostingView(rootView: HomeCircleTileView(store: store, tile: quiet).frame(width: width))
      #expect(host.fittingSize.height < 90)
      #expect(host.fittingSize.height > 40)
    }
  }
}
