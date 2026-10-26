import AppKit
import SwiftUI
import LimmrKit

struct QueuedDraftRow: View {
  @Environment(\.accessibilityReduceMotion) private var systemReduceMotion
  @Environment(\.snapshotReducedMotion) private var snapshotReduceMotion
  private var reduceMotion: Bool { systemReduceMotion || snapshotReduceMotion }
  @Bindable var store: AppStore
  let item: CircleSuggestion
  @State private var draft: QueuedDraftState
  @State private var editing = false
  @State private var offset = 0.0
  @State private var startOffset = 0.0
  @State private var rowWidth = 600.0
  init(store: AppStore, item: CircleSuggestion) {
    self.store = store
    self.item = item
    _draft = State(
      initialValue: QueuedDraftState(
        item, circle: store.circles.first { $0.id == item.circleId },
        fallback: store.suggestionText(item)))
  }
  var body: some View {
    content.background { AppBackground() }.offset(x: offset)
      .background(alignment: .trailing) {
        if offset < -10 {
          Button("Dismiss") { dismiss(.context) }.limmrButton().font(.callout)
            .frame(width: 90).frame(maxHeight: .infinity)
            .background(.secondary.opacity(0.08), in: RoundedRectangle(cornerRadius: 8))
        }
      }
      .onGeometryChange(for: Double.self) {
        $0.size.width
      } action: {
        rowWidth = $0
      }
      .simultaneousGesture(
        DragGesture(minimumDistance: 20).onChanged { value in
          if abs(value.translation.width) > abs(value.translation.height) {
            offset = min(0, startOffset + value.translation.width)
          }
        }.onEnded { _ in finishSwipe(width: rowWidth) }
      )
      .background(
        HorizontalSwipeCapture(
          changed: { delta in
            offset = min(0, offset + delta)
          }, ended: { finishSwipe(width: rowWidth) }
        ).allowsHitTesting(false)
      )
      .clipped()

      .contextMenu { Button("Dismiss") { dismiss(.context) } }
      .accessibilityAction(named: "Dismiss") { dismiss(.accessibility) }
  }
  private var content: some View {
    VStack(alignment: .leading, spacing: 8) {
      if item.kind == "check_in" || item.kind == "nudge" {
        Text(item.kind == "check_in" ? "Check-in" : "Nudge").font(AppType.caption).foregroundStyle(AccessibleSecondary())
        if !item.why.isEmpty { Text(item.why).font(.callout) }
      }
      HStack(spacing: 8) {
        if let id = store.personID(jid: item.chatJid) {
          PersonAvatar(store: store, id: id, name: store.personName(id), size: 26)
        }
        Text(
          "To \(item.chatJid.map(store.chatName) ?? store.circleName(item.circleId)) · \(store.circleName(item.circleId))"
        )
        .font(.callout.weight(.semibold)).lineLimit(1)
        Spacer(minLength: 0)
        if draft.variations.count > 1 {
          HStack(spacing: 6) {
            Button {
              draft.selectVariation(-1)
            } label: {
              Image(systemName: "chevron.left").frame(width: 20, height: 20).contentShape(Rectangle())
            }
            .accessibilityLabel("Previous variation")
            Text("\(draft.variation + 1)/\(draft.variations.count)").monospacedDigit()
            Button {
              draft.selectVariation(1)
            } label: {
              Image(systemName: "chevron.right").frame(width: 20, height: 20).contentShape(Rectangle())
            }
            .accessibilityLabel("Next variation")
          }.font(.caption).limmrButton().foregroundStyle(AccessibleSecondary())
        }
      }
      if draft.text.isEmpty { Text("Waiting for an update draft from your AI.").font(.callout).foregroundStyle(AccessibleSecondary()) }
      if editing {
        DraftBodyEditor(
          text: Binding(get: { draft.text }, set: { draft.edits[draft.variation] = $0 }),
          done: { editing = false }
        )
        .id(draft.variation).frame(height: 58)
      } else {
        Button {
          editing = true
        } label: {
          Text(draft.text).font(.body).multilineTextAlignment(.leading)
            .frame(maxWidth: .infinity, minHeight: 24, alignment: .topLeading).fixedSize(
              horizontal: false, vertical: true
            )
            .contentShape(Rectangle())
        }.limmrButton().accessibilityLabel("Edit message: \(draft.text)")
      }
      HStack(alignment: .center) {
        Spacer(minLength: 0)
        if item.kind == "check_in" || item.kind == "nudge" {
          Button("Edit") { editing = true }.limmrButton().disabled(store.busy)
          Button("Skip") { Task { await store.skipTaskSuggestion(item) } }.limmrButton().disabled(store.busy)
        }
        Button("Send") {
          let request = draft.request
          Task {
            await store.decideSuggestion(
              item, send: true, text: request.text,
              variation: request.variation)
          }
        }.buttonStyle(.borderedProminent).controlSize(.small)
          .disabled(
            store.busy || store.attemptedSuggestionSends.contains(item.id)
              || draft.text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
      }
      ActionFeedback(store: store, action: "suggestion-\(item.id)")
    }.padding(.vertical, 8).padding(.horizontal, 2)
      .frame(maxWidth: .infinity, alignment: .topLeading)
  }
  private func dismiss(_ trigger: DraftDismissTrigger) {
    Task { await store.dismissQueued(item, trigger: trigger) }
  }
  private func finishSwipe(width: Double) {
    if HomePresentation.shouldDismiss(translation: offset, width: width) {
      dismiss(.swipe(translation: offset, width: width))
    }
    withAnimation(reduceMotion ? nil : .easeOut(duration: 0.18)) {
      offset = offset < -40 ? -90 : 0
      startOffset = offset
    }
  }
}

/// Native editing gives the first click a caret at the end, rather than selecting the whole draft.
struct DraftBodyEditor: NSViewRepresentable {
  @Binding var text: String
  /// Escape or clicking elsewhere ends editing; the edit itself is kept.
  var done: () -> Void = {}
  func makeCoordinator() -> Coordinator { Coordinator(self) }
  func makeNSView(context: Context) -> NSScrollView {
    let scroll = NSScrollView()
    let view = NSTextView()
    view.isRichText = false
    view.drawsBackground = false
    view.font = .systemFont(ofSize: NSFont.systemFontSize)
    view.textColor = .labelColor
    view.string = text
    view.textContainerInset = NSSize(width: 0, height: 2)
    view.textContainer?.lineFragmentPadding = 0
    view.isVerticallyResizable = true
    view.isHorizontallyResizable = false
    view.autoresizingMask = [.width]
    view.textContainer?.widthTracksTextView = true
    view.delegate = context.coordinator
    view.setAccessibilityLabel("Message body")
    scroll.documentView = view
    scroll.drawsBackground = false
    scroll.hasVerticalScroller = true
    DispatchQueue.main.async { [weak view] in
      guard let view else { return }
      view.window?.makeFirstResponder(view)
      view.setSelectedRange(NSRange(location: view.string.utf16.count, length: 0))
    }
    return scroll
  }
  func updateNSView(_ scroll: NSScrollView, context: Context) {
    context.coordinator.parent = self
    guard let view = scroll.documentView as? NSTextView else { return }
    if view.string != text { view.string = text }
  }
  class Coordinator: NSObject, NSTextViewDelegate {
    var parent: DraftBodyEditor
    init(_ parent: DraftBodyEditor) { self.parent = parent }
    func textDidChange(_ notification: Notification) {
      if let view = notification.object as? NSTextView { parent.text = view.string }
    }
    func textDidEndEditing(_ notification: Notification) { parent.done() }
    func textView(_ view: NSTextView, doCommandBy selector: Selector) -> Bool {
      guard selector == #selector(NSResponder.cancelOperation(_:)) else { return false }
      view.window?.makeFirstResponder(nil)
      return true
    }
  }
}

/// Two-finger horizontal scrolls do not produce SwiftUI DragGesture events on macOS.
/// Observe only this row's bounds, consume horizontal movement and leave vertical scrolling native.
struct HorizontalSwipeCapture: NSViewRepresentable {
  var changed: (Double) -> Void
  var ended: () -> Void
  func makeNSView(context: Context) -> CaptureView { CaptureView(changed: changed, ended: ended) }
  func updateNSView(_ view: CaptureView, context: Context) {
    view.changed = changed
    view.ended = ended
  }
  final class CaptureView: NSView {
    var changed: (Double) -> Void
    var ended: () -> Void
    var monitor: Any?
    var tracking = false
    init(changed: @escaping (Double) -> Void, ended: @escaping () -> Void) {
      self.changed = changed
      self.ended = ended
      super.init(frame: .zero)
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }
    override func viewDidMoveToWindow() {
      super.viewDidMoveToWindow()
      if let monitor {
        NSEvent.removeMonitor(monitor)
        self.monitor = nil
      }
      guard window != nil else { return }
      monitor = NSEvent.addLocalMonitorForEvents(matching: .scrollWheel) { [weak self] event in
        guard let self, event.window === self.window else { return event }
        let inside = self.bounds.contains(self.convert(event.locationInWindow, from: nil))
        if event.phase == .began { self.tracking = false }
        if inside && abs(event.scrollingDeltaX) > abs(event.scrollingDeltaY)
          && abs(event.scrollingDeltaX) > 0
        {
          self.tracking = true
        }
        guard self.tracking else { return event }
        if event.phase == .ended || event.phase == .cancelled {
          self.ended()
          self.tracking = false
        } else if event.momentumPhase.isEmpty {
          self.changed(event.scrollingDeltaX)
        }
        return nil
      }
    }
    isolated deinit { if let monitor { NSEvent.removeMonitor(monitor) } }
  }
}

enum DraftDismissTrigger {
  case swipe(translation: Double, width: Double)
  case context
  case accessibility
}
extension AppStore {
  func dismissQueued(_ item: CircleSuggestion, trigger: DraftDismissTrigger) async {
    if case .swipe(let translation, let width) = trigger,
      !HomePresentation.shouldDismiss(translation: translation, width: width)
    {
      return
    }
    await decideSuggestion(item, send: false)
  }
}
