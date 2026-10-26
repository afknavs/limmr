# Handoffs and triggers

Builds on `docs/CIRCLES.md`. Not built yet: this is the design for the next lane.

## The problem
"Check Maya's chat and keep making changes to the website until she gets what she wants." Today that means an
agent polling WhatsApp every few minutes: wasteful, slow to respond, and it forgets the project between runs.

## The idea
A **handoff** is an explicit, time-boxed permission for one AI session to talk to one person (or one chat) for you,
about one thing, and to be **woken up the moment they message**, with its full memory of the work.

```
You, in Codex/Claude Code, inside the website project:
  "Maya will message me on WhatsApp. Talk to her for me until she's happy with the site — make the changes she
   asks for. Stop after 3 days."
        │  AI calls start_handoff(...)  → your client asks "Allow?" → you click Allow
        ▼
Limmr stores the handoff: who, goal, which AI session, limits, expiry
        │
Maya messages ─▶ engine sees a new message in a handed-off chat
        │  waits ~60 s for her to finish typing a burst
        ▼
engine runs:  codex exec resume <that session id> "<her new messages + the handoff rules>"
   (or claude -p --resume <id>), in the project folder
        │  the same session that built the first design now has her feedback and full context
        ▼
AI edits the site, replies via Limmr, logs everything
```

No polling: the engine is already receiving every WhatsApp message in real time, so a trigger costs nothing until
the person writes.

## Model
```
Handoff   id, chat_jid / person_id, circle_id?, goal (your words, quoted verbatim), granted_in (agent + session id +
          the exact grant text + time), runner (codex|claude_code), workdir,
          expires_at, limits {max_replies_per_hour: 12, quiet_hours: 23:00–08:00 queue-only,
          max_runs: 50}, escalate_when [money, dates/commitments, personal topics, "talk to the owner",
          anything the AI is unsure about] → pauses that thread and notifies you,
          status (active|paused|ended|expired), stats (runs, replies, last_run_at)
Trigger   the general form: on (message_received | message_matching | mention | keyword) in (chat|circle|person)
          → do (wake_session | notify_me | park_suggestion). A handoff is a trigger plus a send permission.
```

## Rules that keep it safe (and not creepy)
- **Created only by explicit authorisation.** `start_handoff` is an action tool (client shows Allow), must quote your
  instruction, and shows you a summary card: who, goal, which AI, until when. One click on Home or in the menu bar
  ends it.
- **Exact text, internal attribution.** Replies are delivered as supplied. The originating AI is recorded in message authors and Activity.
- **Narrow permission.** The handoff only lets that AI send to that chat. The circle's other rules don't loosen.
- **Escalation, not improvisation.** Money, promises, dates, personal topics, or "can I talk to the owner" → the AI
  replies "I'll check with the owner" and Limmr notifies you.
- **Limits and expiry** by default (3 days, 12 replies/hour, quiet hours queue replies as suggestions).
- **Everything visible.** Home shows "Active handoffs" with the last exchange, Activity logs each run (what woke it,
  what it changed, what it sent).

## Other triggers it unlocks
- "Tell me when my HOD posts in any university group" → notify_me.
- "When Mum sends a photo of a bill, add it to the expenses sheet" → wake a session with that job.
- "If anyone in Internship mentions the demo, draft me a reply" → park_suggestion.

## Later: drafts in bulk (keep in mind, not now)
A clinician says to their agent: "Everyone who hasn't filled in the feedback form, draft a reminder." The agent
queues one draft per person; Home shows them as an editable batch (edit each, Send all, Send selected, Discard).
Same suggestion machinery, plus a batch view and per-message edit before sending.

## Watches (Home widgets)
A **watch** is a standing check on something, created by you or by an AI on your say-so:
- "Tell me when anyone posts about the exam" → scope: University circle; match: exam/test/IA/syllabus mentions;
  action: notify (Mac notification + the dot in Limerence).
- "Keep me updated about Design Club 2026 from Noor and Maya" → scope: those people across circles; action: keep a running
  digest on the watch and notify on asks/decisions.
- A handoff is a watch whose action is "wake this AI session and let it reply".

Home shows watches as a single quiet row of small widgets (max 4 visible, "+N" to expand), each: what it watches,
where, one line of the latest hit ("2 new · IA date moved to 24 Oct"), and a state dot. Click → a compact card:
latest hits, the digest, **Add instruction** (one text field that appends to the watch's instructions, e.g. "ignore
the lab batch B posts"), Pause, Stop. No other chrome. Watches are created from chat ("watch the class groups for exam
news") via `create_watch`, an action tool with the client's Allow, or from a circle page ("Watch this circle…").

## Group participation (later, opt-in)
Many people feel pressure to contribute in work or class groups they don't care about. Limmr can suggest
thoughtful contributions ("Ananya asked for feedback on the deck; here's a reply that picks up her point about
slide 4"), shown as suggestions you send, or (only if you set that circle to allow it) sent by the AI under a watch grant. Default:
suggestions only. Never pretend to have read something you haven't; contributions cite what they reply to.
Later: "this thread is asking for a tool — want me to start a coding session to build it?"

## Later: two editions (keep in mind, not now)
1. **Limmr (the app)**: what exists: download, QR setup, circles, approvals, the full UI.
2. **Limmr for agents (headless)**: discoverable when someone asks any AI "set up a WhatsApp MCP for me". One
   install command, QR in the terminal, full access by default, customised from the terminal
   (`limmr allow/deny/rule …`), same engine and data, so it can later be opened in the app. Needs: a
   terminal QR renderer, a CLI for permissions and circle rules, an MCP-registry listing, and a README written
   for AIs to follow.
