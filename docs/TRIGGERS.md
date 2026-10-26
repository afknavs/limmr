# Watches and session handoffs

Watches are wired into the Go engine: a versioned hub.db migration, transactional
SQL Store, one runtime per process, live-message queue, control routes, Home,
Activity notifications and action MCP tools. The macOS app consumes the exact
[watch contract](lanes/WATCHES-CONTRACT.md); engine wiring does not require a
separate notification process. All build verification uses invented fixtures,
fake session runners and fake Messengers, never a live WhatsApp connection.

A **watch** batches matching messages and notifies, retains hits for a running
digest, or parks an action Suggestion in its circle. The digest text is written
by an AI through `update_watch(digest=...)`; the engine does not generate prose.
A **handoff** resumes an explicitly granted Codex or Claude Code session. Its
send grant covers one chat; current per-agent permissions
and [circle rules](CIRCLES.md#3-send-rules-per-circle-not-per-group-chat) still apply.

## Matching and batching

A Watch records its owner, title, verbatim goal, scope, match, action, append-only
timestamped instructions, status, expiry and original grant provenance. A Record
contains that Watch and an optional HandoffConfig, without duplicate state.
`create_watch` and `start_handoff` reject empty `grant_text`. The quoted instruction
is preserved, not paraphrased. Handoffs default to 72 hours, 12 reply attempts per
rolling hour, 50 runs and local quiet hours 23:00–08:00. Explicit limits must be
positive. Failed/interrupted reservations count toward the run limit.

Scope selectors are a union of chat, confirmed circle and canonical person IDs.
A handoff additionally narrows wakes and replies to its exact granted chat.
The MCP layer validates every scope selector against the authenticated creator's
visibility; it rejects partially visible circles. Current chat permissions are
checked again when messages match and when a debounce batch becomes due.

| Match | Behavior |
| --- | --- |
| `any` | Any incoming message in scope. |
| `keywords` | Any supplied word/phrase, Unicode case/accent folding with word boundaries. |
| `regex` | Go RE2; specify `(?i)` for case-insensitivity. Invalid patterns are rejected. |
| `mentions_me` | WhatsApp mention metadata, never a textual `@me` guess. |
| `from_people` | Canonical sender person ID (or sender ID) in the match's people list. |

The WhatsApp live event handler first persists the message and its media metadata,
then calls a nonblocking enqueue operation. A 512-entry bounded queue moves person
and confirmed-circle enrichment and matching off the storage path. Duplicate
stored live events and history-sync/replayed messages do not enqueue. Queue
overflow skips watch processing for those events, preserves message storage and
reports a degraded Watches health check. Pending debounce batches are memory-only;
there is no crash replay or durable queue/outbox.

Each watch waits 60 seconds after its latest matching arrival, with a maximum of
five minutes after the first arrival. Separate watches do not delay each other.
Incoming messages during a handoff run accumulate for its next batch. Only one
run per handoff is allowed, including a unique persisted `running` constraint.
Instructions appended during one run appear in the next run's prompt. Failed
runs are recorded/notified and never automatically retried.

`FromMe` is suppressed immediately. The queue worker also looks up recorded
outbound message IDs, including echoes without `FromMe`. It never infers
Limmr authorship from a signature in message text.

## Persistence and lifecycle

Hub schema version 3 applies embedded `triggers.Schema`, adds persisted widget
state (`new_hits`, digest, delivered reply count), run-credential bindings and an
outbound-ID registry. SQLite foreign keys are enabled on each connection.
`SQLStore.Update` reserves the write transaction before read/apply/validate/write;
SQL triggers also enforce append-only instructions and immutable original grants.
Hit/run final states are upserted by stable IDs. Initial hit inserts increment
`new_hits` by their message count; finalisation never counts a hit twice. Detail
returns the newest 20 messages, while the Store's hit-history API returns the
newest requested batches oldest first. Digest text and counters survive restart.
Reply reservations use `BEGIN IMMEDIATE`; count and insert share the transaction.
Timestamps written by the Store have fixed-width UTC encodings; rolling-hour
queries compare instants so mixed RFC3339 offsets do not corrupt budgets.

Startup recovery pauses interrupted `running` handoffs and finalises their runs
and hits as cancelled, with exit code -1. It never resumes or replays a CLI.
Run credentials from the previous process are deleted. Health reports the
recovery reason: verify the previous session has stopped before explicitly
resuming. The process starts one `Engine.Run(ctx)` and drains/cancels its runner
workers before closing databases at shutdown. Synchronous/asynchronous runtime
failures and queue overflow surface as one `Watches` check (`ok` or `warn`, with
a degraded reason); private error strings never enter health diagnostics.

The widget API exposes summaries and details, not raw Records: no credential,
workdir, full grant provenance, log path or log content. `runner.label` includes
only a project basename. Detail does not clear unread hits; the app calls `/seen`.
Stopped/expired watches cannot resume. `/v1/watches` includes active/paused first,
then at most five recent ended records; `/v1/home.watches` includes active/paused.
Exact routes, schemas and tool parameters are in [API.md](API.md#watches-and-handoffs).

## Actions and Activity

- `notify`: publish `watch.hit` on the existing Activity SSE path, with the watch
  ID in `note` and wake message IDs. The app posts the Mac notification.
- `digest`: persist batches and unread state; no notification for every hit.
  An AI writes the digest with `update_watch`.
- `park_suggestion`: create an action Suggestion in the watch's circle (or the
  matched chat's confirmed circle), attributed to the granting integration.
- `wake_session`: resume the recorded session with the original goal/grant,
  timestamped instructions and batched message data in stdin.

Activity records creation, pause/resume/stop/expiry, instruction additions, each
run's wake message IDs, state, exit code and successful reply count, plus
escalation pauses and failures. Runtime state changes also invalidate pending
handoff drafts and their queued Suggestions. Message text belongs to local
hit/digest/draft storage, never the general engine log or diagnostics.

A person's stop phrase (default `stop` or `talk to <your first name>`) pauses before waking,
even if it does not match the watch's keyword filter. Explicit `escalate:true`
pauses through `update_watch`. Money, commitments, dates, personal topics and
uncertainty are included in the session prompt; semantic detection is the AI's
responsibility. Notifications for pause/expiry/failure use the same Activity path.

## Session identity and send policy

Every wake mints a random MCP-only bearer credential whose hash is bound to the
granting integration, resumed session, handoff and currently running reservation.
It is deleted when that run returns. Authentication also rechecks the live run,
active handoff, granting identity and enabled integration. Reads through this
credential are narrowed to the granted chat. A handoff ID or arbitrary request
header is context, never authentication. Background sessions cannot create new
grants or mutate another watch. Normal agent/control credentials retain their
existing boundaries.

At draft creation and dispatch the engine checks the binding, current base
permissions, exact chat and strictest circle rule. Text and captions are delivered unchanged. `SendPolicy` returns `allow`, `park` or `deny`; `ReserveSend` repeats
those checks and atomically consumes a slot immediately before delivery. The
runtime's trusted clock controls quiet hours, expiry and budgets. Failed delivery
attempts consume their slot. Successful replies are counted separately.

Quiet hours, exhausted hourly budgets and background `confirm` circles become
signed reply Suggestions; no Messenger call occurs. A `never` circle rejects the
send. A handoff does not loosen circle rules. Owner dispatch of a parked draft
rechecks current policy too. Pause, stop or expiry revoke the run's usable
credential and cancel pending drafts. An already dispatched network operation
cannot be recalled. No unapproved send path is added by watch creation.

## Runner permissions

Separate argv elements are used throughout, never `sh -c`:

```text
codex -C <workdir> -s workspace-write -a never [Limmr MCP overrides] exec resume <session_id> -
claude -p --resume <session_id> --permission-mode acceptEdits --allowedTools Read,Edit,Write,MultiEdit,Glob,Grep,NotebookEdit,mcp__limmr__* --strict-mcp-config --mcp-config <Limmr configuration>
```

Codex's Limmr MCP configuration is overridden for this process with the
local URL and a runtime-only bearer environment variable; existing Limmr
HTTP headers are cleared. Claude receives a strict Limmr-only MCP config
and a file-tool/Limmr tool allowlist. No `--dangerously-*` options are used.
Both run in the granted absolute workdir. This is a session continuation, not a
new anonymous AI job.

Inherited environment is limited to `PATH`, `HOME`, `TMPDIR`, `LANG`, `LC_ALL`,
`LC_CTYPE`, `TERM`; inherited provider keys, shell/loader hooks and caller handoff
variables are dropped. The runtime adds `LIMMR_HANDOFF_ID` and its fresh
`LIMMR_RUN_TOKEN` credential. It never opens CLI login files. Executable
lookup accepts only absolute PATH directories. Production may supply trusted
absolute binary paths in `ProcessOptions`.

Timeout defaults to 15 minutes. Timeout/cancellation kills the process group,
including helper processes; completed parents also have lingering helpers
cleaned up. Combined stdout/stderr goes to an exclusive 0600 per-run log in a
0700 directory below LIMMR_HOME, capped at 20 MiB per run. Logs may contain
private session output and are never exposed through the card or diagnostics.

## Offline verification

```sh
cd engine
LIMMR_HOME=$(mktemp -d) GOFLAGS=-mod=mod GOPROXY=off \
  GOCACHE=$PWD/../.cache/go-build go test -race ./...
```

Integration fixtures cover incoming message → persisted hit → route/Activity,
canonical sender/circle/mention enrichment, history and self-send suppression,
permissions changed during debounce, scoped agent/session/chat checks,
quiet hours/budget/circle parking, pending cancellation, SQL concurrency,
restart recovery, and watch control/MCP contracts. Process-runner tests launch
only fake executables in temporary directories. `--demo` installs a fake runner
for all handoffs, including those created through MCP, and seeds Exam news,
Atlas updates and Website changes. `scripts/demo_proof.py` checks their real
HTTP/MCP surfaces when the environment permits a loopback listener.
