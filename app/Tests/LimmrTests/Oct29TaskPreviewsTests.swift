import AppKit
import Foundation
import LimmrKit
import SwiftUI
import Testing
@testable import Limmr

@Suite("Oct29 task previews", .serialized) @MainActor struct Oct29TaskPreviewsTests {
  @Test func navigationUsesOneTypeAcrossProfileRefreshTaskBackAndSidebar() async {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    for _ in 0..<25 {
      store.selectPage(.circles); store.circlePath = [.person("person-04")]
      #expect(store.navigationPath.wrappedValue == [.people(.person("person-04"))])
      let before = store.navigationPath.wrappedValue
      await store.refresh() // Automatic summary/data refresh must preserve the profile route.
      #expect(store.navigationPath.wrappedValue == before)
      let previousSectionBinding = store.navigationPath
      store.openTask("tsk_study"); await store.activityDidAppear()
      previousSectionBinding.wrappedValue = [] // Deferred back write from People.
      #expect(store.activityPath == [.task("tsk_study")])
      #expect(store.navigationPath.wrappedValue == [.activity(.task("tsk_study"))])
      store.route(.back)
      #expect(store.navigationPath.wrappedValue == before)
      store.navigationPath.wrappedValue = [.activity(.task("stale-transaction"))]
      #expect(store.navigationPath.wrappedValue == before)
      store.selectPage(.overview)
      #expect(store.navigationPath.wrappedValue.isEmpty)
    }
  }
  @Test func searchCoversOCRNamesSenderChatAndFiltersWithoutNotes() {
    var image = WorkspaceItem(id: "i", kind: "file", title: "IMG_001.jpg", addedAt: "2026-10-28T10:00:00Z")
    image.aiName = "Workshop poster"; image.ocrText = "5 November, North Campus"; image.sender = "Maya"; image.chat = "Design Club"; image.mediaType = "image"
    let link = WorkspaceItem(id: "l", kind: "link", title: "Workshop site", url: "https://example.com", addedAt: "2026-10-29T10:00:00Z")
    let items = [link, image, WorkspaceItem(id: "n", kind: "note", title: "Hidden note")]
    #expect(TaskAssetsPresentation.items(items, filter: "All", query: "").map(\.id) == ["i", "l"])
    for query in ["poster", "north campus", "Maya", "Design Club", "IMG_001"] {
      #expect(TaskAssetsPresentation.items(items, filter: "All", query: query).map(\.id) == ["i"])
    }
    #expect(TaskAssetsPresentation.items(items, filter: "Links", query: "").map(\.id) == ["l"])
  }
  @Test func openGraphParsingAndURLPolicy() {
    let meta = MediaPreviewCache.metadata(#"<title>Fallback</title><meta content='Poster &amp; date' property='og:title'><meta property="og:image" content="/poster.jpg"><link href='/icon.png' rel='icon'>"#)
    #expect(meta["og:title"] == "Poster &amp; date" && meta["og:image"] == "/poster.jpg" && meta["icon"] == "/icon.png")
    for url in ["file:///tmp/test", "javascript:alert(1)", "http://127.0.0.1/x", "http://localhost/x", "https://name:password@example.com", "http://router.local/x"] {
      #expect(!MediaPreviewCache.safeRemote(URL(string: url)!))
    }
    #expect(MediaPreviewCache.safeRemote(URL(string: "https://example.com/poster")!))
  }
}
