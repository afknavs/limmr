import SwiftUI

struct SettingsSearchEntry: Identifiable {
  let label: String
  let keywords: String
  let tab: SettingsTab
  var id: String { label }
  static let all: [Self] = [
    .init(label: "Launch Limmr at login", keywords: "startup automatic", tab: .general),
    .init(label: "Current chats", keywords: "days recent older history", tab: .general),
    .init(label: "Keep activity for", keywords: "retention delete log days", tab: .general),
    .init(label: "Approval notifications", keywords: "alerts enable", tab: .general),
    .init(label: "About you", keywords: "profile name studies work", tab: .general),
    .init(label: "New circles", keywords: "automatic ask maximum", tab: .general),
    .init(label: "Export people…", keywords: "save download json contacts", tab: .people),
    .init(label: "Fill in from your AI…", keywords: "people details suggest prompt", tab: .people),
    .init(label: "WhatsApp", keywords: "source connection linked", tab: .sources),
    .init(
      label: "Infer how I know frequent contacts", keywords: "people relationship on device re-run",
      tab: .people),
    .init(label: "Sync with another Limmr", keywords: "dot server mac pair sync", tab: .integrations),
    .init(
      label: "Integrations", keywords: "agents ai permissions access chats tokens",
      tab: .integrations),
    .init(
      label: "First briefs from This Mac", keywords: "on device model apple intelligence summaries efforts naming background",
      tab: .briefs),
    .init(label: "Hidden chats", keywords: "privacy invisible exclude", tab: .hiddenChats),
    .init(
      label: "Allow remote connections", keywords: "chatgpt claude network mcp tunnel", tab: .remote
    ),
    .init(label: "Health", keywords: "diagnostics errors repair connection", tab: .health),
    .init(label: "Set up Limmr", keywords: "setup onboarding welcome", tab: .advanced),
    .init(label: "Explore with sample data", keywords: "demo examples", tab: .advanced),
    .init(label: "Updates", keywords: "version upgrade release", tab: .advanced),
    .init(label: "Data folder", keywords: "storage database files", tab: .advanced),
    .init(label: "Background helper", keywords: "engine stop service", tab: .advanced),
    .init(label: "WhatsApp connection", keywords: "unlink disconnect", tab: .advanced),
    .init(label: "Circles", keywords: "reset start over", tab: .advanced),
    .init(label: "Previous setup", keywords: "migration undo", tab: .advanced),
  ]
  static func matches(_ query: String, showAccessory: Bool = false) -> [Self] {
    let words = query.split(whereSeparator: \.isWhitespace)
    return all.filter { item in
      (showAccessory || item.label != "Accessory light") && words.allSatisfy {
        (item.label + " " + item.keywords + " " + item.tab.rawValue)
          .localizedCaseInsensitiveContains($0)
      }
    }
  }
}
struct SettingsSearchMatch: ViewModifier {
  @Environment(\.accessibilityReduceMotion) private var systemReduceMotion
  @Environment(\.snapshotReducedMotion) private var snapshotReduceMotion
  private var reduceMotion: Bool { systemReduceMotion || snapshotReduceMotion }
  @Bindable var store: AppStore
  let label: String
  @State private var highlighted = false
  func body(content: Content) -> some View {
    content.id(label)
      .background(highlighted ? Color.accentColor.opacity(0.12) : Color.clear, in: RoundedRectangle(cornerRadius: 6))
      .listRowBackground(highlighted ? Color.accentColor.opacity(0.1) : Color.clear)
      .task(id: store.settingsSearchTarget) {
        guard store.settingsSearchTarget == label else {
          highlighted = false
          return
        }
        highlighted = true
        try? await Task.sleep(for: .seconds(2))
        if !Task.isCancelled { withAnimation(reduceMotion ? nil : .default) { highlighted = false } }
      }
  }
}
extension View {
  func settingsMatch(_ label: String, store: AppStore) -> some View {
    modifier(SettingsSearchMatch(store: store, label: label))
  }
}
