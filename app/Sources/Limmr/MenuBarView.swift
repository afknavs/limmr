import SwiftUI
import LimmrKit

struct MenuBarView: View {
  @Bindable var store: AppStore
  var open: () -> Void = {}
  var stateOverride: EngineSurfaceState? = nil
  private var state: EngineSurfaceState { stateOverride ?? store.engineSurfaceState }
  var body: some View {
    ScrollView {
      VStack(alignment: .leading, spacing: 18) {
        HStack {
          Label("Limmr", systemImage: "bubble.left.and.text.bubble.right").font(
            .system(size: 15, weight: .semibold))
          Spacer()
          Badge(text: "\(store.menuWaitingCount) waiting", accent: store.menuWaitingCount > 0)
        }
        HStack(spacing: 32) {
          menuCount(store.menuWatchCount, label: "Watches")
          menuCount(store.pending.count, label: "Approvals")
        }
        EngineStatusBanner(state: state) {
          if state.inProgress || state == .connected { open() }
          else {
            Task { await store.connectionAction(state) }
            if state == .loggedOut { open() }
          }
        }
        if store.pending.isEmpty && store.home.waiting.isEmpty {
          EmptyState(
            symbol: "checkmark.bubble", title: "All caught up",
            detail: "Watch updates and requests for your approval will appear here when they arrive."
          ).frame(height: 220)
        }
        ForEach(store.pending.prefix(3)) { item in
          VStack(alignment: .leading, spacing: 11) {
            HStack {
              Text(store.chatName(item.chatJid)).font(.system(size: 13, weight: .semibold))
              Spacer()
              Text(store.agentName(item.agentId)).font(.system(size: 11)).foregroundStyle(AccessibleSecondary())
            }
            Text(item.text).font(.system(size: 12)).lineSpacing(4).fixedSize(
              horizontal: false, vertical: true)
            if let attachment = item.attachment {
              Label(attachment.name, systemImage: "paperclip").font(.caption)
            }
            HStack {
              Spacer()
              Button("Deny") { Task { await store.resolve(item.id, approve: false) } }.buttonStyle(
                NativeGlassButtonStyle())
                .accessibilityLabel("Deny request to \(store.chatName(item.chatJid))")
                .disabled(store.busy || state != .connected)
              Button("Approve") { Task { await store.resolve(item.id, approve: true) } }.buttonStyle(
                NativeGlassButtonStyle(prominent: true))
                .accessibilityLabel("Approve request to \(store.chatName(item.chatJid))")
                .disabled(store.busy || state != .connected)
            }
          }.padding(15).background(.quaternary.opacity(0.35), in: RoundedRectangle(cornerRadius: 14))
        }
        if !store.home.waiting.isEmpty {
          VStack(alignment: .leading, spacing: 10) {
            BlockHeading(title: "Waiting")
            ForEach(store.home.waiting) { item in
              WaitingTile(store: store, item: item)
            }
          }
        }
        ActionFeedback(store: store, action: store.action)
      }.padding(20)
    }
    .safeAreaInset(edge: .bottom, spacing: 0) {
      VStack(spacing: 12) {
        Divider()
        Button {
          store.selectPage(.overview)
          open()
        } label: {
          HStack {
            Text("Open Limmr")
            Spacer()
            Image(systemName: "arrow.up.right")
          }
        }.limmrButton().font(.system(size: 13)).accessibilityLabel("Open Limmr")
      }.padding(20).background(Color(nsColor: .windowBackgroundColor))
    }
    .frame(width: 390)
      .frame(maxHeight: 620)
      .background(Color(nsColor: .windowBackgroundColor))
  }
  private func menuCount(_ count: Int, label: String) -> some View {
    VStack(alignment: .leading, spacing: 4) {
      Text(count.formatted()).font(.title2.weight(.semibold)).monospacedDigit()
      Text(label).font(.caption).foregroundStyle(AccessibleSecondary())
    }.accessibilityElement(children: .ignore).accessibilityLabel("\(count) \(label)")
  }
}

extension AppStore {
  var menuWaitingCount: Int { pending.count + home.waiting.count }
  var menuWatchCount: Int {
    HomeTriggerPresentation.armedWatches(home.watches, now: demo ? Timestamp.date(DemoData.now)! : Date()).count
  }
}
