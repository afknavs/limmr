#!/bin/zsh
set -euo pipefail
cd "${0:A:h}/.."
export LIMMR_HOME=$(mktemp -d)
trap 'rm -rf "$LIMMR_HOME"' EXIT
binary=${1:-.build/debug/Limmr}
output=build/snapshots/f2
mkdir -p "$output"
for page in remote-approval remote-approval-work integration-detail about-you setup-about-you person-you; do
  for appearance in light dark; do
    extra=()
    [[ "$appearance" == dark ]] && extra=(--dark)
    "$binary" --snapshot "$output/$page-1100x740-$appearance.png" --page "$page" --size 1100x740 "${extra[@]}" < /dev/null >"$output/$page-$appearance.log" 2>&1
  done
done
