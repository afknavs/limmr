#!/bin/zsh
set -euo pipefail
cd "${0:A:h}/.."
export LIMMR_HOME=$(mktemp -d)
trap 'rm -rf "$LIMMR_HOME"' EXIT
binary=${1:-.build/debug/Limmr}
output=build/snapshots/a9
mkdir -p "$output"
pages=(setup-who home-setup proposals-review remote-approval remote-approval-work integration-everything integration-work integration-ended connect-claude-code connect-chatgpt)
for size in 1100x740 800x600; do
  for page in $pages; do
    for appearance in light dark; do
      extra=()
      [[ "$appearance" == dark ]] && extra=(--dark)
      "$binary" --snapshot "$output/$page-$size-$appearance.png" --page "$page" --size "$size" "${extra[@]}" < /dev/null 2>"$output/render.log"
      echo "$page-$size-$appearance.png"
    done
  done
done
python3 scripts/a9-contact-sheet.py
