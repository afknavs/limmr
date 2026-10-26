import Contacts
import LimmrKit
import SwiftUI

struct LocalContact: Sendable {
  var phones: [String]
  var emails: [LabeledValue] = []
  var birthday = ""
  var org = ""
  var title = ""
  var photo: Data? = nil
}
@MainActor protocol ContactsSource {
  func requestAccess() async throws -> Bool
  func read() throws -> [LocalContact]
}
@MainActor struct MacContactsSource: ContactsSource {
  func requestAccess() async throws -> Bool { try await CNContactStore().requestAccess(for: .contacts) }
  func read() throws -> [LocalContact] {
    guard CNContactStore.authorizationStatus(for: .contacts) == .authorized else { throw BackendError.unavailable("Allow Contacts access in System Settings to re-sync.") }
    let keys: [CNKeyDescriptor] = [CNContactPhoneNumbersKey, CNContactEmailAddressesKey, CNContactBirthdayKey, CNContactOrganizationNameKey, CNContactJobTitleKey, CNContactThumbnailImageDataKey].map { $0 as CNKeyDescriptor }
    var contacts: [LocalContact] = []
    try CNContactStore().enumerateContacts(with: CNContactFetchRequest(keysToFetch: keys)) { contact, _ in
      var date = ""
      if let b = contact.birthday, let month = b.month, let day = b.day { date = b.year.map { String(format: "%04d-%02d-%02d", $0, month, day) } ?? String(format: "%02d-%02d", month, day) }
      contacts.append(LocalContact(phones: contact.phoneNumbers.map { $0.value.stringValue }, emails: contact.emailAddresses.map { .init(label: $0.label.map { CNLabeledValue<NSString>.localizedString(forLabel: $0) } ?? "personal", value: $0.value as String, source: "contacts") }, birthday: date, org: contact.organizationName, title: contact.jobTitle, photo: contact.thumbnailImageData))
    }
    return contacts
  }
}
struct ContactsImportBody: Encodable, Sendable {
  struct Match: Encodable, Sendable {
    var personId: String
    var emails: [LabeledValue]
    var birthday: String
    var org: String
    var title: String
    var photoB64: String?
  }
  var matches: [Match]
  // International numbers only; no guess about the owner's country or local area.
  static func phone(_ value: String) -> String? {
    let text = value.trimmingCharacters(in: .whitespacesAndNewlines)
    var digits = text.filter(\.isNumber)
    if text.hasPrefix("00") { digits = String(digits.dropFirst(2)) }
    guard text.hasPrefix("+") || text.hasPrefix("00"), (7...15).contains(digits.count) else { return nil }
    return "+" + digits
  }
  static func matching(_ contacts: [LocalContact], people: [Person]) -> Self {
    var byPhone: [String: LocalContact] = [:]
    for contact in contacts { for value in contact.phones { if let number = phone(value) { byPhone[number] = contact } } }
    return Self(matches: people.compactMap { person in
      let contact = person.jids.filter { $0.hasSuffix("@s.whatsapp.net") }.compactMap { byPhone["+" + $0.components(separatedBy: "@")[0]] }.first
      guard let contact else { return nil }
      return Match(personId: person.id, emails: contact.emails, birthday: contact.birthday, org: contact.org, title: contact.title, photoB64: contact.photo.flatMap { $0.count <= 4 * 1024 * 1024 ? $0.base64EncodedString() : nil })
    })
  }
}
struct ContactsSettingsState: Codable, Sendable {
  var enabled = false
  var lastImportedAt: String? = nil
}
@MainActor struct ContactsImporter {
  let backend: any Backend
  let source: any ContactsSource
  func run(ownerPressedButton: Bool) async throws -> Bool {
    let state: ContactsSettingsState = try await backend.get("/v1/settings/people/contacts")
    guard ownerPressedButton || state.enabled else { return false }
    if ownerPressedButton { guard try await source.requestAccess() else { return false } }
    // A persisted opt-in is required for every background read.
    var people: [Person] = []; var cursor: String? = nil
    repeat {
      let response: PeopleResponse = try await backend.get("/v1/people?all=true&limit=100" + (cursor.map { "&cursor=" + ($0.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? "") } ?? ""))
      people += response.people; cursor = response.nextCursor
    } while cursor?.isEmpty == false
    let body = ContactsImportBody.matching(try source.read(), people: people)
    try await backend.post("/v1/people/contacts-import", body)
    _ = try await backend.request("/v1/settings/people/contacts", method: "PUT", body: Wire.data(ContactsSettingsState(enabled: true, lastImportedAt: ISO8601DateFormatter().string(from: Date()))))
    return true
  }
}
struct ContactsImportSettings: View {
  @Bindable var store: AppStore
  @State private var state = ContactsSettingsState()
  @State private var running = false
  var body: some View {
    VStack(alignment: .leading, spacing: 10) {
      Button(state.enabled ? "Re-sync my Contacts" : "Use my Contacts") {
        Task {
          running = true; defer { running = false }
          await store.perform(action: "contacts-import") {
            if try await ContactsImporter(backend: store.backend, source: MacContactsSource()).run(ownerPressedButton: true) {
              state = try await store.backend.get("/v1/settings/people/contacts")
              store.avatarAttempts = []; store.avatarData = [:]; await store.refresh()
            }
          }
        }
      }.disabled(running || store.demo || store.snapshot)
      Text("Match phone numbers to add photos, email, birthdays and work details. Only matched details are imported. Imported profile details and photos sync to your paired engine.").font(.footnote).foregroundStyle(AccessibleSecondary())
      if state.enabled {
        Text("Re-syncs daily while Limmr is open.").font(.footnote).foregroundStyle(AccessibleSecondary())
        Button("Stop using Contacts") { Task { await store.perform(action: "contacts-import") {
          state.enabled = false
          _ = try await store.backend.request("/v1/settings/people/contacts", method: "PUT", body: Wire.data(state))
        } } }
      }
      ActionFeedback(store: store, action: "contacts-import")
    }.task { if let value: ContactsSettingsState = try? await store.backend.get("/v1/settings/people/contacts") { state = value } }
  }
}
extension AppStore {
  func scheduleContactsImport() {
    guard !demo, !snapshot, contactsImportTask == nil else { return }
    contactsImportTask = Task { [weak self] in
      while !Task.isCancelled {
        guard let self else { return }
        if let state: ContactsSettingsState = try? await backend.get("/v1/settings/people/contacts"), state.enabled,
           state.lastImportedAt.flatMap(Timestamp.date).map({ Date().timeIntervalSince($0) >= 86400 }) ?? true {
          _ = try? await ContactsImporter(backend: backend, source: MacContactsSource()).run(ownerPressedButton: false)
        }
        try? await Task.sleep(for: .seconds(3600))
      }
    }
  }
}
