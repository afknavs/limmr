#!/bin/zsh
set -euo pipefail
root="${0:A:h:h:h}"
cd "$root"
mkdir -p .cache docs/images/e1
binary="$root/app/.build/release/Limmr"
[[ -x "$binary" ]] || { echo "Build the unbundled release executable first." >&2; exit 1; }
for page in activity search detail log home; do
  for theme in light dark; do
    temporary_home=$(mktemp -d "$root/.cache/e1-snapshot-home.XXXXXX")
    args=(--snapshot "$root/docs/images/e1/$page-$theme.png" --page "e1-$page" --size 1100x900)
    [[ "$theme" != dark ]] || args+=(--dark)
    LIMMR_HOME="$temporary_home" "$binary" "${args[@]}" < /dev/null > .cache/e1-snapshot.log 2>&1
    rmdir "$temporary_home"
    echo "$page-$theme captured"
  done
done
