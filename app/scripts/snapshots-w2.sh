#!/bin/zsh
set -euo pipefail
cd "${0:A:h}/.."
export LIMMR_HOME=$(mktemp -d)
trap 'rm -rf "$LIMMR_HOME"' EXIT
binary=${1:-.build/debug/Limmr}
output=build/snapshots/w2
mkdir -p "$output"
for size in 1100x740 800x600; do
  for count in 0 1 4; do
    for appearance in light dark; do
      extra=()
      [[ "$appearance" == dark ]] && extra=(--dark)
      "$binary" --snapshot "$output/home-waiting-$count-$size-$appearance.png" --page "home-waiting-$count" --size "$size" "${extra[@]}" < /dev/null 2>"$output/render.log"
      echo "home-waiting-$count-$size-$appearance.png"
    done
  done
done
for appearance in light dark; do
  extra=()
  [[ "$appearance" == dark ]] && extra=(--dark)
  "$binary" --snapshot "$output/menubar-$appearance.png" --page menubar "${extra[@]}" < /dev/null 2>"$output/render.log"
  for mode in always optional never; do
    "$binary" --snapshot "$output/signoff-$mode-$appearance.png" --page agent-detail --signature-mode "$mode" "${extra[@]}" < /dev/null 2>"$output/render.log"
  done
done
