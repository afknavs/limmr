# Security model

Limmr's security boundary is the Go engine. Hiding a button in the app is not permission enforcement: every tool call must authenticate an agent and check its current permissions before touching data or requesting a send.

## What we protect against

| Threat | Boundary and limit |
| --- | --- |
| Another local app calling the listener | Loopback binding is paired with credentials. Exact Host checks and rejection of local `Origin` headers reduce browser-based attacks. Loopback alone is not authentication. |
| A remote client asking for more than it was granted | OAuth identifies an agent; the engine applies the same capability, circle-access, expiry and chat checks as local MCP. The public proxy reaches port 18801 only. |
| A prompt-injected AI trying to send or impersonate you | Sends follow the circle's rule: **never** blocks them, **confirm in chat** requires a separate confirm quoting the draft id (no sooner than 2 s), and background drafts park on Home. **Send freely** circles trust the AI: choose them deliberately. The engine rechecks the AI’s send permission and current chat access. |
| An agent searching for excluded conversations | The global hidden list, chat scope, circle access and expiry apply to reads, searches, directory results, counts and context resolution, with no existence hints. |
| An agent changing shared context | People writes require their own permission and attribution. General profile edits are reversible; AI-proposed detail values stay suggestions until the owner confirms them. |

An AI can still misread a conversation, write an incorrect fact, send something wrong in a send-freely circle, or ask you to confirm a harmful message. Review the recipient and complete payload. A compromised Mac user account, stolen admin credential or malicious process able to read your files is outside this boundary. Limmr does not prevent an authorised AI client from retaining or forwarding data it has read.

## Credentials

Local agents receive distinct random bearer tokens, shown once for installation. The hub stores SHA-256 digests, including for OAuth tokens. Identity comes from credentials, never a client-chosen header. Disabling an agent or revoking its credentials removes its access; rotate a token if its client configuration leaks.

`control-token` and `status-token` are different: the Mac app needs reusable plaintext files, protected with mode 0600. Control access can administer permissions and approve sends. Status access only returns connection/account status, agent names, pending counts and health, with no message content. It is still private metadata.

Client configuration, backups of that configuration and your shell history may contain bearer credentials. Do not attach them to issues. The session database contains device keys and must never be exported or included in diagnostics.

## Permissions and send rules

Agents have **all**, **only these** or **all except these** chat access, subject to the global hidden list. Reading, searching, people reads/writes, summaries, downloads and send requests are separate capabilities. MCP tool discovery reflects permissions, and execution checks them again.

Local defaults include write, download and request-send access. Remote defaults allow message/search and people reads, with writes, downloads and sends off. Local visibility defaults to all circles, unsorted chats included, with no expiry. Remote approval defaults to Everything unless the owner chooses circles and a duration. Narrow visibility before sharing the connection.

Each circle has a send rule (send freely, confirm in chat, never); chats in no circle confirm in chat and the strictest rule wins. Drafts expire after 30 minutes. "Confirm in chat" relies on the AI client asking you: Limmr enforces the two steps and logs who confirmed, but cannot prove a person pressed the button, so use **never** where that matters. Background runs cannot confirm; their drafts wait on Home for you. Handoffs also respect quiet hours and a reply budget, parking drafts outside them.

## Circle access and expiry

Each AI's permissions include `access`: `circles` is `"all"` or an array of circle IDs, `unsorted` controls chats in no confirmed circle, and `until` is an RFC3339 expiry or null. For compatibility, a missing access object means all circles with no expiry. Unsorted defaults true with all circles and false with a selected-circle list. Selecting no circles with unsorted off shares no chats.

A chat is visible only when it passes **all** of these checks: it is not globally hidden; the existing chat selection allows it; and it belongs to an allowed circle (or is unsorted when that option is enabled, or circle access is all). Overlapping circles use this union for visibility: selecting Work can include a chat also in Family. The strictest circle rule still governs sending. Review membership before granting Work access.

People are visible only through allowed-circle membership or visible-chat participation. Circles, briefs, topics, suggestions and watches stay within the allowed circles; mixed-scope content must not expose excluded context. An agent's own unconfirmed proposals remain visible to it for review. Every list, search hit, count, graph projection and context-resolution result must enforce these boundaries without hinting at excluded data. Capability permissions remain independent: circle visibility does not enable writes or sends.

After `until`, every MCP call except `whoami` fails with an access-ended error directing the AI to ask the owner for renewal. `whoami` still returns `access.ended:true` and an access summary. Expiry writes one `access_ended` Activity entry; it does not delete the profile or history. The owner can **Renew** with a new expiry or no limit, or **End now**, on the integration page. Admin-only permission/access routes manage these values; agents cannot extend their own access.

Remote approval includes the selected access; omitting it preserves the Everything default. **Only some circles** starts with no circles selected and offers **Chats in no circle** separately, plus No limit, 7/30/90 days or a chosen end date. This applies independently to each AI profile. An expired integration cannot regain access by reconnecting with the same credential. Disabled or revoked credentials remain blocked even if the owner extends the date.

Expiry, narrowing and revocation stop future access; they cannot erase data an AI client or provider has already received. Optional skills teach these rules but are not a security boundary: enforcement belongs in the engine. Optional **This Mac** briefs use Apple's on-device model; enabling them is a separate owner choice, off by default, with no MCP credential for the built-in writer. Brief versions attribute that writer and can later be superseded by another permitted AI.

## Local storage and logs

The databases live on the Mac. Limmr does not claim database encryption at rest; use your Mac's disk protection and treat backups as sensitive. WhatsApp transport encryption does not protect decrypted messages once stored or returned to an AI client.

`logs/engine.log` never holds message text, tokens or session keys. It is size-capped. Remote sign-in problems are logged as fixed words only, for example `WARN remote auth failure endpoint=token grant=refresh_token error=invalid_grant reason=refresh_expired`, plus `INFO remote auth issued endpoint=token grant=…` when credentials are issued. Endpoint, grant type, error and reason come from a fixed list (anything else is written as `unlisted`); client IDs, tokens, codes, verifiers, secrets, headers, URLs and bodies are never written. Identical lines are limited to one per minute with a `suppressed=N` count. The **activity database** intentionally differs: read events hold IDs, sent events retain final text, and summaries retain summary text. The app resolves read IDs to the original messages. Activity retention defaults to 180 days and does not erase the message database.

Diagnostics are designed to omit names, numbers, credentials and message text. Review a diagnostics report before sharing it. Never submit databases, QR/pairing codes, raw client configs or screenshots of real conversations.

## Remote access

Remote access stays off until configured. The public endpoint uses HTTPS, OAuth 2.1 with PKCE, dynamic registration and a six-digit code approved on the Mac. Approval associates the client with an agent profile; access is then bounded by that profile. Token/client revocation is available in remote settings.

Expose only port 18801 through the tunnel. Never expose the local MCP/control listener on 18800. The HTTPS proxy operator can be part of the transport trust boundary. Cloud AI providers receive the permitted data returned to their clients, and their retention policies still apply.

## Report a vulnerability

Use [GitHub private vulnerability reporting](https://github.com/afknavs/limmr/security/advisories/new) (**Security → Report a vulnerability**). Include the affected version, expected boundary, reproduction with invented data and impact. Keep exploit details out of public issues. Never include real messages, credentials, databases or pairing codes.
