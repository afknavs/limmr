import LimmrKit
import SwiftUI

struct PaperConversationChart: View {
  let history: PersonConversationHistory
  var headingAbove = false
  private var comparison: String? {
    guard history.weeks.count >= 8 else { return nil }
    let last = history.weeks.suffix(4).reduce(0) { $0 + $1.messages }
    let previous = history.weeks.dropLast(4).suffix(4).reduce(0) { $0 + $1.messages }
    guard previous > 0 else { return nil }
    return Double(last) > Double(previous) * 1.2 ? "More than usual this month" : Double(last) < Double(previous) * 0.8 ? "Quieter this month" : "About usual this month"
  }
  var body: some View {
    let maximum = max(1, history.weeks.map(\.messages).max() ?? 1)
    VStack(alignment: .leading, spacing: 8) {
      if headingAbove { Text("How often you talk · 12 weeks").font(LimmrTheme.font(12, .semibold)).foregroundStyle(LimmrTheme.ink3) }
      HStack(alignment: .bottom, spacing: 5) {
        ForEach(Array(history.weeks.enumerated()), id: \.offset) { index, week in
          RoundedRectangle(cornerRadius: 2).fill(LimmrTheme.ink2.opacity(index >= 9 ? 1 : 0.45))
            .frame(maxWidth: .infinity).frame(height: 40 * Double(week.messages) / Double(maximum))
            .help("Week of \(week.start): \(week.messages) messages")
            .accessibilityLabel("Week of \(week.start), \(week.messages) messages")
        }
      }.frame(height: 40, alignment: .bottom)
      Text(headingAbove ? comparison ?? "Messages per week" : "How often you talk · 12 weeks").font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3)
    }.frame(maxWidth: .infinity, alignment: .leading).accessibilityElement(children: .contain)
  }
}
struct PaperSharedFiles: View {
  @Bindable var store: AppStore
  let history: PersonConversationHistory
  var body: some View {
    VStack(alignment: .leading, spacing: 8) {
      Text("Shared between you · \(history.sharedFilesTotal) files").font(LimmrTheme.font(12, .semibold)).foregroundStyle(LimmrTheme.ink3)
      LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 8), count: 6), spacing: 8) {
        ForEach(Array(history.sharedFiles.prefix(history.sharedFilesTotal > 6 ? 5 : 6)), id: \.key) { file in
          Button { store.openPeopleRoute(.chat(file.chatJid, store.chatName(file.chatJid))) } label: {
            GeometryReader { geometry in
              ZStack {
                LimmrTheme.surface2
                Image(systemName: "doc.text").font(.system(size: 20)).foregroundStyle(LimmrTheme.ink3)
                if let thumbnail = file.thumbnail { ThumbnailImage(data: thumbnail, identity: file.key, pixels: 160).scaledToFill().frame(width: geometry.size.width, height: 40).clipped() }

              }
            }.frame(height: 40).clipShape(RoundedRectangle(cornerRadius: 8))
          }.quietCard().help(file.filename).accessibilityLabel(file.filename.isEmpty ? "Shared file" : file.filename).accessibilityHint("Open the source conversation")
        }
        if history.sharedFilesTotal > 6, let first = history.sharedFiles.first {
          Button("+\(history.sharedFilesTotal - 5)") { store.openPeopleRoute(.chat(first.chatJid, store.chatName(first.chatJid))) }.buttonStyle(.plain).font(LimmrTheme.font(12)).foregroundStyle(LimmrTheme.ink3).frame(maxWidth: .infinity).frame(height: 40).background(LimmrTheme.surface2, in: RoundedRectangle(cornerRadius: 8))
        }
      }
    }
  }
}
