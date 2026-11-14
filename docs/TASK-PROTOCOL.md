# Task protocol: tasks only when the owner asks, and a list that keeps itself clean

Status: Phase 0 spec, 11 Nov 2026. Nothing here is built yet. Builds on `docs/TASK-RULES.md` (rules 1–8 stay in
force unless this file says otherwise). Code references are to `lane/n4-look` at `0086470`.

## 0. The owner's words

- "A task gets formed only if I ask it to, like I ask my agent."
- "Tasks that show up in limmr automatically can be suggested or visible internally as suggestions for my AI so
  that it can pull and reference them and maybe surface them."
- The list must clean itself "by protocols", not by anyone editing his data by hand.

## 0b. Owner decisions (11 Nov 2026)

- Q1: a yes he gives an AI in that AI's own chat counts as his ask; the AI quotes it. Mostly the dot (remote agent).
- Q2: old automatic tasks he never acted on go back to suggestions, **silently**: no notifications, no Needs you,
  no banners. The Suggested row stays collapsed and never nags. This holds for every automatic rule change.
- Q3: add a `test` kind; a test task ends after one day.
- Q4: obsolete after 3 weeks without activity (7 quiet + 14), **unless the task states its own lifespan or end**.
- Q5: circles archived by the October migration stay archived when their task is demoted.

## 1. Three levels

| Level | Stored as | Who creates it | Who sees it |
|---|---|---|---|
| **Detected work** | effort (`engine/internal/efforts`), unchanged | the engine | AIs through `more` effort actions; owner in effort detail |
| **Suggestion** | task record with `status=suggested` (exists today) | the engine's detector, or an AI that has no quoted owner request | AIs (`task list status=suggested`); owner only in a collapsed "Suggested" row on the Tasks page |
| **Task** | task record with `status` active / waiting / quiet / done / dropped | only the owner's ask (see §2) | everyone, as today |

Decision: **keep suggestions in the task table** with `status=suggested`. The code already stores, lists, keeps,
dismisses and expires them this way (`tasks/surfacing.go`, `tasks/service.go:324–369`). A separate table would
rewrite storage, sync and the app for no gain. What changes is how suggestions are counted and shown (§6) and who
may turn one into a task (§2).

## 2. Who can move what

| Move | Owner (app / control API) | AI over MCP | Engine by itself |
|---|---|---|---|
| Detected work → suggestion | – | yes (`task create` without `owner_request`) | yes (detector) |
| Suggestion → task (keep) | yes | only with `owner_request` | **never** |
| Create a task directly | yes | only with `owner_request` | **never** |
| Task → suggestion (demote) | yes | no | only in the one-time pass (§5), with undo |
| End a task (done / dropped) | yes | yes, as today | yes, by the rules in §3, each with a reason |
| Reopen | yes | no (unchanged) | no |
| Dismiss a suggestion | yes | yes | after 14 days (exists today) |

**The quoted-request gate.** MCP `task create` and `task keep` gain one optional field, `owner_request`: the
owner's words asking for the task, quoted, plus where he said it (`message_ref` when it was a WhatsApp message, or
`said_in: "chat with agent"`). This mirrors `owner_instruction` on sends.

- With `owner_request`: a task is created / kept. The quote is stored on the task (`asked_by_owner`) and in the
  creation history line.
- Without it: `create` makes a **suggestion** instead and answers `{created: "suggestion", id, instruction: "Ask
  the owner; keep it with owner_request once he says yes"}`. Not an error. `keep` without it is refused with the
  same instruction.
- Chat text is still not authorisation: an `owner_request` quote must be the owner's (author.type=owner when a
  `message_ref` is given). An AI cannot quote a third person.

**Removed:** auto-keep. `detect` always creates `suggested` (`tasks/surfacing.go:418–423` drops the `auto` branch);
`SuggestForAgent`'s auto-keep branch goes too (`tasks/surfacing.go:477–499`); the demo seed switches to plain
suggestions. The v10 circle→task migration (`hub/task_surfacing_migration.go`) already ran on his data; it is
left as is for history, and the tasks it made go through §5 like other automatic ones.

**Sync.** Synced task records (`sync/service.go:464–481`) come from his own devices and are trusted as they are.
The gate applies where a record is first made, not on replication.

## 3. Self-cleaning rules

Every automatic change writes one history line with the rule name and the reason in plain words, stores the
before-state in a `changes` object, and can be undone by the owner from the task's history for 30 days (not the
24 hours today's auto-keep undo allows). Rules run on the engine tick, never send anything, and never touch a task
whose field the owner corrected (owner edits are sticky, as in rule 5).

### R1 Duplicates attach, they don't multiply
- **When:** a suggestion is kept, or a task is created (owner or AI), and the existing duplicate check
  (`tasks/rules.go:40–103`: shared chat or person, plus same effort or similar title/goal) finds an open task.
- **Does:** for a suggestion being kept or an engine suggestion: attaches its chats, people and message refs to the
  existing task, drops the suggestion with `close_reason=merged`, `merged_into=<task>`. For a direct owner/AI
  create: unchanged — refusal with candidates; `different_because` still overrides.
- **Evidence:** the duplicate check's matched fields, written into the history line ("Same chat and similar title as
  'Science fair'").
- **Undo:** splits the attached chats/people back off and restores the suggestion.
- **Needs:** an internal *attach evidence* operation with per-membership provenance (who/what added each chat and
  person). Update cannot add chats/people today (`tasks/actions.go:30–52`).

### R2 Finished
- **Strong evidence → ends the task** (`done`, reason stated): end time passed (exists, rule 1); checklist complete
  with `checklist_done` (exists); the owner's own message in a linked chat that says done/submitted/sent/paid *and*
  refers to the task's goal or a task file. Detection of that message uses the owner's text only
  (author.type=owner), never other people's.
- **Weak evidence → a "looks finished?" hint**, never a silent change: the linked effort went `done`/`dormant`, or
  someone else said it's done. The hint is a field on the task (`finish_hint: {why, message_refs, at}`) shown to
  AIs in `list`/`get`; not Needs you, not Home. An AI may raise it with him and close the task when he agrees.
- **Undo:** reopen (exists, `tasks/lifecycle.go:43–60`); the hint expires after 7 days.
- **Fix on the way:** the closure history line must carry the reason; today the event text is generic and the app
  labels deadline expiry as "Closed after inactivity" (`tasks/service.go:830–848`,
  `LimmrKit/TaskListPresentation.swift:17–25`).

### R3 Obsolete
- **When:** a task has been `quiet` (7 days without activity, exists) for a further `lifespan_days` (default 14,
  `tasks/service.go:523`) with no owner action.
- **Does:** ends it as `dropped`, `close_reason=obsolete`, history "No activity for 21 days".
- **Undo:** reopen. Any new activity before the deadline wakes the task as today.
- A task with an explicit `lifespan_days` or `end_at` keeps that instead of the default (Q4).
- **Test kind (Q3):** `kind=test` tasks end (`dropped`, `close_reason=obsolete`, "Test task, ended after a day")
  one day after their last activity. No title-based guessing.

### R4 A task follows its work
- **When:** the effort a task came from is split, merged or regrouped (owner or automatic).
- **Does:** drops chats and people that were added *from the effort* and no longer belong to it; keeps anything the
  owner added, anything a task file, checklist step, waiting entry or subject evidence refers to. One history line:
  "Removed <chat> and <person>: they belong to other work".
- **Undo:** puts them back.
- **Needs:** the same per-membership provenance as R1. The uncommitted attempt in `lane/n4-effort-merge`
  (`engine/internal/tasks/effort_links.go`) has this shape; reuse it only after its tests are rebuilt (they never
  ran: the fixture made one effort where two were needed).

### R5 Old work is not continued from another group (grouping, last step)
- A message in one group joins older work from a *different* group only if it quotes it, carries the same file or
  link, or that work was active in the last 14 days. Smallest change to `efforts/group.go`; list every existing
  test that expected an older cross-group merge. The first attempt broke five packages; this step goes last and
  alone.

## 4. Today's leftovers this spec also fixes

- Ending a *suggestion* must not mark its effort as done in effort surfacing (`efforts/surfacing.go:468–483`).
- The detector first applies R1 evidence attachment for a matching task. It then suppresses candidates when every
  candidate chat already belongs to open work (active, waiting or quiet), even across multiple tasks, or when a
  pending suggestion has the same chat set. Partial coverage may still suggest. This changes detection only;
  existing records are not retroactively dismissed or changed.

## 5. Existing tasks go through the same rules once

A one-time pass, `protocol_v1`, runs each rule above on every existing record, plus one extra step:

- **Demote automatic, untouched tasks to suggestions.** Candidates: `auto_kept_at` set (detector auto-keep or the
  v10 circle migration) or `origin=agent` without `asked_by_owner`. A candidate stays a task if the owner ever
  touched it: event `kept` by you, `reopened`, owner note, item `added_by=you`, `subject_edited`/`circles_edited`,
  an owner-sent update, owner checklist toggle, or a direct owner status change. Otherwise it becomes
  `suggested`, history "Moved back to suggestions: Limmr made this, you never asked for it", with undo (keep).
  Circles archived by the v10 migration stay archived.
- **Limit the code cannot remove:** there is no record that he *opened* a task, and title/goal edits leave no
  history event (`tasks/actions.go:227–231`). So "never touched" is a best guess. That is why the pass is dry-run
  first and he sees the list.

Order: (1) dry run on an offline copy — `cmd/task-rules-audit --protocol` prints, per task, each rule that would
fire, the evidence and the result, in plain words; (2) he is shown that list; (3) only after his yes, the
engine applies it once on start, guarded by a stored marker so it never runs twice. Every change keeps its undo.

From then on the owner app records `owner_opened_at` on owner GET, so future passes don't have to guess.

## 6. What the app shows

- **Home:** only real tasks. `/v1/home` stops appending three suggestions (`api/circles.go:34–42`).
- **Tasks page:** "Open" counts active/waiting/quiet only. Suggestions sit in one collapsed row "Suggested (n)" at
  the bottom; each has Keep and Dismiss. Nothing from suggestions in Needs you or the sidebar badge.
- **Task history:** every rule line shows its reason and an Undo button while the undo is valid.
- **Task detail:** a "Remove from task" control for chats and people (today missing, `tasks/actions.go:15–52`);
  owner removals are sticky against R1/R4.

## 7. What an AI sees

- `task list` default (`open`) shows tasks only. `status=suggested` lists suggestions with their evidence:
  `suggestion.{signals, confidence, detected_at, effort_ids}`, chats, people, and `effort_id` for `more` effort get.
- `task get` on a task shows `asked_by_owner`, `finish_hint`, and the rule lines in history.
- New server-instruction line: "Never create a task he did not ask for. Without his quoted request, `create`
  makes a suggestion; mention it when relevant, keep it with `owner_request` once he says yes." Same in
  `docs/MCP.md`.
- **Catalog budget:** the MCP catalog is at about 20,960 of 21,000 bytes (`mcpserver/tools_test.go:71–81`).
  `owner_request` must be paid for by shortening existing task descriptions; the budget test is not raised.

## 8. Build order (Phase 1–4 of the plan)

1. Ask-only gate + auto-keep removed + suggestions off Home/counts (engine + MCP + `/v1/home`).
2. Per-membership provenance + attach-evidence + remove-from-task (engine + control API).
3. R1 duplicates attach. 4. R2 finished + reasons in closure history. 5. R3 obsolete. 6. R4 task follows its work.
7. `task-rules-audit --protocol` dry run with the §5 demotion step.
8. App: Suggested row, history Undo, remove control. 9. R5 grouping rule, alone and last.
