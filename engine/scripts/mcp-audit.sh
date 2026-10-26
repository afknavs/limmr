#!/bin/bash
# Full client-side audit with fake WhatsApp and in-memory HTTP connections.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
mkdir -p "$root/.cache"
audit_home=$(mktemp -d "$root/.cache/a2-mcp-home.XXXXXX")
trap 'rm -rf "$audit_home"' EXIT
export LIMMR_HOME="$audit_home" GOFLAGS=-mod=mod GOPROXY=off GOCACHE="$root/.cache/go-build"
go -C "$root/engine" test ./internal/integration -run '^TestA2(MCPClientAudit|ConcurrentMCPClients)$' -count=1 -v < /dev/null
