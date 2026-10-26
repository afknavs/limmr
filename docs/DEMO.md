# Synthetic demo walkthrough

This walkthrough is for invented data only. It requires no real WhatsApp link, Contacts grant, remote endpoint, OAuth approval or sending permission. The demo is distinct from a live installation.

## A one-minute walkthrough

| Time | Show | Explain |
| --- | --- | --- |
| 0–10s | Home heatmap and three counts | Messages, AI reads and AI sends; select a day to inspect Activity. |
| 10–25s | Suggested tasks | Keep a useful suggestion or choose Not a task. The AI can also create tasks directly. |
| 25–40s | People → Groups and a profile | Group members/access, custom or Contacts avatar, relevant work and the hidden-chat control. |
| 40–50s | Task workspace | Checklist progress, notes and linked items stay with the task. |
| 50–60s | Your dot settings | Knowledge sync is separate from local AI permissions. Only the Mac runs reminders while paired. |

This is a recording script, not an existing demo video or GIF. Produce a final recording only from reviewed synthetic captures; do not substitute a real account. Pointer hover appearance, complete screen-reader interaction and installed-app behavior need their own acceptance checks.

## Reproducible offscreen capture

Build the source app, then use its unbundled executable with a temporary home:

```sh
fixture_home=$(mktemp -d)
LIMMR_HOME="$fixture_home" app/.build/debug/Limmr --snapshot /tmp/limmr-home-demo.png \
  --page y2-home-open --titled-window --size 1280x820
```

The snapshot entry point uses the synthetic backend. Add `--dark` or `--increase-contrast` for the corresponding rendering. Existing pages include `y2-home-suggestions`, `y45-groups`, `y45-group`, `y45-custom`, `y45-initials`, `y45-contacts`, `q4-task-study` and `y7-dot-healthy`. The automation-pause and privacy disclosures must remain visible in any pairing demo.

The `round_y_acceptance.py` script records sequential captures, dimensions, hashes and failures. Its navigation-stress case uses a longer timeout because the 20-cycle offscreen layout test can take over two minutes. A snapshot proves a rendered state, not every mouse or keyboard interaction.

Before including media in a public candidate, inspect every frame for invented names/data, no credentials/QR codes and accurate current limitations. Historical screenshots are excluded by the exporter; public media must be selected deliberately. No media is uploaded or published by these commands.
