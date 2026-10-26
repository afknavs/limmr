# Set up Limmr

Start with the demo, then connect one AI. You can use local clients without enabling remote access.
The printable [setup guide](pdf/Limmr-Setup-Guide.pdf) follows the same steps with demo app captures.

## 1. Install on your Mac

1. Open this repository's **Releases** page on GitHub and download the Limmr DMG for your Mac when a beta release is available. Use the release linked from the repository, rather than a third-party mirror.
2. Open the DMG and drag **Limmr** into **Applications**. Open it from there.
3. The beta is self-signed. If macOS blocks it, right-click Limmr in Applications and choose **Open**. If needed, try opening it once, then go to **System Settings → Privacy & Security → Open Anyway** and confirm. Follow macOS's security prompts; do not disable Gatekeeper.
4. Allow Limmr under **System Settings → General → Login Items** when asked. Its helper keeps your connections available after you close the window. If macOS requires approval, allow it and return to Limmr.

## 2. Try the demo, then link WhatsApp

Choose demo/sample mode first to explore invented chats without pairing or granting AI access.
When you are ready, start setup in Limmr. On your phone, open **WhatsApp → Settings → Linked devices → Link a device** (on Android, Linked devices may be in the overflow menu). Scan Limmr's QR code. Let the first history sync finish; an expired QR needs a fresh code.

Limmr keeps the linked-device session, messages, people map, permissions and activity on your Mac. There is no Limmr-hosted message database. WhatsApp still uses its own service. An AI receives the conversations its permissions allow and may process them at its provider. Revoking access stops future reads; it cannot recall data already received. Optional pairing with another Limmr engine syncs selected owner knowledge, not WhatsApp history or credentials; see [Connecting](CONNECTING.md).

## 3. Connect your AI

In Limmr, choose **Connect an AI** (some surfaces say **Connect an integration**), then choose your client. Use a separate profile for each AI or purpose. Review what it can see, how long access lasts and whether sending is allowed.

### Claude Code — same Mac

Choose **Claude Code**, review the access summary, and click **Create connection** to approve this agent. Then either use **Connect**, or open **Show details** and copy its configuration. To delegate the configuration, choose **Copy setup prompt** and paste it into Claude Code. The approved prompt includes this agent's private credential and tells Claude Code to register the MCP server and install the bundled skill. Keep it out of shared chats, repositories and shell history. Reconnect the client if needed.

### Codex — same Mac

Choose **Codex**, review access and click **Create connection**. Use **Connect** to back up and add the Limmr entry to Codex's configuration, or copy the configuration under **Show details**. **Copy setup prompt** lets Codex merge the MCP entry and install the bundled skill itself. Keep the approved prompt and configuration private. Restart or reconnect Codex after configuration changes; do not create a second `mcp_servers.limmr` block.

### Cursor — same Mac

Choose **Cursor**, review access and click **Create connection**. Copy the configuration under **Show details** into Cursor's MCP settings, preserving any other servers. Or use **Copy setup prompt**, which asks Cursor to merge `mcp.json` and add the bundled instructions. Reconnect the client. If working in a remote workspace, the server must still be reachable on the Mac running Limmr; its loopback address is only for this Mac.

Local clients use `http://127.0.0.1:18800/mcp` with separate agent credentials. Before you create a connection, the setup prompt contains no address or credential; it asks you to approve the agent in Limmr first. You can add the optional Claude Code/Codex skill with **Add the Limmr skill**. Skills guide behavior and do not grant access. Manual details: [Connecting](CONNECTING.md), [integrations](../integrations/README.md).

### ChatGPT — hosted

1. In Limmr, open **Settings → Remote access → Set up remote access**. Choose **Tailscale Funnel**. Install and sign in to Tailscale if needed, then let Limmr check the public address. Wait for **Ready for ChatGPT**; discovery can take 10–15 minutes. Do not expose the local control port yourself.
2. Choose **ChatGPT** in Connect an AI and copy the **Connection address** ending in `/mcp`. **Copy setup prompt** can guide you through the next steps with the address filled in.
3. In ChatGPT, open settings and add a custom connector/app. The repository currently documents **Settings → Apps → Advanced → Developer mode**, then **Create app**. Use **WhatsApp (Limmr)**, the MCP address, and **OAuth**. These labels and availability depend on your account/workspace and need verification for your version. Sign in if prompted.
4. Start the connector's authorization. Match the six-digit code in your browser with the review/pair page that opens in Limmr. Approve only the connection you initiated. Choose the agent, access and duration; review **Let it set things up**, which permits people suggestions and briefs. Sending and media remain off for this remote setup.
5. Return to ChatGPT to finish connecting. If the browser waits after approval, choose **I've approved it, continue**. Then try the starter prompt below.

### Claude on claude.ai — hosted

Complete the same remote-access setup above, then choose **Claude** in Limmr and copy its MCP address. In claude.ai, open settings and add a custom connector; the repository documents **Settings → Connectors → Add custom connector**. Paste the address, sign in if required, and match and approve the six-digit code in Limmr. Review visibility, duration and setup permissions, then return to Claude to finish. **Copy setup prompt** provides step-by-step guidance without a local bearer token. Client menu labels and account availability need verification.

Hosted clients cannot reach `localhost` on your Mac. Remote access gives them an HTTPS MCP/OAuth connection, not your database files or control API. A hosted connection never needs your Mac's control token or a local agent's bearer token.

## 4. Try this first

> Call whoami and tell me which Limmr profile you are, what you can see and what you can do. Then set up Limmr for me: suggest circles from the chats you can access, explain your evidence and help me review them. Do not send any messages.

Look at **Home** for circles and suggestions and **Activity** for what your AI read or changed. Review proposed people details and circle membership. Your own AI may add qualifying circles automatically with Undo; uncertain details still need review. If you only want a read-only connection, turn off **Let it set things up** or the corresponding write permissions.

## Troubleshooting

| What you see | What to do |
| --- | --- |
| Connector cannot reach Limmr | Keep the Mac awake and Limmr's helper running. For local clients check the loopback address and that the client runs on this Mac. For hosted clients copy the public HTTPS MCP address again; never use localhost. Open Limmr's Health view. |
| 401 / unauthorized | The address may be reachable but authentication has not succeeded. For local clients check that this agent is enabled and that its current credential is configured. For hosted clients finish OAuth and approve the matching code. Check access expiry; reconnect if credentials were revoked. A 401 alone does not prove a working connector. |
| Remote access is not ready | Wait for discovery (up to 10–15 minutes), check Tailscale sign-in and Funnel availability, and use the wizard's Retry. Verify its HTTPS address. Do not reset unrelated tunnels. |
| Helper is not running | Open Limmr, follow its helper/Health guidance and allow Limmr in Login Items. If it still fails, use the redacted diagnostics rather than sharing tokens or database files. |
| WhatsApp logged out | Start linking again in Limmr and scan a fresh QR from Linked devices. Reconnection may need another history sync. |
| No pairing review appears | Reopen Limmr and **Settings → Remote access → Review code**. Check that the request is still current, then retry from the client if expired. Never approve an unfamiliar request. |
| AI sees too little | Check the correct profile, circles, duration and Hidden chats. Skills do not widen permissions. Verify with `whoami` and Activity. |

## What your AI can and cannot do

An AI can read and search permitted chats, help organize people/circles and write briefs when granted those capabilities. Global **Hidden chats** stay invisible to every AI, including searches and counts. Circle limits, expiry and revocation apply to future calls.

Sending needs both the integration's permission and the chat's circle rule. **Never** blocks it. **Confirm in chat** needs your confirmation, or an own AI's explicit instruction from you; a requested review waits on Home. **Free** may send immediately once the AI requests it. An AI client must honor your instructions: Limmr cannot prove a person clicked the client's confirmation button. For a read-only beta, leave sending off. Review any send permission before enabling it.

## The WhatsApp risk

Limmr uses an unofficial WhatsApp client library and is not endorsed by WhatsApp or Meta. WhatsApp can change its service, disconnect linked devices or restrict an account. Do not assume your account is protected from that risk; decide whether to link it. Demo mode lets you explore without linking. See [Security](SECURITY.md) for the access and privacy model.

## Linux and dots

[Run Limmr on Linux or a dot](handoffs/DOT-ENGINE.md) for the one-command source build, WhatsApp linking, Mac pairing and scheduled sync.
