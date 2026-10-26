import Foundation
import Testing

@testable import Limmr
@testable import LimmrKit

@Suite("S2 Settings and About you", .serialized)
struct S2Tests {
  @MainActor @Test func aboutYouUsesSelfBriefAndDerivedContexts() async throws {
    let store = AppStore(demo: true, snapshot: true)
    await store.refresh()
    let circle = try #require(store.aboutYouCircle)
    #expect(circle.kind == "self")
    await store.loadCircle(circle.id)
    let brief = try #require(store.circleDetails[circle.id]?.brief)
    #expect(store.aboutYouBrief == brief)
    if let index = store.home.today.firstIndex(where: { $0.circle.id == circle.id }) {
      var newer = brief
      newer.version += 1
      newer.summary = "A newer understanding from your AI."
      store.home.today[index].brief = newer
      #expect(store.aboutYouBrief == newer)
      store.home.today[index].brief = brief
    }
    store.ownerProfile.about = "UNRELATED_SEED"
    #expect(store.aboutYouBrief?.summary == brief.summary)
    #expect(!store.aboutYouBrief!.summary.contains("UNRELATED_SEED"))
    #expect(!store.aboutYouContexts.isEmpty)
    #expect(store.aboutYouContexts.allSatisfy { $0.kind != "self" && $0.confirmed })
    store.navigate(URL(string: "limmr://people/me")!)
    #expect(store.page == .circles && store.circlePath == [.person("me")])
    await store.useSampleData(false, realBackend: EmptyBackend())
    #expect(store.aboutYouBrief == nil && store.aboutYouContexts.isEmpty)
  }

  @MainActor @Test func aboutYouLoadsAgentAttributionRatherThanSeed() async throws {
    let backend = DemoBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    let id = try #require(store.aboutYouCircle?.id)
    await store.loadCircle(id)
    #expect(store.aboutYouBrief?.writtenBy == "agent-2")
    #expect(store.aboutYouBrief?.writtenAt.isEmpty == false)
    let response: BriefsResponse = try await backend.get("/v1/circles/\(id)/briefs")
    #expect(store.aboutYouBrief == response.briefs.first)
  }

  @Test func hiddenListsPartitionByChatJIDAndPersistIndependently() async throws {
    let backend = DemoBackend()
    let response: ChatsResponse = try await backend.get("/v1/chats")
    let people = HiddenChatKind.people.chats(in: response.chats)
    let groups = HiddenChatKind.groups.chats(in: response.chats)
    #expect(!people.isEmpty && !groups.isEmpty)
    #expect(people.allSatisfy { !$0.jid.hasSuffix("@g.us") })
    #expect(groups.allSatisfy { $0.jid.hasSuffix("@g.us") })
    #expect(Set((people + groups).map(\.jid)) == Set(response.chats.map(\.jid)))
    var settings: AppSettings = try await backend.get("/v1/settings")
    settings.hiddenChats = [people[0].jid, groups[0].jid]
    _ = try await backend.request("/v1/settings", method: "PUT", body: Wire.data(settings))
    let saved: AppSettings = try await backend.get("/v1/settings")
    #expect(saved.hiddenChats == settings.hiddenChats)
  }

  // Structural checks complement the rendered demo snapshots.
  @Test func chromeAndSharedReadOnlyCardContracts() throws {
    let root = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
      .deletingLastPathComponent().deletingLastPathComponent().appendingPathComponent("Sources/Limmr")
    func source(_ path: String) throws -> String {
      try String(contentsOf: root.appendingPathComponent(path), encoding: .utf8)
    }
    let sidebar = try source("RootView.swift")
    #expect(sidebar.contains("Label(\"Settings\", systemImage: \"gearshape\")"))
    #expect(!sidebar.contains("Image(systemName: \"ellipsis\")"))
    #expect(sidebar.range(of: "Label(\"Settings\"")!.lowerBound < sidebar.range(of: "SidebarPresence(store:")!.lowerBound)
    let settings = try source("HealthSettings.swift")
    #expect(!settings.contains("NavigationSplitView"))
    #expect(settings.contains("AboutYouCard(store: store)"))
    let person = try source("People/PersonPage.swift")
    #expect(person.contains("if id == \"me\" { AboutYouCard(store: store) }"))
    let about = try source("AboutYou.swift").components(separatedBy: "private struct OwnerSeedCard")[0]
    #expect(!about.contains("TextField(") && !about.contains("TextEditor("))
    #expect(about.contains("Text(brief.summary)"))
    #expect(about.contains("Last updated by") && about.contains("Open in People"))
    let app = try source("LimmrApp.swift")
    #expect(app.contains("Settings {") && app.contains("Button(\"Refresh\")"))
  }
}
