#!/bin/bash
set -euo pipefail
exec < /dev/null
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
mkdir -p .cache/h2 app/build/h2
binary=app/.build/out/Products/Debug/Limmr
if [[ ! -x "$binary" ]]; then binary=app/.build/debug/Limmr; fi
if [[ ! -x "$binary" ]]; then
  echo "Build the unbundled Swift executable first." >&2
  exit 1
fi
for page in h2-home h2-empty-triggers h2-odd; do
  for size in 1100x740 1440x1000; do
    for appearance in light dark; do
      snapshot_home=$(mktemp -d "$root/.cache/h2/home.XXXXXX")
      args=(--snapshot "app/build/h2/$page-$appearance-$size.png" --page "$page" --size "$size")
      if [[ "$appearance" == dark ]]; then args+=(--dark); fi
      LIMMR_HOME="$snapshot_home" "$binary" "${args[@]}" < /dev/null
      rm -rf "$snapshot_home"
    done
  done
done
