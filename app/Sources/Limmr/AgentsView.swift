import LimmrKit
import SwiftUI

struct AgentsView: View {
  @Bindable var store: AppStore
  var body: some View {
    Group {
      if let agent = store.currentAgent {
        AgentDetail(store: store, agent: agent, narrow: true).id(agent.id).padding(24)
      } else {
        Form {
          YourDotSection(store: store)
          Section {
            if store.agents.isEmpty {
              QuietBlockState(title: "Your AIs connect here", detail: "Connect an integration to choose which conversations it can read.")
            }
            ForEach(store.agents) { agent in
              Button {
                store.selectedAgent = agent.id
              } label: {
                HStack(spacing: 16) {
                  EntityIconButton(
                    store: store, bucket: "agents", id: agent.id, icon: agent.icon,
                    fallback: agent.symbol)
                  VStack(alignment: .leading, spacing: 4) {
                    Text(agent.name).font(AppType.rowTitle).lineLimit(1).truncationMode(.tail)
                    Text(store.scopeSummary(agent)).font(.callout).foregroundStyle(AccessibleSecondary())
                  }
                  Spacer()
                  Image(systemName: "chevron.right").font(AppType.caption).foregroundStyle(AccessibleSecondary())
                }.padding(.vertical, 6).contentShape(Rectangle())
              }.limmrButton()
            }
          } header: {
            SettingsPaneTitle(title: "Your AIs")
          }
          Section {
            Button("Connect an integration", systemImage: "plus") { store.showConnect = true }
            SetupGuideLink()
          }
        }.formStyle(.grouped).scrollContentBackground(.hidden)
      }
    }
    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    .task(id: store.selectedAgent) {
      // SnapshotRunner already awaited this fixture; avoid racing capture with a second load.
      if !store.snapshot || store.permissionsAgentID != store.selectedAgent {
        await store.loadPermissions()
      }
    }
  }

}

struct AgentDetail: View {
  @Bindable var store: AppStore
  let agent: Agent
  let narrow: Bool
  @State private var chooseChats = false
  @State private var confirmRemoval = false
  @State private var rotatedToken: String?
  @State private var more = false
  @State private var nameEditor = AgentNameEditor()
  @FocusState private var nameFocused: Bool
  @Environment(\.snapshotExpandedOptions) private var expandedOptions

  init(store: AppStore, agent: Agent, narrow: Bool, nameEditor: AgentNameEditor = AgentNameEditor())
  {
    self.store = store
    self.agent = agent
    self.narrow = narrow
    _nameEditor = State(initialValue: nameEditor)
  }

  var body: some View {
    ScrollView {
      VStack(alignment: .leading, spacing: 24) {
        if narrow {
          Button("All integrations", systemImage: "chevron.left") { store.selectedAgent = nil }
            .limmrButton()
        }
        HStack(spacing: 16) {
          EntityIconButton(
            store: store, bucket: "agents", id: agent.id, icon: agent.icon, fallback: agent.symbol)
          VStack(alignment: .leading, spacing: 6) {
            if nameEditor.editing {
              TextField("Integration name", text: $nameEditor.draft)
                .font(AppType.pageTitle).textFieldStyle(.plain)
                .focused($nameFocused)
                .disabled(nameEditor.saving)
                .onSubmit {
                  Task { await nameEditor.save { try await store.renameAgent(agent.id, name: $0) } }
                }
                .onExitCommand { nameEditor.cancel() }
              Text(nameEditor.saving ? "Saving…" : "Return to save · Esc to cancel")
                .font(AppType.caption).foregroundStyle(AccessibleSecondary())
            } else {
              Button {
                nameEditor.begin(agent.name)
                nameFocused = true
              } label: {
                HStack(spacing: 8) {
                  Text(agent.name).font(AppType.pageTitle).lineLimit(1).truncationMode(.tail)
                  Image(systemName: "pencil").font(.callout).foregroundStyle(AccessibleSecondary())
                }
              }
              .limmrButton()
              .accessibilityLabel("Rename \(agent.name)")
              .help("Rename integration")
            }
            if let error = nameEditor.error {
              Text(error).font(.callout).foregroundStyle(AccessibleSecondary()).fixedSize(
                horizontal: false, vertical: true)
            }
          }
          Spacer()
          Text(store.lastSeen(agent)).font(.callout).foregroundStyle(AccessibleSecondary())
        }
        AIPresetChoice(
          preset: Binding(
            get: { agent.preset ?? "given" },
            set: { kind in
              Task {
                await store.switchPreset(kind, agentID: agent.id)
              }
            })
        ).disabled(store.busy)
        if agent.preset != "own" { IntegrationAccess(store: store, agent: agent) }
        if store.permissionsAgentID == agent.id {
          DisclosureGroup("More options", isExpanded: $more) {
            VStack(alignment: .leading, spacing: 16) {
              VStack(alignment: .leading, spacing: 0) {
                VStack(alignment: .leading, spacing: 24) {
                  VStack(alignment: .leading, spacing: 16) {
                    question("Can it send messages?")
                    Picker(
                      "Can it send messages?", selection: store.permissionBinding(\.requestSend)
                    ) {
                      Text("Off").tag(false)
                      Text("Use circle rules").tag(true)
                    }
                    .pickerStyle(.segmented).labelsHidden()
                  }
                }
              }

              VStack(alignment: .leading, spacing: 16) {
                question("Individual chat limits")
                Picker("Individual chat limits", selection: store.permissionBinding(\.chats.mode)) {
                  Text("All chats").tag("all")
                  Text("Only some").tag("only")
                  Text("All except").tag("all_except")
                }
                .pickerStyle(.segmented).labelsHidden()
                HStack {
                  Text(scopeSummary).font(.callout).foregroundStyle(AccessibleSecondary())
                  Spacer()
                  if store.permissions.chats.mode != "all" {
                    Button("Choose chats…") { chooseChats = true }
                      .buttonStyle(NativeGlassButtonStyle())
                  }
                }
              }
              Divider()
              capability("Read messages", store.permissionBinding(\.readMessages))
              capability("Search chats", store.permissionBinding(\.search))
              capability("Read the people map", store.permissionBinding(\.readPeople))
              capability("Add notes about people", store.permissionBinding(\.writePeople))
              capability("Save summaries", store.permissionBinding(\.saveSummaries))
              capability("Download media", store.permissionBinding(\.downloadMedia))
              HStack {
                Button("Rotate credential") { Task { await rotate() } }
                Button("Revoke access") {
                  Task {
                    await store.perform {
                      try await store.backend.post("/v1/agents/\(agent.id)/revoke")
                      await store.refresh()
                    }
                  }
                }
                Button("Remove integration", role: .destructive) { confirmRemoval = true }
              }
              .buttonStyle(NativeGlassButtonStyle())
              if let rotatedToken {
                DisclosureGroup("New credential · shown once") {
                  CopyField(title: "Reconnect integration", value: rotatedToken)
                }
              }
              ActionFeedback(store: store)
            }
            .padding(.top, 16)
          }
          .font(.callout).disabled(store.busy)
          ActionFeedback(store: store, action: "permissions")
          if store.permissionsSaved {
            Text("Saved").font(.callout).foregroundStyle(AccessibleSecondary())
          }
        } else {
          if store.error == nil { ProgressView("Loading access settings…") }
          ActionFeedback(store: store, action: "permissions")
        }
      }
      .padding(.bottom, 32)
    }
    .frame(maxWidth: 760)
    .onAppear { more = expandedOptions }
    .sheet(isPresented: $chooseChats) { ChatPicker(store: store).dismissOnEscape() }
    .confirmationDialog("Remove \(agent.name)?", isPresented: $confirmRemoval) {
      Button("Remove integration", role: .destructive) {
        Task {
          await store.perform {
            _ = try await store.backend.request(
              "/v1/agents/\(agent.id)", method: "DELETE", body: nil)
            store.selectedAgent = nil
            await store.refresh()
          }
        }
      }
    }
  }

  private var scopeSummary: String {
    var draft = agent
    draft.permissions = store.permissions
    return store.scopeSummary(draft)
  }
  private func question(_ title: String) -> some View { Text(title).font(AppType.rowTitle) }
  private func capability(_ title: String, _ value: Binding<Bool>) -> some View {
    Toggle(title, isOn: value).toggleStyle(.switch).controlSize(.small)
  }
  private func rotate() async {
    await store.perform {
      let data = try await store.backend.request(
        "/v1/agents/\(agent.id)/rotate", method: "POST", body: nil)
      struct Credential: Decodable { var token: String }
      rotatedToken = try Wire.decode(Credential.self, from: data).token
    }
  }
}
