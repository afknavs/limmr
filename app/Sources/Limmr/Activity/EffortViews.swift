import LimmrKit
import SwiftUI

struct EffortStatus: View {
  let status: String
  let label: String
  var body: some View {
    Text(label).font(AppType.caption.weight(.medium)).foregroundStyle(
      status == "on_you" ? Color.accentColor : .secondary
    )
    .padding(.horizontal, 9).padding(.vertical, 5)
    .background(
      (status == "on_you" ? Color.accentColor : Color.primary).opacity(0.07), in: Capsule())
  }
}

struct EffortCard: View {
  @Environment(\.colorSchemeContrast) private var systemContrast
  @Environment(\.snapshotIncreasedContrast) private var snapshotContrast
  private var increasedContrast: Bool { systemContrast == .increased || snapshotContrast }
  let effort: Effort
  let open: () -> Void
  var body: some View {
    Button(action: open) {
      VStack(alignment: .leading, spacing: 16) {
        HStack(alignment: .top) {
          Text(effort.title).font(AppType.rowTitle).lineLimit(1).truncationMode(.tail)
          Spacer(minLength: 12)
          EffortStatus(status: effort.status, label: effort.statusLabel)
        }
        if !effort.summary.isEmpty {
          Text(effort.summary).font(.callout).foregroundStyle(AccessibleSecondary())
            .fixedSize(horizontal: false, vertical: true)
        }
        if effort.createdBy == "This Mac" || effort.createdBy == "this-mac" {
          Text("Named by This Mac").font(AppType.caption).foregroundStyle(AccessibleSecondary())
        }
        Text(effort.meta).font(AppType.caption).foregroundStyle(AccessibleTertiary())
          .fixedSize(horizontal: false, vertical: true)
      }.frame(maxWidth: .infinity, alignment: .leading).padding(16)
        .background(.primary.opacity(0.025), in: RoundedRectangle(cornerRadius: 10))
        .overlay(
          RoundedRectangle(cornerRadius: 10).stroke(increasedContrast ? Color.primary.opacity(0.65) : Color.secondary.opacity(0.12), lineWidth: increasedContrast ? 1 : 0.5)
        )
        .contentShape(RoundedRectangle(cornerRadius: 10))
    }.limmrButton(.row(radius: 10, bordered: false), id: "effort").accessibilityLabel(
      Spoken.effort(effort))
      .accessibilityValue(effort.statusLabel)
      .accessibilityHint("Open this work and its timeline")
  }
}

struct EffortDetailView: View {
  @Bindable var store: AppStore
  let detail: EffortDetailResponse
  @State private var updated: Effort?
  @State private var trackedTaskId: String?
  @State private var tracking = false
  @Environment(\.dismiss) private var dismiss
  private var effort: Effort { updated ?? detail.effort }
  var body: some View {
    ScrollView {
      VStack(alignment: .leading, spacing: 16) {
        Text(effort.title).font(AppType.pageTitle).lineLimit(1).truncationMode(.tail)
          .help(effort.title)
        EffortStatus(status: effort.status, label: effort.statusLabel)
        HStack(spacing: 16) {
          if effort.status != "done" {
            Button("Not on me") { changeStatus("moving") }.disabled(effort.status == "moving")
            Button("Done", systemImage: "checkmark") { changeStatus("done") }
          }
          if store.effortUndo[effort.id] != nil {
            Button("Undo") { Task { updated = await store.undoEffortStatus(effort.id) ?? updated } }
              .accessibilityLabel("Undo status change for \(effort.title)")
          }
          Button(effort.pinned ? "Unpin" : "Pin", systemImage: effort.pinned ? "pin.slash" : "pin") {
            Task { updated = await store.updateEffort(effort.id, values: ["pinned": !effort.pinned]) ?? updated }
          }
          Button("Hide", systemImage: "eye.slash") {
            Task {
              if await store.updateEffort(effort.id, values: ["hidden": true]) != nil {
                if !store.activityPath.isEmpty { store.clearActivityRoute() }
                else { dismiss() }
              }
            }
          }
        }.limmrButton().disabled(store.busy)
        ActionFeedback(store: store, action: "effort-\(effort.id)")
        if !detail.effort.summary.isEmpty { Text(detail.effort.summary).font(.callout) }
        Text(detail.effort.meta).font(AppType.caption).foregroundStyle(AccessibleSecondary())
        if effort.createdBy != "Limmr" {
          Text("Named by \(effort.createdBy)").font(AppType.caption).foregroundStyle(AccessibleSecondary())
        }
        if let taskId = trackedTaskId ?? detail.taskId {
          TaskWorkspaceView(store: store, id: taskId, embedded: true).id(taskId)
        } else {
          Text("Tasks").font(AppType.blockHeading).foregroundStyle(AccessibleSecondary())
          Button("Track this", systemImage: "checklist") {
            tracking = true
            Task {
              await store.perform(action: "track-effort") {
                let task = try await TaskClient(backend: store.backend).create(.init(effort: effort))
                trackedTaskId = task.id
                await store.refresh()
              }
              tracking = false
            }
          }.limmrButton().disabled(tracking || store.busy)
          ActionFeedback(store: store, action: "track-effort")
        }
        Text("Timeline").font(AppType.blockHeading).padding(.top, 8)
        if detail.timeline.isEmpty {
          Text("Conversations and AI work will appear here as this purpose develops.")
            .font(.callout).foregroundStyle(AccessibleSecondary())
        }
        ForEach(detail.timeline) { item in
          HStack(alignment: .top, spacing: 16) {
            Text(Timestamp.date(item.at)?.formatted(.dateTime.month(.abbreviated).day()) ?? "")
              .font(AppType.caption).foregroundStyle(AccessibleSecondary()).frame(width: 64, alignment: .leading)
            VStack(alignment: .leading, spacing: 6) {
              if let message = item.message {
                Text(message.isFromMe ? "You" : store.senderName(message.sender)).font(AppType.rowTitle)
                  .lineLimit(1).truncationMode(.tail)
                Text(message.displayText)
                  .font(.callout).textSelection(.enabled)
                Text(store.chatName(message.chatJid)).font(AppType.caption).foregroundStyle(AccessibleSecondary())
                  .lineLimit(1).truncationMode(.tail)
              } else if let action = item.action {
                Text(store.agentName(action.agentId) + " · " + action.verb).font(AppType.rowTitle)
                if let note = action.note { Text(note).font(.callout).foregroundStyle(AccessibleSecondary()) }
              }
            }
          }
          Divider()
        }
        Text("Titles and status inferred locally unless edited · \(effort.createdBy)")
          .font(AppType.caption).foregroundStyle(AccessibleSecondary())
      }.padding(24).frame(maxWidth: 740, alignment: .leading).frame(maxWidth: .infinity)
    }
  }
  private func changeStatus(_ status: String) {
    Task { updated = await store.setEffortStatus(effort.id, from: effort.status, to: status) ?? updated }
  }

}

extension AppStore {
  func updateEffort(_ id: String, values: [String: Bool]) async -> Effort? {
    var updated: Effort?
    let source = backend
    let generation = backendGeneration
    await perform(action: "effort-\(id)") {
      let data = try await source.request("/v1/efforts/\(id)", method: "PATCH", body: Wire.data(values))
      guard generation == self.backendGeneration else { return }
      updated = try Wire.decode(Effort.self, from: data)
      self.effortRevision += 1
      await self.refresh()
      await self.refreshHomeEfforts()
    }
    return updated
  }
}

struct EffortSheet: View {
  @Bindable var store: AppStore
  let id: String
  @Environment(\.dismiss) private var dismiss
  @State private var detail: EffortDetailResponse?
  @State private var error: String?
  var body: some View {
    VStack(spacing: 0) {
      HStack {
        Spacer()
        Button("Done") { dismiss() }
      }.padding(24)
      if let detail {
        EffortDetailView(store: store, detail: detail)
      } else if let error {
        ContentUnavailableView(error, systemImage: "text.magnifyingglass")
      } else {
        ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
      }
    }.frame(minWidth: 440, idealWidth: 740, maxWidth: 740,
            minHeight: 300, idealHeight: 620, maxHeight: 620)
      .task {
        do { detail = try await store.backend.get("/v1/efforts/\(id)") } catch {
          self.error = "This effort could not be loaded."
        }
      }
  }
}

@MainActor extension AppStore {
  private func displayEffortStatus(_ id: String, _ status: String) {
    for i in homeEfforts.indices where homeEfforts[i].id == id { homeEfforts[i].status = status }
    for i in home.circleUpdates.indices where home.circleUpdates[i].effortId == id { home.circleUpdates[i].status = status }
    effortRevision += 1
  }
  func setEffortStatus(_ id: String, from old: String, to status: String, offerUndo: Bool = true) async -> Effort? {
    let generation = backendGeneration
    displayEffortStatus(id, status)
    do {
      let data = try await backend.request("/v1/efforts/\(id)", method: "PATCH", body: Wire.data(["status": status]))
      let value = try Wire.decode(Effort.self, from: data)
      guard generation == backendGeneration else { return nil }
      displayEffortStatus(id, value.status)
      if offerUndo {
        effortUndo[id] = old
        Task { [weak self] in
          try? await Task.sleep(for: .seconds(6))
          guard let self, generation == self.backendGeneration, self.effortUndo[id] == old else { return }
          self.effortUndo[id] = nil
        }
      }
      return value
    } catch {
      guard generation == backendGeneration else { return nil }
      displayEffortStatus(id, old)
      failWithoutRetry("Couldn’t update this work. Try again.")
      return nil
    }
  }
  func undoEffortStatus(_ id: String) async -> Effort? {
    guard let old = effortUndo.removeValue(forKey: id) else { return nil }
    let current = homeEfforts.first { $0.id == id }?.status ?? "moving"
    return await setEffortStatus(id, from: current, to: old, offerUndo: false)
  }
}
