import AppKit
import LimmrKit
import SwiftUI

/// Commands are routed through the same store for menus, keyboard input and tests.
enum AppCommand: Equatable {
  case search, section(Int), settings, back, escape, moveRow(Int), openRow
}

enum ActivityRoute: Hashable {
  case effort(String), task(String)
}

extension AppStore {
  var canGoBack: Bool { !circlePath.isEmpty || !activityPath.isEmpty || pendingActivityRoute != nil }

  func openActivityRoute(_ route: ActivityRoute) {
    if page == .activity, activityNavigationReady {
      pendingActivityRoute = nil
      activityReturnPage = nil
      activityPath = [route]
    } else {
      let origin = page
      let path = circlePath
      selectPage(.activity)
      activityReturnPath = path
      pendingActivityRoute = route
      activityReturnPage = origin == .activity ? nil : origin
    }
  }

  var activityBackTitle: String {
    switch activityReturnPage {
    case .overview: "Home"
    case .circles: "People"
    default: "Activity"
    }
  }

  /// Back from a task or effort: to the page it was opened from, else the Activity list.
  func leaveActivityRoute() {
    if let origin = activityReturnPage {
      let path = activityReturnPath
      selectPage(origin)
      if origin == .circles { circlePath = path }
    } else { clearActivityRoute() }
  }

  /// Called by the mounted stack, after its root destination registration exists.
  func activityDidAppear() async {
    await Task.yield()
    guard !Task.isCancelled, page == .activity else { return }
    activityNavigationReady = true
    if let route = pendingActivityRoute {
      pendingActivityRoute = nil
      activityPath = [route]
    }
  }

  func clearActivityRoute() {
    pendingActivityRoute = nil
    activityPath = []
    activityReturnPage = nil
    activityReturnPath = []
  }
  func leavePeopleRoute() {
    if !circlePath.isEmpty { circlePath.removeLast() }
    else { selectPage(.circles) }
  }
  func route(_ command: AppCommand) {
    switch command {
    case .search:
      guard !showSetup else { return }
      if page == .circles { circlePath = [] }
      if page == .activity { clearActivityRoute() }
      pendingSearchPage = page
      searchRequest += 1
    case .section(let number):
      if (1...3).contains(number) { circlePath = []; selectedPerson = "" }
      switch number {
      case 1: selectPage(.overview)
      case 2: selectPage(.circles)
      case 3: selectPage(.activity)
      case 4: showSettings = true
      default: break
      }
    case .settings: showSettings = true
    case .back:
      if page == .circles { leavePeopleRoute() }
      else if page == .activity { leaveActivityRoute() }
    case .moveRow(let step): rowStep = step; rowRequest += 1
    case .openRow: rowOpenRequest += 1
    case .escape:
      escapeRequest += 1
      connectKind = nil; showConnect = false; showRemote = false; showProposalReview = false
    }
  }
}

struct LimmrCommands: Commands {
  let store: AppStore
  @Environment(\.openSettings) private var openSettings
  var body: some Commands {
    CommandGroup(replacing: .appSettings) {
      Button("Settings…") { openSettings() }.keyboardShortcut(",")
    }
    CommandGroup(after: .textEditing) {
      Button("Find on This Page") { store.route(.search) }.keyboardShortcut("f")
        .disabled(store.showSetup)
    }
    CommandMenu("Navigate") {
      Button("Home") { store.route(.section(1)) }.keyboardShortcut("1")
      Button("People") { store.route(.section(2)) }.keyboardShortcut("2")
      Button("Activity") { store.route(.section(3)) }.keyboardShortcut("3")
      Button("Settings") { openSettings() }.keyboardShortcut("4")
      Divider()
      Button("Back") { store.route(.back) }.keyboardShortcut("[")
        .disabled(!store.canGoBack)
      Button("Dismiss and Clear Search") {
        // Let the focused sheet/popover perform its native cancel action first.
        NSApp.keyWindow?.cancelOperation(nil)
        store.route(.escape)
      }.keyboardShortcut(.escape, modifiers: [])
      Divider()
      Button("Previous Row (↑)") { store.route(.moveRow(-1)) }
        .disabled(store.showSetup)
      Button("Next Row (↓)") { store.route(.moveRow(1)) }
        .disabled(store.showSetup)
      Button("Open Selected Row (Return)") { store.route(.openRow) }
        .disabled(store.showSetup)
    }
  }
}

/// Stable selection, including empty/replaced result sets. No wrap at list boundaries.
struct KeyboardSelection: Equatable {
  var id: String?
  mutating func move(_ step: Int, in ids: [String]) {
    guard !ids.isEmpty else { id = nil; return }
    let current = id.flatMap { ids.firstIndex(of: $0) }
    let next = current.map { min(ids.count - 1, max(0, $0 + step)) } ?? (step < 0 ? ids.count - 1 : 0)
    id = ids[next]
  }
  func current(in ids: [String]) -> String? { id.flatMap { ids.contains($0) ? $0 : nil } }
}

/// Outlines share the row/card geometry. Insets expand decoration only, so
/// unfocused standard-contrast layout and pixels remain unchanged.
enum ControlOutline {
  case link
  case row(radius: CGFloat, horizontal: CGFloat = 0, vertical: CGFloat = 0, bordered: Bool = true)

  var radius: CGFloat {
    switch self { case .link: 100; case .row(let radius, _, _, _): radius }
  }
  var horizontal: CGFloat {
    switch self { case .link: 8; case .row(_, let horizontal, _, _): horizontal }
  }
  var vertical: CGFloat {
    switch self { case .link: 6; case .row(_, _, let vertical, _): vertical }
  }
  var isLink: Bool { if case .link = self { true } else { false } }
  var bordered: Bool {
    switch self { case .link: false; case .row(_, _, _, let bordered): bordered }
  }
}

/// Attach to the Button itself: no extra focus stop and native Space activation is retained.
private struct KeyboardFocusRing: ViewModifier {
  var outline: ControlOutline
  var id: String?
  @FocusState private var focused: Bool
  @Environment(\.colorSchemeContrast) private var systemContrast
  @Environment(\.snapshotIncreasedContrast) private var snapshotContrast
  @Environment(\.snapshotFocusedControl) private var snapshotFocus
  private var increasedContrast: Bool { systemContrast == .increased || snapshotContrast }
  private var showsFocus: Bool { focused || (id != nil && snapshotFocus == id) }
  func body(content: Content) -> some View {
    linkContent(content).focused($focused).focusEffectDisabled()
      .overlay {
        RoundedRectangle(cornerRadius: outline.radius).stroke(
          showsFocus ? Color.accentColor : increasedContrast && outline.bordered ? Color.primary.opacity(0.65) : .clear,
          lineWidth: showsFocus ? 3 : 1)
          .padding(.horizontal, -outline.horizontal).padding(.vertical, -outline.vertical)
          .allowsHitTesting(false).accessibilityHidden(true)
      }
  }
  @ViewBuilder private func linkContent(_ content: Content) -> some View {
    if increasedContrast && outline.isLink { content.underline() }
    else { content }
  }
}
private struct EscapeDismissal: ViewModifier {
  @Environment(\.dismiss) private var dismiss
  func body(content: Content) -> some View { content.onExitCommand { dismiss() } }
}
/// Retain native Button semantics, including inherited styles on button groups.
private struct KeyboardButtonStyle: PrimitiveButtonStyle {
  var outline: ControlOutline
  var id: String?
  @Environment(\.isEnabled) private var enabled
  @Environment(\.keyboardRowNavigation) private var listOwnsArrows
  func makeBody(configuration: Configuration) -> some View {
    Button(role: configuration.role, action: configuration.trigger) { configuration.label }
      .buttonStyle(.plain).modifier(KeyboardFocusRing(outline: outline, id: id))
      .onKeyPress(.return) { guard enabled else { return .ignored }; configuration.trigger(); return .handled }
      .onKeyPress(keys: [.upArrow, .downArrow]) { press in
        guard !listOwnsArrows else { return .ignored }
        if press.key == .downArrow { NSApp.keyWindow?.selectNextKeyView(nil) }
        else { NSApp.keyWindow?.selectPreviousKeyView(nil) }
        return .handled
      }
      .accessibilityValue(enabled ? "Available" : "Unavailable")
      .accessibilityHint("Press Return or Space to activate")
  }
}
private struct KeyboardRowNavigation: EnvironmentKey { static let defaultValue = false }
private struct SnapshotIncreasedContrast: EnvironmentKey { static let defaultValue = false }
private struct SnapshotFocusedControl: EnvironmentKey { static let defaultValue: String? = nil }
private struct SnapshotReducedMotion: EnvironmentKey { static let defaultValue = false }
extension EnvironmentValues {
  var snapshotFocusedControl: String? {
    get { self[SnapshotFocusedControl.self] }
    set { self[SnapshotFocusedControl.self] = newValue }
  }
  var keyboardRowNavigation: Bool {
    get { self[KeyboardRowNavigation.self] }
    set { self[KeyboardRowNavigation.self] = newValue }
  }
  var snapshotIncreasedContrast: Bool {
    get { self[SnapshotIncreasedContrast.self] }
    set { self[SnapshotIncreasedContrast.self] = newValue }
  }
  var snapshotReducedMotion: Bool {
    get { self[SnapshotReducedMotion.self] }
    set { self[SnapshotReducedMotion.self] = newValue }
  }
}
extension View {
  func dismissOnEscape() -> some View { modifier(EscapeDismissal()) }
  func limmrButton(_ outline: ControlOutline = .link, id: String? = nil) -> some View {
    buttonStyle(KeyboardButtonStyle(outline: outline, id: id))
  }
  func keyboardRow(_ id: String, selection: String?, radius: CGFloat = 8, horizontal: CGFloat = 0, vertical: CGFloat = 0) -> some View {
    overlay {
      RoundedRectangle(cornerRadius: radius).stroke(
        selection == id ? Color.accentColor : .clear, lineWidth: 3)
        .padding(.horizontal, -horizontal).padding(.vertical, -vertical)
        .allowsHitTesting(false).accessibilityHidden(true)
    }
    .accessibilityAddTraits(selection == id ? [.isSelected] : [])
    .id(id)
  }
}

/// Copy used directly by the views, so the tests assert the spoken output.
enum Spoken {
  static func count(_ value: Int, _ noun: String) -> String {
    "\(value) \(noun == "New" ? "new messages" : noun.lowercased())"
  }
  static func sidebarValue(_ page: Page, selected: Page, queued: Int) -> String {
    let state = page == selected ? "Selected" : "Unselected"
    return page == .overview && queued > 0 ? "\(state). \(queued) queued drafts" : state
  }
  static func heatmap(_ days: [HistoryDay], agents: Bool) -> String {
    let noun = agents ? "agent actions" : "messages"
    let total = days.reduce(0) { $0 + (agents ? $1.agentActivity : $1.messages) }
    guard let busiest = days.max(by: { (agents ? $0.agentActivity : $0.messages) < (agents ? $1.agentActivity : $1.messages) }), total > 0 else {
      return "\(count(total, noun)) in the displayed history"
    }
    return "\(count(total, noun)) in the displayed history. Busiest day \(busiest.date)."
  }
  static func map(_ map: CircleMap) -> String {
    "\(count(map.nodes.filter { $0.kind == "person" }.count, "people")), \(count(map.nodes.filter { $0.kind == "circle" }.count, "circles")), \(count(map.edges.filter { $0.state != "reverted" }.count, "connections"))"
  }
  static func effort(_ effort: Effort) -> String { "\(effort.title). \(effort.summary)" }
  static func node(_ node: CircleMapNode) -> String { "\(node.title), \(node.kind)" }
}

/// Hierarchical secondary ink is intentionally promoted in Increase Contrast.
/// Custom Canvas and low-opacity metadata cannot rely on AppKit to recolor them.
struct AccessibleSecondary: ShapeStyle {
  func resolve(in environment: EnvironmentValues) -> Color {
    environment.colorSchemeContrast == .increased || environment.snapshotIncreasedContrast
      ? .primary : .secondary
  }
}
struct AccessibleTertiary: ShapeStyle {
  func resolve(in environment: EnvironmentValues) -> Color {
    environment.colorSchemeContrast == .increased || environment.snapshotIncreasedContrast
      ? .primary : Color.secondary.opacity(0.5)
  }
}
