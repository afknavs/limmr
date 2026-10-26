#!/bin/bash
set +x
set -euo pipefail
# Release-only metadata and signing; app/build.sh remains the development/Developer ID builder.
root="$(cd "$(dirname "$0")/../.." && pwd)"
[[ $# -eq 1 ]] || { echo 'Expected a staged app bundle.' >&2; exit 2; }
bundle="$1"
: "${LIMMR_VERSION:?Release version is required}"
: "${LIMMR_BUILD_NUMBER:?Release build number is required}"
: "${LIMMR_UPDATE_FEED:?Release feed URL is required}"
: "${LIMMR_UPDATE_PUBLIC_KEY:?Release public key is required}"
[[ -x "$bundle/Contents/Helpers/limmr-engine" && -x "$bundle/Contents/MacOS/Limmr" ]] || {
  echo 'Release bundle is missing an executable.' >&2; exit 2;
}
python3 - "$bundle/Contents/Info.plist" <<'PYPLIST'
import base64, os, plistlib, sys
key = base64.b64decode(os.environ['LIMMR_UPDATE_PUBLIC_KEY'], validate=True)
if len(key) != 32:
    raise SystemExit('Expected a 32-byte Ed25519 public key.')
path = sys.argv[1]
with open(path, 'rb') as f:
    info = plistlib.load(f)
for field, env in [('CFBundleShortVersionString', 'LIMMR_VERSION'), ('CFBundleVersion', 'LIMMR_BUILD_NUMBER'),
                   ('LimmrUpdateFeed', 'LIMMR_UPDATE_FEED'), ('LimmrUpdatePublicKey', 'LIMMR_UPDATE_PUBLIC_KEY')]:
    info[field] = os.environ[env]
with open(path, 'wb') as f:
    plistlib.dump(info, f)
PYPLIST
for target in "$bundle/Contents/Helpers/limmr-engine" "$bundle/Contents/MacOS/Limmr" "$bundle"; do
  /usr/bin/codesign --force --sign - --options runtime --entitlements "$root/scripts/release/entitlements.plist" "$target"
done
/usr/bin/codesign --verify --deep --strict "$bundle"
