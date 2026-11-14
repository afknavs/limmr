import AppKit
import CoreText
import LimmrKit
import SwiftUI

/// Register optional bundled OFL font resources before views request their fonts.
/// A missing/invalid resource leaves the system fallback in LimmrTheme.font intact.
enum PaperFonts {
  static func register(in bundle: Bundle = .main) {
    for url in bundle.urls(forResourcesWithExtension: "ttf", subdirectory: "Fonts") ?? [] {
      CTFontManagerRegisterFontsForURL(url as CFURL, .process, nil)
    }
  }
}
struct PaperMark: View {
  var body: some View {
    Canvas { context, size in
      let sx = size.width / 76, sy = size.height / 84
      func rect(_ x: Double, _ y: Double, _ w: Double, _ h: Double, _ r: Double, _ color: Color) {
        context.fill(Path(roundedRect: CGRect(x: (x - 12) * sx, y: (y - 8) * sy, width: w * sx, height: h * sy), cornerRadius: r * sx), with: .color(color))
      }
      rect(22.9,12.2,18.2,62.8,3.6,LimmrTheme.ink)
      rect(17.8,70,64.4,17.8,3.6,LimmrTheme.ink)
      rect(63.3,46.7,18.9,41.1,3.6,LimmrTheme.ink)
      rect(49.6,17.8,23.7,23.3,2,LimmrTheme.brand)
    }.accessibilityHidden(true)
  }
}
struct PaperMarker: View {
  var body: some View { RoundedRectangle(cornerRadius: 2).fill(LimmrTheme.brand).frame(width: 8, height: 8).accessibilityHidden(true) }
}
struct PaperDot: View {
  var color: Color = LimmrTheme.info
  var body: some View { Circle().fill(color).frame(width: 6, height: 6).accessibilityHidden(true) }
}
struct PaperChip: View {
  let text: String
  var kind = "plain"
  var body: some View {
    HStack(spacing: 6) {
      if kind == "need" { PaperMarker() }
      if kind == "working" { PaperDot() }
      Text(text).lineLimit(2).fixedSize(horizontal: false, vertical: true)
    }.font(LimmrTheme.font(12, .medium)).padding(.horizontal, 8).padding(.vertical, 4).frame(minHeight: 22)
      .foregroundStyle(kind == "need" ? LimmrTheme.needsYou : kind == "working" ? LimmrTheme.infoText : LimmrTheme.ink2)
      .background(kind == "need" ? LimmrTheme.needBackground : kind == "working" ? LimmrTheme.infoBackground : LimmrTheme.surface2, in: Capsule())
  }
}
struct PaperButtonStyle: ButtonStyle {
  var primary = false
  @Environment(\.isEnabled) private var enabled
  func makeBody(configuration: Configuration) -> some View {
    configuration.label.font(LimmrTheme.font(12, .semibold)).fixedSize(horizontal: true, vertical: false).padding(.horizontal, 12).frame(minHeight: 28)
      .foregroundStyle(primary ? LimmrTheme.onPrimary : LimmrTheme.ink)
      .background(primary ? LimmrTheme.ink : LimmrTheme.surface2, in: RoundedRectangle(cornerRadius: 12))
      .opacity(!enabled ? 0.4 : configuration.isPressed ? 0.7 : 1)
  }
}
struct PaperSegments: View {
  let values: [String]
  @Binding var selection: String
  var labels: [String: String] = [:]
  var body: some View {
    HStack(spacing: 0) {
      ForEach(values, id: \.self) { value in
        Button { selection = value } label: {
          Text(labels[value] ?? value).fixedSize(horizontal: true, vertical: false).font(LimmrTheme.font(12, selection == value ? .semibold : .medium))
            .padding(.horizontal, 12).frame(height: 26)
            .background(selection == value ? LimmrTheme.panel : .clear, in: RoundedRectangle(cornerRadius: 10))
        }.buttonStyle(.plain).accessibilityAddTraits(selection == value ? [.isSelected] : [])
      }
    }.padding(2).background(LimmrTheme.surface2, in: RoundedRectangle(cornerRadius: 12))
  }
}
struct PaperSteps: View {
  let progress: TaskProgress
  var working = false
  var body: some View {
    if progress.total > 0 {
      HStack(spacing: 3) {
        ForEach(0..<min(40, progress.total), id: \.self) { step in
          RoundedRectangle(cornerRadius: 2).fill(step < progress.done ? LimmrTheme.ink2 : step == progress.done && working ? LimmrTheme.info : LimmrTheme.line2).frame(height: 3)
        }
      }.accessibilityElement(children: .ignore).accessibilityLabel("\(progress.done) of \(progress.total) steps done")
    }
  }
}
struct PaperNumber: View {
  let value: String
  let label: String
  var body: some View {
    VStack(alignment: .leading, spacing: 4) {
      Text(value).font(LimmrTheme.font(20, .semibold)).tracking(-0.4)
      Text(label).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3)
    }.accessibilityElement(children: .ignore).accessibilityLabel(value + " " + label)
  }
}
struct PaperAvatarStack: View {
  @Bindable var store: AppStore
  let ids: [String]
  var size: CGFloat = 18
  var body: some View {
    HStack(spacing: -7) {
      ForEach(Array(ids.prefix(4)), id: \.self) { id in
        PersonAvatar(store: store, id: id, name: store.personName(id), size: size)
          .overlay(Circle().stroke(LimmrTheme.surface, lineWidth: 1.5))
      }
    }.accessibilityLabel(ids.prefix(4).map(store.personName).joined(separator: ", "))
  }
}
struct PaperSidebar: View {
  @Bindable var store: AppStore
  @Environment(\.limmrWindowWidth) private var windowWidth
  private var rail: Bool { PaperLayout.sidebarWidth(windowWidth) == 56 }
  @Environment(\.openSettings) private var openSettings
  var body: some View {
    VStack(alignment: .leading, spacing: 2) {
      HStack(spacing: 10) { PaperMark().frame(width: 24, height: 24); if !rail { Text("Limmr").font(LimmrTheme.font(17, .bold)) } }.frame(maxWidth: .infinity, alignment: rail ? .center : .leading).padding(.horizontal, rail ? 0 : 8).padding(.top, 32).padding(.bottom, 14)
      ScrollView { VStack(alignment:.leading,spacing:2) {
      ForEach(Page.allCases) { page in
        row(page.rawValue, page.symbol, selected: store.sidebarDestination == "page:" + page.rawValue) { store.selectPage(page) } trailing: {
          if page == .tasks {
            let count = TaskListPresentation.open(store.home.tasks).filter(\.needsYou).count
            if count > 0 && !rail { Text("\(count)").font(LimmrTheme.font(12, .semibold)).foregroundStyle(.white).padding(.horizontal, 5).frame(minWidth: 18, minHeight: 18).background(LimmrTheme.brand, in: RoundedRectangle(cornerRadius: 5)) }
          }
        }
      }
      if !store.circles.isEmpty {
        section("Circles")
        ForEach(store.circles) { circle in
          row(circle.name, circle.displaySymbol, selected: store.sidebarDestination == "circle:" + circle.id) { store.selectSidebar("circle:" + circle.id) } trailing: { EmptyView() }
            .contextMenu { CircleIdentityMenu(store: store, circle: circle) }
        }
      }
      let agents = AIIdentity.rows(store.agents, sync: store.connectionSync)
      if !agents.isEmpty {
        section("Your AIs")
        ForEach(agents) { agent in
          row(AIIdentity.name(agent, sync: store.connectionSync), AIIdentity.symbol(agent, sync: store.connectionSync), selected: false) {
            store.selectSidebar("ai:" + agent.id); openSettings()
          } trailing: {
            let present = AIIdentity.isPresent(agent, now: store.presentationNow)
            if PaperLayout.sidebarWidth(windowWidth) > 200 {
              Text(present ? "working" : store.lastSeen(agent)).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3).lineLimit(1)
            }
            if present { PaperDot() }
          }.contextMenu {
            Button("Open integration") { store.selectSidebar("ai:" + agent.id); openSettings() }
            Button("Show activity") { store.activityAgent = agent.id; store.selectPage(.activity) }
          }
        }
      }
      } }.scrollIndicators(.hidden)
      Spacer(minLength: 8)
      row("Settings", "slider.horizontal.3", selected: false) { openSettings() } trailing: { EmptyView() }
      HStack(spacing: 8) {
        PaperDot(color: store.status?.connected == true && !store.isWhatsAppReconnecting ? LimmrTheme.green : LimmrTheme.ink3)
        if !rail { Text(store.whatsappFooter).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3) }
      }.frame(maxWidth: .infinity, alignment: rail ? .center : .leading).padding(.horizontal, rail ? 0 : 10).padding(.top, 6).help(store.whatsappFooter)
    }.padding(.horizontal, rail ? 8 : 12).padding(.vertical, 12).background(LimmrTheme.canvas)
  }
  @ViewBuilder private func section(_ text: String) -> some View {
    if rail { Divider().padding(.vertical, 8).accessibilityLabel(text) }
    else { Text(text).font(LimmrTheme.font(12, .semibold)).foregroundStyle(LimmrTheme.ink3).padding(.horizontal, 10).padding(.top, 16).padding(.bottom, 4) }
  }
  private func row<T: View>(_ text: String, _ symbol: String, selected: Bool, action: @escaping () -> Void, @ViewBuilder trailing: () -> T) -> some View {
    Button(action: action) {
      HStack(spacing: 12) {
        Image(systemName: symbol).font(.system(size: 16)).frame(width: 18)
        if !rail {
          Text(text).font(LimmrTheme.font(14, selected ? .semibold : .medium)).lineLimit(2)
          Spacer(minLength: 0); trailing()
        }
      }.foregroundStyle(selected ? LimmrTheme.ink : LimmrTheme.ink2).padding(.horizontal, 10).frame(height: 36)
        .background(selected ? LimmrTheme.navOn : .clear, in: RoundedRectangle(cornerRadius: 10)).contentShape(Rectangle())
        .overlay(alignment: .topTrailing) {
          if rail && text == "Tasks" && TaskListPresentation.open(store.home.tasks).contains(where: \.needsYou) { PaperMarker().padding(2) }
        }
    }.buttonStyle(.plain).help(rowHint(text)).accessibilityLabel(rowHint(text)).accessibilityAddTraits(selected ? [.isSelected] : [])
  }
  private func rowHint(_ text: String) -> String {
    if text == "Tasks" { return "Tasks, \(TaskListPresentation.open(store.home.tasks).filter(\.needsYou).count) need you" }
    if let agent = AIIdentity.rows(store.agents, sync: store.connectionSync).first(where: { AIIdentity.name($0, sync: store.connectionSync) == text }) {
      return text + ", " + (AIIdentity.isPresent(agent, now: store.presentationNow) ? "working" : store.lastSeen(agent))
    }
    return text
  }
}

struct PaperSearch: View {
  @Bindable var store: AppStore
  @Binding var query: String
  var prompt: String
  @FocusState private var focused: Bool
  var body: some View {
    HStack(spacing: 8) {
      Image(systemName: "magnifyingglass").foregroundStyle(LimmrTheme.ink3)
      TextField(prompt, text: $query).textFieldStyle(.plain).focused($focused).font(LimmrTheme.font(14)).accessibilityLabel(prompt)
      if query.isEmpty { Text("⌘F").font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3) }
    }.padding(.horizontal, 12).frame(height: 32).background(LimmrTheme.surface, in: RoundedRectangle(cornerRadius: 12))
      .onChange(of: store.searchRequest) { _, _ in focused = true; store.pendingSearchPage = nil }
      .onChange(of: store.escapeRequest) { _, _ in query = ""; focused = false }
  }
}
struct PaperPageHeader<T: View>: View {
  var title: String
  var subtitle = ""
  @ViewBuilder var trailing: T
  var body: some View {
    ViewThatFits(in: .horizontal) {
      HStack(spacing: 12) { heading; Spacer(minLength: 8); trailing }
      VStack(alignment: .leading, spacing: 12) { heading; trailing }
    }.padding(.bottom, 12)
  }
  private var heading: some View {
    HStack(spacing: 12) {
      Text(title).font(LimmrTheme.TypeScale.page).tracking(-0.48).fixedSize()
      if !subtitle.isEmpty { Text(subtitle).font(LimmrTheme.font(14)).foregroundStyle(LimmrTheme.ink2).lineLimit(2) }
    }
  }
}
