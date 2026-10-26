#!/bin/zsh
set -euo pipefail
cd "${0:A:h}/.."
export LIMMR_HOME=$(mktemp -d)
trap 'rm -rf "$LIMMR_HOME"' EXIT
binary=${1:-.build/debug/Limmr}
output=build/snapshots/u3
mkdir -p "$output"
for appearance in light dark; do
  extra=()
  [[ "$appearance" == dark ]] && extra=(--dark)
  for page in home-waiting-0 home-waiting-1 home-waiting-4 home-dashboard home-today activity-sends activity-reads people people-map-hover agent-detail menubar; do
    "$binary" --snapshot "$output/$page-$appearance.png" --page "$page" --size 1440x1100 "${extra[@]}" < /dev/null 2>"$output/render.log"
    echo "$page-$appearance.png"
  done
  for mode in always optional never; do
    "$binary" --snapshot "$output/signoff-$mode-$appearance.png" --page agent-detail --signature-mode "$mode" --size 1100x900 "${extra[@]}" < /dev/null 2>"$output/render.log"
    echo "signoff-$mode-$appearance.png"
  done
  for page in home-waiting-0 home-waiting-4 activity-sends people; do
    "$binary" --snapshot "$output/$page-800x600-$appearance.png" --page "$page" --size 800x600 "${extra[@]}" < /dev/null 2>"$output/render.log"
    echo "$page-800x600-$appearance.png"
  done
done
