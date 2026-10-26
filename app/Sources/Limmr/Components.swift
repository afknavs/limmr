import SwiftUI
import LimmrKit

/// One hierarchy for the main window and Settings. Native control text keeps its system sizing.
enum AppType {
  static let pageTitle = Font.system(size: 26, weight: .bold)
  static let blockHeading = Font.system(size: 13, weight: .semibold)
  static let rowTitle = Font.system(size: 13, weight: .medium)
  static let caption = Font.system(size: 11)
}

struct CountLabel: View {
  let value: Int
  let title: String
  var prominent = false
  var wrapsTitle = false
  var body: some View {
    VStack(alignment: .leading, spacing: 4) {
      Text(value.formatted()).font(prominent ? .system(size: 20, weight: .semibold, design: .rounded) : AppType.rowTitle)
        .monospacedDigit().lineLimit(1)
      Text(title).font(AppType.caption).foregroundStyle(AccessibleSecondary()).lineLimit(wrapsTitle ? 2 : 1)
        .truncationMode(.tail).help(title)
    }.accessibilityElement(children: .ignore).accessibilityLabel(Spoken.count(value, title))
  }
}

struct QuietBlockState: View {
  let title: String
  let detail: String
  var body: some View {
    VStack(alignment: .leading, spacing: 8) {
      Text(title).font(AppType.rowTitle)
      Text(detail).font(.callout).foregroundStyle(AccessibleSecondary())
        .fixedSize(horizontal: false, vertical: true)
    }.padding(16).frame(maxWidth: .infinity, alignment: .leading)
      .background(.primary.opacity(0.025), in: RoundedRectangle(cornerRadius: 8))
  }
}

struct AppBackground: View {
  var surface = false
  @Environment(\.colorScheme) private var scheme
  var body: some View {
    Color(white: scheme == .dark ? (surface ? 0.15 : 0.105) : (surface ? 1 : 0.95))
  }
}
struct Panel<Content: View>: View {
  @Environment(\.snapshotRendering) private var snapshot
  var padding: CGFloat = 20
  var height: CGFloat? = nil
  @ViewBuilder var content: Content
  var body: some View {
    VStack(alignment: .leading, spacing: 0) { content }
      .padding(padding)
      .frame(maxWidth: .infinity, alignment: .leading)
      .frame(height: height, alignment: .topLeading)
      .background {
        if snapshot {
          AppBackground(surface: true).clipShape(
            RoundedRectangle(cornerRadius: 18, style: .continuous))
        } else {
          RoundedRectangle(cornerRadius: 18, style: .continuous).fill(.regularMaterial)
        }
      }
  }
}
struct AgentIcon: View {
  let symbol: String
  var size: CGFloat = 32
  var body: some View {
    Image(systemName: symbol).font(.system(size: size * 0.43, weight: .medium)).frame(
      width: size, height: size
    ).background(.quaternary.opacity(0.6), in: RoundedRectangle(cornerRadius: size * 0.28))
  }
}
struct Badge: View {
  let text: String
  var accent = false
  var body: some View {
    Text(text).font(.system(size: 11, weight: .medium)).padding(.horizontal, 9).padding(
      .vertical, 5
    ).foregroundStyle(accent ? Color.accentColor : Color.secondary).background(
      (accent ? Color.accentColor : Color.secondary).opacity(0.09), in: Capsule())
  }
}
struct EmptyState: View {
  let symbol: String
  let title: String
  let detail: String
  var body: some View {
    VStack(spacing: 12) {
      Image(systemName: symbol).font(.system(size: 32, weight: .light)).foregroundStyle(AccessibleSecondary())
      Text(title).font(.title3.weight(.medium))
      Text(detail).foregroundStyle(AccessibleSecondary()).multilineTextAlignment(.center).frame(maxWidth: 340)
    }.padding(40).frame(maxWidth: .infinity, maxHeight: .infinity)
  }
}
struct CopyField: View {
  let title: String
  let value: String
  @State private var copied = false
  var body: some View {
    VStack(alignment: .leading, spacing: 8) {
      Text(title).font(.caption).foregroundStyle(AccessibleSecondary())
      HStack(spacing: 12) {
        Text(value).font(.system(size: 13, design: .monospaced)).textSelection(.enabled).lineLimit(
          3)
        Spacer(minLength: 8)
        Button {
          NSPasteboard.general.clearContents()
          NSPasteboard.general.setString(value, forType: .string)
          copied = true
        } label: {
          Image(systemName: copied ? "checkmark" : "doc.on.doc")
        }.buttonStyle(.borderless).help("Copy \(title)")
      }.padding(13).background(.quaternary.opacity(0.4), in: RoundedRectangle(cornerRadius: 10))
    }
  }
}
struct StepRow: View {
  let number: Int
  let title: String
  let detail: String
  var body: some View {
    HStack(alignment: .top, spacing: 14) {
      Text("\(number)").font(.system(size: 12, weight: .semibold)).frame(width: 26, height: 26)
        .background(.quaternary, in: Circle())
      VStack(alignment: .leading, spacing: 5) {
        Text(title).font(.system(size: 14, weight: .medium))
        Text(detail).font(.system(size: 13)).foregroundStyle(AccessibleSecondary()).fixedSize(
          horizontal: false, vertical: true)
      }
    }
  }
}

private struct SnapshotRenderingKey: EnvironmentKey { static let defaultValue = false }
extension EnvironmentValues {
  var snapshotRendering: Bool {
    get { self[SnapshotRenderingKey.self] }
    set { self[SnapshotRenderingKey.self] = newValue }
  }
}
/// Interactive controls use real Liquid Glass. Offscreen print capture cannot
/// composite glass, so exports use the system's bordered accessibility equivalent
/// rather than fabricating glass with materials or strokes.
struct NativeGlassButtonStyle: PrimitiveButtonStyle {
  var prominent = false
  var tile = false
  @Environment(\.snapshotRendering) private var snapshot
  func makeBody(configuration: Configuration) -> some View {
    let button = Button(role: configuration.role, action: configuration.trigger) {
      configuration.label.foregroundStyle(prominent ? Color.white : Color.primary)
    }.controlSize(.large).buttonBorderShape(tile ? .roundedRectangle(radius: 18) : .capsule)
    if snapshot {
      if prominent {
        button.buttonStyle(.borderedProminent)
      } else {
        button.buttonStyle(.bordered).tint(.gray).foregroundStyle(.primary)
      }
    } else {
      if prominent { button.buttonStyle(.glassProminent) } else { button.buttonStyle(.glass) }
    }
  }
}

/// Errors stay by their action and retry the same operation.
struct ActionFeedback: View {
  @Bindable var store: AppStore
  var action = "general"
  var body: some View {
    if store.action == action, let message = store.error {
      HStack(alignment: .firstTextBaseline, spacing: 12) {
        Text(message).font(.callout).foregroundStyle(AccessibleSecondary())
        if store.canRetry {
          Button("Try again") { Task { await store.retry() } }
            .buttonStyle(NativeGlassButtonStyle())
            .disabled(store.busy)
        }
      }
      .fixedSize(horizontal: false, vertical: true)
    }
  }
}

private struct SnapshotExpandedOptionsKey: EnvironmentKey { static let defaultValue = false }
extension EnvironmentValues {
  var snapshotExpandedOptions: Bool {
    get { self[SnapshotExpandedOptionsKey.self] }
    set { self[SnapshotExpandedOptionsKey.self] = newValue }
  }
}

private struct LimmrWindowWidthKey: EnvironmentKey { static let defaultValue: Double = 1100 }
extension EnvironmentValues {
  var limmrWindowWidth: Double {
    get { self[LimmrWindowWidthKey.self] }
    set { self[LimmrWindowWidthKey.self] = newValue }
  }
}

extension CircleUpcoming {
  var displayText: String {
    guard let day = Timestamp.date(date + "T12:00:00Z") else { return text }
    return day.formatted(.dateTime.weekday(.abbreviated).day().month(.abbreviated)) + " · " + text
  }
}

private struct SnapshotHomeSectionKey: EnvironmentKey { static let defaultValue: String? = nil }
extension EnvironmentValues {
  var snapshotHomeSection: String? {
    get { self[SnapshotHomeSectionKey.self] }
    set { self[SnapshotHomeSectionKey.self] = newValue }
  }
}
