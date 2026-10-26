#!/bin/zsh
set -euo pipefail
cd "${0:A:h}/.."
export LIMMR_HOME=$(mktemp -d)
trap 'rm -rf "$LIMMR_HOME"' EXIT
binary=${1:-.build/debug/Limmr}
output=build/snapshots/u4
mkdir -p "$output"
for appearance in light dark; do
  extra=()
  [[ "$appearance" == dark ]] && extra=(--dark)
  for page in u4-home u4-dashboard u4-circles u4-replies u4-activity u4-activity-expanded u4-activity-log u4-icon-picker u4-settings-search; do
    "$binary" --snapshot "$output/$page-$appearance.png" --page "$page" --size 1440x1100 "${extra[@]}" < /dev/null 2>"$output/render.log"
    echo "$page-$appearance.png"
  done
  "$binary" --snapshot "$output/u4-home-800x600-$appearance.png" --page u4-home --size 800x600 "${extra[@]}" < /dev/null 2>"$output/render.log"
done
