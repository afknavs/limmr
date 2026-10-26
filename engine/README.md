# Limmr engine

One Go helper for WhatsApp linking/history/media, per-agent MCP access, circle send rules,
versioned briefs and a shared context protocol, OAuth and the local app control API. The contract is
[`../docs/API.md`](../docs/API.md). Upstream porting credits and licenses are in
`NOTICE`, `LICENSE` and `third_party/go-sqlite3/LICENSE`.

Build and test with the preinstalled offline module cache:

```sh
cd engine
GOFLAGS=-mod=mod GOPROXY=off GOCACHE=$PWD/../.cache/go-build go test ./...
GOFLAGS=-mod=mod GOPROXY=off GOCACHE=$PWD/../.cache/go-build go build -o bin/limmr-engine ./cmd/limmr-engine
python3 scripts/demo_proof.py
```

The smoke script starts `--demo` in a throwaway home, uses curl for control and
MCP calls without printing credentials, and stops only the child it started.
Tests use temporary homes, invented fixtures, fake WhatsApp/pairing/launchctl and
in-memory HTTP. Tests never connect WhatsApp or manipulate real services.

For an interactive invented demo (stop it with Control-C):

```sh
LIMMR_HOME=$(mktemp -d) bin/limmr-engine --demo < /dev/null
```

Flags: `--home` (also `LIMMR_HOME`), `--port` (18800), `--remote-port` (18801),
`--demo`. Remote access stays off until configured through the local control API.
The binary binds only IPv4 loopback. It does not configure AI clients or tunnels.

SQLite is bundled through go-sqlite3 with FTS5 always enabled; no build tag,
extension installation or system SQLite is required. CGO and a C compiler are
required to build; the resulting native binary needs no Python or Node runtime.
The smoke utility alone uses Python's standard library and curl.

Safety properties:

- Per-agent permissions apply first, then the strictest confirmed circle rule:
  free, confirm in chat, or never. Confirm requires a draft id and a two-second
  guard. Background confirm drafts become Suggestions; the owner can edit/send
  them on the Mac. Crash-interrupted deliveries are never retried automatically.
- Confirm in chat relies on the AI client asking you. Limmr cannot prove a
  human clicked Allow; use Never for circles requiring that assurance.
- Credentials persist as digests; local app/status files are 0600. SDK/dependency
  logging is disabled, and engine logs contain fixed operational messages only.
- Owner-hidden chats override every agent scope, including LID aliases. The people
  map, stats, facts/evidence, graph and activity apply the same projection.
- Attachments must already be staged by the owner, are copied into private storage,
  and are pinned by SHA-256. Agents cannot request arbitrary filesystem paths.
- Migration/undo are local control actions only. The implementation is tested on
  fake stores and a fake launchctl runner. Its manifest/backups support recovery;
  never delete migration backups while recovery or undo remains necessary.

Demo mode intentionally uses a fake Messenger and invented QR/pair codes. Live
WhatsApp interoperability, public OAuth discovery and real migration require the
owner's later account/approval steps. They are not exercised by build tests.

Circles demo: Alex, an invented 20-year-old student and freelancer, has seven
circles, versioned briefs, two Atlas topics, three suggestions and two proposals.
`GET /v1/home`, `/v1/map` and the circle MCP tools share the app's JSON shapes.

`bin/limmr-engine plan-refresh` outputs a JSON plan without constructing a
WhatsApp client or opening a listener. Use `--demo` and a throwaway home for tests.
The nightly runner `scripts/refresh-briefs.py` and LaunchAgent template in
`launchagents/` are **not installed or enabled by the build**. Settings default to
off at 06:00 local time; the template polls the configured time once per minute.
When explicitly installed later, the runner uses the owner's own Codex plan and
a dedicated `limmr-nightly` agent with media disabled. The engine always
parks that identity's sends as Suggestions, including in free circles; only the
owner's explicit Send action may deliver them. It invokes
`codex exec` with a fixed never-send prompt and a bearer credential in an
environment variable (not command-line text). Installing and exercising that job
requires a user action outside build verification.
