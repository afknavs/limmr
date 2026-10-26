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
