#!/bin/bash
set +x
set -euo pipefail
umask 077

root="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$root"
self_signed=false
if [[ "${1:-}" == --self-signed ]]; then
  self_signed=true
  shift
fi
if [[ $# -ne 1 || ! "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo 'Usage: scripts/release/release.sh [--self-signed] <version> (for example 0.3.0)' >&2
  exit 2
fi
if ! "$self_signed"; then
  : "${LIMMR_SIGN_ID:?Set LIMMR_SIGN_ID to your Developer ID Application identity}"
  : "${LIMMR_NOTARY_PROFILE:?Set LIMMR_NOTARY_PROFILE to a notarytool Keychain profile}"
  [[ "$LIMMR_SIGN_ID" == 'Developer ID Application: '* ]] || { echo 'A Developer ID Application identity is required.' >&2; exit 2; }
fi
: "${LIMMR_UPDATE_PUBLIC_KEY:?Set the base64 Ed25519 public key}"
: "${LIMMR_GITHUB_REPO:?Set LIMMR_GITHUB_REPO to owner/limmr}"
: "${LIMMR_BUILD_NUMBER:?Set an increasing positive build number}"
: "${LIMMR_RELEASE_NOTES:?Set LIMMR_RELEASE_NOTES to a Markdown file}"
[[ "$LIMMR_GITHUB_REPO" =~ ^[A-Za-z0-9_.-]+/limmr$ ]] || { echo 'Expected owner/limmr.' >&2; exit 2; }
[[ "$LIMMR_BUILD_NUMBER" =~ ^[1-9][0-9]{0,3}$ ]] || { echo 'Build must be an integer from 1 to 9999.' >&2; exit 2; }
[[ "$(uname -m)" == arm64 ]] || { echo 'This release recipe builds arm64 on Apple silicon.' >&2; exit 2; }
[[ -f "$LIMMR_RELEASE_NOTES" ]] || { echo 'Release notes are missing.' >&2; exit 2; }
export LIMMR_VERSION="$1"
LIMMR_RELEASE_NOTES="$(cd "$(dirname "$LIMMR_RELEASE_NOTES")" && pwd)/$(basename "$LIMMR_RELEASE_NOTES")"
export LIMMR_RELEASE_NOTES
export LIMMR_UPDATE_FEED="https://github.com/$LIMMR_GITHUB_REPO/releases/latest/download/appcast.json"
export LIMMR_DOWNLOAD_URL="https://github.com/$LIMMR_GITHUB_REPO/releases/download/v$1/Limmr-$1-arm64.zip"
export LIMMR_HOME
LIMMR_HOME="$(mktemp -d)"
trap 'rm -rf "$LIMMR_HOME"' EXIT
export CLANG_MODULE_CACHE_PATH="$root/app/.build/clang-cache"
export SWIFTPM_MODULECACHE_OVERRIDE="$root/app/.build/swift-cache"
out="$root/app/build/releases/$1"
[[ ! -e "$out" ]] || { echo 'Release output already exists. Preserve it or move it before rebuilding.' >&2; exit 2; }
mkdir -p "$out"
(
  cd engine
  GOFLAGS=-mod=mod GOPROXY=off GOCACHE="$root/.cache/go-build" GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 \
    go build -trimpath -ldflags "-X limmr/internal/health.Version=$LIMMR_VERSION" -o bin/limmr-engine ./cmd/limmr-engine < /dev/null
)
if "$self_signed"; then
  # Ignore any inherited Developer ID identity: no certificate or notary credential is used.
  env -u LIMMR_SIGN_ID app/build.sh < /dev/null
else
  app/build.sh < /dev/null
fi
bundle="$out/Limmr.app"
/usr/bin/ditto app/build/Limmr.app "$bundle"
if "$self_signed"; then
  scripts/release/sign-self-signed.sh "$bundle" < /dev/null
fi
/usr/bin/codesign --verify --deep --strict "$bundle"
if ! "$self_signed"; then
  # Notarize a temporary ZIP, staple the app, then construct the final update ZIP.
  /usr/bin/ditto -c -k --sequesterRsrc --keepParent "$bundle" "$out/notary.zip"
  xcrun notarytool submit "$out/notary.zip" --keychain-profile "$LIMMR_NOTARY_PROFILE" --wait < /dev/null
  xcrun stapler staple "$bundle" < /dev/null
  xcrun stapler validate "$bundle" < /dev/null
  /usr/sbin/spctl --assess --type execute "$bundle"
  rm "$out/notary.zip"
fi
zipfile="$out/Limmr-$1-arm64.zip"
/usr/bin/ditto -c -k --sequesterRsrc --keepParent "$bundle" "$zipfile"
dmgstage="$out/dmg-stage"
mkdir "$dmgstage"
/usr/bin/ditto "$bundle" "$dmgstage/Limmr.app"
ln -s /Applications "$dmgstage/Applications"
dmg="$out/Limmr-$1-arm64.dmg"
hdiutil create -volname Limmr -srcfolder "$dmgstage" -ov -format UDZO "$dmg" < /dev/null
if "$self_signed"; then
  codesign --sign - "$dmg"
else
  codesign --sign "$LIMMR_SIGN_ID" --timestamp "$dmg"
  xcrun notarytool submit "$dmg" --keychain-profile "$LIMMR_NOTARY_PROFILE" --wait < /dev/null
  xcrun stapler staple "$dmg" < /dev/null
  xcrun stapler validate "$dmg" < /dev/null
fi
rm -rf "$dmgstage"
swift -module-cache-path "$CLANG_MODULE_CACHE_PATH" scripts/release/sign-feed.swift \
  "$zipfile" "$out/appcast.json" "$out/appcast-canonical.json" < /dev/null
dmgsha="$(shasum -a 256 "$dmg" | cut -d ' ' -f 1)"
sed -e "s|@VERSION@|$1|g" -e "s|@DMG_SHA256@|$dmgsha|g" -e "s|@REPO@|$LIMMR_GITHUB_REPO|g" \
  scripts/release/limmr.rb.template > "$out/limmr.rb"
if "$self_signed"; then
  echo 'Ad-hoc signed release; not notarized. First install needs Open Anyway.'
fi
echo 'Release built and verified. Review the artifacts and run this command yourself:'
printf 'gh release create %q --repo %q --title %q --notes-file %q %q %q %q\n' \
  "v$1" "$LIMMR_GITHUB_REPO" "Limmr $1" "$LIMMR_RELEASE_NOTES" "$zipfile" "$dmg" "$out/appcast.json"
