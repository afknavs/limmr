# MCP client workflow

The local and remote transports advertise the same **20 tools** regardless of capabilities.
Each call rechecks permissions, circle/chat visibility, expiry and hidden chats. Private admin
tools are not exposed. The exact schemas are in [API.md](API.md#compact-mcp-catalog-t1).
Start with `whoami`, then catch up using `get_updates`. Use `find`/`read` for relevant visible
context and `brief` for a circle's current understanding. Exhaust pagination before updating briefs.
Expiry cannot recall data already received by an AI.

Call `more` with `{}` to discover rare action names, required/optional fields, full schemas and
descriptions. Execute an action with `{action,args}`; cached legacy aliases retain their original
validation and result shape. Unknown arguments/types, missing required fields and invalid enums
return accepted-field guidance without echoing the supplied value.

## Naming efforts

An effort is a shared purpose supported by conversations, not one activity row or one group.
Task/artefact evidence, request phrases, quote chains, participants, time and cohesion support
grouping. Broad chat blobs and one-off chatter are filtered. `confidence` scores supporting evidence;
it is not a calibrated probability, and high message volume does not automatically increase it.

1. Discover waiting names:

   ```json
   {"action":"efforts.unnamed","args":{"limit":20}}
   ```

   The result is `{efforts,total}`. Each candidate has `id`, `revision`, `top_terms`,
   `artefact_names`, `message_count`, `chat_count`, `participant_count`. MCP never supplies the
   owner's naming snippets or participant first names. A candidate is returned only if every
   involved chat is visible to the caller.

2. Read evidence as needed with `more {action:"efforts.get",args:{id}}`, then write a short
   supported title and state, using the candidate's exact ID:

   ```json
   {"action":"efforts.update","args":{"id":"<candidate id>","title":"Festival budget","summary":"Review the festival proposal."}}
   ```

   This invented title illustrates the shape; derive names from permitted evidence.
   Titles are 1–40 characters, summaries at most 500 characters. Editing requires `save_summaries`;
   reads require `read_messages` and `read_people`, and queries additionally need `search`.
   The update's `created_by` credits the AI; the named effort becomes eligible for app display/search.
   Due dates must match an explicit evidence date (RFC3339), or be empty to clear.

3. Read back with `efforts.get` and check the title/credit. The naming queue should no longer
   include it. `efforts.list` includes unnamed internal purposes too; control/app lists omit them.
   Other actions are `efforts.merge {id,other_id}`, `efforts.split {id,message_refs}` and
   `efforts.attach {id,message_refs}`. Each reference is `{chat_jid,message_id}`.

Efforts expose title, summary, status, optional due, people/chats, circle IDs, message/action refs,
timestamps, naming credit, confidence, pinned and hidden. Status is `on_you`, `waiting_on_them`,
`moving`, `done` or `dormant`. Hidden work stays out of lists. `brief(action=get)` and `get_updates`
can include a scoped `effort_naming_hint` when naming is allowed.

Activity can also name work through Apple's on-device model. Only the control API supplies bounded
snippets and first names; conditional revision writes protect competing edits and credit **This Mac**.
Requests start at least 20 seconds apart; uncertain/unavailable output stays queued. There is no remote
model fallback. Sample mode uses a deterministic invented response and no external model.
Activity explains waiting names and keeps **Detailed log** available even when no named efforts exist.

## Sends during an update

Send rules and explicit owner instructions still apply as documented in [API.md](API.md).
During a prepared update, outbound MCP calls fail promptly with the tool error
“Limmr is updating; try again in a minute”. Reads remain available. Wait and check send status
before retrying a delivery whose outcome is uncertain; a read/status call never resends.
Only the authenticated control API can prepare/resume: it drains for at most 10 seconds and
holds a five-minute process-local lease. Resume never retries queued work. The installer requires
the signed feed's exact engine version and attempts resume on success and failure, including rollback.
Installed macOS update/recovery remains an owner-run check; synthetic tests prove the contract.

## Audit

`engine/scripts/mcp-audit.sh < /dev/null` uses the real SDK and authenticated HTTP serialization
over in-memory pipes, synthetic stores and a fake messenger. It covers all listed tools, rare
actions, legacy aliases, argument failures and concurrent clients. A6 adds SDK naming/readback
and update-pause checks; see [A6-REPORT.md](A6-REPORT.md) for current catalog size and gate evidence.
Byte/4 token estimates are approximations, not model-tokenizer measurements.

## Tracking owner work with tasks

The twentieth tool, `task`, stores an owner workspace while efforts remain derived.
Use `create` with `effort_id` to track visible work, or provide a title, kind, chats
and people from scratch. `list`/`get` read it; `update` writes a summary, waiting or
schedule; `attach` adds links/files/notes; `checklist` manages steps. `draft_check_in`
and `draft_nudge` save text for owner review. `needs_you` flags attention with a reason.
Exact shapes and routes are in [API.md](API.md#tasks-and-the-task-timeline).

For a checklist addition, use `{"action":"checklist","id":"tsk_example","op":"add","steps":[{"text":"Read"}]}`.
`items` is accepted as an alias for `steps`, and `text` can supply one step. Change
checklists with this action rather than `update`. Set `lifespan_days` on short tasks;
omit it when creating a long task.

Reads need `read_messages`; writes also need `save_summaries`. You must see every
task chat. Only the creator or a visible task's own-preset AI can update it. Hidden
chat items never appear, including after a permission change. `get_updates` adds
`task_due` with task ID, note and progress, plus `task_waiting_cleared` and `task_closed`.

The minute ticker only parks check-in/nudge Suggestions. It never sends. For a send,
use the existing `send`/`send_message` path with `task_id` to record task history;
current circle and confirmation rules still apply. Supplying a task ID cannot grant
send permission. The separate status-credential task feed contains task summaries
and timeline labels, never message text or saved draft prose.

### Suggested work (Y3)

Circles are lasting parts of life. One-off projects, events and errands are tasks: use `task create`.
`task(action:"list",status:"suggested")` lists visible suggestions.
`task(action:"keep",id)` activates one without changing its origin;
`task(action:"dismiss",id,reason?)` marks it not a task and suppresses the same effort/chat-topic.
Both require `read_messages`, `save_summaries` and visibility of every task chat.
These two actions can claim fully visible suggestions regardless of creator; ordinary task
writes retain the existing creator/own-AI rule. Own AIs receive `task_suggested` updates,
including automatic tracking and migrated projects, subject to current chat permissions.
Project circle proposals need three named people across two chats; smaller proposals return
`{"converted_to_task":"tsk_…"}` with the proposal's why as the first suggestion signal.

Task lifecycle: `task create` / `task update` accepts `end_at` (RFC3339),
`end_condition` (`agent_done` or `checklist_done`) and `lifespan_days` (quiet days for short tasks).
An agent completes work with `task update {id,status:"done"}`. Timers queue a check-in
and a durable `task_due` update; they never send. The owner can Undo automatic closure.

Task argument errors are checked per action, including nested paths, and contain a correct call.
Examples: `create {"action":"create","title":"Exam preparation","kind":"long"}`;
`list {"action":"list","status":"open","limit":20}`; `get` / `brief` / `keep` use
`{"action":"get","id":"tsk_example"}` (substitute the action); `dismiss` adds `"reason":"Not needed"`;
`update {"action":"update","id":"tsk_example","status":"active"}`;
`attach {"action":"attach","id":"tsk_example","item":{"kind":"note","text":"Review next"}}`;
`checklist {"action":"checklist","id":"tsk_example","op":"add","steps":[{"text":"Read"}]}`;
`draft_check_in` / `draft_nudge` use `{"action":"draft_nudge","id":"tsk_example","text":"Please share the syllabus"}`;
`needs_you {"action":"needs_you","id":"tsk_example","reason":"Choose a time"}`.
Checklist `items` and bare `text` are accepted aliases; update uses the checklist action for steps.

N1 task reads: list defaults to `open` and excludes quiet. Request `status:"quiet"` or `"all"`
explicitly to include it. Quiet/wake with `{"action":"update","id":"tsk_example","status":"quiet"}`
or `status:"active"`. Quiet state and workspace activity wakeups sync to a paired dot; timers never send.
Stored-reference errors name nested fields such as `check_in.to`, `waiting.on.id`, and `steps[0].id`.
`lifespan_days` is accepted only for short tasks, including updates to an existing task.

`get_updates` and `brief {action:"get",id:"circle_id"}` include `recently:[string,...]`: three to five
plain lines from stored events, attributed where the writer is known, without message/note bodies.
It covers 24 hours or, when no permitted activity exists, three days. Counts, titles and fallback
selection respect the caller's current circle/chat access; a brief covers only its circle.

### Local files

Local agents may supply `path` to `send`, `upload_attachment` or `notify_me`, using
an absolute path or `~/poster.pdf`. Only regular files under the owner's home
are accepted. Symlinks, credential-like paths and Limmr data are refused. The
accepted bytes are copied and SHA-256 checked; changing the original cannot
change the approved file. Remote agents cannot use paths.

Limits apply to every source, using detected content: **documents 100 MiB;
images, video and audio 16 MiB each**. The document limit is Limmr's conservative
interoperability cap, not a claim that every WhatsApp client has the same maximum.
Inline base64 additionally has an 8 MiB decoded transport cap. Larger files must
use a link or be compressed. Documents retain their filename and MIME type;
images and videos carry the supplied caption. Audio with text becomes a document
so its text is preserved. `as_document:true` forces a document envelope without
relaxing the content's size limit. The main window's **Review files** popover shows an icon, name,
size and target chat, including captionless sends. Legacy file approvals show the
same preview in the menu bar.

### Remote and resumable uploads

Choose chunked upload for remote agents; no one-time public upload URL is
created. Keep **each base64 string <=1 MiB encoded** (<=768 KiB decoded).

1. Call `upload_attachment` with `filename`, `total_size`, `chunk_count`,
   `chunk_index:0`, and `content_base64` for the first chunk. Counts are 1..256;
   the total is 1..100 MiB and the media limit is checked before promotion.
2. Retain the returned `upload_id`. Submit remaining zero-based indexes with
   `upload_id`, `chunk_index`, `content_base64`. Chunks may arrive out of order.
   Repeating an index with identical bytes is safe; different bytes are refused.
3. After interruption call `upload_attachment {upload_id}` or
   `send_status {upload_id}`. Use `missing_chunks`, `uploaded_bytes` and
   `expires_at` to resume. Only the uploading AI can resume/reuse its upload.
4. Completion returns `attachment_id`. Send using
   `send {recipient,attachment_id,text?,key?,owner_instruction?}` under the usual
   send/approval rules. A 6 MiB PDF needs eight full 768 KiB chunks.

Uploads expire one hour after creation, including across restarts. There are at
most four active uploads per AI and 256 MiB reserved across the engine. An
expired upload must restart without `upload_id`. Completed files have the normal
seven-day outbox retention. For an approved paired send, the copied file also
travels in the private durable sync queue/history.

`send_status` accepts exactly one of `upload_id`, `message_id`, `approval_id` or
`key`. Upload status includes missing chunks and bytes. Send status contains
`upload.phase` (ready, uploading, uploaded), acknowledged bytes/total and the
original message ID plus sent/delivered/read timestamps and counts. The WhatsApp
library exposes upload start and completion, not intermediate byte percentages.

### Which device sends

`whoami.sending` gives `holder_side`, a plain-language `summary`,
`last_synced_at`, `lease_epoch`, `lease_expires_at`, `lease_active` and
`peer_ack_required`. The app shows the same role and last sync. A paired
non-holder queues approved sends; their identity and files sync to the holder.
The holder waits for a durable peer acknowledgement before dispatch. An expired
lease queues on both sides. Owner recovery never comes from an ordinary AI call.

A stored outgoing echo with the same reserved ID passively reconciles an
uncertain attempt as sent. No extra send or read acknowledgement is made.
An unknown delivery outcome is `uncertain`: **do not create another send to
retry it**. Restore sync and reconcile the original identity. Uncertain or
in-flight attempts block role transfer. A never-paired engine works standalone.
Unpairing a non-holder retains a local sending fence: wait until the old lease
expires plus one minute and explicitly confirm that the old device has stopped.
After unpairing each engine may dispatch only requests it originated; imported
queues remain quarantined. Read the [partition rule](lanes/SYNC-CONTRACT.md#coordination-schema-18-protocol-2).
