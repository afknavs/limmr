# Frequently asked questions

## Does Limmr need an AI?

You can link WhatsApp, organise circles and people, review activity and set permissions without connecting an AI. To generate briefs, connect an AI you already use, or choose **This Mac** for Apple's on-device first briefs where the model is available (macOS 26+ on supported Macs). This Mac is optional and off by default; it does not require a cloud AI connection. You can also choose **Later** and organise circles yourself.

## How do I let my AI set it up?

Connect it and say **“Set up Limmr for me.”** It checks its permissions, proposes circles for you to Keep/Edit/Dismiss, suggests known people details for your confirmation, sets known circle roles and writes briefs. Remote AIs start without write permissions: enable people writes and summary saving if you want one to complete setup. The [optional skills](../integrations/README.md) teach this workflow, but the MCP server guides clients without them too. No skill is installed automatically.

## Can I share Work without sharing Family?

Yes. Give the work AI its own profile, choose **Only some circles → Work**, leave **Chats in no circle** off, and set a duration. Review Work's chat membership first: overlapping Work/Family chats are visible through Work. Hidden chats and the existing chat selection still restrict access. At expiry, future calls stop, while the profile and history remain. Renew or End now in **Settings → Integrations**. Data already received by an AI cannot be recalled. [Recipe](CONNECTING.md#share-work-keep-family).

## Does an AI send messages by itself?

Only where you allow it. Each circle has a send rule: **send freely** (the AI sends straight away), **confirm in chat** (the AI shows you the draft and sends only after you say yes in that conversation) or **never**. Chats in no circle default to confirm in chat, and a chat in several circles takes the strictest rule. Drafts from background runs that need confirming wait on Home for you instead.

## Does Limmr change outgoing text?

Messages and file captions are delivered exactly as supplied. Limmr records the originating AI internally in message authors and Activity.

## Is everything local?

Limmr's stores and engine are on the machine running that engine (your Mac or dot). WhatsApp still communicates with WhatsApp. An AI client can receive the messages its permissions allow, and a cloud AI may send them to its provider. Remote access additionally makes an authenticated MCP endpoint reachable over your chosen HTTPS URL. There is no Limmr-hosted message server.

## Can an agent see my hidden chats?

The hidden list excludes chats for every agent on that engine. Hidden lists and AI permissions do not sync; configure them separately on both paired engines. Those chats should not appear in lists, searches, directory results or counts. Individual agents can have a narrower list too. Hiding a chat cannot recall data a client previously received.

## What access does a new agent get?

Local agents start with message/search, people reads/writes, summaries, media downloads and send requests. Remote agents start with message/search and people reads; writes, downloads and sends are off. Local circle access defaults to Everything with no expiry. Remote approval also defaults to Everything unless you select circles and a duration. Review both visibility and capabilities before connecting.

## Does closing the window stop Limmr?

No. The Go helper runs as a LaunchAgent so connected clients can keep working. Use **Settings → Stop engine** to stop it. Current circle send rules still apply: free-circle sends can proceed, confirm-circle background drafts wait on Home, and Never blocks sends. Closing the window does not change those rules.

## Can I move from whatsapp-mcp?

Choose **Move my existing setup** during linking. The migration provides a dry run, disables the old jobs without deleting them, copies the device and message stores, upgrades the schema and imports activity. It uses the existing linked session so you do not need another QR scan. Settings provides migration undo.

Migration transfers sensitive session keys. It runs only when you explicitly choose it, never during builds. Contributors must use invented stores, leave live services untouched and never open or copy the live session database.

## Is this an official WhatsApp integration?

No. Limmr uses the unofficial whatsmeow multi-device library. WhatsApp changes can break compatibility and using it carries account risk. Never use it for bulk messaging.

## Can I use it on Windows or Intel Macs?

The app targets macOS 26+ on Apple silicon. Other platforms are outside the current app requirements. The Go engine being portable does not establish support for the complete app elsewhere.

## Why Go inside a Swift app?

SwiftUI provides the native Mac interface. Go runs whatsmeow and MCP in one bundled helper, avoiding a separate Python or Node service. See [Architecture](ARCHITECTURE.md).

## Does the activity log copy every message?

Read activity stores message IDs; the app looks up the originals when you open a detail. Sent activity stores final text, and saved summaries store summary text. This is separate from the operational log, which never contains message text or credentials. Activity retention defaults to 180 days; it is not message-history retention.

## What should I include in a bug report?

Your macOS and app versions, steps using invented data, what you expected and a reviewed redacted diagnostics report from **Health**. Keep real conversations, phone numbers, credentials, pairing codes and databases out of issues. For a vulnerability, follow [private reporting instructions](SECURITY.md#report-a-vulnerability).

## What changes when I pair a dot?

Allowlisted owner knowledge syncs; messages/media, sessions, AI credentials, general settings and derived efforts do not. Profile photos and imported profile details may travel. Each engine enforces its own local hidden-chat and AI permission choices. While paired, only one engine (the Mac) runs reminders and check-ins; the dot never takes over on its own. See [Run Limmr on Linux or a dot](handoffs/DOT-ENGINE.md) for setup, scheduling and lease handover.
