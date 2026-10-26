#!/usr/bin/env python3
"""Exercise the actual release recipe offline; only build/Apple service boundaries are faked.

Ad-hoc bundle signing, ZIP creation, metadata, Ed25519 signing, and archive checks are real.
Developer ID signing/notarization requests are recorded, never sent to Apple or Keychain.
"""
import os
from pathlib import Path
import plistlib
import shutil
import subprocess
import sys

source = Path(__file__).resolve().parents[2]
work = Path(sys.argv[1]) / 'release-modes'
work.mkdir()
for mode in ('self-signed', 'self-signed-inherited-identity', 'developer-id'):
    root = work / mode
    scripts = root / 'scripts/release'
    scripts.mkdir(parents=True)
    (root / 'app').mkdir()
    (root / 'engine').mkdir()
    stubs = root / 'stubs'
    stubs.mkdir()
    log = root / 'calls'

    def executable(path, text):
        path.write_text(text)
        path.chmod(0o700)

    for name in ('sign-self-signed.sh', 'sign-feed.swift', 'entitlements.plist', 'limmr.rb.template'):
        shutil.copy2(source / 'scripts/release' / name, scripts / name)
    recipe = (source / 'scripts/release/release.sh').read_text()
    # Only the test COPY substitutes Gatekeeper assessment (no notarized certificate offline).
    recipe = recipe.replace('/usr/sbin/spctl', 'spctl')
    executable(scripts / 'release.sh', recipe)
    executable(stubs / 'go', '#!/bin/bash\nset -eu\n[[ "$GOPROXY" == off ]]\necho engine-build >> "$TEST_CALLS"\n')
    executable(stubs / 'uname', '#!/bin/bash\necho arm64\n')
    executable(stubs / 'xcrun', '#!/bin/bash\nprintf "xcrun %s\\n" "$*" >> "$TEST_CALLS"\n')
    executable(stubs / 'spctl', '#!/bin/bash\necho spctl >> "$TEST_CALLS"\n')
    executable(stubs / 'hdiutil', '''#!/bin/bash
set -eu
[[ "$1" == create ]]
for last; do :; done
printf 'synthetic DMG boundary' > "$last"
echo dmg-create >> "$TEST_CALLS"
''')
    executable(stubs / 'codesign', '''#!/bin/bash
set -eu
printf 'dmg-sign %s\\n' "$*" >> "$TEST_CALLS"
''')
    # A tiny, never-launched app fixture stands in for the expensive Swift/Go build. No keys read.
    executable(root / 'app/build.sh', '''#!/usr/bin/env python3
import os, pathlib, plistlib, shutil, subprocess
root = pathlib.Path(__file__).resolve().parents[1]
bundle = root / 'app/build/Limmr.app'
with open(os.environ['TEST_CALLS'], 'a') as log:
    log.write('app-build ' + os.environ.get('LIMMR_SIGN_ID', 'ad-hoc') + '\\n')
for name in ('Contents/MacOS/Limmr', 'Contents/Helpers/limmr-engine'):
    target = bundle / name
    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile('/usr/bin/true', target)
    target.chmod(0o755)
    subprocess.run(['/usr/bin/codesign', '--force', '--sign', '-', str(target)], check=True, capture_output=True)
info = dict(CFBundleIdentifier='dev.limmr.app', CFBundleExecutable='Limmr', CFBundlePackageType='APPL', LSMinimumSystemVersion='26.0')
# The production Developer ID build embeds these fields before signing, too.
if os.environ.get('LIMMR_SIGN_ID'):
    for field, env in [('CFBundleShortVersionString', 'LIMMR_VERSION'), ('CFBundleVersion', 'LIMMR_BUILD_NUMBER'),
                       ('LimmrUpdateFeed', 'LIMMR_UPDATE_FEED'), ('LimmrUpdatePublicKey', 'LIMMR_UPDATE_PUBLIC_KEY')]:
        info[field] = os.environ[env]
with (bundle / 'Contents/Info.plist').open('wb') as f:
    plistlib.dump(info, f)
subprocess.run(['/usr/bin/codesign', '--force', '--sign', '-', str(bundle)], check=True, capture_output=True)
''')
    env = os.environ.copy()
    env.update(PATH=str(stubs) + os.pathsep + env['PATH'], TEST_CALLS=str(log),
               LIMMR_GITHUB_REPO='example/limmr')
    # Avoid any inherited real certificate/profile. The feed key is the selftest's throwaway file.
    env.pop('LIMMR_SIGN_ID', None)
    env.pop('LIMMR_NOTARY_PROFILE', None)
    args = [str(scripts / 'release.sh')]
    if mode.startswith('self-signed'):
        args.append('--self-signed')
    if mode != 'self-signed':
        env.update(LIMMR_SIGN_ID='Developer ID Application: Synthetic (TESTTEAM01)', LIMMR_NOTARY_PROFILE='synthetic-notary')
    args.append('0.3.0')
    run = subprocess.run(args, env=env, stdin=subprocess.DEVNULL, capture_output=True, text=True)
    if run.returncode:
        # Fixture diagnostics cannot contain keys: every input is synthetic and signer hides key errors.
        raise SystemExit(f'{mode} recipe failed:\n{run.stdout}\n{run.stderr}')
    calls = log.read_text()
    out = root / 'app/build/releases/0.3.0'
    bundle = out / 'Limmr.app'
    with (bundle / 'Contents/Info.plist').open('rb') as f:
        info = plistlib.load(f)
    assert info['CFBundleVersion'] == '30'
    assert info['CFBundleShortVersionString'] == '0.3.0'
    assert info['LimmrUpdatePublicKey'] == env['LIMMR_UPDATE_PUBLIC_KEY']
    assert info['LimmrUpdateFeed'] == 'https://github.com/example/limmr/releases/latest/download/appcast.json'
    if mode.startswith('self-signed'):
        assert 'app-build ad-hoc' in calls and 'xcrun' not in calls and 'spctl' not in calls
        assert 'dmg-sign --sign - ' in calls and 'not notarized' in run.stdout
        for target in (bundle, bundle / 'Contents/MacOS/Limmr', bundle / 'Contents/Helpers/limmr-engine'):
            check = subprocess.run(['/usr/bin/codesign', '--display', '--verbose=4', str(target)], capture_output=True, text=True, check=True)
            assert 'Signature=adhoc' in check.stderr and 'runtime' in check.stderr
    else:
        assert 'app-build Developer ID Application: Synthetic (TESTTEAM01)' in calls
        assert calls.count('xcrun notarytool submit') == 2
        assert calls.count('xcrun stapler staple') == 2
        assert calls.count('xcrun stapler validate') == 2
        assert 'spctl' in calls and 'dmg-sign --sign Developer ID Application:' in calls and '--timestamp' in calls
    subprocess.run(['/usr/bin/codesign', '--verify', '--deep', '--strict', str(bundle)], check=True, capture_output=True)
    # Run the same PRODUCTION Swift feed/hash verifier used by the parent selftest.
    shutil.copy2(out / 'Limmr-0.3.0-arm64.zip', out / 'Limmr.zip')
    shutil.copy2(out / 'appcast-canonical.json', out / 'canonical.json')
    subprocess.run([str(Path(sys.argv[1]) / 'verify'), str(out)], env=env, stdin=subprocess.DEVNULL, check=True, capture_output=True)
    assert 'gh release create' in run.stdout and '@VERSION@' not in (out / 'limmr.rb').read_text()
    # Existing output must be refused before any build/sign/notary operation is retried.
    again = subprocess.run(args, env=env, stdin=subprocess.DEVNULL, capture_output=True)
    assert again.returncode != 0 and log.read_text() == calls
    print(f'Offline {mode} recipe passed (real ad-hoc signatures, ZIP and signed feed; mocked build/Apple services/DMG).')
