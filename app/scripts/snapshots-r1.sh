#!/bin/zsh
set -euo pipefail
cd "${0:A:h}/.."
export LIMMR_HOME=$(mktemp -d)
trap 'rm -rf "$LIMMR_HOME"' EXIT
binary=${1:-.build/debug/Limmr}
output=build/snapshots/r1
mkdir -p "$output"
pages=(home people person settings-integrations setup-welcome)
for page in $pages; do
  for appearance in light dark; do
    extra=()
    [[ "$appearance" == dark ]] && extra=(--dark)
    "$binary" --snapshot "$output/$page-1100x740-$appearance.png" --page "$page" --size 1100x740 "${extra[@]}" < /dev/null 2>"$output/$page-$appearance.log"
    echo "$page-1100x740-$appearance.png"
  done
done
