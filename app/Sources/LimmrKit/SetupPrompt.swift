import Foundation

/// Credentials enter the copy text only after the owner creates this connection in Limmr.
public enum SetupPrompt {
  public static func text(client: String, approved: Bool = false, token: String? = nil,
                          remoteURL: String? = nil, integrationsPath: String = "/Applications/Limmr.app/Contents/Resources/integrations") -> String {
    let introduction = "Limmr is a Mac app that connects my WhatsApp to AI tools through MCP, with per-AI permissions and Activity."
    let verify = "Once connected, call whoami and report your identity, access, expiry and capabilities. Then explain what you can do. Do not send messages or change my chats during setup."
    if client == "chatgpt" || client == "claude_ai" {
      let name = client == "chatgpt" ? "ChatGPT" : "claude.ai"
      let address = remoteURL.flatMap { URL(string: $0) }.flatMap { $0.scheme == "https" && $0.host != nil ? $0.absoluteString : nil }
      return """
      \(introduction)
      Walk me through adding Limmr to \(name), one step at a time. \(address.map { "MCP address: \($0)." } ?? "First help me enable Settings → Remote access in Limmr and wait for Ready for ChatGPT, then copy its HTTPS MCP address.")
      Open the client's settings and add a custom connector using OAuth. Ask me to sign in if required. Match the six-digit code and approve the pairing page in Limmr, then return to the client. Never ask for a local bearer or control token. Menu labels and availability depend on my account; check what I see.
      \(verify)
      """
    }
    guard approved, let token, !token.isEmpty else {
      return """
      \(introduction)
      Help me connect \(clientName(client)) on this Mac. First open Limmr → Connect an AI → \(clientName(client)), review access and choose Create connection. I must approve the agent there before its private configuration can be copied. Then ask me to copy a fresh setup prompt from that page. Do not look for existing credentials or use another agent's token.
      \(verify)
      """
    }
    let url = "http://127.0.0.1:18800/mcp"
    let setup: String
    switch client {
    case "claude_code":
      // JSON string quoting is also valid shell double quoting except $ and backticks.
      let header = "Authorization: Bearer \(token)".replacingOccurrences(of: "'", with: "'\\''")
      setup = "Register with: claude mcp add --scope user --transport http limmr \(url) --header '\(header)'"
    case "codex":
      let encoder = JSONEncoder(); encoder.outputFormatting = .withoutEscapingSlashes
      let quotedURL = String(decoding: try! encoder.encode(url), as: UTF8.self)
      let header = String(decoding: try! encoder.encode("Bearer \(token)"), as: UTF8.self)
      setup = """
      Back up ~/.codex/config.toml, then merge this single block without overwriting other settings or duplicating an existing limmr entry:
      [mcp_servers.limmr]
      url = \(quotedURL)
      http_headers = { Authorization = \(header) }
      """
    default:
      let config = ["mcpServers": ["limmr": ["url": url, "headers": ["Authorization": "Bearer \(token)"]] as [String: Any]]]
      let data = try! JSONSerialization.data(withJSONObject: config, options: [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes])
      setup = "Back up and merge this into \(client == "cursor" ? "~/.cursor/mcp.json" : "your client's MCP configuration"), preserving all other entries:\n" + String(decoding: data, as: UTF8.self)
    }
    let skill: String
    switch client {
    case "claude_code": skill = "Install the bundled claude-code/skills/limmr folder into ~/.claude/skills/limmr (or use its plugin, not both)."
    case "codex": skill = "Install the bundled codex/skills/limmr folder into ~/.codex/skills/limmr."
    default: skill = "Read the bundled chatgpt/instructions.md and add its guidance to this project's AI instructions; for Cursor use a project .cursor/rules/limmr.mdc rule with frontmatter appropriate to this project."
    }
    return """
    \(introduction)
    Set up \(clientName(client)) on this same Mac. I approved this agent by creating its connection in Limmr.
    Keep this private: this prompt contains this agent's bearer token. Never log it, commit it, share it or include it in your reply. Store configuration and backups with owner-only permissions; avoid shell history. Resolve an existing Limmr entry with me before replacing it.
    \(setup)
    Bundled integrations directory: \(integrationsPath)
    \(skill) Preserve an existing skill/rule and ask before replacing it. If the directory is unavailable, locate integrations/ in my Limmr source checkout or use Limmr's optional skill button; do not invent or download a package.
    Reconnect or restart the client if needed. \(verify)
    """
  }
  public static func clientName(_ client: String) -> String {
    switch client { case "claude_code": "Claude Code"; case "codex": "Codex"; case "cursor": "Cursor";
    case "chatgpt": "ChatGPT"; case "claude_ai": "claude.ai"; default: "your AI" }
  }
}
