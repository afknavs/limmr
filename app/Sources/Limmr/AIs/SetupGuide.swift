import AppKit
import SwiftUI
import LimmrKit

@MainActor enum SetupGuide {
  static var url: URL? {
    // Installed bundles are self-contained; unbundled snapshots use this worktree's resource.
    if let bundled = Bundle.main.url(forResource: "Limmr-Setup-Guide", withExtension: "pdf") { return bundled }
    let resource = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
      .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
      .appending(path: "Resources/Limmr-Setup-Guide.pdf")
    return FileManager.default.fileExists(atPath: resource.path) ? resource : nil
  }
  static func open() { if let url { NSWorkspace.shared.open(url) } }
}
struct SetupGuideLink: View {
  var body: some View {
    Button("Setup guide (PDF)", systemImage: "book.closed") { SetupGuide.open() }
      .disabled(SetupGuide.url == nil)
  }
}
struct CopySetupPrompt: View {
  let kind: ConnectKind
  var approved = false
  var token: String?
  var remoteURL: String?
  @State private var copied = false
  var body: some View {
    VStack(alignment: .leading, spacing: 6) {
      Button("Copy setup prompt", systemImage: "doc.on.doc") {
        let resources = Bundle.main.resourceURL?.appending(path: "integrations").path
          ?? "/Applications/Limmr.app/Contents/Resources/integrations"
        let text = SetupPrompt.text(client: kind.kind, approved: approved, token: token,
                                    remoteURL: remoteURL, integrationsPath: resources)
        NSPasteboard.general.clearContents()
        copied = NSPasteboard.general.setString(text, forType: .string)
      }.buttonStyle(.bordered)
      Text(copied ? "Copied. Paste it into \(SetupPrompt.clientName(kind.kind))." : "Let your AI walk you through setup.")
        .font(.caption).foregroundStyle(AccessibleSecondary())
    }
  }
}
