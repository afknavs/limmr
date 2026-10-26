# Releasing Limmr

## Self-signed releases (no Apple Developer Program)

Limmr can ship from GitHub without an Apple Developer account. These releases are **ad-hoc signed
and not notarized**. A separate Ed25519 key proves each update came from you. The release recipe
builds **Apple silicon (arm64), macOS 26 or newer**, on an Apple silicon Mac; it does not build Intel
or universal releases. No third-party updater package is needed.

On first install, drag Limmr to Applications, then right-click → Open. If macOS blocks it, use
System Settings › Privacy & Security › Open Anyway. This is a one-time step for the first install.
Later verified updates remove the download quarantine in-app, so the replacement can launch without
that Gatekeeper prompt.

In-app installation uses the engine's atomic pause/drain API. Installed releases with this API
can prepare updates automatically and install when the user chooses **Restart to update**.
Older engines without the capability route require a manual install after stopping Limmr and its
helper. Signing, staging, atomic replacement, version verification and rollback have offline tests;
an owner-run installed-app upgrade remains required before shipping that flow.

You need a GitHub `limmr` repository with release permission, an Ed25519 feed key, Xcode/Command Line
Tools, Swift, Python 3, and Go with the modules already cached. Builds and offline tests use throwaway
homes; they do not touch a live WhatsApp account. Never put private keys or credentials in this repo.

## Set up once

Create the feed key yourself, outside the build sandbox:

```sh
swift scripts/release/keygen.swift
```

It stores a non-synchronizing, this-Mac-only generic password in Keychain, service
`dev.limmr.release.ed25519`, account `signing`, and prints **only the public key**. Save that public
key in your release environment as `LIMMR_UPDATE_PUBLIC_KEY`. Re-running refuses to overwrite an
existing key. Back up your signing key securely through your own Keychain recovery process. Losing
it means existing apps cannot trust later feeds. Self-signed updates refuse a changed embedded key;
a key change needs a separately designed migration or a manual reinstall.

Set these nonsecret release settings in your shell:

```sh
export LIMMR_GITHUB_REPO='your-github-name/limmr'
export LIMMR_UPDATE_PUBLIC_KEY='the-public-key-printed-above'
export LIMMR_BUILD_NUMBER=1
export LIMMR_RELEASE_NOTES='/absolute/path/to/release-notes.md'
```

The first release must already contain the correct public key and feed URL. `app/build.sh` embeds
them from the release environment before signing. The checked-in Info.plist leaves them empty, so
a development build never checks a pretend feed. Do not edit the signed bundle afterward.

## First release and every later release

Write short Markdown release notes. Choose a numeric version such as `0.3.0` and a strictly increasing
integer build number (1–9999 in this recipe). A higher marketing version with a lower build number is
also refused. Then run one command:

```sh
scripts/release/release.sh --self-signed 0.3.0
```

The script uses a throwaway `LIMMR_HOME`, builds the arm64 Go engine offline and the release app,
embeds the version and public feed key, and signs the engine, app executable and bundle ad-hoc
(`codesign -s -`) with hardened runtime and the existing entitlements (no runtime exceptions).
It verifies the signatures, makes a ZIP and a drag-to-Applications DMG, signs the DMG ad-hoc, and
writes and Ed25519-signs `appcast.json`. It skips Developer ID, notarization, stapling and Gatekeeper
assessment. Even an inherited `LIMMR_SIGN_ID` is ignored in this mode. It does not publish, modify
launchd, change a user's installed app, or interact with WhatsApp. Output goes under
`app/build/releases/<version>/`; an existing output folder is never overwritten.

It prints a shell-escaped `gh release create` command. Inspect the app, notes, ZIP, DMG and feed, then
run that command yourself. The script **does not publish**. Publish a stable, non-prerelease GitHub
release containing all three assets: versioned ZIP, versioned DMG and **`appcast.json`**. GitHub's latest
release download URL is the stable feed address:

```text
https://github.com/your-github-name/limmr/releases/latest/download/appcast.json
```

For a later release, change the version argument, increment `LIMMR_BUILD_NUMBER`, and point
`LIMMR_RELEASE_NOTES` at the new notes. Keep the bundle identifier and feed key consistent.
For self-signed installations, the installed app's public feed key is the trust root: the signed feed
covers the archive URL, size and SHA-256 as well as the version and build. The archive must match
before extraction. The candidate then needs valid, intact strict/nested code signatures, including
its engine helper, the same bundle identifier, matching feed metadata, and the installed public key.
Unsigned or tampered bundles and silent feed-key changes are refused. Ad-hoc signing alone does not
prove who made a bundle; the signed feed and pinned key establish that trust.

The signer also supports `LIMMR_UPDATE_PRIVATE_KEY` (base64 raw Ed25519 bytes), or
`LIMMR_UPDATE_PRIVATE_KEY_FILE` (raw 32-byte file, mode 0600). Prefer Keychain for real releases.
Never type a private key into a command/history/log. For offline tests only,
`keygen.swift --file <new-temp-path>` creates a throwaway 0600 key without touching Keychain.
`LIMMR_UPDATE_KEYCHAIN_SERVICE` overrides the Keychain service name. Signing scripts suppress shell
tracing and never print the private key.

## Optional later: Developer ID and notarization

If you join the Apple Developer Program later, the original release path remains available. Set a
**Developer ID Application** identity in `LIMMR_SIGN_ID` and a notarytool Keychain profile name in
`LIMMR_NOTARY_PROFILE`. Keep the certificate private key in Keychain. Use Apple's interactive
`xcrun notarytool store-credentials` flow yourself; do not put credentials in commands or release notes.
Then omit `--self-signed`:

```sh
scripts/release/release.sh 0.3.0
```

The default path requires that identity/profile, signs with hardened runtime and the same
entitlements, notarizes and staples the app before making the final ZIP, then signs, notarizes and
staples the DMG. Notarization requires network access and must succeed. The feed-signing key and
GitHub release steps remain the same. Keep that key when moving from ad-hoc to Developer ID.
An installation with a Team ID requires the same team and designated signing requirement for every
update; it never accepts an ad-hoc candidate. Those checks use Security.framework, not a plist claim.
Actual Developer ID signing/notarization still needs an owner-run test with real credentials.

## What installed users see

At launch, and hourly while open, the app considers a quiet check. It makes at most one automatic
feed GET per 24 hours, even after a failed attempt or an app restart. There is no redundant HEAD.
An explicit **Check now** can make another request. Updates are HTTPS only, including redirects.
GitHub's HTTPS asset redirects are supported. Feed requests are capped at 256 KiB; archives are
streamed to disk, capped at the signed size (at most 1 GiB), and SHA-256 checked before extraction.

Settings → Advanced has **Updates: Automatically / Ask me / Off**:

- **Automatically** checks and prepares a verified update. It does not restart by itself.
- **Ask me** checks and shows the notes; the user chooses to download.
- **Off** disables scheduled checks. Check now remains an explicit user action.

Home quietly shows “Limmr 0.3 is ready · Restart to update” after preparation. The notes sheet lets
the user postpone. The app refuses to restart during app actions, brief writing, pending approvals
or queued drafts. The engine must additionally pause *all* outbound work atomically. Missing support,
a busy helper or an invalid reply blocks installation without quitting the app.

Only after archive and bundle verification does the updater remove `com.apple.quarantine` from the
staged bundle and its contents, without following symlinks. Failed verification leaves quarantine intact.
The detached update runner re-verifies the feed, archive and staged code, waits for the old UI to
exit, pauses/drains sends, unregisters the old bundled SMAppService agent and verifies it has stopped.
It exchanges the two bundles using macOS `RENAME_SWAP` on the same volume: there is no moment when
`Limmr.app` is missing. It runs the **new bundle's** repair entry point to unregister/register the
LaunchAgent, then waits up to 60 seconds for authenticated `/v1/status`. It reopens the app only
after the engine answers. A failed new engine restores the old bundle, re-registers its helper,
checks status again, and reopens it. A failed rollback leaves both bundles for manual recovery.
The retained backup is `Previous.app` inside the hidden sibling `.limmr-update-<id>` folder.

The app must be called `Limmr.app`, installed in a writable folder (normally `/Applications` or
`~/Applications`), outside a DMG/App Translocation, without a symlink at its install path. There is
no privileged installer or silent authorization prompt. Install manually if the destination is not
writable. Backups are deliberately retained; remove them manually after confirming the release.
Prepared downloads survive app restarts; their signed feed is checked on loading, and the archive
and code are verified again before swapping. An interrupted download can leave its hidden staging folder.

## Homebrew

`limmr.rb` in the release output is filled from `scripts/release/limmr.rb.template`, using the final
DMG's SHA-256. Put it in your own tap (`Casks/limmr.rb`) after review. Users install with
`brew install --cask your-tap/limmr`, and explicitly upgrade with
`brew upgrade --cask --greedy limmr` (`--greedy` includes casks declaring `auto_updates`). Keep the app
closed and its helper stopped before replacing it through Homebrew. A later app launch uses the
existing reconnect path to repair helper registration if needed. Uninstall keeps user data.

## Pulling a bad release

If no one installed it yet, restore `appcast.json` on the latest GitHub release to the last known good
**signed feed**, and stop distributing the bad assets. Never edit a feed field without re-signing.
Clients still on an older version can get the good update. An already installed higher version will
refuse that lower feed; this is intentional downgrade protection.

For people already on the bad release, rebuild the good code as a **higher version and higher build**,
sign it using the same release mode (and notarize if using Developer ID), and publish that corrective
release. Do not reuse a versioned asset URL for different bytes. Manual recovery is to stop the helper, retain the failed app, restore the backup
as `Limmr.app`, launch it, and use Settings → Health to repair its helper. Do not restore an older
engine over a data schema it cannot read; database migrations must remain backward compatible across
an update/rollback pair. The updater rolls back the app, not user databases.

## Feed signature format

The payload has exactly `version`, `build` (integer), `min_macos`, `notes_markdown`, `url`, `size`
(integer), and lowercase `sha256`. Canonical bytes are UTF-8 JSON with sorted keys, unescaped slashes,
no whitespace, and no trailing newline, using Foundation JSONSerialization. The signature is a
base64 Ed25519 signature over those payload bytes. The final feed adds `ed25519_signature`.
Unknown fields, malformed versions, bad signatures, unsupported macOS versions, non-HTTPS URLs,
oversized downloads, size/hash mismatches and downgrades are refused. `appcast-canonical.json`
in the local output preserves the signed payload for audit; it need not be published.

## Engine handoff

The engine implements the control-token-only capability, prepare and resume routes described in
[SPEC §3.8](SPEC.md#38-signed-in-app-updates-and-engine-handoff). Prepare closes every outbound
entry point and drains for at most 10 seconds. A timeout unblocks without claiming success;
reads remain available. A successful pause expires after five minutes or clears on explicit resume
or engine restart. Concurrent prepare returns 409 without altering the current lease.

Release builds embed `LIMMR_VERSION` in `health.Version` using Go linker flags. The installer
requires that exact signed-feed version from authenticated status after the swap; another version
causes rollback. Rollback checks the previous authenticated engine version. All transaction failures
attempt resume, including failures during rollback. Tests inject backend and OS operations.

Before using a live account, the owner must run a controlled upgrade with a fake messenger and two
releases signed with the chosen mode. Installed SMAppService stop/register/restart behavior,
real signed release downloads and actual Developer ID notarization/publication remain owner-run
checks; offline tests do not establish them.

## Offline checks

```sh
scripts/release/selftest.sh < /dev/null
cd app
LIMMR_HOME=$(mktemp -d) swift build --disable-sandbox --cache-path .build/cache --config-path .build/config --security-path .build/security < /dev/null
LIMMR_HOME=$(mktemp -d) swift test --no-parallel --disable-sandbox --cache-path .build/cache --config-path .build/config --security-path .build/security < /dev/null
```

Use repo-local `CLANG_MODULE_CACHE_PATH` and `SWIFTPM_MODULECACHE_OVERRIDE` if the system cache is
unwritable. The self-test creates a fresh file key in a temp folder, signs a synthetic archive, then
compiles the production verifier and checks canonical bytes and archive hash. It exercises both release
modes and inherited-identity isolation with real ad-hoc fixture signatures, ZIPs and signed feeds;
Swift/Go builds, DMG creation and Apple certificate/notary/Gatekeeper operations are mocked. It checks
that the self-signed path never calls notarization and the default path still requests it. No real key,
Keychain credential or network is used. Swift tests cover feed/hash tampering, version ordering,
downgrade refusal, intact/tampered/unsigned ad-hoc bundles and helpers, pinned-key changes, metadata,
quarantine ordering, rate limiting, engine-gate refusal and success/rollback transactions. The
Developer-ID-to-ad-hoc guard uses a synthetic installed Team ID with a real ad-hoc candidate identity;
no Developer ID certificate is needed for that policy test.
Shell syntax checks run in the self-test; shellcheck can additionally be run when installed.

For UI review, run the app's existing snapshot command with a throwaway `LIMMR_HOME`:
`.build/debug/Limmr --snapshot <out.png> --page settings-advanced` and
`.build/debug/Limmr --snapshot <out.png> --page home --update-preview`, adding `--dark` for dark appearance.
Only synthetic demo data is used; preview actions are disabled. Never launch a bundled Limmr.app
for lane verification (including through `open` or LaunchServices).
