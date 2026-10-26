#!/bin/zsh
set -euo pipefail
cd "${0:A:h}"
umask 022
export CLANG_MODULE_CACHE_PATH="$PWD/.build/clang-cache"
export SWIFTPM_MODULECACHE_OVERRIDE="$PWD/.build/swift-cache"
swift build -c release --disable-sandbox --cache-path .build/cache --config-path .build/config --security-path .build/security -debug-info-format none
engine_binary="${LIMMR_ENGINE_BINARY:-../engine/bin/limmr-engine}"
[[ -x "$engine_binary" ]] || { echo "Build engine/bin/limmr-engine before packaging Limmr." >&2; exit 1; }
bundle="$PWD/build/Limmr.app"
mkdir -p "$bundle/Contents/MacOS" "$bundle/Contents/Resources" "$bundle/Contents/Library/LaunchAgents" "$bundle/Contents/Helpers"
cp .build/release/Limmr "$bundle/Contents/MacOS/Limmr"
if [[ -d ../integrations ]]; then
  rm -rf "$bundle/Contents/Resources/integrations"
  cp -R ../integrations "$bundle/Contents/Resources/integrations"
fi
cp Resources/Limmr-Setup-Guide.pdf "$bundle/Contents/Resources/"
cp Resources/Info.plist "$bundle/Contents/Info.plist"
cp Resources/dev.limmr.engine.plist "$bundle/Contents/Library/LaunchAgents/"
rm -f "$bundle/Contents/Helpers/limmr-engine"
cp "$engine_binary" "$bundle/Contents/Helpers/limmr-engine"
swift -module-cache-path "$PWD/.build/clang-cache" scripts/icon.swift "$PWD/build" < /dev/null
cp build/Limmr.icns "$bundle/Contents/Resources/"
chmod -R a+rX "$bundle"
if [[ -n "${LIMMR_SIGN_ID:-}" ]]; then
  : "${LIMMR_VERSION:?Release version is required}"
  : "${LIMMR_BUILD_NUMBER:?Release build number is required}"
  : "${LIMMR_UPDATE_FEED:?Release feed URL is required}"
  : "${LIMMR_UPDATE_PUBLIC_KEY:?Release public key is required}"
  [[ -f "$bundle/Contents/Helpers/limmr-engine" ]] || { echo "Build the engine before signing." >&2; exit 1; }
  # plistlib reads public metadata directly from the environment, preserving arbitrary XML characters.
  python3 - "$bundle/Contents/Info.plist" <<'PYPLIST'
import os, plistlib, sys
path = sys.argv[1]
with open(path, 'rb') as f:
    info = plistlib.load(f)
for key, env in [('CFBundleShortVersionString', 'LIMMR_VERSION'), ('CFBundleVersion', 'LIMMR_BUILD_NUMBER'),
                 ('LimmrUpdateFeed', 'LIMMR_UPDATE_FEED'), ('LimmrUpdatePublicKey', 'LIMMR_UPDATE_PUBLIC_KEY')]:
    info[key] = os.environ[env]
with open(path, 'wb') as f:
    plistlib.dump(info, f)
PYPLIST
  codesign --force --sign "$LIMMR_SIGN_ID" --options runtime --timestamp --entitlements ../scripts/release/entitlements.plist "$bundle/Contents/Helpers/limmr-engine"
  codesign --force --sign "$LIMMR_SIGN_ID" --options runtime --timestamp --entitlements ../scripts/release/entitlements.plist "$bundle/Contents/MacOS/Limmr"
  codesign --force --sign "$LIMMR_SIGN_ID" --options runtime --timestamp --entitlements ../scripts/release/entitlements.plist "$bundle"
else
  if [[ -f "$bundle/Contents/Helpers/limmr-engine" ]]; then
    codesign --force --sign - "$bundle/Contents/Helpers/limmr-engine"
  fi
  codesign --force --sign - "$bundle"
fi
codesign --verify --deep --strict "$bundle"
echo "Built app/build/Limmr.app"
