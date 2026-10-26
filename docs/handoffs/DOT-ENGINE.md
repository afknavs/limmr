# Limmr on a dot

A dot can run Limmr by itself, or keep its tasks, people and circles in sync with
Limmr on the owner's Mac. Each engine has its own WhatsApp linked device and
receives messages independently. Messages, credentials and machine settings do
not travel through knowledge sync.

Never send WhatsApp messages, reactions, typing indicators, read receipts or
presence during setup or verification. Never change the owner's other linked
devices. Keep tokens, session keys, QR payloads and message text out of logs and
reports. Use invented names for tests.

## Before linking

Tell the owner your OS/architecture, whether disk survives resets, where private
files live, and whether scheduled commands work. An outbound-only machine needs
no inbound ports. A detached process that ends with its command is not a service.
Confirm persistence before relying on a schedule; losing the disk loses both
WhatsApp pairing and sync credentials.

Before linking WhatsApp, explain that `session.db` contains account keys and
messages are stored on this machine. Limmr uses the unofficial whatsmeow linked
device protocol, which carries an account-ban risk. Ask the owner to approve
linking. Knowledge pairing is a separate approval on their Mac.

## Build from a clone or ZIP

On Debian/Ubuntu install the prerequisites with `apt-get install -y gcc python3`
(use `dnf install -y gcc python3` on Fedora). Then:

```sh
git clone https://github.com/afknavs/limmr && cd limmr && scripts/linux/build.sh --test
```

You can also extract the repository ZIP and run `scripts/linux/build.sh --test`
from that directory; no `.git` directory is needed. The script finds a real Go
compiler at or above `engine/go.mod`'s version, or downloads that version from
go.dev and checks the official SHA-256 before installing it privately. An
unrelated program called `go` is ignored. Use `LIMMR_GO=/path/to/go` to select a
compiler, or `--no-download` to require one already installed.

The binary is `$HOME/.local/bin/limmr-engine` (`PREFIX` changes the destination).
Go modules and build cache live under `$HOME/.cache/limmr-build`. Tests and the
health/MCP smoke check use temporary homes and fake WhatsApp; they never link an
account. Success prints `Smoke test passed`, the engine version and binary hash.
The complete source includes `integrations/` and the shared Swift test fixtures.
Python 3 and a C compiler are build helpers; the engine itself needs neither Go
nor Python to run. Native CGO SQLite builds need the system C runtime.

Add `$HOME/.local/bin` to your PATH. Set `LIMMR_HOME` to persistent private storage,
for example `$HOME/limmr-data`, and create it with mode 0700. Keep credentials at
0600. The script does not install a service. With no supervisor, run the engine
by hand while you need it; stopping the command stops message processing.
Workflow artifacts are also available from [Dot engines](https://github.com/afknavs/limmr/actions/workflows/dot-engine.yml)
for Linux amd64 and arm64, alongside checksums and a complete source archive.

## Dot only

1. Start `LIMMR_HOME="$HOME/limmr-data" limmr-engine < /dev/null` using a supervisor
   if your machine has one. Without a supervisor, run it for the duration of a
   command or conversation; it cannot process incoming messages while stopped.
   The local control API is `http://127.0.0.1:18800`. Leave remote access off.
2. Read `control-token` privately from `LIMMR_HOME`. Use it as a bearer header for
   local `/v1/` calls; never print it or put it in shell history or process args.
3. After the owner's linking approval, call `POST /v1/link/start` with
   `{"method":"phone","phone":"<country code and digits>"}` or `{"method":"qr"}`.
   Watch `GET /v1/link/events`. Show the current phone code or render the current
   QR for the owner. They open WhatsApp → Settings → Linked devices → Link a
   device. Stop when the stream reports `ready`; an error supplies a reason/fix.
   Discard pairing material after use. Do not call migration routes.
4. Create a local AI profile through `POST /v1/agents` with your chosen invented
   setup name, `kind:"custom"`, `transport:"local"`. Store the returned token
   privately with mode 0600. Review its permissions with the owner.
5. Use MCP at `/mcp` with that agent token. Start with `whoami`, then
   `get_updates`; persist its cursor. Follow `docs/MCP.md`. Setup never needs a
   WhatsApp send. Use the compact `task` tool for tasks; circles represent people.

A never-paired engine runs its own reminders and check-ins as before. Scheduled
`sync` commands exchange knowledge only; they do not replace a running engine's
WhatsApp connection or automation runtime.

## Dot + Mac (synced)

1. Complete Dot only, using the dot's own WhatsApp link. On the Mac the owner sets
   up remote access in Limmr and gives you its base address, for example
   `https://<mac>/limmr`. Do not guess it or change their tunnel configuration.
2. Start this command on the dot (it makes only outbound requests):

   ```sh
   LIMMR_HOME="$HOME/limmr-data" limmr-engine sync pair --peer https://<mac>/limmr < /dev/null
   ```

   Show the six-digit code it prints. The owner opens Settings → Integrations →
   **Sync with another Limmr → Pair…**, types that code, and approves “This AI is my dot. Let it sync
   my tasks, people and circles.” The command waits up to ten minutes. It uses
   OAuth with PKCE and polls for approval; no callback listener is needed.
3. Wait for the command's successful exit and `Paired. Sync complete` status.
   The rotating refresh credential is saved as `LIMMR_HOME/sync-oauth.json`, mode
   0600. Do not copy this file between engines. Pairing grants only knowledge
   sync; it does not grant message reads, media access or sending permission.
4. Schedule one short command every five minutes:

   ```sh
   LIMMR_HOME="$HOME/limmr-data" limmr-engine sync --peer https://<mac>/limmr --once < /dev/null
   ```

   `--once` is the recommended mode for a dot. Success exits 0 and prints a clear
   status. Failure exits 1 with a redacted reason; retry at the next scheduled
   run. Without `--once`, the command stays alive and repeats every five minutes.
   Serialize scheduled invocations so refresh-token rotation cannot overlap.
5. Both sides may edit knowledge offline. The next run pulls changes and missing
   photos, uploads the dot's photos, then pushes changes. Cursors survive process
   restarts; interrupted pages can be retried. Similar independently created
   tasks/circles stay separate until the owner chooses Merge or Keep both.

**Who runs automations:** while paired, only the lease holder runs reminders,
check-ins, watches and task suggestions. Pairing makes the Mac the holder, and the
dot never takes over on silence: a partitioned dot cannot tell a Mac that is off
from one it cannot reach, so a time-based takeover could let both send. Authority
moves only when the holder hands it over with `POST /v1/sync/lease {"take":false}`
(it stops at once; the other side starts after it syncs the handoff, so there is a
gap, never an overlap) or when the owner unpairs. If the Mac is gone for good, run
`limmr-engine sync unpair --home <dot home>` on the dot; it then runs standalone.
Unpair on the Mac as well, or both will run reminders.

Pause stops knowledge exchanges and keeps the pairing and lease. Resume allows
scheduled runs again. Unpair in Settings revokes the dot's sync permission and
keeps all knowledge. On the dot, call `DELETE /v1/sync/pair` through its local
control API too; this removes its refresh file and restores its independent
lease. Both copies then work independently. A Mac-side unpair cannot remotely
contact an outbound-only dot; its next sync fails until local unpair/re-pair.

## Verification and recovery

Use `GET /v1/sync` locally to inspect last sync, counts, lease and merge reviews.
`POST /v1/sync/now` initiates an exchange on a machine holding outbound pairing
credentials (including two machines reachable on one network). The listening
Mac waits for the dot's schedule; it has no dot refresh token or inbound route.
Do not infer a successful sync from a running schedule alone. Read status back.

If a step fails, make one focused fix and retry once. If it still fails, stop and
report the exact command and exit code without secrets. Never reset databases,
relink WhatsApp, or discard the owner's data to repair knowledge pairing.

## Paste this to your AI

```text
Set up Limmr on this Linux machine using https://github.com/afknavs/limmr.
First tell me the architecture, storage persistence and scheduling support.
Clone it (or use a complete ZIP), then run scripts/linux/build.sh --test.
Use persistent private LIMMR_HOME storage for the real engine. Follow
docs/handoffs/DOT-ENGINE.md for the local API and WhatsApp QR or phone code;
explain where account keys/messages live and ask before linking my account.
Never send, mark read, change presence or touch my other linked devices.
Keep tokens, QR payloads and session keys private. If I want Mac sync, ask
for its remote base URL, run sync pair, show me only the six-digit approval
code, then schedule serialized sync --peer <url> --once every five minutes.
Verify sync status after a run. Do not claim a detached command is a service.
```
