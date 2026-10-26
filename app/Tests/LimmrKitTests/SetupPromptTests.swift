import Testing
@testable import LimmrKit

struct SetupPromptTests {
  @Test func credentialsRequireOwnerApprovalForEveryLocalClient() {
    let fake = "fixture-agent-credential"
    for client in ["claude_code", "codex", "cursor", "custom"] {
      let before = SetupPrompt.text(client: client, token: fake)
      #expect(!before.contains(fake))
      #expect(!before.contains("127.0.0.1"))
      #expect(before.contains("Create connection"))
      let after = SetupPrompt.text(client: client, approved: true, token: fake)
      #expect(after.contains(fake))
      #expect(after.contains("Keep this private"))
      #expect(after.contains("http://127.0.0.1:18800/mcp"))
      #expect(after.contains("whoami"))
      #expect(after.contains("integrations"))
      #expect(!SetupPrompt.text(client: client, approved: true).contains("Authorization"))
    }
  }
  @Test func nativeInstructionsMatchEachClient() {
    let args = "fixture-only"
    #expect(SetupPrompt.text(client: "claude_code", approved: true, token: args).contains("claude mcp add --scope user --transport http"))
    let codex = SetupPrompt.text(client: "codex", approved: true, token: args)
    #expect(codex.contains("[mcp_servers.limmr]"))
    #expect(codex.contains("codex/skills/limmr"))
    let cursor = SetupPrompt.text(client: "cursor", approved: true, token: args)
    #expect(cursor.contains("~/.cursor/mcp.json"))
    #expect(cursor.contains("chatgpt/instructions.md"))
    #expect(cursor.contains(".cursor/rules/limmr.mdc"))
  }
  @Test func hostedClientsUseOAuthWithoutBearerCredentials() {
    for client in ["chatgpt", "claude_ai"] {
      let text = SetupPrompt.text(client: client, approved: true, token: "fixture-not-for-hosted", remoteURL: "https://limmr.example.com/limmr/mcp")
      #expect(text.contains("https://limmr.example.com/limmr/mcp"))
      #expect(text.contains("OAuth"))
      #expect(text.contains("six-digit code"))
      #expect(text.contains("whoami"))
      #expect(!text.contains("fixture-not-for-hosted"))
      #expect(!text.contains("127.0.0.1"))
      #expect(SetupPrompt.text(client: client, remoteURL: "http://invalid.example/mcp").contains("Ready for ChatGPT"))
    }
  }
}
