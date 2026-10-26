#!/bin/zsh
set -euo pipefail
cd "${0:A:h}/.."
export LIMMR_HOME=$(mktemp -d)
binary=${1:-.build/debug/Limmr}
output=build/snapshots/f1
mkdir -p "$output"
for page in people-current proposals-review settings-advanced-reset settings-hidden; do
  for appearance in light dark; do
    extra=()
    [[ "$appearance" == dark ]] && extra=(--dark)
    "$binary" --snapshot "$output/$page-1100x740-$appearance.png" --page "$page" --size 1100x740 "${extra[@]}" < /dev/null 2>"$output/render.log"
    echo "$page-1100x740-$appearance.png"
  done
done
