# Contributing

Thanks for helping with Limmr. Small fixes, clear bug reports and accessibility improvements are welcome. Read the contributor safety rules in [AGENTS.md](AGENTS.md) before running anything.

## Keep development separate from real WhatsApp

- Never send messages, files, reactions, typing indicators, presence, read receipts or group changes through any real account during development. Test send paths with a fake `Messenger`.
- Use invented people, chats and messages in fixtures, screenshots and reports. Screenshots come only from the app's demo snapshot mode.
- Set `LIMMR_HOME` to a throwaway directory for tests and manual runs. Never write to the default Application Support directory during a build.
- Leave any existing `~/whatsapp-mcp` installation and its services untouched. Never open or copy its live `store/whatsapp.db`, which holds session keys. Migration tests use fake stores.
- Never print, commit or attach credentials. Do not open `.bridge-token`, `.mcp-token`, `remote.json`, `~/.codex/auth.json` or `~/Credentials/`.
- Do not edit real AI-client configs, LaunchAgents or tunnel routes as part of tests. Those are explicit user actions in the finished app.

## Build and test

Use macOS 26+ on Apple silicon, Xcode with Swift 6.2+ and command-line tools, and the Go version in `engine/go.mod`. SwiftPM has no third-party dependencies. The engine uses CGO/SQLite, so a working C toolchain is required.

From the repository root:

```sh
export LIMMR_HOME="$(mktemp -d)"
```

Keep that environment for the following commands. Go tests, from the root:

```sh
(cd engine && GOFLAGS=-mod=mod GOPROXY=off GOCACHE="$PWD/../.cache/go-build" nice -n 15 go test -p 2 ./...)
```

This is the offline build command: Go modules must already be cached. A missing dependency is a blocker for an offline run, not a reason to change the live installation or silently fetch packages. The GitHub workflow runs on a fresh runner and lets Go fetch dependencies there.

Swift build and tests:

```sh
(cd app && nice -n 15 swift build && nice -n 15 swift test)
```

Bundle the app from the root:

```sh
mkdir -p engine/bin
(cd engine && GOFLAGS=-mod=mod GOPROXY=off GOCACHE="$PWD/../.cache/go-build" go build -o bin/limmr-engine ./cmd/limmr-engine)
app/build.sh
```

The result is `app/build/Limmr.app`, signed ad hoc. Build the engine first: `app/build.sh` only copies an already-built helper from `engine/bin/limmr-engine`.

Do not launch a live engine to verify docs or UI. Manual UI verification uses demo mode or snapshot mode, with the throwaway home. Do not click migration, client install or remote setup against your real configuration while developing. Give any headless process you start `< /dev/null`; stop only processes you started.

## Before a pull request

Keep the change focused and run checks relevant to it. Describe the problem, resulting behaviour and verification, including any check you could not run. Permission changes need coverage for both allowed and denied access; send tests must prove nothing sends before approval and use a fake messenger throughout.

Check documentation paths and commands against the implementation. Use `<!-- TODO: ... -->` source comments for pending release details. Preserve upstream licences and notices. Do not attach real data, even in a failing test or a private-looking screenshot.

Stage only files you own; other lanes may be working in the same checkout. Use small commits with plain messages and no `Co-Authored-By` trailers. If `index.lock` exists, wait for the other Git operation and retry; do not remove someone else's lock. Build agents must never push or create a GitHub repository.

See [the code of conduct](CODE_OF_CONDUCT.md) for participation expectations, and [security reporting](docs/SECURITY.md#report-a-vulnerability) for sensitive findings.

For a native Linux build and smoke test, see [Run Limmr on Linux or a dot](docs/handoffs/DOT-ENGINE.md). Run Go and Swift suites separately.
