import SwiftUI
import AppKit
import LimmrKit

struct StarterPrompt: View {
  var body: some View {
    VStack(alignment: .leading, spacing: 6) {
      Text("Then ask your AI").font(.callout.weight(.semibold))
      HStack {
        Text(AppStore.starterPrompt).textSelection(.enabled)
        Spacer()
        Button("Copy") { NSPasteboard.general.clearContents(); NSPasteboard.general.setString(AppStore.starterPrompt, forType: .string) }
      }
      Text("It will suggest circles for you to keep in Limmr, then fill in their briefs.").font(.callout).foregroundStyle(AccessibleSecondary())
    }.multilineTextAlignment(.leading).padding(12).background(.primary.opacity(0.025), in: RoundedRectangle(cornerRadius: 10))
  }
}
struct IntegrationSkill: View {
  let kind: ConnectKind
  let sample: Bool
  @State private var installed = false
  @State private var written: [String] = []
  @State private var feedback: String?
  @State private var destination: URL?
  private var resources: IntegrationResources? {
    IntegrationResources.locate(bundleResources: Bundle.main.resourceURL,
      repository: URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent(), sample: sample)
  }
  var body: some View {
    VStack(alignment: .leading, spacing: 10) {
      if kind == .claudeCode || kind == .codex {
        Text("Limmr skill").font(.headline)
        Text("Optional guidance for circles, people and briefs.").font(.callout).foregroundStyle(AccessibleSecondary())
        if installed {
          Text("Added \(written.count) file\(written.count == 1 ? "" : "s")").font(.callout)
          DisclosureGroup("What was written") {
            if let destination { Text(destination.path).textSelection(.enabled) }
            ForEach(written, id: \.self) { Text($0).font(.caption) }
          }
          Button("Remove") {
            guard let destination, !sample else { return }
            do { try SkillInstaller().remove(destination: destination); installed = false; feedback = "Limmr skill removed." }
            catch { feedback = error.localizedDescription }
          }
        } else {
          Button("Add the Limmr skill") {
            guard !sample else { return }
            let folder = kind == .codex ? ".codex" : ".claude"
            let target = FileManager.default.homeDirectoryForCurrentUser.appending(path: "\(folder)/skills/limmr")
            do {
              guard let resources else { throw BackendError.unavailable("The bundled integrations folder is missing. Rebuild Limmr to include it.") }
              let receipt = try SkillInstaller().install(source: resources.skill(kind: kind.kind), destination: target)
              written = receipt.files.keys.sorted(); destination = target; installed = true; feedback = nil
            } catch { feedback = error.localizedDescription }
          }.buttonStyle(.bordered).disabled(sample || resources == nil)
          if !sample {
            DisclosureGroup("Where it goes") {
              Text(kind == .codex ? "~/.codex/skills/limmr/" : "~/.claude/skills/limmr/").font(.caption).textSelection(.enabled)
            }
          }
        }
      } else if kind.remote {
        Text("Help your AI use Limmr").font(.headline)
        Text("Add these instructions to a project or your AI’s custom instructions.").font(.callout).foregroundStyle(AccessibleSecondary())
        Button("Copy instructions") {
          do {
            guard let resources else { throw BackendError.unavailable("The bundled integrations folder is missing.") }
            let text = try resources.instructions()
            NSPasteboard.general.clearContents(); NSPasteboard.general.setString(text, forType: .string)
            feedback = "Instructions copied."
          } catch { feedback = error.localizedDescription }
        }.buttonStyle(.bordered).disabled(resources == nil)
      }
      if resources == nil { Text("Bundled instructions aren’t available in this build.").font(.caption).foregroundStyle(AccessibleSecondary()) }
      if sample && !kind.remote { Text("Sample mode · Installation is available in the running app.").font(.caption).foregroundStyle(AccessibleSecondary()) }
      if let feedback { Text(feedback).font(.callout).foregroundStyle(AccessibleSecondary()) }
    }
    .onAppear {
      guard !sample, kind == .codex || kind == .claudeCode else { return }
      let folder = kind == .codex ? ".codex" : ".claude"
      let target = FileManager.default.homeDirectoryForCurrentUser.appending(path: "\(folder)/skills/limmr")
      if let receipt = try? SkillInstaller().receipt(destination: target) {
        destination = target; written = receipt.files.keys.sorted(); installed = true
      }
    }
  }
}
