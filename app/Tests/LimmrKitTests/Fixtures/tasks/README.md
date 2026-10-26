# Shared task wire fixtures

Produced by `TestQ5SharedTaskFixtures` through real engine control/status handlers,
using a temporary home and the invented demo seeds. No live helper or network is used.

From the repository root:

```sh
LIMMR_HOME=$(mktemp -d) LIMMR_UPDATE_TASK_FIXTURES=1 \
  GOFLAGS=-mod=mod GOPROXY=off GOCACHE=$PWD/.cache/go-build \
  /opt/homebrew/bin/go -C engine test ./internal/integration \
  -run '^TestQ5SharedTaskFixtures$' -count=1 < /dev/null
```

A normal Go test compares the generated JSON with these files and fails on drift.
`Q5TaskContractTests` decodes every JSON file with the app's production models.
The test adds a note, summary, due check-in and linked task to exercise the wire.

`home.json` projects the real Home response's `tasks` and task-only `queued` slices;
`suggestion.json` retains task suggestions from the real suggestion envelope.
Other files retain the full response. Random task, step and item IDs are given
stable fixture identifiers, including references inside suggestion IDs. Arrays
whose order depends on random IDs are canonicalized, including simultaneous feed
events. JSON fields, values, omitted fields and nulls otherwise remain unchanged.
