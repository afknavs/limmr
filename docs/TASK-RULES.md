# Task rules

1. **Tasks end.** A task is one outcome with a short goal saying what done means. Set `end_at` (with a timezone) for an event or deadline. Mark it `done` when the goal is met, or `dropped` when abandoned (`close_reason: failed` for failure). At the first engine tick on the next local calendar day after its end time, it is archived. Ended tasks leave Home and open lists; `task list status=all/closed` and `task get` retain their history. Quiet days alone no longer finish a task. An explicitly chosen `checklist_done` condition still completes it.
2. **Follow-ups are new tasks.** “Email the participants” after an event ends gets its own goal and `follows` pointing to the ended task. `get` includes `follows_context` with the old people, chats and files when still accessible. This link grants no extra permissions. Only the owner can reopen, using the existing reopen control.
3. **One outcome, one task.** Create checks open, quiet and proposed tasks for shared chats or people and similar titles/goals. A refusal returns candidates and creates nothing. Update one, or repeat with `different_because` explaining the different outcome (at least three words). Merge B into A using `task merge id=A from_id=B`: A keeps both histories/files; B stays archived with `merged_into=A`. The override reason stays in creation history.
4. **Use the owner's words.** Titles have at most five words and 60 characters. Name the outcome and use familiar names, such as “Dad's career”. Do not copy job titles or jargon from a document. New MCP tasks require an explicit goal. Legacy internal/control callers omitting a goal record the outcome title as their goal; existing records are not rewritten.
5. **Evidence before attribution.** `people` are participants, not the person the task is about. Use `subject` only with `subject_evidence` containing accessible message references supporting that person. Unsure means unset. Owner corrections through the control API (`owner_correction: true`, including clearing the subject) are sticky; inference and agent edits cannot overwrite them. Automatic detection does not infer a subject from chat membership.
6. **Covers stay put.** Choose a relevant image from the task's own files once. Rank relevance to the goal, never chat recency. If none is suitable, pin one of three built-in covers selected from the task ID. New media cannot replace it. Use explicit `task set_cover id=… item_id=…` (or the owner's cover picker) to replace it. Deleted/inaccessible sources use the same stable fallback. No image generation.
7. **Circles last; tasks finish.** Family, university and close friends are circles. Projects, events, fundraisers and errands are tasks. Circle creation warns when its kind/name/goal looks finite. Only repeat with `task_override_reason` explaining the lasting life area. `task convert_circle id=… title=… goal=…` preserves all people/chats and archives the original circle, atomically. It never deletes it.
8. **Needs you expires.** Answered requests, ended tasks and requests with no follow-up for seven days leave Needs you. The shared `model.NeedsYouTTL` constant controls expiry. Requests and timelines remain in history; expiry changes surfacing, not stored evidence.

## Review existing data without applying changes

From `engine/`, run `GOFLAGS=-mod=mod GOPROXY=off go run ./cmd/task-rules-audit --database /path/to/offline-hub.db --timezone Asia/Kolkata` (optional `--now 2026-11-10T00:00:00+05:30`). The JSON lists tasks that would be archived, possible duplicate pairs and circles that look like tasks. It uses read-only, immutable SQLite, runs no migrations and makes no changes. Supply a consistent offline copy/fixture with its WAL already checkpointed. The command rejects live Limmr and WhatsApp paths. `tasks.AuditDatabase` is also available to fixture tests. These are review candidates, never automatic merge/conversion decisions.


For the one-time protocol pass, add `--protocol` (defaults to plain text), or `--protocol --format json` for structured output:

```sh
GOFLAGS=-mod=mod GOPROXY=off go run ./cmd/task-rules-audit --protocol --database /path/to/offline-hub.db --timezone Asia/Kolkata --now 2026-11-11T12:00:00+05:30
```

Each record shows its title/status, origin and AI creator, participant/chat counts, planned actions, plain reasons and evidence IDs/dates (dates only in text). Text groups outcomes under Stays a task, Becomes a suggestion, Ends and Left for you; Not checked counts missing-data rule checks, explained inside each task block. Automatic untouched tasks, including v10 circle migrations and unrequested agent tasks, go back to suggestions; recorded owner touches explain why a task stays. Demoted records do not undergo task-only rules; an already-passed end date can still expire the resulting suggestion. Duplicate real tasks are left for review, never merged. Separate Existing suggestions and Already ended groups avoid counting them as new demotions/endings. Existing suggestions show their 14-day expiry. Archived migration circles remain archived.

A hub-only copy has neither WhatsApp messages nor cached efforts (`effort_edits` are corrections, not efforts). R2/R4 report `not checked: needs messages, not in this copy`. When a supplied fixture/offline database includes the message schema, the planner uses the live completion readers and effort reconstruction plus the shared membership predicates; legacy membership candidates remain review-only. Inactivity uses stored last activity on hub-only copies and linked conversation activity when present. The audit command has no apply option, migration, marker write, notification or runtime launch. This command does not grant approval for a later apply.


## Approve and apply the one-time pass

Implementation and verification status: see `docs/reports/t2-step7b.md` and `t2-step8.md` (all checks green, 13 Nov 2026).
Live application is a separate owner approval and backup operation, performed later by the person applying it.
This worktree build and the earlier dry-run approval do not grant that permission.

Preview the owner's keeps on the same consistent offline fixture/copy:

```sh
GOFLAGS=-mod=mod GOPROXY=off go run ./cmd/task-rules-audit --protocol --keep task_example_a,task_example_b --database /path/to/offline-hub.db --timezone Asia/Kolkata
```

When separately authorized to apply, the applying person first backs up the store, then writes
`$LIMMR_HOME/protocol-v1-approved.json` with the actual approval timestamp and reviewed task IDs:

```json
{"approved_at":"2026-11-11T12:00:00+05:30","keep_task_ids":["task_example_a","task_example_b"]}
```

Restart the engine normally. After migrations and before the task runtime's first tick, it reads that exact file.
Absent, unreadable or malformed approval logs a skipped line and writes no marker. The timestamp must be RFC3339;
`keep_task_ids` must be an array of nonempty strings (an empty array is allowed). Unknown IDs are ignored and their
count is logged. Keeps prevent demotion, receive owner origin and the stored owner-request text
"Kept in your review of old tasks", and get that history line once. Ordinary deadline/completion/obsolete rules
still apply. The audit and startup use the same planner, including the keeps.

All changes, history, undo records and the marker commit in one transaction; errors roll everything back and
prevent startup from reaching the task runtime. On a consistent offline copy, check the marker with:

```sql
SELECT data FROM objects WHERE bucket='settings' AND id='protocol_v1_applied_at';
```

Its JSON string is the actual application time. Once present, the pass never applies again, even if the approval
file remains or changes. The pass never reopens circles archived by the v10 migration. Changes use normal task
and history sync capture and only the ordinary task-changed refresh event; they create no notification, Needs you,
Home/Recently line, notify_me request or other broadcast.

Undo a demotion through the task history's existing rule-change undo (control API:
`POST /v1/tasks/{id}/changes/{change_id}/undo`, using the change ID in that history line). It lasts 30 days and
can be consumed once. Undo restores the previous status and records an owner keep with the same owner-request
text, so the task passes the quoted-request gate later. It leaves migration circles archived. Endings and membership
removals use their existing rule undo protections.

## Snooze and detector coverage (u2)

Owner Later stores `later_at` and `later_until` on the synced task. Every attention surface uses the same snooze
and seven-day expiry projection. The same AI reason is a no-op; a different reason or a newly pending review after
`later_at` clears the snooze. Expiry simply restores the card, without a notification or light cue.

After the existing R1 duplicate/evidence rule, the detector suppresses a candidate if every candidate chat already
belongs to active, waiting or quiet tasks (the union counts), or a pending suggestion already has the same chat set.
Titles and chat order do not affect these two suppression rules. Existing records are retained unchanged.
