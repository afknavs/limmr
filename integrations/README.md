# Optional Limmr guidance for your AI

Connect the Limmr MCP server first using [Connecting](../docs/CONNECTING.md). The server supplies its own instructions and permission-filtered tools, so these extras are optional. They teach the same setup, context, privacy and send protocol; they do not create a connection or grant access.

| Package | For | Contents |
| --- | --- | --- |
| [Claude Code plugin](claude-code/) | Claude Code | `.claude-plugin/plugin.json` and `skills/limmr/SKILL.md` |
| [Codex skill](codex/skills/limmr/SKILL.md) | Codex | Skill with name/description frontmatter |
| [Short instructions](chatgpt/instructions.md) | ChatGPT / Claude.ai | Text for custom instructions or a Project |

## Add from the app

In the Claude Code or Codex connection sheet, choose **Add the Limmr skill** if you want it. It is never preselected or installed by connecting MCP. The app copies the bundled skill into `~/.claude/skills/limmr/` or `~/.codex/skills/limmr/`, shows what it wrote and offers **Remove**. ChatGPT and Claude.ai offer **Copy instructions** instead; paste the text into the desired client or Project yourself.

## Add by hand

Run these only when you choose to install, from the repository root. Check an existing destination before copying; do not replace your own customised skill without a backup. Choose either the Claude plugin or the standalone Claude skill to avoid duplicate guidance.

For a Claude Code session, load the plugin without installing it:

```sh
claude --plugin-dir ./integrations/claude-code
```

For a persistent standalone Claude Code skill:

```sh
mkdir -p ~/.claude/skills
cp -R integrations/claude-code/skills/limmr ~/.claude/skills/
```

For Codex:

```sh
mkdir -p ~/.codex/skills
cp -R integrations/codex/skills/limmr ~/.codex/skills/
```

Start a new client session after copying. To remove a manual installation, remove only the `limmr` folder you copied, after preserving any customisations. To stop using the session plugin, omit `--plugin-dir` on the next launch.

For ChatGPT or Claude.ai, copy [instructions.md](chatgpt/instructions.md) into custom instructions or the relevant Project. Remove that text there to undo it. No client settings are modified by this repository or its build.

## Try it

Ask your connected AI: **“Set up Limmr for me.”** It should first call `whoami`, then `get_started`, suggest circles for you to review in Limmr, fill only known people details and write briefs when permitted. A work-only AI must stay within the circles you shared. See the [work AI recipe](../docs/CONNECTING.md#share-work-keep-family).

## Package checks

From the repository root, `claude plugin validate integrations/claude-code` checks the Claude plugin. Tool names and argument shapes follow [API](../docs/API.md) and [Round 10](../docs/lanes/ROUND10-CONTRACT.md). Both full skills have the same content; the short instructions stay within 1,500 characters. These checks do not connect to WhatsApp or install a skill.
