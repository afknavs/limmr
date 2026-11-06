import LimmrKit
import SwiftUI

struct FileApprovalPreview: View {
  let file: Attachment
  let target: String

  private var symbol: String {
    switch URL(fileURLWithPath: file.name).pathExtension.lowercased() {
    case "pdf": "doc.richtext"
    case "png", "jpg", "jpeg", "heic", "webp", "gif": "photo"
    case "mp4", "mov": "film"
    case "mp3", "m4a", "ogg", "wav": "waveform"
    case "zip": "doc.zipper"
    default: "doc"
    }
  }
  var body: some View {
    HStack(alignment: .center, spacing: 16) {
      Image(systemName: symbol)
        .font(.system(size: 28, weight: .medium))
        .foregroundStyle(Color.accentColor)
        .frame(width: 60, height: 68)
        .background(Color.accentColor.opacity(0.08), in: RoundedRectangle(cornerRadius: 12))
        .accessibilityHidden(true)
      VStack(alignment: .leading, spacing: 5) {
        Text(file.name).font(.headline).lineLimit(2).textSelection(.enabled)
        Text(ByteCountFormatter.string(fromByteCount: file.size, countStyle: .file))
          .font(.callout).foregroundStyle(AccessibleSecondary())
        Text("To \(target)").font(.callout)
      }
      Spacer(minLength: 0)
    }
    .padding(14)
    .background(.primary.opacity(0.035), in: RoundedRectangle(cornerRadius: 16))
    .accessibilityElement(children: .combine)
  }
}
