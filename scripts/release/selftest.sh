#!/bin/bash
set +x
set -euo pipefail
umask 077
root="$(cd "$(dirname "$0")/../.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
export LIMMR_HOME="$work/home"
export LIMMR_UPDATE_PRIVATE_KEY_FILE="$work/key"
export LIMMR_UPDATE_PUBLIC_KEY
LIMMR_UPDATE_PUBLIC_KEY="$(swift -module-cache-path "$work/modules" "$root/scripts/release/keygen.swift" --file "$work/key" < /dev/null)"
# An inherited real-key override must never be consulted by this offline test.
unset LIMMR_UPDATE_PRIVATE_KEY LIMMR_UPDATE_KEYCHAIN_SERVICE
export LIMMR_VERSION=0.3.0 LIMMR_BUILD_NUMBER=30
export LIMMR_DOWNLOAD_URL=https://example.invalid/Limmr.zip
export LIMMR_RELEASE_NOTES="$work/notes.md"
printf 'A quieter Home.\n\nUnicode: café; links: https://example.invalid/notes.\n' > "$work/notes.md"
printf 'synthetic archive' > "$work/Limmr.zip"
swift -module-cache-path "$work/modules" "$root/scripts/release/sign-feed.swift" \
  "$work/Limmr.zip" "$work/appcast.json" "$work/canonical.json" < /dev/null
# Compile the PRODUCTION verifier with a tiny harness: catches canonicalization drift.
cat > "$work/main.swift" <<'SWIFT'
import Foundation
let env = ProcessInfo.processInfo.environment
let base = URL(fileURLWithPath: CommandLine.arguments[1])
let key = Data(base64Encoded: env["LIMMR_UPDATE_PUBLIC_KEY"]!)!
let feed = try UpdateFeed.verified(Data(contentsOf: base.appendingPathComponent("appcast.json")), publicKey: key)
try feed.verifyArchive(base.appendingPathComponent("Limmr.zip"))
guard try feed.canonicalBytes() == Data(contentsOf: base.appendingPathComponent("canonical.json")) else { exit(1) }
print("Throwaway key generation, release signing and production verification passed.")
SWIFT
swiftc -module-cache-path "$work/modules" "$root/app/Sources/LimmrKit/Updates.swift" "$work/main.swift" -o "$work/verify" < /dev/null
"$work/verify" "$work" < /dev/null
# keygen refuses to replace a pre-existing key, even in test-file mode.
if swift -module-cache-path "$work/modules" "$root/scripts/release/keygen.swift" --file "$work/key" < /dev/null > /dev/null 2>&1; then
  echo 'Keygen unexpectedly overwrote an existing key.' >&2
  exit 1
fi
python3 "$root/scripts/release/selftest-modes.py" "$work" < /dev/null
bash -n "$root/scripts/release/release.sh" "$root/scripts/release/selftest.sh" "$root/scripts/release/sign-self-signed.sh"
zsh -n "$root/app/build.sh"
