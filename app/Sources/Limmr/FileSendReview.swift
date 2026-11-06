import LimmrKit
import SwiftUI

// A shared review surface keeps file metadata available across all destinations,
// including captionless sends, without changing the Home or Circles views.
struct FileSendReviewBanner: View {
  @Bindable var store: AppStore
  @State private var presented = false
  private var files: [CircleSuggestion] {
    store.fileReviewSuggestions
  }
  var body: some View {
    if !files.isEmpty {
      HStack {
        Label("\(files.count) file \(files.count == 1 ? "send needs" : "sends need") your approval", systemImage: "paperclip")
          .font(.callout)
        Spacer()
        Button("Review files") { presented = true }.buttonStyle(NativeGlassButtonStyle())
      }
      .padding(.vertical, 12)
      .popover(isPresented: $presented) {
        VStack(alignment: .leading, spacing: 16) {
          Text("Review file sends").font(.title2.bold())
          ScrollView {
            VStack(spacing: 16) {
              ForEach(files) { FileSendReviewCard(store: store, item: $0) }
            }
          }
          Text("Approved files queue on the sending device. Check Activity for delivery.")
            .font(.caption).foregroundStyle(AccessibleSecondary())
        }
        .padding(24).frame(width: 660, height: 560).dismissOnEscape()
      }
    }
  }
}

struct FileSendReviewCard: View {
  @Bindable var store: AppStore
  let item: CircleSuggestion
  @State private var text: String
  @State private var editing = false
  init(store: AppStore, item: CircleSuggestion) {
    self.store = store; self.item = item
    _text = State(initialValue: store.suggestionText(item))
  }
  var body: some View {
    Panel {
      VStack(alignment: .leading, spacing: 12) {
        Text("\(store.agentName(item.createdBy)) → \(store.chatName(item.chatJid ?? ""))")
          .font(.headline)
        if let attachment = item.attachment {
          FileApprovalPreview(file: attachment, target: store.chatName(item.chatJid ?? ""))
        }
        if editing {
          TextEditor(text: $text).font(.body).frame(minHeight: 80)
            .accessibilityLabel("File caption")
        } else if !text.isEmpty {
          Text(text).font(.body).lineSpacing(4).textSelection(.enabled)
        }
        HStack(spacing: 12) {
          Spacer(minLength: 0)
          Button(editing ? "Done editing" : "Edit caption") { editing.toggle() }.limmrButton()
          Button("Deny") { Task { await store.decideSuggestion(item, send: false) } }
            .buttonStyle(NativeGlassButtonStyle())
          Button("Approve file") {
            Task { await store.decideSuggestion(item, send: true, text: text == store.suggestionText(item) ? nil : text) }
          }.buttonStyle(NativeGlassButtonStyle(prominent: true))
        }.disabled(store.busy)
        ActionFeedback(store: store, action: "suggestion-\(item.id)")
      }
    }
  }
}
