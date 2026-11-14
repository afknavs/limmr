import SwiftUI
import LimmrKit

struct ApprovalCard: View {
  @Bindable var store: AppStore
  let approval: Approval
  @State private var text: String
  @State private var editing = false

  init(store: AppStore, approval: Approval) {
    self.store = store
    self.approval = approval
    _text = State(initialValue: approval.text)
  }

  private var recipientName: String {
    approval.firstContact == true ? (approval.firstContactPerson?.name ?? store.chatName(approval.chatJid)) : store.chatName(approval.chatJid)
  }

  var body: some View {
    Panel {
      VStack(alignment: .leading, spacing: 12) {
        identity
        if let line = approval.firstContactLine {
          Label(line, systemImage: "person.crop.circle.badge.plus")
            .font(.callout.weight(.medium)).fixedSize(horizontal: false, vertical: true)
          Text("This starts a conversation. Reply access follows the AI’s existing permissions.")
            .font(.callout).foregroundStyle(AccessibleSecondary())
        }
        if let attachment = approval.attachment {
          FileApprovalPreview(file: attachment, target: recipientName)
        }
        if editing {
          TextEditor(text: $text).font(.body).frame(minHeight: 80)
        } else {
          Text(text).font(.body).lineSpacing(4).textSelection(
            .enabled)
        }
        HStack(spacing: 12) {
          Spacer(minLength: 0)
          Button(editing ? "Done editing" : "Edit") { editing.toggle() }
            .limmrButton()
          Button("Deny") {
            Task { await store.resolve(approval.id, approve: false) }
          }
          .buttonStyle(NativeGlassButtonStyle())
          Button("Approve") {
            Task {
              await store.resolve(
                approval.id, approve: true, text: editing || text != approval.text ? text : nil)
            }
          }
          .buttonStyle(NativeGlassButtonStyle(prominent: true))
        }
        .disabled(store.busy)
        ActionFeedback(store: store, action: "approval-\(approval.id)")
      }
    }

  }

  private var identity: some View {
    HStack(spacing: 12) {
      EntityIconButton(store: store, bucket: "agents", id: approval.agentId, icon: store.agents.first { $0.id == approval.agentId }?.icon, fallback: store.agents.first { $0.id == approval.agentId }?.symbol ?? "puzzlepiece.extension")
      Text("\(store.agentName(approval.agentId)) → \(recipientName)")
        .font(.headline).fixedSize(horizontal: false, vertical: true)
    }
  }
}
