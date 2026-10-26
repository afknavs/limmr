# Limmr

Your WhatsApp, connected to the AI tools you use—with permissions you control.

Pronounced “limmer”. Limmr brings your people, groups, tasks and AI activity into a native Mac app, with a standalone engine for Linux and your dot.

## Get started

1. Download the Mac DMG from [Releases](https://github.com/afknavs/limmr/releases), drag Limmr to Applications and open it. The beta is ad-hoc signed and not notarized; if macOS blocks the first launch, use **System Settings → Privacy & Security → Open Anyway**.
2. Follow the [setup guide PDF](docs/pdf/Limmr-Setup-Guide.pdf) or [step-by-step guide](docs/SETUP.md). Try the invented demo, then link WhatsApp and choose **Connect an AI**.
3. Review your AI's access and use **Copy setup prompt** to let it finish connecting.

Paste this into your AI to begin:

```text
Help me connect Limmr, the Mac app that gives my AI permissioned WhatsApp access.
Walk me through installing it from https://github.com/afknavs/limmr/releases,
trying demo mode and choosing my client in Connect an AI. Ask me to approve the
agent in Limmr and copy its setup prompt. Hosted ChatGPT/claude.ai need Remote
access and OAuth approval on my Mac. Keep credentials private. Once connected,
call whoami and explain my access. Do not send messages during setup.
```

The Mac app requires Apple silicon and macOS 26 or newer. [Run Limmr on Linux or a dot](docs/handoffs/DOT-ENGINE.md) from a clone or source ZIP with one build command.

## What it does

- **Home:** circles, tasks, suggestions, message-history heatmap and things that need your attention.
- **People:** a shared map of people and groups, profile details, photos and briefs your AIs can help maintain.
- **Tasks:** durable work with checklists and workspace items. Keep or dismiss suggestions; create tasks yourself or with your AI.
- **Permissions:** each AI gets its own profile, chat/circle access, capabilities and optional expiry.
- **Activity:** see what each AI read, changed or sent and follow the work it belongs to.

Connect ChatGPT, Claude, Claude Code, Codex, Cursor or another MCP client. Circles are lasting groups of people in your life; tasks are time-bound work; groups are WhatsApp group chats.

## Mac only, dot only, or Mac + dot

| Setup | How it works |
| --- | --- |
| Mac only | The app and its engine run on your Mac. Closing the window leaves the helper running. |
| Dot only | A Linux engine connects your AI directly. Keep it running to receive messages and run automations. |
| Mac + dot | Each engine has its own WhatsApp linked device. People, photos, circles and tasks sync; messages, media, sessions, AI credentials and machine settings stay separate. |

While paired, only one engine runs reminders, watches and other automations: the Mac initially. The dot never takes over on silence. The holder can hand over authority; the peer starts after its next sync. Unpair on both sides to return to standalone operation. Hidden chats and AI permissions are local to each engine, so review both. Sync runs on the dot's schedule. [Linux and pairing guide](docs/handoffs/DOT-ENGINE.md).

## Privacy and sends

Data stays on the machine running each engine. There is no Limmr-hosted message service. An AI receives only the data its permissions allow; a cloud client may send that data to its provider. Revoking access stops future calls and cannot recall data already received. Synced people details and photos are shared owner knowledge.

Hide chats from every AI or restrict an AI to selected circles. Hiding a DM does not hide that person's messages in a visible group. Remote access is optional and uses HTTPS/OAuth; keep the control API private.

Circle send rules are **send freely**, **confirm in chat**, or **never**. The strictest rule wins. Confirmation relies on the AI following the conversation; the engine cannot prove a person clicked a button. Use Never where that matters.

Limmr uses the unofficial whatsmeow WhatsApp library, so WhatsApp changes may break compatibility and using it carries account risk. [Security model](docs/SECURITY.md).

## Contributing

Bug reports, small fixes and accessibility improvements are welcome. Start with [Contributing](CONTRIBUTING.md), the [architecture](docs/ARCHITECTURE.md), [beta guide](docs/BETA.md) and [FAQ](docs/FAQ.md). Use invented data and fake WhatsApp for development. Never attach real conversations, databases, tokens or pairing codes to issues.

Report vulnerabilities privately through [GitHub Security → Report a vulnerability](https://github.com/afknavs/limmr/security/advisories/new).

## Licence

[MIT](LICENSE), copyright 2026 Abhinav. Limmr uses [whatsmeow](https://github.com/tulir/whatsmeow), [whatsapp-mcp](https://github.com/lharries/whatsapp-mcp) and the [Very Good Plugins fork](https://github.com/verygoodplugins/whatsapp-mcp). Upstream notices and dependency licences are retained in [NOTICE](NOTICE).
