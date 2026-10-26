import LimmrKit
import SwiftUI

struct SettingsView: View {
  @Environment(\.accessibilityReduceMotion) private var systemReduceMotion
  @Environment(\.snapshotReducedMotion) private var snapshotReduceMotion
  private var reduceMotion: Bool { systemReduceMotion || snapshotReduceMotion }
  @Bindable var store: AppStore
  @Environment(\.snapshotRendering) private var snapshot
  var hiddenKind: HiddenChatKind = .people

  var body: some View {
    HStack(spacing: 0) {
      VStack(alignment: .leading, spacing: 8) {
        TextField("Search settings", text: $store.settingsQuery).textFieldStyle(.roundedBorder)
          .padding(8)
        if !store.settingsQuery.isEmpty {
          ScrollView {
            VStack(alignment: .leading, spacing: 8) {
              ForEach(SettingsSearchEntry.matches(store.settingsQuery, showAccessory: store.accessoryAvailable)) { result in
                Button {
                  store.settingsTab = result.tab
                  store.settingsSearchTarget = result.label
                } label: {
                  VStack(alignment: .leading, spacing: 4) {
                    Text(result.label).font(.callout)
                    Text(result.tab.rawValue).font(AppType.caption).foregroundStyle(AccessibleSecondary())
                  }.frame(maxWidth: .infinity, alignment: .leading).padding(8)
                }.limmrButton()
              }
              if SettingsSearchEntry.matches(store.settingsQuery, showAccessory: store.accessoryAvailable).isEmpty {
                Text("No matching settings").font(AppType.caption).foregroundStyle(AccessibleSecondary()).padding(8)
              }
            }
          }
        } else {
          Group {
            if snapshot {
              VStack(alignment: .leading, spacing: 6) {
                Text("Settings").font(AppType.rowTitle).padding(.horizontal, 8).padding(.vertical, 16)
                ForEach(SettingsTab.allCases) { tab in
                  Button {
                    store.settingsTab = tab
                  } label: {
                    Label(tab.rawValue, systemImage: tab.symbol)
                      .font(.callout).foregroundStyle(.primary)
                      .frame(maxWidth: .infinity, alignment: .leading).padding(8)
                      .background(
                        store.settingsTab == tab ? Color.accentColor.opacity(0.09) : .clear,
                        in: RoundedRectangle(cornerRadius: 8))
                  }.limmrButton()
                }
                Spacer()
              }.padding(.horizontal, 8)
            } else {
              List(selection: $store.settingsTab) {
                ForEach(SettingsTab.allCases) { tab in
                  Label(tab.rawValue, systemImage: tab.symbol).tag(tab)
                }
              }.listStyle(.sidebar)
            }
          }
        }
      }
      .frame(width: 180)
      .frame(maxHeight: .infinity)
      .background { Color.primary.opacity(0.025).ignoresSafeArea(edges: .top) }
      Divider().ignoresSafeArea(edges: .top)
      settingsPage(store.settingsTab)
        .background { AppBackground().ignoresSafeArea(edges: .top) }
    }
    // Same chrome as the main window: no separate pale toolbar strip on top.
    .pageSurface()
    .background(MainWindowSurfaceReader())
    .frame(minWidth: 640, minHeight: 420)
    .sheet(isPresented: $store.showConnect) { ConnectPicker(store: store).dismissOnEscape() }
    .sheet(
      isPresented: Binding(
        get: { store.showRemote && store.connectKind == nil }, set: { store.showRemote = $0 })
    ) { RemoteWizard(store: store).dismissOnEscape() }
    .sheet(item: store.settingsConnectKind) { kind in ConnectView(store: store, kind: kind).dismissOnEscape() }
  }

  private func settingsPage(_ tab: SettingsTab) -> some View {
    Group {
      if tab == .integrations {
        AgentsView(store: store).settingsMatch("Integrations", store: store)
      } else {
        ScrollViewReader { proxy in
          Form {
            switch tab {
            case .integrations: EmptyView()
            case .general: GeneralSettings(store: store)
            case .sources: SourcesSettings(store: store)
            case .people: PeopleSettings(store: store)
            case .briefs: BriefsSettings(store: store)
            case .hiddenChats: HiddenChatsSettings(store: store, kind: hiddenKind)
            case .remote: RemoteSettings(store: store)
            case .health: HealthView(store: store).settingsMatch("Health", store: store)
            case .advanced: AdvancedSettings(store: store)
            }
          }
          .formStyle(.grouped)
          .scrollContentBackground(.hidden)
          .frame(maxWidth: 760, alignment: .leading).frame(maxWidth: .infinity, alignment: .leading)
          .task(id: store.settingsSearchTarget) {
            guard !store.settingsSearchTarget.isEmpty else { return }
            await Task.yield()
            withAnimation(reduceMotion ? nil : .default) { proxy.scrollTo(store.settingsSearchTarget, anchor: .center) }
          }
        }
      }
    }
  }
}

struct GeneralSettings: View {
  @Bindable var store: AppStore
  var body: some View {
    Section {
      VStack(alignment: .leading, spacing: 6) {
        Toggle("Launch Limmr at login", isOn: store.launchAtLoginBinding)
          .disabled(store.system.login.isChanging)
        Text(store.system.login.isChanging ? "Updating…" : store.system.login.status.detail)
          .font(.footnote).foregroundStyle(AccessibleSecondary())
        if store.system.login.status == .requiresApproval {
          Button("Open Login Items") { store.system.login.openLoginItems() }
        }
        if let error = store.system.login.error {
          Text(error).font(.callout).foregroundStyle(.red)
        }
      }
      .settingsMatch("Launch Limmr at login", store: store)
      .task { store.system.login.refresh() }
      .onReceive(NotificationCenter.default.publisher(for: NSApplication.didBecomeActiveNotification)) { _ in
        store.system.login.refresh()
      }
      Stepper(
        "Current chats: last \(store.settings.currentDays) days",
        value: store.settingsBinding(\.currentDays), in: 1...3650
      ).settingsMatch("Current chats", store: store)
      Text("Setup focuses on your life now. Older chats stay available below the current ones.")
        .font(.footnote).foregroundStyle(AccessibleSecondary())
      Picker("Keep activity for", selection: store.settingsBinding(\.retentionDays)) {
        Text("30 days").tag(30)
        Text("90 days").tag(90)
        Text("180 days").tag(180)
        Text("1 year").tag(365)
      }
      .settingsMatch("Keep activity for", store: store)
      Text("People, circles and what your AIs learned stay until you remove them.")
        .font(.footnote).foregroundStyle(AccessibleSecondary())
      LabeledContent("Approval notifications") {
        Button("Enable…") {
          Task { await store.perform { try await store.notifications.authorize() } }
        }
      }.settingsMatch("Approval notifications", store: store)
    } header: {
      SettingsPaneTitle(title: "General")
    }
    .toggleStyle(.switch)
    Section { AboutYouCard(store: store).settingsMatch("About you", store: store) }
    Section {
      Picker("New circles from your own AIs", selection: store.settingsBinding(\.autoCircles)) {
        Text("Add automatically").tag(true)
        Text("Ask me first").tag(false)
      }.settingsMatch("New circles", store: store)
      Stepper(
        "Ask before more than \(store.settings.maxCircles) circles",
        value: store.settingsBinding(\.maxCircles), in: 1...100)
    }
    SettingsFeedback(store: store)
    ActionFeedback(store: store)
  }
}

struct HiddenChatsSettings: View {
  @Bindable var store: AppStore
  @State private var query = ""
  @State var kind: HiddenChatKind = .people
  var body: some View {
    Section {
      Picker("Chat type", selection: $kind) {
        ForEach(HiddenChatKind.allCases) { kind in Text(kind.rawValue).tag(kind) }
      }.pickerStyle(.segmented)
      TextField("Search chats", text: $query).settingsMatch("Hidden chats", store: store)
    } header: {
      SettingsPaneTitle(title: "Hidden chats")
    } footer: {
      Text("Hidden chats are invisible to every integration. Hiding a person hides only your direct chat. Their messages in groups you share stay visible unless you hide that group too.")
    }
    Section {
      CurrentChatSections(store: store, chats: kind.chats(in: store.chats), query: query) { chat in
        Toggle(
          chat.name,
          isOn: store.hiddenChatsBinding(for: [chat.jid])
        )
      }
      if kind.chats(in: store.chats).isEmpty {
        Text(store.chats.isEmpty ? "Link WhatsApp to choose hidden chats." : "No \(kind.rawValue.lowercased()) chats loaded.")
          .foregroundStyle(AccessibleSecondary())
      }
    }
    .toggleStyle(.switch)
    SettingsFeedback(store: store)
  }
}

struct SettingsFeedback: View {
  @Bindable var store: AppStore
  var body: some View {
    ActionFeedback(store: store, action: "settings")
    if store.settingsSaved {
      Text("Saved").font(.callout).foregroundStyle(AccessibleSecondary())
    }
  }
}

struct SettingsPaneTitle: View {
  let title: String
  var body: some View {
    Text(title).font(AppType.pageTitle).foregroundStyle(.primary)
      .textCase(nil).padding(.top, 8).padding(.bottom, 16)
  }
}

enum HiddenChatKind: String, CaseIterable, Identifiable {
  case people = "People"
  case groups = "Groups"
  var id: String { rawValue }
  func chats(in chats: [Chat]) -> [Chat] {
    chats.filter { ($0.jid.hasSuffix("@g.us")) == (self == .groups) }
  }
}
