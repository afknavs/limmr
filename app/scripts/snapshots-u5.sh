#!/bin/zsh
set -euo pipefail
cd "${0:A:h}/.."
export LIMMR_HOME=$(mktemp -d)
trap 'rm -rf "$LIMMR_HOME"' EXIT
binary=${1:-.build/out/Products/Debug/Limmr}
output=build/snapshots/u5
mkdir -p "$output"
for appearance in light dark; do
  extra=()
  [[ "$appearance" == dark ]] && extra=(--dark)
  for size in 1440x3000 1440x1100 800x600; do
    "$binary" --snapshot "$output/home-$size-$appearance.png" --page u5-home --size "$size" "${extra[@]}" < /dev/null 2>"$output/render.log"
  done
  "$binary" --snapshot "$output/home-quiet-$appearance.png" --page u5-home-quiet --size 1440x1100 "${extra[@]}" < /dev/null 2>"$output/render.log"
done
