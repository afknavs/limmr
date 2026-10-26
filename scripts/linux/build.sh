#!/bin/sh
# Bootstrap a native Linux engine from a clone or an extracted source archive.
set -eu
set +x
umask 077
export GOTOOLCHAIN=local
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
usage() {
    cat <<'HELP'
Usage: scripts/linux/build.sh [--test] [--no-download] [--check-go] [--help]
Build Limmr on Linux amd64 or arm64, then smoke-test with invented data.
  --test         Also run the engine suite in a temporary LIMMR_HOME
  --no-download  Require an installed, sufficiently recent Go toolchain
  --check-go     Print the selected Go path and exit (diagnostic; no build)
Environment: LIMMR_GO selects Go; PREFIX defaults to $HOME/.local.
Requires a C compiler and Python 3. Go is downloaded with a verified SHA-256
when needed. No service, WhatsApp link or pairing is created by this script.
Guide: docs/handoffs/DOT-ENGINE.md
HELP
}
fail() { printf '%s\n' "$*" >&2; exit 1; }
test_suite=false
download=true
check_go=false
for arg do
    case "$arg" in
        --help|-h) usage; exit 0 ;;
        --test) test_suite=true ;;
        --no-download) download=false ;;
        --check-go) check_go=true ;;
        *) usage >&2; fail "Unknown argument: $arg" ;;
    esac
done
required=$(awk '$1 == "go" {print $2; exit}' "$root/engine/go.mod")
[ -n "$required" ] || fail 'engine/go.mod has no Go version.'
valid_go() {
    # Reject unrelated programs named go, prereleases and malformed versions.
    version=$("$1" version 2>/dev/null) || return 1
    printf '%s\n' "$version" | awk -v minimum="$required" '
        /^go version go[0-9]+\.[0-9]+(\.[0-9]+)? [^ ]+$/ {
            sub(/^go/, "", $3); split($3, actual, "."); split(minimum, need, ".")
            for (i=1; i<=3; i++) {
                if (actual[i]+0 > need[i]+0) exit 0
                if (actual[i]+0 < need[i]+0) exit 1
            }
            exit 0
        }
        !/^go version go[0-9]+\.[0-9]+(\.[0-9]+)? [^ ]+$/ {exit 1}'
}
go_bin=
if [ -n "${LIMMR_GO:-}" ] && valid_go "$LIMMR_GO"; then
    go_bin=$LIMMR_GO
elif path_go=$(command -v go) && valid_go "$path_go"; then
    go_bin=$path_go
elif valid_go "$HOME/.local/limmr-go/bin/go"; then
    go_bin=$HOME/.local/limmr-go/bin/go
fi
if [ -z "$go_bin" ]; then
    "$download" || fail "Go >= $required is required. Set LIMMR_GO to a real Go executable or rerun without --no-download to install it privately."
    [ "$(uname -s)" = Linux ] || fail 'Go auto-download supports Linux only. Set LIMMR_GO to an installed Go.'
    command -v python3 >/dev/null || fail 'Install Python 3: apt-get install -y python3 / dnf install -y python3'
    python3 "$root/scripts/linux/download-go.py" "$required"
    go_bin=$HOME/.local/limmr-go/bin/go
    valid_go "$go_bin" || fail 'Downloaded Go failed its version check.'
fi
# Resolve relative overrides before changing directory.
case "$go_bin" in /*) ;; *) go_bin=$(command -v "$go_bin") ;; esac
case "$go_bin" in /*) ;; *) go_bin="$PWD/$go_bin" ;; esac
if "$check_go"; then printf '%s\n' "$go_bin"; exit 0; fi
[ "$(uname -s)" = Linux ] || fail 'Build on Linux; on macOS use the native contributor build. --help and --check-go work on either system.'
if [ -n "${CC:-}" ]; then
    command -v "$CC" >/dev/null || fail 'C compiler missing. Debian/Ubuntu: apt-get install -y gcc; Fedora: dnf install -y gcc'
elif command -v cc >/dev/null; then CC=cc
elif command -v gcc >/dev/null; then CC=gcc
else fail 'C compiler missing. Debian/Ubuntu: apt-get install -y gcc; Fedora: dnf install -y gcc'
fi
command -v python3 >/dev/null || fail 'Install Python 3: apt-get install -y python3 / dnf install -y python3'
export CC CGO_ENABLED=1
export GOPATH="$HOME/.cache/limmr-build/gopath"
export GOCACHE="$HOME/.cache/limmr-build/go-build"
export GOMODCACHE="$GOPATH/pkg/mod"
export GOPROXY="${LIMMR_GOPROXY:-https://proxy.golang.org,direct}"
export GOFLAGS=-mod=mod
export GOTOOLCHAIN=local
# Shared task/integration fixtures are distribution inputs, including in ZIPs.
for fixture in integrations/chatgpt/instructions.md integrations/codex/skills/limmr/SKILL.md integrations/claude-code/skills/limmr/SKILL.md app/Tests/LimmrKitTests/Fixtures/tasks/feed.json; do
    [ -f "$root/$fixture" ] || fail "Incomplete source tree: missing $fixture. Use a complete clone or source archive."
done
PREFIX=${PREFIX:-"$HOME/.local"}
mkdir -p "$PREFIX/bin" "$GOPATH" "$GOCACHE"
PREFIX=$(CDPATH= cd -- "$PREFIX" && pwd)
cd "$root/engine"
nice -n 15 "$go_bin" build -trimpath -buildvcs=false -o "$PREFIX/bin/limmr-engine" ./cmd/limmr-engine < /dev/null
if "$test_suite"; then
    test_home=$(mktemp -d)
    trap 'rm -rf "$test_home"' EXIT HUP INT TERM
    LIMMR_HOME="$test_home" nice -n 15 "$go_bin" test -p 2 ./... < /dev/null
    rm -rf "$test_home"
    trap - EXIT HUP INT TERM
fi
nice -n 15 python3 "$root/scripts/linux/smoke.py" "$PREFIX/bin/limmr-engine" < /dev/null
cat <<'NEXT'
Next: link WhatsApp through the local API, then connect your AI. To use a Mac
as well, pair in Settings → Integrations → Sync with another Limmr → Pair…,
then schedule a serialized sync --peer <url> --once every five minutes.
Read docs/handoffs/DOT-ENGINE.md for linking, pairing and scheduling.
No service manager is configured here. Keep the engine running with your own
supervisor, or run LIMMR_HOME="$HOME/limmr-data" ~/.local/bin/limmr-engine
by hand while you need it. A stopped engine cannot receive messages.
NEXT
