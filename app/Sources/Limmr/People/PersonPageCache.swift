import LimmrKit

/// Session-only detail cache. A different backend generation never reuses a profile.
/// The list remains the synchronous source for name, photo and circle placements.
@MainActor enum PersonPageCache {
  private struct Entry {
    weak var store: AppStore?
    var generation: Int
    var profiles: [String: PersonDetail] = [:]
    var order: [String] = []
  }
  private static var entries: [ObjectIdentifier: Entry] = [:]
  static func cached(_ id: String, store: AppStore) -> PersonDetail? {
    let entry = entries[ObjectIdentifier(store)]
    guard entry?.generation == store.backendGeneration else { return nil }
    return entry?.profiles[id]
  }
  static func save(_ detail: PersonDetail, store: AppStore) {
    entries = entries.filter { $0.value.store != nil }
    let key = ObjectIdentifier(store)
    var entry = entries[key] ?? Entry(store: store, generation: store.backendGeneration)
    if entry.generation != store.backendGeneration { entry = Entry(store: store, generation: store.backendGeneration) }
    entry.order.removeAll { $0 == detail.person.id }; entry.order.append(detail.person.id)
    entry.profiles[detail.person.id] = detail
    while entry.order.count > 24 { entry.profiles.removeValue(forKey: entry.order.removeFirst()) }
    entries[key] = entry
  }
}

extension AppStore {
  func peoplePreview(_ id: String) -> Person? {
    if let person = people.first(where: { $0.id == id }) { return person }
    if let node = circleMap.nodes.first(where: { $0.id == id && $0.kind == "person" }) {
      return Person(id: id, jids: [], name: node.title, howKnown: node.howKnown ?? "")
    }
    return nil
  }
  func loadPersonProgressively(_ id: String) async {
    selectedPerson = id
    // loadPerson clears its published detail while fetching. The view reads our
    // cached value throughout that refresh, including on its very first body.
    await loadPerson()
    if let profile, profile.person.id == id { PersonPageCache.save(profile, store: self) }
  }
}
