import AppKit
import SwiftUI
import Foundation
import Testing
@testable import Limmr
@testable import LimmrKit

@MainActor private final class FakeContacts: ContactsSource {
  var requested = 0
  var reads = 0
  var allowed = true
  func requestAccess() async throws -> Bool { requested += 1; return allowed }
  func read() throws -> [LocalContact] { reads += 1; return [LocalContact(phones: ["+1 (555) 010-0001"], emails: [.init(label: "work", value: "maya@example.com", source: "contacts")])] }
}
private actor ContactsFixtureBackend: Backend {
  var enabled = false
  var imported: Data?
  func request(_ path: String, method: String, body: Data?) async throws -> Data {
    if path == "/v1/settings/people/contacts" {
      if method == "PUT", let body { enabled = try Wire.decode(ContactsSettingsState.self, from: body).enabled }
      return try Wire.data(ContactsSettingsState(enabled: enabled))
    }
    if path.hasPrefix("/v1/people?") { return try Wire.data(PeopleResponse(people: [Person(id: "maya", jids: ["15550100001@s.whatsapp.net"], name: "Maya Rao")])) }
    if path == "/v1/people/contacts-import" { imported = body; return Data("{}".utf8) }
    throw BackendError.http(404)
  }
  nonisolated func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> { AsyncThrowingStream { $0.finish() } }
  func didImportEmail() -> Bool { imported.flatMap { String(data: $0, encoding: .utf8) }?.contains("maya@example.com") == true }
}
@Suite("Y45 People", .serialized) struct Y45Tests {
  @MainActor @Test func groupSenderUsesDetailMembersBeyondPeopleCache() {
    var person = Person(id: "maya", jids: ["15550100001@s.whatsapp.net", "12345@lid"], name: "Maya Rao")
    #expect(GroupMessageRow.memberName(sender: "12345@lid", members: [person]) == "Maya Rao")
    person.label = "Maya"
    #expect(GroupMessageRow.memberName(sender: "15550100001@s.whatsapp.net", members: [person]) == "Maya")
    #expect(GroupMessageRow.memberName(sender: "missing@lid", members: [person]) == nil)
  }

  @MainActor @Test func personHideSwitchPersistsAllAliasesAndTracksSettings() async throws {
    let backend = DemoBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    var person = try #require(store.people.first { $0.id == "person-04" })
    let phone = try #require(person.jids.first)
    let lid = "15550100004@lid"
    person.jids.append(lid)
    let group = DemoData.groupJid(4)
    let unrelated = DemoData.personJid(5)
    let jids = store.directChatJIDs(for: person)
    #expect(Set(jids) == [phone, lid])
    let profileSwitch = store.hiddenChatsBinding(for: jids)
    let settingsPhone = store.hiddenChatsBinding(for: [phone])
    let settingsLID = store.hiddenChatsBinding(for: [lid])
    store.hiddenChatsBinding(for: [unrelated]).wrappedValue = true
    #expect(!profileSwitch.wrappedValue)
    profileSwitch.wrappedValue = true
    await store.waitForSettingsSave()
    #expect(settingsPhone.wrappedValue && settingsLID.wrappedValue)
    #expect(!store.hiddenChatsBinding(for: [group]).wrappedValue)
    let saved: AppSettings = try await backend.get("/v1/settings")
    #expect(Set(saved.hiddenChats) == [phone, lid, unrelated])

    // A partial Settings selection is off until every alias is hidden.
    settingsPhone.wrappedValue = false
    #expect(!profileSwitch.wrappedValue && settingsLID.wrappedValue)
    settingsPhone.wrappedValue = true
    #expect(profileSwitch.wrappedValue)
    profileSwitch.wrappedValue = false
    await store.waitForSettingsSave()
    let restored: AppSettings = try await backend.get("/v1/settings")
    #expect(restored.hiddenChats == [unrelated])
    #expect(!settingsPhone.wrappedValue && !settingsLID.wrappedValue)
    await store.refresh()
    #expect(!profileSwitch.wrappedValue)
  }

  @MainActor @Test func groupHideSwitchAndSettingsTrackEachOtherIndependently() async throws {
    let backend = DemoBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    let group = DemoData.groupJid(4), otherGroup = DemoData.groupJid(3)
    let person = try #require(store.people.first { $0.id == "person-04" })
    let personSwitch = store.hiddenChatsBinding(for: store.directChatJIDs(for: person))
    let groupSwitch = store.hiddenChatsBinding(for: [group])
    let settingsSwitch = store.hiddenChatsBinding(for: [group])
    personSwitch.wrappedValue = true
    groupSwitch.wrappedValue = true
    #expect(settingsSwitch.wrappedValue && personSwitch.wrappedValue)
    #expect(!store.hiddenChatsBinding(for: [otherGroup]).wrappedValue)
    await store.waitForSettingsSave()
    let saved: AppSettings = try await backend.get("/v1/settings")
    #expect(saved.hiddenChats.contains(group))
    settingsSwitch.wrappedValue = false
    #expect(!groupSwitch.wrappedValue && personSwitch.wrappedValue)
    await store.waitForSettingsSave()
    await store.refresh()
    #expect(!groupSwitch.wrappedValue && personSwitch.wrappedValue)
  }

  @MainActor @Test func personWithoutDirectChatHasDisabledEmptySwitch() async throws {
    let backend = DemoBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh()
    let person = try #require(store.people.first { $0.id == "person-04" })
    store.chats.removeAll { person.jids.contains($0.jid) }
    let jids = store.directChatJIDs(for: person)
    #expect(jids.isEmpty)
    let toggle = store.hiddenChatsBinding(for: jids)
    toggle.wrappedValue = true
    #expect(!toggle.wrappedValue && store.settings.hiddenChats.isEmpty)
    var groupOnly = person
    groupOnly.jids = [DemoData.groupJid(4)]
    #expect(store.directChatJIDs(for: groupOnly).isEmpty)
  }

  @MainActor @Test func olderDirectChatOutsideLoadedPageStillEnablesSwitch() async throws {
    let store = AppStore(demo: true, snapshot: true, backend: DemoBackend())
    await store.refresh()
    let person = try #require(store.people.first { $0.id == "person-04" })
    store.chats.removeAll { person.jids.contains($0.jid) }
    store.chatCursor = "next-demo-page"
    #expect(store.directChatJIDs(for: person).isEmpty)
    await store.loadDirectChat(for: person)
    #expect(store.directChatJIDs(for: person) == person.jids)
  }

  @MainActor @Test func contactsNeverReadBeforeOptInAndDenialDoesNotImport() async throws {
    let backend = ContactsFixtureBackend(); let source = FakeContacts()
    let importer = ContactsImporter(backend: backend, source: source)
    #expect(try await importer.run(ownerPressedButton: false) == false)
    #expect(source.requested == 0 && source.reads == 0)
    source.allowed = false
    #expect(try await importer.run(ownerPressedButton: true) == false)
    #expect(source.requested == 1 && source.reads == 0)
    source.allowed = true
    #expect(try await importer.run(ownerPressedButton: true))
    #expect(source.reads == 1 && source.requested == 2)
    #expect(await backend.didImportEmail())
    #expect(try await importer.run(ownerPressedButton: false))
    #expect(source.requested == 2 && source.reads == 2)
  }
  @Test func contactsMatchOnlyNormalizedInternationalPhoneJIDs() {
    let contact = LocalContact(phones: ["+1 (555) 010-0001"], emails: [.init(label: "work", value: "maya@example.com")])
    let people = [Person(id: "maya", jids: ["15550100001@s.whatsapp.net"], name: "Maya Rao"), Person(id: "lid", jids: ["15550100001@lid"], name: "Noor Chen")]
    let result = ContactsImportBody.matching([contact], people: people)
    #expect(result.matches.count == 1 && result.matches.first?.personId == "maya")
    #expect(ContactsImportBody.phone("001 555 010 0001") == "+15550100001")
    #expect(ContactsImportBody.phone("555 010 0001") == nil)
  }
  @MainActor @Test func avatarUsesStablePaletteAndOneOrTwoInitials() {
    #expect(PersonAvatar.initials("  Maya  Rao ") == "MR")
    #expect(PersonAvatar.initials("Noor") == "N")
    #expect(PersonAvatar.initials("Maya June Rao") == "MR")
    #expect(PersonAvatar.paletteIndex("maya") == PersonAvatar.paletteIndex("maya"))
    #expect((0..<5).contains(PersonAvatar.paletteIndex("noor")))
  }
  @MainActor @Test func demoGroupsPhotosAccessAndLatelyReadBack() async throws {
    let backend = DemoBackend()
    let groups: GroupsResponse = try await backend.get("/v1/groups")
    let group = try #require(groups.groups.first { $0.photo == true })
    let detail: GroupDetail = try await backend.get("/v1/groups/\(group.jid)")
    #expect(!detail.members.isEmpty && !(detail.recent ?? []).isEmpty && detail.summary != nil)
    let data = try await backend.request("/v1/groups/\(group.jid)/photo", method: "GET", body: nil)
    #expect(NSImage(data: data) != nil)
    let summary: PersonSummaryResponse = try await backend.get("/v1/people/person-04/summary")
    #expect(summary.summary?.text.isEmpty == false)
    let avatar = try await backend.request("/v1/people/person-04/avatar", method: "GET", body: nil)
    #expect(NSImage(data: avatar) != nil)
    let encoded = try await backend.request("/v1/people/person%2D04/avatar", method: "GET", body: nil)
    #expect(encoded == avatar)
    let imported: PersonDetail = try await backend.get("/v1/people/person-05")
    #expect(imported.person.details?.emails.first?.source == "contacts")
    _ = try await backend.request("/v1/groups/\(group.jid)/access", method: "PATCH", body: Wire.data(["agent_id": "agent-1", "send": "never"]))
    let changed: GroupDetail = try await backend.get("/v1/groups/\(group.jid)")
    #expect(changed.group.agentAccess?.first?.send == "never")
  }
  @MainActor @Test func ownerContactsAndSuggestedEmailsDecodeRenderAndExport() async throws {
    _ = NSApplication.shared
    let backend = DemoBackend()
    let store = AppStore(demo: true, snapshot: true, backend: backend)
    await store.refresh(); store.selectedPerson = "person-04"; await store.loadPerson()
    let profile = try #require(store.profile)
    #expect(profile.person.details?.emails.first?.value == "maya.rao@example.com")
    #expect(profile.person.detailSuggestions?.contains { $0.field == "emails" } == true)
    let host = NSHostingView(rootView: CirclePersonPage(store: store, id: "person-04"))
    host.setFrameSize(NSSize(width: 760, height: 820)); host.layoutSubtreeIfNeeded()
    #expect(host.fittingSize.width.isFinite)
    let data = try await backend.request("/v1/people/export", method: "GET", body: nil)
    let export = String(data: data, encoding: .utf8) ?? ""
    #expect(export.contains("maya.rao@example.com") && export.contains("noor@example.com"))
  }
}
