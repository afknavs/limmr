# Connecting your AI tools

Give each client its own agent profile. That keeps its credentials, permissions and activity separate. In Limmr, choose **Connect an AI**, pick the client, then review the agent's permission page.

Hosted clients may require connector or developer-mode access for your account or workspace; menu labels can vary.

## Local or remote?

Claude Code, Codex and Cursor can use `http://127.0.0.1:18800/mcp` on the same Mac, with a per-agent bearer token. ChatGPT and claude.ai need a public HTTPS URL and OAuth; turn on remote access first. A hosted client cannot reach your Mac through `localhost`.

Remote access is optional. It exposes the remote MCP listener, not the app's control API or database files. Any AI you connect can receive the data it is allowed to read, whether the connection is local or remote.

## Set up remote access

### With Tailscale Funnel

1. Install and sign in to Tailscale yourself. In Limmr, open **Settings → Remote access → Set up remote access**.
2. Choose Tailscale Funnel. Funnel gives the remote listener a public HTTPS address. The wizard detects the Tailscale app or command-line executable and shows existing Funnel port usage.
3. Use **443** for ChatGPT. If another app already uses it, choose **Share port 443 (recommended for ChatGPT)**: Limmr uses `/limmr` and leaves the existing root and other apps’ routes in place. Free **8443** and **10000** remain available, labelled **May not work with ChatGPT**. Existing routes are protected unless Limmr created them.
4. Review **Details**, then click **Share port 443** or **Enable Funnel**. The app rechecks the routes, adds only its own paths and configures the engine’s public URL and OAuth issuer. On a free port 443, Limmr uses the root directly.
5. Allow 10–15 minutes for the URL to become reachable. The wizard resolves public DNS and pins HTTPS checks to a public IP, so a tailnet-only connection cannot pass verification. It checks authorization-server discovery, protected-resource discovery and the unauthenticated `/mcp` bearer challenge. Wait for **Ready for ChatGPT**, then copy the MCP address. A failed or cancelled check restores the previous remote settings only while this wizard still owns the configuration and no approved AI clients exist. Concurrent changes are left alone. Approved connections keep remote access on; the wizard offers **Turn off** for an explicit decision. Limmr’s routes remain so you can retry verification.

<details>
<summary>Command details</summary>

The app detects `/Applications/Tailscale.app/Contents/MacOS/Tailscale` or `tailscale` on PATH and inspects `tailscale funnel status --json`. Sharing an occupied HTTPS port 443 adds exactly these routes:

```sh
tailscale funnel --bg --https=443 --set-path=/limmr http://127.0.0.1:18801
tailscale funnel --bg --https=443 --set-path=/.well-known/oauth-authorization-server/limmr http://127.0.0.1:18801/.well-known/oauth-authorization-server
tailscale funnel --bg --https=443 --set-path=/.well-known/oauth-protected-resource/limmr http://127.0.0.1:18801/.well-known/oauth-protected-resource
tailscale funnel --bg --https=443 --set-path=/.well-known/openid-configuration/limmr http://127.0.0.1:18801/.well-known/oauth-authorization-server
```

Tailscale strips each mount path. Limmr records the routes it created in its data folder; removing remote access removes only matching entries from that journal. A manually replaced target is left alone. No command resets an entire port. These commands run only when you click the setup action, never during a build or test.

</details>

### With your own HTTPS URL

Choose **I have my own HTTPS URL** in the wizard and paste the URL, for example `https://limmr.example.com` or `https://limmr.example.com/limmr`. Configure your tunnel or reverse proxy to forward to `http://127.0.0.1:18801`, preserving the public host expected by the engine. It must carry MCP HTTP traffic and OAuth endpoints; the public URL is also the OAuth issuer.

Do not forward port 18800. Wait for the wizard's discovery check to succeed. The connector URL is your public base URL plus `/mcp`, such as `https://limmr.example.com/mcp`.

## ChatGPT

1. Finish remote setup above. In Limmr, choose the **ChatGPT** tile and copy the connector URL.
2. In ChatGPT, go to **Settings → Apps → Advanced** and turn **Developer mode** on.
3. Choose **Create app**. Use **WhatsApp (Limmr)** as the name, paste the URL ending in `/mcp`, and choose **OAuth** authentication.
4. Create the app and start the connection. The authorisation page shows a six-digit code. Limmr automatically opens its review sheet on its independent pairing check (within two seconds on a responsive local helper), reopening the window if needed; if it is in the background, it also sends a notification when notifications are allowed.
5. In Limmr, verify the code and choose or create the **ChatGPT** agent profile. Before approving, choose **What it can see**: Everything, or Only some circles with the circles, unsorted-chat choice and duration you want.
6. Review **Let it set things up**. It grants people writes and summary saving (`fill_in`), so your AI can propose circles, suggest people details and write briefs. It starts on for your own AI; turn it off for a read-only connection. Sending and media remain off. Click **Approve**, return to ChatGPT, and let it finish connecting. Never approve a code you did not initiate.

If these controls are absent, check your account or workspace's connector/developer-mode availability. Never paste the Mac's control token into ChatGPT. A remote client uses OAuth, not the local bearer setup.

## Claude on claude.ai

1. Finish remote setup, then copy the URL ending in `/mcp` from Limmr's **Claude** tile.
2. In claude.ai, go to **Settings → Connectors → Add custom connector**.
3. Give it a name, paste the URL and start the authorisation flow.
4. Match the six-digit code with Limmr’s automatic review sheet. Choose or create the Claude agent profile, select its circles and duration, and review **Let it set things up** before approving. Uncheck it for read-only access; sending and media remain off.
5. Return to Claude.ai and finish connecting. If notifications are blocked, the review sheet still appears in Limmr. If you closed it, reopen **Settings → Remote access → Review code**.

## Moving from an old WhatsApp MCP

1. Finish your WhatsApp migration separately using Limmr’s migration controls when you are ready. Remote setup does not stop old services, move stores or replace the old public endpoint.
2. Set up remote access on port 443 using the wizard above. When sharing, the issuer is `https://<your-host>/limmr` and the connector URL is `https://<your-host>/limmr/mcp`. Existing routes, including an old MCP route on another port, stay in place.
3. Add a fresh connector in ChatGPT or Claude.ai. Old client registrations and tokens do not transfer. Match and approve a fresh six-digit code on your Mac.
4. Verify `whoami` and a read call, then check Activity. Before changing an address used by approved clients, the wizard warns that ChatGPT will need to reconnect at the new address. Existing registrations and credentials are retained and remain tied to their original address. They cannot authenticate at a different address; restoring the old address restores unexpired credentials. Disconnecting a client still deletes its credentials.

If the browser stays on the code page after approval, choose **I’ve approved it, continue**. This navigates directly to the server rather than using the browser’s background fetch. The page also checks by navigation about every five seconds, including when JavaScript is disabled. Expired requests require a new connection attempt.

Health and status use cached public discovery results. The engine probes in the background when the listener starts or its configuration changes, then about every minute. A slow or unreachable public address does not delay local health/status responses. Health shows **Checking public discovery…** initially and the last check time afterward.

## Disconnect a remote client

Open **Settings → Remote access → Disconnect** beside that client. This revokes its registration, access tokens and refresh tokens immediately, including previously rotated tokens. Remove the connector in ChatGPT or Claude.ai too. Reconnecting requires a new registration and approval. To pause all remote connections, switch off **Allow remote connections**; this disables the listener and removes only Funnel routes created and still owned by Limmr. Your own tunnel and other apps’ routes stay in place.

## Claude Code

1. Install Claude Code, then choose its tile in Limmr.
2. Use the install button to add the local HTTP server with that agent's bearer token. No remote access is needed.
3. Restart or reconnect Claude Code as needed, then ask it to call `whoami` and confirm its identity and permissions.

<details>
<summary>Manual connection</summary>

The app's installation action runs the following shape of command. `REPLACE_WITH_AGENT_TOKEN` is a placeholder, never a real credential in this repository:

```sh
claude mcp add --scope user --transport http limmr http://127.0.0.1:18800/mcp --header "Authorization: Bearer REPLACE_WITH_AGENT_TOKEN"
```

The app supplies the token shown once for this agent. A manual command may leave it in shell history. Keep the generated configuration private.

</details>

## Codex

1. Choose **Codex** in Limmr and use the install button.
2. Limmr backs up `~/.codex/config.toml` before adding a `[mcp_servers.limmr]` block with the local URL and that agent's credentials. If an entry already exists, resolve it before reconnecting.
3. Restart or reconnect Codex, call `whoami`, and review the agent's permissions in Limmr.

<details>
<summary>Manual connection with an environment variable</summary>

For a CLI that supports it, the equivalent registration is:

```sh
codex mcp add limmr --url http://127.0.0.1:18800/mcp --bearer-token-env-var LIMMR_AGENT_TOKEN
```

Set `LIMMR_AGENT_TOKEN` privately in the environment of the Codex process using the token issued to this agent. This command stores the variable's name, not its value. A GUI app must also receive that environment variable; setting it in an unrelated terminal is not enough.

The corresponding configuration is:

```toml
[mcp_servers.limmr]
url = "http://127.0.0.1:18800/mcp"
bearer_token_env_var = "LIMMR_AGENT_TOKEN"
```

The app's install button uses `http_headers` instead, retaining a bearer credential in the protected config file. Keep both that file and its backups private. Do not add a second block if the button already installed one. Check `codex mcp add --help` for your installed version; syntax above was checked locally during this build.

</details>

## Cursor and other MCP clients

1. Choose **Cursor** or **Other** in Limmr and create a local agent profile.
2. Copy the generated JSON into your client's MCP server settings. The client must support Streamable HTTP and an Authorization header.
3. Reconnect the client, call `whoami` and review its permissions.

<details>
<summary>Example JSON</summary>

```json
{
  "mcpServers": {
    "limmr": {
      "url": "http://127.0.0.1:18800/mcp",
      "headers": {
        "Authorization": "Bearer REPLACE_WITH_AGENT_TOKEN"
      }
    }
  }
}
```

Use the client-specific snippet shown by the app if its format differs. Keep the real token out of shared settings and screenshots.

</details>

## Optional skills and instructions

The connection and skill are separate. MCP supplies the tools and its own guide; an AI with no skill can still set up and use Limmr. Adding a skill never widens access.

- **Claude Code:** the connection sheet's optional **Add the Limmr skill** copies the bundled skill to `~/.claude/skills/limmr/`. The shipped Claude Code plugin can also be loaded by hand.
- **Codex:** the optional button copies the skill to `~/.codex/skills/limmr/`.
- **ChatGPT / Claude.ai:** choose **Copy instructions**, then paste into your custom instructions or the relevant Project yourself.
- **Cursor / other clients:** no skill is required; follow the MCP server's guidance. You may supply the short instructions where your client accepts them.

The add buttons are never preselected, show what they wrote and offer Remove. For manual installation and removal, see [integrations](../integrations/README.md). Avoid installing both the Claude plugin and its standalone skill.

## Let your AI set it up

After connecting, say **“Set up Limmr for me.”** The AI calls `whoami` first and `get_started` next. It groups visible chats into proposed circles and explains why. Review the batch on Home with **Keep all**, or Keep, Edit or Dismiss individual circles. After you keep them, the AI suggests people details it already knows, sets known per-circle roles and writes the first briefs from messages. Confirm detail suggestions in Limmr; the AI must ask about uncertain identities or relationships.

A remote AI may need you to enable people writes and summary saving on its integration page. Visibility alone does not grant these capabilities. If you prefer, choose **This Mac** during setup for Apple's on-device first briefs when available, or **Later** for manual organisation.

## Share work. Keep family.

Use a separate profile for your work AI, even if you also use the same client personally.

1. Keep and review a **Work** circle first. Inspect its included chats: a chat in both Work and Family is still visible through Work, so remove overlaps you do not want to share or hide that chat globally.
2. In the remote approval sheet, select **Only some circles**, select **Work**, and leave **Chats in no circle** off. All circle toggles start off when you choose this mode. For an existing local or remote AI, make the same choices in **Settings → Integrations → that AI** before letting it read.
3. Choose **For → 30 days** (or 7 days, 90 days or Until…). Use No limit only if intended. Review the summary, such as **Work · until 17 Nov**, before approval.
4. Keep sends, downloads and writes disabled unless needed. Ask the work AI to call `whoami` and verify its reported access. It should only receive permitted Work context, including in searches and counts.
5. Use **Renew** to extend access or **End now** to stop it. An ended integration keeps its profile and history, with an Ended label. It can still call `whoami` to learn that access ended; other MCP calls fail until renewed.

The circle limit combines with the existing chat selection and global Hidden chats. Enabling unsorted chats includes chats in no **confirmed** circle, which may contain personal conversations. Expiry and revocation prevent future reads; they cannot recall data the AI already received. See [Security](SECURITY.md#circle-access-and-expiry).

## Check the connection

Ask the client to call `whoami` first and check its identity, access summary, expiry and capabilities, then list the chats it can read. In Limmr, confirm the agent's last-seen time and the entry in **Activity**. To test sending during development, use a fake messenger and invented data; do not send through a real account.

If authorisation fails, check **Health**, the agent's enabled state, access expiry and its credential. For remote clients, also check discovery and match the public URL with the one configured in Limmr. Revoke an unknown pending request. Rotating a local token requires updating the client; revoking a remote client requires authorising it again.
